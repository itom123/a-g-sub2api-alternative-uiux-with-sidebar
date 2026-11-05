#!/usr/bin/env python3
"""Versioned macOS restored-copy canary; never activates production.

Run on the host with python3 -I and --env-file for the production DB settings.
All inputs must already exist locally; this program has no SSH/upload/extraction.
--root must be an absolute, nonexistent task directory (0700). Never reuse it.

Package manifest v1 (paths are supplied separately, never executed from JSON):
  {"schema":1,"binary_sha256":"...","frontend_files":{"index.html":"...",...},
   "migration_files":{"001_init.sql":"<raw file sha256>",...},
   "embedded_migrations":{"001_init.sql":"<TrimSpace SQL sha256>",...},
   "controller_sha256":"...","build_identity":{"version":"0.2.19"},
   "new_commit":"..."}
The package builder MUST derive embedded_migrations from the exact SQL embedded
in this binary, not from a different checkout. Startup independently verifies it.
No SQL is applied here. Any unrecorded migration, ledger change, startup failure,
or checksum mismatch blocks eligibility. Only the exact historical 250 pair is
accepted; never repair schema_migrations or bypass other checksum differences.

Evidence is automated canary review. A successful controller prepare leaves
its immutable host lock held for the separately configured activation policy.
"""
import argparse
import base64
import errno
import datetime as dt
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

DBPORT, REDISPORT, APPPORT = 16434, 16381, 18082
HISTORICAL_250 = frozenset((
    '33bc8a11ef09cfea79a23fafeaf94958c659f8213d01e0fa8bff2f8a51dd9791',
    '1a5b4c18a37db68a4d6dcfe4bf4e649a6df209e169ca99f0193d5c631fab62b1'))
PATH = '/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin'
LEDGER_QUERY = "SELECT COALESCE(json_agg(t ORDER BY filename)::text,'[]') FROM (SELECT filename,checksum,applied_at FROM schema_migrations) t"
ATLAS_QUERY = "SELECT COALESCE(json_agg(t ORDER BY version)::text,'[]') FROM atlas_schema_revisions t"


class Failure(Exception):
    """Only fixed, credential-free messages may cross the CLI boundary."""


def require(ok, message):
    if not ok:
        raise Failure(message)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as src:
        for block in iter(lambda: src.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def inside(path, parent):
    try:
        Path(path).relative_to(parent)
        return True
    except ValueError:
        return False


def ordinary(path, directory=False):
    path = Path(path)
    require(path.is_absolute(), 'input paths must be absolute')
    for part in (path,) + tuple(path.parents):
        require(not part.is_symlink(), 'input path contains a symlink')
    require(path.is_dir() if directory else path.is_file(), 'input missing or not ordinary')
    return path.resolve()


def tree_hashes(directory):
    directory = ordinary(directory, True)
    hashes = {}
    for path in sorted(directory.rglob('*')):
        require(not path.is_symlink(), 'tree contains a symlink')
        require(path.is_file() or path.is_dir(), 'tree contains a special file')
        if path.is_file():
            hashes[path.relative_to(directory).as_posix()] = digest(path)
    return hashes


def save(path, value):
    # Task directory is exclusively created; never overwrite successful evidence.
    with Path(path).open('x', encoding='utf-8') as out:
        json.dump(value, out, sort_keys=True, indent=2)
        out.write('\n')
        out.flush()
        os.fsync(out.fileno())
    Path(path).chmod(0o600)


def migration_hashes(directory):
    result = {}
    for path in sorted(directory.glob('*.sql')):
        # Preserve CRLF bytes: Go strings.TrimSpace removes boundaries, not CRLF.
        text = path.read_bytes().decode('utf-8').strip()
        if text:
            result[path.name] = hashlib.sha256(text.encode('utf-8')).hexdigest()
    require(bool(result), 'migration package is empty')
    return result


def check_migrations(ledger, embedded):
    recorded = {row['filename']: row['checksum'] for row in ledger}
    require(len(recorded) == len(ledger) and bool(recorded), 'invalid restored migration ledger')
    for name, checksum in embedded.items():
        require(name in recorded, 'new migration is forbidden')
        if recorded[name] != checksum:
            require(name == '250_openclaw_provider_descriptions_live.sql'
                    and frozenset((recorded[name], checksum)) == HISTORICAL_250,
                    'migration checksum mismatch; package exact historical SQL')
    # Extra ledger entries may be historical SQL intentionally absent from embed.
    # Startup plus full before/after snapshots must still leave every row intact.


def merge_frontend(old, new, destination):
    """Use the versioned source index; retain only cached asset/font URLs.

    No old HTML, inline code, branding or untracked root-level resources are
    inherited. The raw and deployed manifests remain separately attestable.
    """
    old_hashes, new_hashes = tree_hashes(old), tree_hashes(new)
    require('index.html' in old_hashes and 'index.html' in new_hashes, 'frontend index missing')
    require((new / 'index.html').read_text(encoding='utf-8').lower().count('</head>') == 1,
            'staged index head is invalid')
    for name in set(old_hashes) & set(new_hashes):
        if name.startswith(('assets/', 'fonts/')):
            require(old_hashes[name] == new_hashes[name], 'cached asset path collision')
    shutil.copytree(new, destination)
    for name in old_hashes:
        if name.startswith(('assets/', 'fonts/')) and name not in new_hashes:
            target = destination / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(old / name, target)
    merged = tree_hashes(destination)
    require(merged['index.html'] == new_hashes['index.html'], 'source frontend index changed')
    require(all(merged.get(name) == checksum for name, checksum in new_hashes.items()),
            'new frontend artifact changed during merge')
    require(all(merged.get(name) == checksum for name, checksum in old_hashes.items()
                if name.startswith(('assets/', 'fonts/'))), 'cached old frontend asset lost')
    return merged


def read_baseline(path):
    baseline = json.loads(ordinary(Path(path)).read_text(encoding='utf-8'))
    require(isinstance(baseline, dict), 'baseline must be an object')
    commit = baseline.get('source_commit')
    require(isinstance(commit, str) and re.fullmatch(r'[0-9a-f]{40}', commit), 'baseline exact source commit missing')
    require('commit' not in baseline or baseline['commit'] == commit, 'baseline commit aliases disagree')
    require(isinstance(baseline.get('version'), str) and bool(re.fullmatch(r'[0-9A-Za-z._+-]+', baseline['version'])),
            'baseline version missing')
    require(isinstance(baseline.get('binary_sha256'), str) and re.fullmatch(r'[0-9a-f]{64}', baseline['binary_sha256']),
            'baseline binary digest missing')
    require(isinstance(baseline.get('frontend_pointer'), str) and bool(baseline['frontend_pointer']), 'baseline frontend pointer missing')
    hashes = baseline.get('frontend_files')
    require(isinstance(hashes, dict) and 'index.html' in hashes, 'baseline frontend inventory missing')
    for name, checksum in hashes.items():
        require(isinstance(name, str) and name and not Path(name).is_absolute()
                and '..' not in Path(name).parts and '\\' not in name
                and isinstance(checksum, str) and re.fullmatch(r'[0-9a-f]{64}', checksum), 'invalid baseline frontend entry')
    protected = baseline.get('configuration_hashes')
    required = {'.env', 'run.sh', 'launchd-run.sh', 'deployment.json'}
    require(isinstance(protected, dict) and required.issubset(protected)
            and set(protected).issubset(required | {'frontend/Caddyfile'}), 'baseline protected configuration inventory missing')
    require(all(isinstance(h, str) and re.fullmatch(r'[0-9a-f]{64}', h) for h in protected.values()),
            'baseline protected configuration digest invalid')
    return baseline


DB_ENV_KEYS = frozenset(('DATABASE_HOST', 'DATABASE_PORT', 'DATABASE_USER',
                         'DATABASE_PASSWORD', 'DATABASE_DBNAME', 'DATABASE_SSLMODE'))


def load_env_file(path):
    """Read only DB connection keys; shell expansion and execution never occur."""
    values = {}
    for line in ordinary(Path(path)).read_text(encoding='utf-8').splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        if line.startswith('export '):
            line = line[7:].lstrip()
        name, separator, raw = line.partition('=')
        name = name.strip()
        if name not in DB_ENV_KEYS:
            continue
        require(separator and name not in values, 'production DB environment entry is invalid or duplicated')
        # Shell-style comments start after whitespace outside quotes. Keep
        # literal # characters inside passwords, including unquoted values.
        quote, escaped = None, False
        for index, character in enumerate(raw):
            if escaped:
                escaped = False
            elif character == '\\' and quote != "'":
                escaped = True
            elif character in ('"', "'"):
                if quote is None:
                    quote = character
                elif quote == character:
                    quote = None
            elif character == '#' and quote is None and index > 0 and raw[index - 1].isspace():
                raw = raw[:index]
                break
        try:
            tokens = shlex.split(raw, comments=False, posix=True)
        except ValueError:
            raise Failure('production DB environment entry has invalid quoting') from None
        require(len(tokens) <= 1, 'production DB environment entry has unquoted whitespace')
        value = tokens[0] if tokens else ''
        require('\x00' not in value, 'production DB environment entry contains a NUL')
        values[name] = value
    return values


def windowless():
    return {'creationflags': subprocess.CREATE_NO_WINDOW} if os.name == 'nt' else {}


def stop(proc):
    if proc and proc.poll() is None:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()  # Only the exact Popen child owned by this invocation.
            proc.wait(timeout=5)


def fixture_jwt(user_id, email, password_hash, secret):
    version = int.from_bytes(hashlib.sha256((email.lower().strip() + '\n' + password_hash).encode()).digest()[:8], 'big') & 0x7fffffffffffffff
    def enc(value):
        return base64.urlsafe_b64encode(json.dumps(value, separators=(',', ':')).encode()).rstrip(b'=')
    now = int(time.time())
    signing = enc({'alg': 'HS256', 'typ': 'JWT'}) + b'.' + enc({
        'user_id': user_id, 'email': email, 'role': 'user', 'token_version': version,
        'session_id': secrets.token_hex(8), 'iat': now, 'nbf': now - 5, 'exp': now + 600})
    return (signing + b'.' + base64.urlsafe_b64encode(hmac.new(secret.encode(), signing, hashlib.sha256).digest()).rstrip(b'=')).decode()


class Canary:
    def __init__(self, args):
        self.a = args
        self.root = Path(args.root)
        self.live = Path(args.app_root).expanduser()
        self.trusted_baseline = None
        self.stage = 'preflight'
        self.children = []
        self.logs = []
        self.pg_attempted = False
        self.sequence = 0
        self.created = False
        # Never forward production secrets to arbitrary tools or the controller.
        self.env = {'PATH': PATH, 'HOME': str(Path.home()), 'LANG': 'C', 'LC_ALL': 'C',
                    'PGCONNECT_TIMEOUT': '5', 'PGOPTIONS': '-c statement_timeout=30000 -c lock_timeout=5000'}
        self.production = dict(os.environ)
        self.password, self.admin_password, self.redis_password, self.jwt = (secrets.token_hex(32) for _ in range(4))
        self.admin_env = dict(self.env, PGPASSWORD=self.admin_password)

    def run(self, args, env=None, timeout=120, input_text=None, allowed=(0,), include_stderr=False):
        self.sequence += 1
        prefix = self.root / ('command-%03d' % self.sequence)
        # All output is protected on-host, never included in exceptions/console.
        with prefix.with_suffix('.stdout').open('xb') as out, prefix.with_suffix('.stderr').open('xb') as err:
            try:
                result = subprocess.run([str(x) for x in args], cwd=str(self.root), env=env or self.env,
                                        stdin=subprocess.DEVNULL if input_text is None else None,
                                        input=None if input_text is None else input_text.encode(),
                                        stdout=out, stderr=err, timeout=timeout, **windowless())
            except (OSError, subprocess.SubprocessError):
                raise Failure('bounded subprocess failed; inspect protected command logs') from None
        require(result.returncode in allowed, 'command failed; inspect protected command logs')
        text = prefix.with_suffix('.stdout').read_text(encoding='utf-8')
        if include_stderr:
            text += prefix.with_suffix('.stderr').read_text(encoding='utf-8')
        return text

    def spawn(self, args, env, label, cwd=None):
        log = (self.root / (label + '.log')).open('xb')
        self.logs.append(log)
        proc = subprocess.Popen([str(x) for x in args], cwd=str(cwd or self.root), env=env,
                                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, **windowless())
        self.children.append(proc)
        return proc

    def sql(self, query, db='canary', user='canary_admin'):
        # No API exists here for arbitrary production SQL. Every call is pinned
        # to the new disposable instance; SQL goes via stdin, never process args.
        env = self.admin_env if user == 'canary_admin' else dict(self.env, PGPASSWORD=self.password)
        return self.run(['psql', '-X', '-h', '127.0.0.1', '-p', str(DBPORT), '-U', user,
                         '-d', db, '-At', '-v', 'ON_ERROR_STOP=1', '-q'], input_text=query, env=env).strip()

    def load_baseline(self):
        self.trusted_baseline = read_baseline(self.a.baseline_json)
        self.old_sha = self.trusted_baseline['binary_sha256']
        self.old_commit = self.trusted_baseline['source_commit']
        self.old_version = self.trusted_baseline['version']

    def preflight(self):
        require(sys.platform == 'darwin', 'on-host execution requires macOS; use offline harness elsewhere')
        self.live = ordinary(self.live, True)
        require(self.live == (Path.home() / '.local/share/sub2api').resolve(),
                'live mode is restricted to the current user macOS app root')
        self.load_baseline()
        require(self.a.new_commit != self.old_commit, 'new commit is already installed')
        require(bool(re.fullmatch(r'[0-9a-f]{40}', self.a.new_commit))
                and self.a.new_commit != self.old_commit, 'exact distinct new commit is required')
        require(self.root.is_absolute() and not self.root.exists() and not self.root.is_symlink(), 'root must be absolute and fresh')
        ordinary(self.root.parent, True)
        require(bool(re.fullmatch(r'/[A-Za-z0-9/._-]+', str(self.root))) and '..' not in self.root.parts,
                'task root must use simple safe macOS path components')
        require(inside(self.root, Path.home().resolve()) and not inside(self.root, self.live)
                and not inside(self.live, self.root), 'task root overlaps production or is outside home')
        self.binary = ordinary(Path(self.a.binary))
        self.frontend = ordinary(Path(self.a.frontend), True)
        self.migrations = ordinary(Path(self.a.migrations), True)
        self.controller = ordinary(Path(self.a.controller))
        for directory in (self.frontend, self.migrations):
            require(not inside(self.root, directory), 'task root must not be inside package trees')
        package_path = ordinary(Path(self.a.package_json))
        self.package = json.loads(package_path.read_text(encoding='utf-8'))
        require(self.package.get('schema') == 1, 'package manifest schema must be 1')
        require(self.package.get('new_commit') == self.a.new_commit, 'package commit identity differs')
        identity = self.package.get('build_identity') or {}
        require(identity.get('commit', self.a.new_commit) == self.a.new_commit, 'package build commit differs')
        self.new_version = identity.get('version')
        require(isinstance(self.new_version, str) and bool(re.fullmatch(r'[0-9A-Za-z._+-]+', self.new_version)),
                'package build version missing')
        self.verify_package()
        if self.a.env_file:
            self.production = load_env_file(self.a.env_file)
        for name in ('DATABASE_HOST', 'DATABASE_PORT', 'DATABASE_USER', 'DATABASE_PASSWORD', 'DATABASE_DBNAME'):
            require(bool(self.production.get(name)), 'required production DB environment variable missing')
        require(self.production['DATABASE_PORT'].isdigit() and int(self.production['DATABASE_PORT']) != DBPORT,
                'production database port is invalid or collides with disposable port')
        self.baseline = self.check_baseline()
        for port in (DBPORT, REDISPORT, APPPORT):
            self.port_free(port)
        for name in ('pg_dump', 'pg_restore', 'psql', 'initdb', 'pg_ctl', 'redis-server', 'redis-cli', 'sandbox-exec'):
            require(shutil.which(name, path=PATH), 'required on-host executable missing')
        self.root.mkdir(mode=0o700)
        self.root.chmod(0o700)
        self.created = True
        self.env['TMPDIR'] = str(self.root)
        # Freeze inputs once, then validate the frozen bytes as well.
        shutil.copy2(self.binary, self.root / 'new-sub2api')
        shutil.copy2(self.live / 'bin/sub2api', self.root / 'old-sub2api')
        shutil.copy2(self.controller, self.root / 'controller.py')
        self.new_binary = self.root / 'new-sub2api'
        self.old_binary = self.root / 'old-sub2api'
        self.controller_copy = self.root / 'controller.py'
        require(digest(self.new_binary) == self.package['binary_sha256']
                and digest(self.old_binary) == self.old_sha
                and digest(self.controller_copy) == self.package['controller_sha256'], 'input changed during staging')
        self.new_binary.chmod(0o700)
        self.old_binary.chmod(0o700)
        self.merged = merge_frontend(Path(self.baseline['frontend_path']), self.frontend, self.root / 'merged-frontend')
        require(self.check_baseline() == self.baseline, 'production baseline changed while copying frontend')
        save(self.root / 'package.json', self.package)
        save(self.root / 'baseline.json', self.baseline)

    def verify_package(self):
        require(digest(self.binary) == self.package.get('binary_sha256'), 'package binary hash differs')
        require(tree_hashes(self.frontend) == self.package.get('frontend_files'), 'package frontend hash differs')
        require(tree_hashes(self.migrations) == self.package.get('migration_files'), 'package SQL file hash differs')
        self.embedded = migration_hashes(self.migrations)
        require(self.embedded == self.package.get('embedded_migrations'), 'embedded SQL attestation differs from package SQL')
        require(digest(self.controller) == self.package.get('controller_sha256'), 'tested controller hash differs')
        # Do not silently edit/monkeypatch the reviewed controller. This adapter
        # itself supports 3.9; a controller using pathlib's 3.10 API needs a newer
        # host interpreter, selected by invoking this adapter with that Python.
        if sys.version_info < (3, 10) and b'.is_relative_to(' in self.controller.read_bytes():
            raise Failure('tested controller needs Python 3.10+; rerun adapter with a compatible host interpreter')

    def check_baseline(self):
        live = ordinary(self.live, True)
        require(not os.path.lexists(str(live / 'deploy-controller/active.json')), 'active deploy lock exists; do not clear it')
        require(digest(ordinary(live / 'bin/sub2api')) == self.old_sha, 'production binary differs from trusted baseline')
        pointer = live / 'frontend/current'
        require(pointer.is_symlink(), 'production frontend pointer must be a symlink')
        target = pointer.resolve(strict=True)
        require(inside(target, live / 'frontend/releases') and os.readlink(pointer) == self.trusted_baseline['frontend_pointer'],
                'production frontend baseline differs')
        require(tree_hashes(target) == self.trusted_baseline['frontend_files'], 'production frontend inventory differs')
        for name, checksum in self.trusted_baseline['configuration_hashes'].items():
            require(digest(ordinary(live / name)) == checksum, 'production protected configuration differs')
        return {'old_binary_sha256': self.old_sha, 'frontend_pointer': os.readlink(pointer),
                'frontend_path': str(target), 'frontend_files': tree_hashes(target),
                'configuration_hashes': dict(self.trusted_baseline['configuration_hashes'])}

    @staticmethod
    def port_free(port, wait=0):
        # TIME_WAIT left by this canary's own closed HTTP connections is not a
        # listener; any live listener still fails both the bind and the probe.
        deadline = time.monotonic() + wait
        while True:
            try:
                for family, host in ((socket.AF_INET, '127.0.0.1'), (socket.AF_INET6, '::1')):
                    with socket.socket(family, socket.SOCK_STREAM) as sock:
                        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                        sock.bind((host, port))
                    with socket.socket(family, socket.SOCK_STREAM) as probe:
                        probe.settimeout(1)
                        if probe.connect_ex((host, port)) == 0:
                            raise OSError('listener present')
                return
            except OSError:
                if time.monotonic() >= deadline:
                    raise Failure('required exact canary port is not free') from None
                time.sleep(0.5)

    def backup_restore(self):
        self.stage = 'fresh_database_backup'
        dump = self.root / 'database.dump'
        pg_env = dict(self.env, PGPASSWORD=self.production['DATABASE_PASSWORD'])
        if self.production.get('DATABASE_SSLMODE'):
            pg_env['PGSSLMODE'] = self.production['DATABASE_SSLMODE']
        self.run(['pg_dump', '-h', self.production['DATABASE_HOST'], '-p', self.production['DATABASE_PORT'],
                  '-U', self.production['DATABASE_USER'], '-d', self.production['DATABASE_DBNAME'],
                  '--format=custom', '--no-password', '--lock-wait-timeout=10s', '--file', dump], env=pg_env, timeout=300)
        require(dump.is_file() and dump.stat().st_size > 0, 'fresh database dump missing')
        dump.chmod(0o600)
        listing = self.run(['pg_restore', '--list', dump], timeout=30)
        require('TABLE DATA' in listing and 'schema_migrations' in listing, 'backup catalog verification failed')
        save(self.root / 'database-backup.json', {'verified': False, 'catalog_verified': True,
             'reference': str(dump), 'sha256': digest(dump), 'bytes': dump.stat().st_size})
        self.stage = 'disposable_database_restore'
        cluster = self.root / 'postgres'
        pwfile = self.root / 'postgres-password'
        pwfile.write_text(self.admin_password + '\n', encoding='utf-8')
        pwfile.chmod(0o600)
        self.run(['initdb', '-D', cluster, '-U', 'canary_admin', '--auth-local=scram-sha-256',
                  '--auth-host=scram-sha-256', '--pwfile', pwfile, '--encoding=UTF8'], timeout=90)
        pwfile.unlink()
        # TCP only: long task paths can exceed macOS's Unix socket path limit.
        # No socket is created in /tmp or the task root; listen only on loopback.
        self.pg_attempted = True
        self.run(['pg_ctl', '-D', cluster, '-l', self.root / 'postgres.log', '-w', '-t', '20', '-o',
                  "-h 127.0.0.1 -p %d -c unix_socket_directories='' -c max_connections=40" % DBPORT, 'start'], timeout=30)
        self.sql("CREATE ROLE canary_app LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;\nCREATE DATABASE canary OWNER canary_app;" % self.password, 'postgres')
        self.run(['pg_restore', '-h', '127.0.0.1', '-p', str(DBPORT), '-U', 'canary_admin', '-d', 'canary',
                  '--no-owner', '--no-privileges', '--role=canary_app', '--exit-on-error', dump], env=self.admin_env, timeout=300)
        # The app has no superuser password and cannot grant itself PG file or
        # program execution. SCRAM (not trust) matters even on loopback.
        self.sql('REVOKE pg_read_server_files, pg_write_server_files, pg_execute_server_program FROM canary_app;')
        require(self.sql("SELECT (NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolreplication)::text FROM pg_roles WHERE rolname='canary_app'") == 'true', 'canary role is privileged')
        self.ledger = json.loads(self.sql(LEDGER_QUERY))
        self.atlas = json.loads(self.sql(ATLAS_QUERY))
        check_migrations(self.ledger, self.embedded)
        # The application may read, but cannot insert/update/delete ledger rows.
        self.sql('ALTER TABLE schema_migrations OWNER TO canary_admin; REVOKE ALL ON schema_migrations FROM PUBLIC, canary_app; GRANT SELECT ON schema_migrations TO canary_app;\nALTER TABLE atlas_schema_revisions OWNER TO canary_admin; REVOKE ALL ON atlas_schema_revisions FROM PUBLIC, canary_app; GRANT SELECT ON atlas_schema_revisions TO canary_app;')
        save(self.root / 'migrations-before.json', {'schema_migrations': self.ledger, 'atlas': self.atlas})

    def setup_runtime(self):
        self.stage = 'fenced_runtime_setup'
        config = self.root / 'redis.conf'
        config.write_text('bind 127.0.0.1\nport %d\nsave ""\nappendonly no\ndir "%s"\nrequirepass %s\n' % (REDISPORT, self.root, self.redis_password), encoding='utf-8')
        self.redis = self.spawn(['redis-server', config], self.env, 'redis')
        for _ in range(30):
            require(self.redis.poll() is None, 'disposable Redis exited')
            response = self.run(['redis-cli', '-h', '127.0.0.1', '-p', str(REDISPORT), 'PING'],
                                env=dict(self.env, REDISCLI_AUTH=self.redis_password), timeout=3, allowed=(0, 1))
            if response.strip() == 'PONG':
                break
            time.sleep(0.1)
        else:
            raise Failure('disposable Redis did not become healthy')
        # Same profile for proof and both binaries. Absolute paths are escaped.
        def quoted(path):
            return json.dumps(str(path))
        self.profile = self.root / 'canary.sb'
        # Homebrew's python executable is a framework launcher. Permit its exact
        # resolved re-exec target as well, not arbitrary Python installations.
        framework = Path(sys.base_prefix) / 'Resources/Python.app/Contents/MacOS/Python'
        proof_exec = framework.resolve() if framework.is_file() else Path(sys.executable).resolve()
        # Do not let the sandboxed app tamper with PG config/credentials, dumps,
        # evidence, staging binaries, or the controller. Only runtime dirs and
        # its already-open stdout logs are writable.
        for directory in ('data-new', 'data-old', 'home', 'runtime-tmp'):
            (self.root / directory).mkdir(mode=0o700, exist_ok=True)
        write_rules = '\n'.join('(allow file-write* (subpath %s))' % quoted(self.root / name)
                                for name in ('data-new', 'data-old', 'home', 'runtime-tmp'))
        self.profile.write_text('''(version 1)
(allow default)
(deny network-outbound)
(allow network-outbound (remote ip "localhost:%d") (remote ip "localhost:%d"))
(deny file-write*)
%s
(allow file-write* (literal %s) (literal %s))
(deny file-read* (subpath %s) (subpath %s) (literal %s))
(deny process-exec)
(allow process-exec (literal %s) (literal %s) (literal %s) (literal %s))
''' % (DBPORT, REDISPORT, write_rules, quoted(self.root / 'new.log'), quoted(self.root / 'old.log'),
       quoted(self.live), quoted(self.root / 'postgres'), quoted(self.root / 'database.dump'),
       quoted(self.new_binary), quoted(self.old_binary), quoted(Path(sys.executable).resolve()), quoted(proof_exec)), encoding='utf-8')
        probe = '''import errno,json,socket
rows=[]
for host,port,allowed in [('127.0.0.1',16434,True),('127.0.0.1',16381,True),('127.0.0.1',8080,False),('1.1.1.1',443,False)]:
 s=socket.socket();s.settimeout(2)
 try:
  s.connect((host,port)); row={'port':port,'connected':True,'errno':0}
 except OSError as e: row={'port':port,'connected':False,'errno':e.errno}
 finally: s.close()
 rows.append(row)
print(json.dumps(rows))
'''
        proof = json.loads(self.run(['sandbox-exec', '-f', self.profile, proof_exec, '-I', '-c', probe], timeout=12))
        require(len(proof) == 4 and all(row['connected'] for row in proof[:2])
                and all(not row['connected'] and row['errno'] in (errno.EPERM, errno.EACCES) for row in proof[2:]),
                'sandbox denial not proven (refused connections/timeouts are not proof)')
        save(self.root / 'network-proof.json', {'profile_sha256': digest(self.profile), 'results': proof, 'passed': True})

    def make_fixture(self):
        self.stage = 'restored_only_payment_fixture'
        self.fixture_email = 'payment-canary-' + secrets.token_hex(10) + '@invalid.example'
        self.fixture_password_hash = 'canary-no-login-' + secrets.token_hex(32)
        # Fresh own rows only. Explicit column list fails closed on schema drift.
        self.user = int(self.sql("INSERT INTO users(email,password_hash,role,status,balance_notify_enabled) VALUES ('%s','%s','user','active',false) RETURNING id;" % (self.fixture_email, self.fixture_password_hash)))
        self.group = int(self.sql("INSERT INTO groups(name,platform,subscription_type,status) VALUES ('payment-canary-%s','openai','subscription','active') RETURNING id;" % secrets.token_hex(8)))
        self.other_group = int(self.sql("INSERT INTO groups(name,platform,subscription_type,status) VALUES ('payment-canary-%s','openai','subscription','active') RETURNING id;" % secrets.token_hex(8)))
        self.plan = int(self.sql("INSERT INTO subscription_plans(group_id,name,price,fixed_price_cny,currency,validity_days,validity_unit,for_sale,eligible_user_ids,installment_schedule_cents) VALUES (%d,'isolated-full-fixed',800,1305,'CNY',30,'day',true,'[%d]','[100000,30500]') RETURNING id;" % (self.group, self.user)))
        self.sql("INSERT INTO settings(key,value) VALUES ('SUBSCRIPTION_SIDEBAR_ALLOWLIST','%d') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value;\nUPDATE payment_provider_instances SET enabled=false;\nINSERT INTO settings(key,value) VALUES ('payment_enabled','false'),('RECHARGE_FEE_RATE','15'),('SUBSCRIPTION_USD_TO_CNY_RATE','7.5') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value;" % self.user)
        # Restored settings can contain SMTP/webhook endpoints. Fence networking
        # independently, and remove notification destinations in this copy only.
        self.sql("UPDATE settings SET value='' WHERE key ~* '(smtp|webhook|notification)' AND key <> 'admin_api_key';")
        # The app prefers the persisted JWT secret over JWT_SECRET. Rotate it in
        # this disposable restored copy only, so the fixture token is never
        # signed with the production secret.
        rotated = self.sql("WITH u AS (UPDATE security_secrets SET value='%s' WHERE key='jwt_secret' RETURNING 1) SELECT count(*) FROM u;" % self.jwt)
        require(rotated == '1', 'restored JWT secret row missing; refusing to sign fixture token')
        self.admin_key = self.sql("SELECT value FROM settings WHERE key='admin_api_key';")
        require(bool(self.admin_key), 'restored admin read credential missing')
        self.token = fixture_jwt(self.user, self.fixture_email, self.fixture_password_hash, self.jwt)
        self.order_counts = self.sql('SELECT (SELECT count(*) FROM payment_orders)::text || \'|\' || (SELECT count(*) FROM payment_sessions)::text;')

    def app_env(self, data):
        home = self.root / 'home'
        home.mkdir(exist_ok=True)
        env = dict(self.env, HOME=str(home), TMPDIR=str(self.root / 'runtime-tmp'), DATA_DIR=str(data), AUTO_SETUP='false',
                   SERVER_HOST='127.0.0.1', SERVER_PORT=str(APPPORT), SERVER_MODE='release', RUN_MODE='standard',
                   JWT_SECRET=self.jwt, TOTP_ENCRYPTION_KEY=secrets.token_hex(32),
                   DATABASE_HOST='127.0.0.1', DATABASE_PORT=str(DBPORT), DATABASE_USER='canary_app',
                   DATABASE_PASSWORD=self.password, DATABASE_DBNAME='canary', DATABASE_SSLMODE='disable',
                   DATABASE_MAX_OPEN_CONNS='8', DATABASE_MAX_IDLE_CONNS='2',
                   REDIS_HOST='127.0.0.1', REDIS_PORT=str(REDISPORT), REDIS_PASSWORD=self.redis_password,
                   REDIS_DB='0', REDIS_POOL_SIZE='8', REDIS_MIN_IDLE_CONNS='1',
                   LOG_OUTPUT_TO_FILE='false', LOG_OUTPUT_TO_STDOUT='true', LOG_LEVEL='warn',
                   PRICING_DATA_DIR=str(data / 'pricing'), PRICING_REMOTE_URL='http://127.0.0.1:9/disabled',
                   PRICING_HASH_URL='http://127.0.0.1:9/disabled')
        for key in ('TOKEN_REFRESH', 'BATCH_IMAGE', 'BATCH_IMAGE_QUEUE', 'DASHBOARD_AGGREGATION', 'USAGE_CLEANUP',
                    'OPS_CLEANUP', 'OPS_AGGREGATION', 'OPENCLAW_GATEWAY', 'OPENCLAW_CONCIERGE', 'OPENCLAW_DEVICE_ENROLLMENT'):
            env[key + '_ENABLED'] = 'false'
        return env

    def http(self, path, admin=False, token=False):
        # A hard read-only allowlist: no provider creates, reconciliation, webhook,
        # order verification, POST, PUT, DELETE, redirects, cookies, or proxies.
        allowed = ('/health', '/api/v1/admin/system/version', '/api/v1/admin/groups?page=1&page_size=1',
                   '/api/v1/admin/accounts', '/api/v1/payment/config', '/api/v1/payment/subscription-checkout?')
        require(path in allowed[:-1] or path.startswith(allowed[-1]), 'HTTP path outside read-only canary allowlist')
        headers = {'Cache-Control': 'no-store'}
        if admin:
            headers['x-api-key'] = self.admin_key
        if token:
            headers['Authorization'] = 'Bearer ' + self.token
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, req, fp, code, msg, headers, newurl):
                return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        req = urllib.request.Request('http://127.0.0.1:%d%s' % (APPPORT, path), headers=headers, method='GET')
        try:
            with opener.open(req, timeout=3) as response:
                return response.status, json.loads(response.read(2 * 1024 * 1024))
        except urllib.error.HTTPError as exc:
            try:
                body = json.loads(exc.read(65536))
            except ValueError:
                body = {}
            return exc.code, body

    def assert_ledger_unchanged(self):
        require(json.loads(self.sql(LEDGER_QUERY)) == self.ledger, 'schema migration ledger changed')
        require(json.loads(self.sql(ATLAS_QUERY)) == self.atlas, 'atlas migration ledger changed')

    def payment_checks(self):
        path = '/api/v1/payment/subscription-checkout?plan_id=%d&payment_type=alipay' % self.plan
        status, body = self.http(path, token=True)
        quote = body.get('data', {})
        require(status == 200 and body.get('code') == 0 and quote.get('total_cents') == 130500
                and quote.get('next_amount') == 1305 and not quote.get('pending_order'),
                'fresh checkout did not quote full fixed CNY above 1000')
        # Different group, SAME platform: detects same-group-only regressions.
        self.sql("INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status) VALUES (%d,%d,now(),now()+interval '1 day','active');" % (self.user, self.other_group))
        status, blocked = self.http(path, token=True)
        require(status == 409 and blocked.get('reason') == 'SUBSCRIPTION_PLATFORM_ALREADY_ACTIVE', 'same-platform active subscription did not block real HTTP quote')
        require(self.sql('SELECT (SELECT count(*) FROM payment_orders)::text || \'|\' || (SELECT count(*) FROM payment_sessions)::text;') == self.order_counts, 'read-only quote created order/session rows')
        return {'full_fixed_cny': 1305, 'total_cents': 130500, 'same_platform_http_409': True, 'gateway_create_called': False}

    def exercise(self):
        samples = {}
        for label, binary, commit, version in (('new', self.new_binary, self.a.new_commit, self.new_version),
                                                ('old', self.old_binary, self.old_commit, self.old_version)):
            self.stage = label + '_runtime'
            self.port_free(APPPORT, wait=30)
            data = self.root / ('data-' + label)
            data.mkdir(mode=0o700, exist_ok=True)
            (data / 'config.yaml').write_text('{}\n', encoding='utf-8')
            (data / '.installed').touch()
            env = self.app_env(data)
            proc = self.spawn(['sandbox-exec', '-f', self.profile, binary], env, label, data)
            try:
                ready = False
                deadline = time.monotonic() + 75
                while time.monotonic() < deadline:
                    require(proc.poll() is None, 'canary runtime exited; inspect protected runtime log (including migration errors)')
                    try:
                        status, body = self.http('/health')
                        if status == 200 and body.get('status') == 'ok':
                            ready = True
                            break
                    except (OSError, ValueError):
                        pass
                    time.sleep(0.5)
                require(ready, 'canary readiness deadline expired')
                version_text = self.run(['sandbox-exec', '-f', self.profile, binary, '--version'], env=env, timeout=5, include_stderr=True)
                match = re.search(r'Sub2API ([^\s]+) \(commit: ([^,]+),', version_text)
                require(match is not None and match.group(1) == version and match.group(2) == commit,
                        'canary binary exact version/commit mismatch')
                status, body = self.http('/api/v1/admin/system/version', admin=True)
                require(status == 200 and body.get('data', {}).get('version') == version,
                        'canary runtime API version mismatch')
                status, body = self.http('/api/v1/admin/groups?page=1&page_size=1', admin=True)
                require(status == 200 and body.get('code') == 0, 'restored admin API read failed')
                require(self.http('/api/v1/admin/accounts')[0] == 401 and self.http('/api/v1/payment/config')[0] == 401,
                        'runtime authentication boundary failed')
                status, body = self.http('/api/v1/payment/config', token=True)
                require(status == 200 and body.get('code') == 0, 'restored fixture payment JWT read failed')
                self.assert_ledger_unchanged()
                result = {'version': version, 'commit': commit, 'health': True, 'auth_401': True, 'admin_read': True, 'fixture_payment_read': True}
                if label == 'new':
                    result['payment'] = self.payment_checks()
                samples[label] = result
            finally:
                stop(proc)
            # Recheck after shutdown, not just while startup is in progress.
            self.assert_ledger_unchanged()
            require(self.sql('SELECT (SELECT count(*) FROM payment_orders)::text || \'|\' || (SELECT count(*) FROM payment_sessions)::text;') == self.order_counts, 'runtime unexpectedly created payment rows')
        save(self.root / 'migrations-after.json', {'schema_migrations': self.ledger, 'atlas': self.atlas})
        return samples

    def cleanup(self):
        errors = []
        for proc in reversed(self.children):
            try:
                stop(proc)
            except Exception:
                errors.append('owned child cleanup failed')
        if self.pg_attempted:
            cluster = self.root / 'postgres'
            try:
                try:
                    self.run(['pg_ctl', '-D', cluster, '-m', 'fast', '-w', '-t', '15', 'stop'], timeout=22, allowed=(0, 3))
                except Exception:
                    # Escalate only on this exact fresh task cluster, never a
                    # listener PID, production service, or process-name kill.
                    self.run(['pg_ctl', '-D', cluster, '-m', 'immediate', '-w', '-t', '10', 'stop'], timeout=16, allowed=(0, 3))
                # status=3 proves this exact task cluster is no longer running.
                self.run(['pg_ctl', '-D', cluster, 'status'], timeout=5, allowed=(3,))
            except Exception:
                errors.append('owned PostgreSQL cleanup failed')
        for log in self.logs:
            log.close()
        require(not errors, 'owned runtime cleanup incomplete; prepare is forbidden')

    def controller_prepare(self):
        self.stage = 'controller_prepare'
        require(self.check_baseline() == self.baseline, 'production baseline changed before prepare')
        require(tree_hashes(self.root / 'merged-frontend') == self.merged, 'merged frontend changed')
        require(digest(self.new_binary) == self.package['binary_sha256']
                and digest(self.controller_copy) == self.package['controller_sha256'], 'frozen artifact changed')
        config = {'app_root': str(self.live), 'old_build': {'version': self.old_version, 'commit': self.old_commit},
                  'new_build': {'version': self.new_version, 'commit': self.a.new_commit},
                  'runtime_probe': {'mode': 'binary_inode'}, 'local_base': self.a.local_base,
                  'public_base': self.a.public_base}
        save(self.root / 'release-config.json', config)
        evidence = {'reviewed': True, 'reviewer': 'prepare_canary.py automated evidence checks',
                    'review_scope': 'automated canary; activation is controlled by separately installed deployment policy',
                    'rollback_compatible': True,
                    'database_backup': {'verified': True, 'reference': str(self.root / 'database.dump'),
                                        'sha256': digest(self.root / 'database.dump'), 'restored': True},
                    'canary': {'eligible': True, 'reference': str(self.root / 'canary-result.json'),
                               'sha256': digest(self.root / 'canary-result.json')},
                    'configuration_hashes': self.baseline.get('configuration_hashes', {}),
                    'production_database_writes': False, 'no_new_migrations': True}
        save(self.root / 'evidence.json', evidence)
        # This is the sole controller invocation. No code path invokes arm,
        # activate, runner, watchdog, rollback, launchctl, or production SQL.
        save(self.root / 'prepare-invocation.json', {'command': 'prepare', 'started': time.time(),
             'controller_sha256': digest(self.controller_copy)})
        try:
            receipt = json.loads(self.run([sys.executable, '-I', self.controller_copy, 'prepare',
                     '--config', self.root / 'release-config.json', '--evidence', self.root / 'evidence.json',
                     '--binary', self.new_binary, '--frontend', self.root / 'merged-frontend'], timeout=90))
            require(receipt.get('state') == 'prepared', 'controller did not return prepared state')
            attempt = Path(receipt.get('attempt', ''))
            require(attempt.is_absolute() and inside(attempt, self.live / 'deploy-controller/attempts')
                    and not (attempt / 'activation.json').exists() and not (attempt / 'armed.json').exists(),
                    'invalid prepare-only attempt receipt')
            save(self.root / 'prepare-receipt.json', receipt)
        except Exception:
            save(self.root / 'prepare-receipt.json', {'state': 'unknown_or_failed', 'activated_by_adapter': False,
                 'inspect_host_lock_and_private_logs': True, 'do_not_retry_or_clear_lock': True})
            raise Failure('controller prepare failed or receipt uncertain; inspect on-host lock, do not retry') from None
        return receipt

    def execute(self):
        self.preflight()
        samples = None
        failed = False
        try:
            self.backup_restore()
            self.setup_runtime()
            self.make_fixture()
            samples = self.exercise()
        except BaseException:
            failed = True
            raise
        finally:
            try:
                self.cleanup()
            except Exception:
                if not failed:
                    raise
        self.stage = 'canary_evidence'
        result = {'status': 'passed', 'runtimes': samples, 'cleanup_verified': True,
                  'database_dump_sha256': digest(self.root / 'database.dump'),
                  'old_binary_sha256': self.old_sha, 'new_binary_sha256': digest(self.new_binary),
                  'network_proof': str(self.root / 'network-proof.json'), 'no_new_migrations': True,
                  'production_database_writes': False, 'activation_performed': False}
        save(self.root / 'canary-result.json', result)
        receipt = self.controller_prepare()
        return {'state': 'prepared_only', 'attempt': receipt['attempt'], 'root': str(self.root), 'activation_performed': False}



OVERVIEW = '/api/v1/admin/ops/dashboard/overview?'
PERCENTILES = ('p50_ms', 'p90_ms', 'p95_ms', 'p99_ms', 'avg_ms', 'max_ms')

class DashboardCanary(Canary):
    def make_fixture(self):
        super().make_fixture()
        self.stage = 'restored_only_dashboard_fixture'
        # All IDs belong to this newly inserted synthetic user. Existing account
        # is only a foreign-key reference in this disposable restored data
        account = int(self.sql('SELECT min(id) FROM accounts;'))
        require(account > 0, 'restored account FK reference missing')
        self.insight_key = int(self.sql("INSERT INTO api_keys(user_id,key,name,status) VALUES (%d,'dashboard-canary-used-%s','Dashboard canary used','active') RETURNING id;" % (self.user, self.jwt[:16])))
        self.sql("INSERT INTO api_keys(user_id,key,name,status) SELECT %d,'dashboard-canary-zero-%s-'||n,'Dashboard zero '||n,'active' FROM generate_series(1,1005) n;" % (self.user, self.jwt[:16]))
        self.sql("INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,model,requested_model,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,actual_cost,total_cost,billing_type,created_at) SELECT %d,%d,%d,%d,'fallback','dashboard-requested',1,2,3,4,0,1,n%%2,now()-interval '30 minutes' FROM generate_series(1,6002) n;" % (self.user, self.insight_key, account, self.group))
        self.sql("INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,model,requested_model,input_tokens,actual_cost,total_cost,billing_type,created_at) VALUES (%d,%d,%d,%d,'old-retained','',1,7,7,0,now()-interval '45 days'),(%d,%d,%d,%d,'paid','',1,2,3,1,now()-interval '20 minutes'),(%d,%d,%d,%d,'future','',1,99,99,0,now()+interval '1 day');" % ((self.user, self.insight_key, account, self.group) * 3))
        self.insight_before = self.sql("SELECT u.balance::text||'|'||k.quota_used::text FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=%d AND k.id=%d;" % (self.user, self.insight_key))

    def insight_http(self, path, authenticated=True):
        require(path.startswith('/api/v1/usage/dashboard/snapshot-v2?') and not any(x in path for x in ('\\', '#', '..')), 'dashboard canary route invalid')
        headers = {'Cache-Control': 'no-store'}
        if authenticated: headers['Authorization'] = 'Bearer ' + self.token
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args): return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        request = urllib.request.Request('http://127.0.0.1:%d%s' % (APPPORT, path), headers=headers, method='GET')
        try:
            with opener.open(request, timeout=15) as response:
                return response.status, json.loads(response.read(4 * 1024 * 1024))
        except urllib.error.HTTPError as exc:
            try: body = json.loads(exc.read(65536))
            except ValueError: body = {}
            return exc.code, body

    def dashboard_checks(self):
        dates = self.sql("SELECT (min(created_at) at time zone 'UTC')::date::text||'|'||(now() at time zone 'UTC')::date::text FROM usage_logs WHERE user_id=%d AND api_key_id=%d AND model IN ('fallback','paid');" % (self.user, self.insight_key)).split('|')
        params = {'start_date': dates[0], 'end_date': dates[1], 'timezone': 'UTC', 'include_key_activity': 'true',
                  'include_trend': 'false', 'include_model_stats': 'false', 'include_group_stats': 'false'}
        path = '/api/v1/usage/dashboard/snapshot-v2?' + urllib.parse.urlencode(params)
        require(self.insight_http(path, False)[0] == 401, 'dashboard authentication boundary failed')
        code, body = self.insight_http(path)
        data = body.get('data', {})
        activity = data.get('key_activity') or {}
        require(code == 200 and data.get('key_activity_status') == 'available' and activity.get('coverage') == 'retained_logs', 'exact native dashboard aggregate unavailable')
        keys = activity.get('keys', [])
        require(len(keys) == 1006, 'complete owned-key metadata truncated or foreign key leaked')
        key = next((k for k in keys if k.get('api_key_id') == self.insight_key), {})
        require(key.get('requests') == 6003 and key.get('total_tokens') == 60021, 'complete selected-range usage totals differ')
        # Use the API's own captured instant; a midnight transition between the
        # API response and independent SQL must not change the expected day.
        for stamp in (activity.get('as_of'), activity.get('range_start'), activity.get('range_end_exclusive')):
            require(isinstance(stamp, str) and stamp and all(ch in '0123456789-:+.TZ' for ch in stamp), 'invalid native cutoff')
        today_cost = float(self.sql("SELECT COALESCE(sum(actual_cost),0) FROM usage_logs WHERE user_id=%d AND api_key_id=%d AND created_at >= date_trunc('day','%s'::timestamptz at time zone 'UTC') at time zone 'UTC' AND created_at < '%s'::timestamptz;" % (self.user, self.insight_key, activity['as_of'], activity['as_of'])))
        require(key.get('range_actual_cost') == 2 and key.get('lifetime_actual_cost') == 9 and key.get('today_actual_cost') == today_cost, 'retained lifetime/free/subscription expense semantics differ')
        require(key.get('favourite_model') == 'dashboard-requested', 'requested favourite-model dimension differs')
        require(len([k for k in keys if k.get('requests') == 0]) == 1005, 'zero-use key set differs')
        windows = activity.get('active_windows', [])
        require([w.get('id') for w in windows] == ['60m', '12h', '24h'], 'active window contract differs')
        require(all(w.get('requests') == 6003 and w.get('tokens') == 60021 for w in windows), 'active complete aggregate differs')
        for window, minutes in zip(windows, (60, 720, 1440)):
            require(abs(window.get('rpm', -1) - 6003 / minutes) < 1e-8 and abs(window.get('tpm', -1) - 60021 / minutes) < 1e-8, 'active rates differ')
        # Read through the Go driver against a migration-complete restored schema;
        # then independently reconcile the returned selected range in native PG.
        expected = json.loads(self.sql("SELECT json_build_object('requests',count(*),'tokens',sum(input_tokens+output_tokens+cache_creation_tokens+cache_read_tokens),'cost',sum(actual_cost)) FROM usage_logs WHERE user_id=%d AND api_key_id=%d AND created_at>='%s'::timestamptz AND created_at<'%s'::timestamptz;" % (self.user, self.insight_key, activity['range_start'], activity['range_end_exclusive'])))
        require(expected == {'requests':key['requests'], 'tokens':key['total_tokens'], 'cost':key['range_actual_cost']}, 'native independent SQL reconciliation differs')
        serialized = json.dumps(activity)
        require('account_cost' not in serialized and 'dashboard-canary-used-' not in serialized, 'aggregate leaked secret fields')
        # A user_id query override cannot escape the JWT subject.
        other_code, other_body = self.insight_http(path + '&user_id=1')
        # Only today's field can legitimately change if the next request
        # crosses UTC midnight. The owned set and selected/lifetime totals stay exact.
        def stable_keys(items):
            return [{k: v for k, v in item.items() if k != 'today_actual_cost'} for item in items]
        other_keys = other_body.get('data', {}).get('key_activity', {}).get('keys')
        require(other_code == 200 and isinstance(other_keys, list) and stable_keys(other_keys) == stable_keys(keys), 'dashboard subject override escaped scope')
        after = self.sql("SELECT u.balance::text||'|'||k.quota_used::text FROM users u JOIN api_keys k ON k.user_id=u.id WHERE u.id=%d AND k.id=%d;" % (self.user, self.insight_key))
        require(after == self.insight_before, 'dashboard read changed balance/quota')
        result = {'passed':True, 'retained_keys':1006, 'range_requests':6003, 'range_tokens':60021,
                  'range_actual_cost':2, 'lifetime_actual_cost':9, 'windows_requests':[6003]*3,
                  'native_postgres_schema_and_go_driver':True, 'independent_sql_reconciliation':True,
                  'auth_subject_override_isolated':True, 'balance_quota_unchanged':True, 'production_writes':False}
        save(self.root / 'dashboard-native-result.json', result)
        return result

    def payment_checks(self):
        payment = super().payment_checks()
        payment['dashboard'] = self.dashboard_checks()
        return payment


class OpsHealthCanary(DashboardCanary):
    def ops_http(self, params, authenticated=True):
        path = OVERVIEW + urllib.parse.urlencode(params)
        require(path.startswith(OVERVIEW) and not any(x in path for x in ('\\', '#', '..')), 'ops canary route invalid')
        headers = {'Cache-Control': 'no-store'}
        if authenticated: headers['x-api-key'] = self.admin_key
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args): return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        request = urllib.request.Request('http://127.0.0.1:%d%s' % (APPPORT, path), headers=headers, method='GET')
        try:
            with opener.open(request, timeout=20) as response:
                return response.status, json.loads(response.read(4 * 1024 * 1024))
        except urllib.error.HTTPError as exc:
            try: body = json.loads(exc.read(65536))
            except ValueError: body = {}
            return exc.code, body

    def ops_checks(self):
        require(self.ops_http({'time_range': '1h', 'mode': 'raw'}, authenticated=False)[0] == 401, 'ops overview authentication boundary failed')
        code, body = self.ops_http({'time_range': '1h', 'mode': 'raw'})
        data = body.get('data') or {}
        require(code == 200 and body.get('code') == 0, 'ops overview unavailable on restored copy')
        score = data.get('health_score')
        require(isinstance(score, int) and 0 <= score <= 100, 'health score missing or out of range')
        overhead = data.get('gateway_overhead')
        require(isinstance(overhead, dict) and isinstance(overhead.get('window_minutes'), int) and overhead['window_minutes'] >= 1
                     and isinstance(overhead.get('sample_count'), int) and overhead['sample_count'] >= 0, 'gateway overhead missing in unfiltered view')
        code, filtered = self.ops_http({'time_range': '1h', 'mode': 'raw', 'platform': 'openai'})
        require(code == 200 and 'gateway_overhead' not in (filtered.get('data') or {}), 'gateway overhead leaked into filtered view')

        # Same explicit window for both modes: the pre-aggregated path must now
        # report the exact window percentiles, identical to raw mode.
        end = dt.datetime.now(dt.timezone.utc).replace(second=0, microsecond=0)
        window = {'start_time': (end - dt.timedelta(hours=24)).strftime('%Y-%m-%dT%H:%M:%SZ'), 'end_time': end.strftime('%Y-%m-%dT%H:%M:%SZ')}
        code, raw = self.ops_http(dict(window, mode='raw'))
        require(code == 200 and raw.get('code') == 0, 'raw 24h overview failed')
        code, pre = self.ops_http(dict(window, mode='preagg'))
        comparison = 'not_populated'
        if code == 409:
            require('PREAGG' in json.dumps(pre), 'unexpected preagg conflict')
        else:
            require(code == 200 and pre.get('code') == 0, 'preagg 24h overview failed')
            raw_data, pre_data = raw['data'], pre['data']
            for metric in ('ttft', 'duration'):
                require({k: (raw_data.get(metric) or {}).get(k) for k in PERCENTILES} == {k: (pre_data.get(metric) or {}).get(k) for k in PERCENTILES},
                             'preagg percentiles differ from exact raw window')
            comparison = 'identical'
        ttft = (raw.get('data') or {}).get('ttft') or {}
        result = {'passed': True, 'auth_401': True, 'health_score_1h': score, 'gateway_overhead_unfiltered': True,
                  'gateway_overhead_filtered_absent': True, 'preagg_vs_raw_24h': comparison,
                  'ttft_24h_p90_ms': ttft.get('p90_ms'), 'ttft_24h_p99_ms': ttft.get('p99_ms'),
                  'health_score_24h': (raw.get('data') or {}).get('health_score'), 'production_writes': False}
        save(self.root / 'ops-health-result.json', result)
        return result

    def payment_checks(self):
        payment = super().payment_checks()
        payment['ops_health'] = self.ops_checks()
        return payment


class OpenClawPricingCanary(OpsHealthCanary):
    def group_http(self, group_id, payload=None, authenticated=True):
        require(isinstance(group_id, int) and group_id > 0, 'invalid restored group id')
        headers = {'Cache-Control': 'no-store', 'Content-Type': 'application/json'}
        if authenticated:
            headers['x-api-key'] = self.admin_key
        data = None if payload is None else json.dumps(payload).encode()

        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args):
                return None

        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        req = urllib.request.Request('http://127.0.0.1:%d/api/v1/admin/groups/%d' % (APPPORT, group_id),
                                     data=data, headers=headers, method='GET' if data is None else 'PUT')
        try:
            with opener.open(req, timeout=15) as response:
                return response.status, json.loads(response.read(2 * 1024 * 1024))
        except urllib.error.HTTPError as exc:
            try:
                body = json.loads(exc.read(65536))
            except ValueError:
                body = {}
            return exc.code, body

    def pricing_checks(self):
        group_id = int(self.sql("SELECT id FROM groups WHERE name='openai-龙虾' AND deleted_at IS NULL;"))
        code, before = self.group_http(group_id)
        require(code == 200 and before.get('code') == 0, 'managed group read failed')
        identity = {key: before['data'][key] for key in ('name', 'platform', 'status', 'subscription_type', 'is_exclusive')}
        require(self.group_http(group_id, {'rate_multiplier': 0.427}, authenticated=False)[0] == 401,
                     'unauthenticated pricing write was not rejected')
        for multiplier in (0.427, 1.25):
            code, body = self.group_http(group_id, {'rate_multiplier': multiplier})
            require(code == 200 and body.get('data', {}).get('rate_multiplier') == multiplier,
                         'admin multiplier update failed')
            code, body = self.group_http(group_id)
            require(code == 200 and body.get('data', {}).get('rate_multiplier') == multiplier,
                         'admin multiplier did not persist through API read')
            require(float(self.sql('SELECT rate_multiplier FROM groups WHERE id=%d;' % group_id)) == multiplier,
                         'admin multiplier did not persist in restored database')
            require({key: body['data'][key] for key in identity} == identity, 'managed group identity changed')
        for payload in ({'name': 'renamed-managed-group'}, {'status': 'inactive'}, {'is_exclusive': True}):
            code, body = self.group_http(group_id, payload)
            require(code == 403 and body.get('reason') == 'OPENCLAW_BILLING_LOCKED',
                         'managed group identity protection failed')
        result = {'passed': True, 'admin_multiplier_values': [0.427, 1.25], 'api_and_database_persistence': True,
                  'unauthenticated_write_401': True, 'identity_write_403': True, 'production_writes': False}
        save(self.root / 'openclaw-pricing-result.json', result)
        return result

    def payment_checks(self):
        result = super().payment_checks()
        result['openclaw_pricing'] = self.pricing_checks()
        return result


ProductionCanary = OpenClawPricingCanary


def parser():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    for name in ('root', 'package-json', 'binary', 'frontend', 'migrations', 'new-commit', 'controller', 'baseline-json'):
        p.add_argument('--' + name, required=True)
    p.add_argument('--app-root', default=str(Path.home() / '.local/share/sub2api'),
                   help='current user macOS production root; must match the tested controller root')
    p.add_argument('--env-file', help='safely parse only production DB settings without shell evaluation')
    p.add_argument('--local-base', default='http://127.0.0.1:8080')
    p.add_argument('--public-base', required=True)
    p.add_argument('--deadline-seconds', type=int, default=600)
    return p


def main():
    os.umask(0o077)
    args = parser().parse_args()
    worker = ProductionCanary(args)
    def interrupted(signum, frame):
        raise Failure('interrupted; owned resources will be cleaned up')
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, interrupted)
    def deadline_elapsed(signum, frame):
        raise Failure('total canary deadline elapsed; activation blocked')
    if args.deadline_seconds <= 0 or args.deadline_seconds > 900:
        print(json.dumps({'status': 'failed', 'error': 'invalid total canary deadline', 'activation_performed': False}), file=sys.stderr)
        return 1
    if hasattr(signal, 'SIGALRM'):
        signal.signal(signal.SIGALRM, deadline_elapsed)
        signal.alarm(args.deadline_seconds)
    try:
        print(json.dumps(worker.execute(), sort_keys=True))
        return 0
    except BaseException as exc:
        # Never expose DB values, HTTP bodies, subprocess args, JWTs or tracebacks.
        if worker.created:
            failure_path = worker.root / 'failure.json'
            if not failure_path.exists():
                save(failure_path, {'status': 'failed', 'stage': worker.stage, 'reviewed': False,
                                   'rollback_compatible': False, 'activation_performed': False})
        message = str(exc) if isinstance(exc, Failure) else 'unexpected failure; inspect protected on-host artifacts'
        print(json.dumps({'status': 'failed', 'stage': worker.stage, 'error': message, 'activation_performed': False}), file=sys.stderr)
        return 1
    finally:
        if hasattr(signal, 'SIGALRM'):
            signal.alarm(0)


if __name__ == '__main__':
    sys.exit(main())
