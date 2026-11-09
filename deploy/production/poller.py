#!/usr/bin/env python3
"""Pull exact-SHA, green GitHub production releases; never build on the host.

The launch agent runs this once every five minutes. Durable intent records and
the deployment controller own crash recovery; an uncertain activation is never
replayed. Credentials are read only by the canary from the host's existing env.
"""
import argparse
import contextlib
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import time
import uuid

# Verified tool directories must stay byte-for-byte unchanged across restarts.
# In particular, module() must not cache github_api beside its source.
sys.dont_write_bytecode = True

HERE = Path(__file__).resolve().parent
SHA = re.compile(r'[0-9a-f]{40}')
DIGEST = re.compile(r'[0-9a-f]{64}')
TERMINAL = {'committed', 'rolled_back', 'rollback_failed', 'prepare_failed', 'aborted_untouched'}
MAX_ARCHIVE = 512 * 1024 * 1024
MAX_EXTRACTED = 1024 * 1024 * 1024


class Failure(RuntimeError):
    pass


def require(condition, reason):
    if not condition:
        raise Failure(reason)


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    loaded = importlib.util.module_from_spec(spec)
    sys.modules[name] = loaded
    spec.loader.exec_module(loaded)
    return loaded


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def read_json(path):
    path = Path(path)
    require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 8 * 1024 * 1024, 'invalid-json-file')
    return json.loads(path.read_text(encoding='utf-8'))


def save(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_name(path.name + '.' + uuid.uuid4().hex + '.tmp')
    with temporary.open('x', encoding='utf-8') as output:
        os.chmod(temporary, 0o600)
        json.dump(value, output, sort_keys=True, indent=2)
        output.write('\n')
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, path)
    if os.name != 'nt':
        fd = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def inventory(directory):
    directory = Path(directory)
    require(directory.is_dir() and not directory.is_symlink(), 'invalid-directory')
    result = {}
    for item in sorted(directory.rglob('*')):
        require(not item.is_symlink() and (item.is_dir() or item.is_file()), 'linked-or-special-file')
        if item.is_file():
            result[item.relative_to(directory).as_posix()] = digest(item)
    return result


@contextlib.contextmanager
def lock(path):
    with Path(path).open('a+b') as handle:
        if handle.tell() == 0:
            handle.write(b'0')
            handle.flush()
        try:
            if os.name == 'nt':
                import msvcrt
                handle.seek(0)
                msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            raise Failure('poller-already-running') from None
        try:
            yield
        finally:
            if os.name == 'nt':
                handle.seek(0)
                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def validate_manifest(manifest, deployment, repository, sha):
    require(manifest.get('schema') == 1 and deployment.get('schema') == 1, 'unsupported-package-schema')
    require(deployment.get('repository') == repository and manifest.get('repository') == repository, 'wrong-package-repository')
    require(deployment.get('new_commit') == sha and manifest.get('new_commit') == sha, 'wrong-package-commit')
    require(deployment.get('platform') == 'darwin/arm64' and manifest.get('platform') == 'darwin/arm64', 'wrong-package-platform')
    identity = manifest.get('build_identity', {})
    require(identity.get('commit') == sha and identity.get('version') == deployment.get('version'), 'wrong-build-identity')
    require(identity.get('goos') == 'darwin' and identity.get('goarch') == 'arm64', 'wrong-binary-platform')
    source_tree = manifest.get('source_tree_sha256')
    require(isinstance(source_tree, str) and bool(DIGEST.fullmatch(source_tree))
            and source_tree == deployment.get('source_tree_sha256')
            and source_tree == identity.get('source_tree_sha256'), 'wrong-source-tree')
    for key in ('binary_sha256', 'controller_sha256'):
        require(bool(DIGEST.fullmatch(manifest.get(key, ''))), 'invalid-package-hash')
    for key in ('frontend_files', 'migration_files', 'embedded_migrations', 'tools_files'):
        require(isinstance(manifest.get(key), dict) and bool(manifest[key]), 'empty-package-inventory')
        for name, checksum in manifest[key].items():
            require(safe_member(name) and bool(DIGEST.fullmatch(checksum)), 'invalid-inventory-entry')
    require('index.html' in manifest['frontend_files'] and 'poller.py' in manifest['tools_files']
            and 'controller.py' in manifest['tools_files'] and 'prepare_canary.py' in manifest['tools_files']
            and 'activate_prepared.py' in manifest['tools_files'] and 'github_api.py' in manifest['tools_files']
            and 'install.py' in manifest['tools_files'], 'incomplete-package')
    require(manifest['tools_files']['controller.py'] == manifest['controller_sha256'], 'controller-hash-conflict')


def safe_member(name):
    path = PurePosixPath(name)
    return bool(name) and '\\' not in name and ':' not in name and not name.startswith('/') \
        and all(part not in ('', '.', '..') for part in name.split('/')) and path.as_posix() == name


def extract_verified(archive, destination, deployment, repository, sha):
    """Extract only one validated, bounded package into a fresh private folder."""
    require(not destination.exists() and not destination.is_symlink(), 'package-directory-exists')
    require(Path(archive).is_file() and not Path(archive).is_symlink()
            and Path(archive).stat().st_size <= MAX_ARCHIVE, 'invalid-package-archive')
    require(Path(archive).stat().st_size == deployment.get('archive_size'), 'archive-size-mismatch')
    expected_archive = 'sub2api-production-' + sha + '-darwin-arm64.tar.gz'
    require(deployment.get('archive_name') == expected_archive, 'wrong-archive-name')
    require(digest(archive) == deployment.get('archive_sha256'), 'archive-hash-mismatch')
    members_by_name = {}
    total = 0
    with tarfile.open(archive, 'r:gz') as source:
        seen = set()
        for member in source:
            require(len(seen) < 10000, 'archive-file-limit')
            require(safe_member(member.name) and member.isfile() and not member.issparse(), 'unsafe-archive-member')
            folded = member.name.casefold()
            require(folded not in seen, 'duplicate-archive-member')
            seen.add(folded)
            total += member.size
            require(member.size <= MAX_ARCHIVE and total <= MAX_EXTRACTED, 'archive-size-limit')
            members_by_name[member.name] = member
        require('manifest.json' in members_by_name, 'missing-package-manifest')
        member = members_by_name['manifest.json']
        require(member.size <= 8 * 1024 * 1024, 'manifest-size-limit')
        manifest_bytes = source.extractfile(member).read()
        require(hashlib.sha256(manifest_bytes).hexdigest() == deployment.get('manifest_sha256'), 'manifest-hash-mismatch')
        manifest = json.loads(manifest_bytes)
        validate_manifest(manifest, deployment, repository, sha)
        expected = {'manifest.json': deployment['manifest_sha256'], 'bin/sub2api': manifest['binary_sha256']}
        for field, prefix in (('frontend_files', 'frontend/'), ('migration_files', 'migrations/'), ('tools_files', 'tools/')):
            expected.update({prefix + key: value for key, value in manifest[field].items()})
        require(set(expected) == set(members_by_name), 'package-inventory-mismatch')
        # A killed extraction must never leave a partial final payload that the
        # next polling cycle would mistake for a verified cached package.
        temporary = destination.with_name('.' + destination.name + '.' + uuid.uuid4().hex + '.extract')
        temporary.mkdir(mode=0o700, parents=False)
        try:
            for name in sorted(expected):
                target = temporary.joinpath(*PurePosixPath(name).parts)
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                h = hashlib.sha256()
                with source.extractfile(members_by_name[name]) as input_file, target.open('xb') as output:
                    for chunk in iter(lambda: input_file.read(1024 * 1024), b''):
                        h.update(chunk)
                        output.write(chunk)
                    output.flush()
                    os.fsync(output.fileno())
                require(h.hexdigest() == expected[name], 'package-file-hash-mismatch')
                target.chmod(0o700 if name == 'bin/sub2api' else 0o600)
            if os.name != 'nt':
                directories = [p for p in temporary.rglob('*') if p.is_dir()] + [temporary]
                for directory in sorted(directories, key=lambda p: len(p.parts), reverse=True):
                    fd = os.open(directory, os.O_RDONLY)
                    try:
                        os.fsync(fd)
                    finally:
                        os.close(fd)
            require(not destination.exists() and not destination.is_symlink(), 'package-directory-exists')
            os.rename(temporary, destination)
            if os.name != 'nt':
                fd = os.open(destination.parent, os.O_RDONLY)
                try:
                    os.fsync(fd)
                finally:
                    os.close(fd)
        finally:
            if temporary.exists():
                require(temporary.parent.resolve() == destination.parent.resolve()
                        and not temporary.is_symlink(), 'unsafe-extraction-cleanup')
                shutil.rmtree(temporary)
    return manifest


def baseline(app_root, version=None, commit=None):
    pointer = app_root / 'frontend/current'
    require(pointer.is_symlink(), 'frontend-pointer-missing')
    target = pointer.resolve(strict=True)
    require(target.is_relative_to(app_root / 'frontend/releases'), 'frontend-pointer-outside-release-root')
    binary = app_root / 'bin/sub2api'
    require(binary.is_file() and not binary.is_symlink(), 'backend-binary-missing')
    if version is None or commit is None:
        response = subprocess.run([str(binary), '--version'], stdin=subprocess.DEVNULL,
                                  capture_output=True, text=True, timeout=10)
        match = re.search(r'Sub2API ([^\s]+) \(commit: ([0-9a-f]{40}),', response.stdout + response.stderr)
        require(response.returncode == 0 and match is not None, 'backend-build-identity-unavailable')
        version, commit = match.groups()
    protected = ('.env', 'run.sh', 'launchd-run.sh', 'deployment.json')
    require(all((app_root / name).is_file() and not (app_root / name).is_symlink() for name in protected),
            'protected-configuration-missing')
    if (app_root / 'frontend/Caddyfile').exists():
        protected += ('frontend/Caddyfile',)
    require(all(not (app_root / name).is_symlink() for name in protected), 'linked-configuration-file')
    return {'version': version, 'source_commit': commit, 'binary_sha256': digest(binary),
            'frontend_pointer': os.readlink(pointer), 'frontend_files': inventory(target),
            'configuration_hashes': {name: digest(app_root / name) for name in protected}}


def verify_baseline(app_root, installed):
    current = baseline(app_root, installed['version'], installed['source_commit'])
    for key in ('binary_sha256', 'frontend_pointer', 'frontend_files', 'configuration_hashes'):
        require(current[key] == installed[key], 'installed-baseline-drift:' + key)
    return current


def cli(script, args, timeout=30, log=None):
    options = {'creationflags': subprocess.CREATE_NO_WINDOW} if os.name == 'nt' else {}
    if log is not None:
        with Path(log).open('xb') as output:
            result = subprocess.run([sys.executable, '-I', str(script)] + args, stdin=subprocess.DEVNULL,
                                    stdout=output, stderr=subprocess.STDOUT, timeout=timeout, **options)
        require(result.returncode == 0, 'protected-command-failed')
        return None
    result = subprocess.run([sys.executable, '-I', str(script)] + args, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=timeout, **options)
    require(result.returncode == 0, 'controller-command-failed')
    return json.loads(result.stdout)


class Poller:
    def __init__(self, config, api=None, clock=time.time):
        require(config.get('schema') == 1, 'unsupported-config-schema')
        require(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', config.get('repository', '')) is not None, 'invalid-repository')
        self.config, self.clock = config, clock
        self.app = Path(config['app_root']).expanduser().resolve(strict=True)
        self.state = self.app / 'auto-deploy'
        require(self.state.is_dir() and not self.state.is_symlink(), 'poller-not-installed')
        self.installed_path = self.state / 'installed.json'
        self.operation_path = self.state / 'operation.json'
        if api is None:
            api = module('production_github_api', HERE / 'github_api.py').GitHub(config['repository'])
        self.api = api

    def status(self, state, **details):
        result = dict(state=state, checked_at=self.clock(), **details)
        save(self.state / 'status.json', result)
        return result

    def operation(self, **changes):
        current = read_json(self.operation_path) if self.operation_path.exists() else {}
        current.update(changes, updated_at=self.clock())
        save(self.operation_path, current)
        return current

    def gate_current(self, sha):
        require(self.api.main_sha() == sha, 'superseded-candidate')
        gates = self.api.gate(sha)
        require(gates.get('ready') is True, 'checks-not-green')
        return gates

    def stage(self, sha):
        release = self.api.release(sha)
        require(release is not None and not release.get('draft') and release.get('tag_name') == 'production-' + sha,
                'production-release-not-ready')
        assets = release.get('assets', [])
        require(len({item['name'] for item in assets}) == len(assets), 'duplicate-release-assets')
        by_name = {item['name']: item for item in assets}
        require('deployment.json' in by_name, 'deployment-metadata-missing')
        stage = self.state / 'packages' / sha
        stage.mkdir(mode=0o700, parents=True, exist_ok=True)
        deployment_path = stage / 'deployment.json'
        if not deployment_path.exists():
            self.api.download(by_name['deployment.json']['browser_download_url'], deployment_path, max_bytes=1024 * 1024)
        deployment = read_json(deployment_path)
        archive_name = deployment.get('archive_name', '')
        require(archive_name == 'sub2api-production-' + sha + '-darwin-arm64.tar.gz' and archive_name in by_name,
                'production-archive-missing')
        require(bool(DIGEST.fullmatch(deployment.get('archive_sha256', ''))) and
                bool(DIGEST.fullmatch(deployment.get('manifest_sha256', ''))), 'invalid-release-hashes')
        archive = stage / archive_name
        if not archive.exists():
            self.api.download(by_name[archive_name]['browser_download_url'], archive,
                              expected_sha256=deployment['archive_sha256'], max_bytes=MAX_ARCHIVE)
        package = stage / 'payload'
        if not package.exists():
            manifest = extract_verified(archive, package, deployment, self.config['repository'], sha)
        else:
            require(digest(archive) == deployment['archive_sha256'], 'staged-archive-changed')
            manifest = read_json(package / 'manifest.json')
            validate_manifest(manifest, deployment, self.config['repository'], sha)
            require(digest(package / 'manifest.json') == deployment['manifest_sha256'], 'staged-manifest-changed')
            for field, folder in (('frontend_files', 'frontend'), ('migration_files', 'migrations'), ('tools_files', 'tools')):
                require(inventory(package / folder) == manifest[field], 'staged-package-changed')
            require(digest(package / 'bin/sub2api') == manifest['binary_sha256'], 'staged-binary-changed')
        return stage, package, manifest, deployment

    def controller_status(self, operation):
        root = Path(operation['canary_root'])
        receipt = read_json(root / 'prepare-receipt.json')
        require(receipt.get('state') == 'prepared', 'uncertain-prepare-receipt')
        return cli(root / 'controller.py', ['status', '--attempt', receipt['attempt']])

    def finish(self, operation):
        root = Path(operation['canary_root'])
        state = self.controller_status(operation)
        terminal = state.get('state', {}).get('state')
        if terminal not in TERMINAL:
            receipt = read_json(root / 'prepare-receipt.json')
            if terminal != 'prepared' or (Path(receipt['attempt']) / 'armed.json').exists():
                # launchd jobs bootstrapped from an attempt directory disappear
                # after logout/reboot. Restore only the frozen watchdog, never
                # the runner or activation, and preserve all original deadlines.
                cli(root / 'controller.py', ['ensure-watchdog', '--attempt', receipt['attempt']], timeout=90)
                state = self.controller_status(operation)
                terminal = state.get('state', {}).get('state')
        if terminal in {'committed', 'rolled_back'} and state.get('host_lock_held') is not False:
            # Successful state is durable before lock release. Let only the
            # frozen controller settle its owned lock, then verify fresh status.
            receipt = read_json(root / 'prepare-receipt.json')
            try:
                cli(root / 'controller.py', ['ensure-watchdog', '--attempt', receipt['attempt']], timeout=90)
            except Exception:
                # Missing/conflicting decisions or a foreign owner fail closed;
                # only the fresh status below can establish completed cleanup.
                pass
            state = self.controller_status(operation)
            terminal = state.get('state', {}).get('state')
        if terminal in TERMINAL and terminal != 'rollback_failed' and state.get('host_lock_held') is not False:
            # Never publish installed/tooling receipts on missing cleanup proof.
            # rollback_failed intentionally remains a terminal failure with lock.
            return self.status('activation-pending', commit=operation['sha'], reason='terminal-cleanup-pending')
        if terminal not in TERMINAL:
            # A crash between the poller's intent and the adapter's first arm
            # intent leaves a prepared lock with no watchdog. Close it safely
            # after the bounded adapter lifetime; never retry activation.
            if terminal == 'prepared' and self.clock() - operation.get('activation_started_at', self.clock()) > 600:
                receipt = read_json(root / 'prepare-receipt.json')
                cli(root / 'controller.py', ['close-untouched', '--attempt', receipt['attempt']], timeout=90)
                self.operation(phase='aborted_untouched', reason='activation-not-started', retry_after=self.clock() + 900)
                return self.status('aborted_untouched', commit=operation['sha'])
            return self.status('activation-pending', commit=operation['sha'])
        if terminal != 'committed':
            self.operation(phase=terminal, retry_after=self.clock() + 86400)
            return self.status(terminal, commit=operation['sha'])
        manifest = read_json(Path(operation['package_root']) / 'manifest.json')
        installed = baseline(self.app, manifest['build_identity']['version'], operation['sha'])
        require(installed['binary_sha256'] == manifest['binary_sha256'], 'committed-binary-drift')
        attempt_config = read_json(Path(state['attempt']) / 'config.json')
        require(installed['frontend_files'] == attempt_config['new_frontend_hashes'], 'committed-frontend-drift')
        require(installed['configuration_hashes'] == read_json(root / 'baseline.json')['configuration_hashes'], 'committed-config-drift')
        installed.update(schema=1, repository=self.config['repository'], attempt=state['attempt'],
                         raw_frontend_files=manifest['frontend_files'], archive_sha256=operation['archive_sha256'],
                         installed_at=self.clock(), source_tree_sha256=manifest.get('source_tree_sha256'))
        save(self.installed_path, installed)
        # Switch the verified deployment tooling only after the production attempt
        # commits. A running poller keeps its resolved old modules until exit.
        tools = Path(operation['package_root']) / 'tools'
        require(inventory(tools) == manifest['tools_files'], 'committed-tooling-drift')
        temporary = self.state / ('current.' + uuid.uuid4().hex)
        os.symlink(tools, temporary, target_is_directory=True)
        os.replace(temporary, self.state / 'current')
        self.operation(phase='committed', committed_at=self.clock())
        return self.status('committed', commit=operation['sha'], attempt=Path(state['attempt']).name)

    def resume(self, operation):
        phase = operation.get('phase')
        if phase in ('activation_started', 'activation_pending'):
            # Read status only. The detached controller runner/watchdog has
            # bounded deadlines and owns completion/recovery after parent exit.
            return self.finish(operation)
        if phase in ('canary_started', 'prepared'):
            root = Path(operation['canary_root'])
            if (root / 'failure.json').exists():
                self.operation(phase='canary_failed', retry_after=self.clock() + 86400)
                return self.status('canary-failed', commit=operation['sha'])
            if not (root / 'prepare-receipt.json').exists():
                return self.status('canary-pending-inspection', commit=operation['sha'])
            receipt = read_json(root / 'prepare-receipt.json')
            require(receipt.get('state') == 'prepared', 'uncertain-prepare-receipt')
            if (self.state / 'PAUSED').exists() or self.config.get('enabled') is not True:
                cli(root / 'controller.py', ['close-untouched', '--attempt', receipt['attempt']], timeout=90)
                self.operation(phase='aborted_untouched', reason='paused-before-activation', retry_after=self.clock() + 900)
                return self.status('paused', commit=operation['sha'])
            try:
                self.gate_current(operation['sha'])
            except Exception:
                cli(root / 'controller.py', ['close-untouched', '--attempt', receipt['attempt']], timeout=90)
                self.operation(phase='aborted_untouched', retry_after=self.clock() + 900)
                raise
            # Durable activation intent precedes invocation. An exception or
            # crash after this point is reconciled exclusively through status.
            self.operation(phase='activation_started', activation_started_at=self.clock())
            package = Path(operation['package_root'])
            try:
                cli(package / 'tools/activate_prepared.py', ['--canary-root', str(root)], timeout=520,
                    log=Path(operation['stage']) / ('activation-' + root.name + '.log'))
            except Exception:
                pass
            return self.finish(read_json(self.operation_path))
        return None

    def once(self):
        with lock(self.state / 'poller.lock'):
            operation = read_json(self.operation_path) if self.operation_path.exists() else {}
            resumed = self.resume(operation)
            if resumed is not None:
                return resumed
            if (self.state / 'PAUSED').exists() or self.config.get('enabled') is not True:
                return self.status('paused')
            installed = read_json(self.installed_path)
            sha = self.api.main_sha()
            require(bool(SHA.fullmatch(sha)), 'invalid-main-commit')
            if sha == installed['source_commit']:
                return self.status('up-to-date', commit=sha)
            if operation.get('sha') == sha and operation.get('retry_after', 0) > self.clock():
                return self.status('candidate-blocked', commit=sha, reason=operation.get('reason', operation.get('phase')))
            self.operation(sha=sha, phase='checking', started_at=self.clock())
            try:
                self.gate_current(sha)
                require(self.api.is_descendant(installed['source_commit'], sha), 'candidate-not-descendant-of-installed')
                current = verify_baseline(self.app, installed)
                stage, package, manifest, deployment = self.stage(sha)
                self.gate_current(sha)
                baseline_path = stage / 'host-baseline.json'
                save(baseline_path, current)
                canary_parent = Path.home() / '.local/share/sub2api-auto-canaries'
                canary_parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                require(not canary_parent.is_symlink() and not canary_parent.is_relative_to(self.app), 'unsafe-canary-root')
                root = canary_parent / (sha[:12] + '-' + uuid.uuid4().hex)
                self.operation(phase='canary_started', stage=str(stage), package_root=str(package), canary_root=str(root),
                               archive_sha256=deployment['archive_sha256'])
                args = ['--root', str(root), '--package-json', str(package / 'manifest.json'),
                        '--binary', str(package / 'bin/sub2api'), '--frontend', str(package / 'frontend'),
                        '--migrations', str(package / 'migrations'), '--new-commit', sha,
                        '--controller', str(package / 'tools/controller.py'), '--baseline-json', str(baseline_path),
                        '--app-root', str(self.app), '--public-base', self.config['public_base'],
                        '--local-base', self.config.get('local_base', 'http://127.0.0.1:8080'),
                        '--env-file', str(self.app / '.env'), '--deadline-seconds', '900']
                # One log per attempt: a retry of the same candidate must not collide with the
                # previous attempt's log (cli opens it exclusively).
                cli(package / 'tools/prepare_canary.py', args, timeout=940, log=stage / ('canary-' + root.name + '.log'))
                self.operation(phase='prepared')
                return self.resume(read_json(self.operation_path))
            except Exception as exc:
                reason = str(exc) if isinstance(exc, Failure) else type(exc).__name__
                current_op = read_json(self.operation_path)
                if current_op.get('phase') == 'canary_started':
                    root = Path(current_op['canary_root'])
                    if not root.exists() or (root / 'failure.json').exists():
                        # Keep the daily backoff and the underlying reason; a failed canary
                        # must not be retried every 15 minutes under a generic "blocked".
                        self.operation(phase='canary_failed', reason='canary-failed', detail=reason,
                                       retry_after=self.clock() + 86400)
                        current_op = read_json(self.operation_path)
                if current_op.get('phase') not in ('activation_started', 'activation_pending', 'canary_started', 'prepared',
                                                   'canary_failed'):
                    self.operation(phase='blocked', reason=reason, retry_after=self.clock() + 900)
                raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    os.umask(0o077)
    worker = None
    try:
        config = read_json(args.config)
        require(sys.platform == 'darwin', 'production-poller-requires-macos')
        worker = Poller(config)
        result = worker.once()
        print(json.dumps(result, sort_keys=True))
        return 0
    except Exception as exc:
        reason = str(exc) if isinstance(exc, Failure) else type(exc).__name__
        result = {'state': 'blocked', 'reason': reason}
        if worker is not None:
            result = worker.status('blocked', reason=reason)
        print(json.dumps(result, sort_keys=True))
        return 1


if __name__ == '__main__':
    sys.exit(main())
