#!/usr/bin/env python3
"""Bounded, offline fake harness. No sockets, SSH, PG, Redis, or real controller.
Run: python -I /absolute/path/test_prepare_canary.py
All nested children (the one CLI refusal test) use CREATE_NO_WINDOW on Windows.
"""
import ast
import base64
import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).resolve().parent.parent / 'prepare_canary.py'
spec = importlib.util.spec_from_file_location('payment_canary', SCRIPT)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
NEW = 'c' * 40
OLD_COMMIT = 'b' * 40
OLD_SHA = hashlib.sha256(b'fake-old-binary').hexdigest()


class FakeProcess:
    def __init__(self):
        self.returncode = None
        self.terminated = False

    def poll(self):
        return self.returncode

    def terminate(self):
        self.terminated = True
        self.returncode = 0

    def wait(self, timeout):
        return self.returncode

    def kill(self):
        self.returncode = -9


class Fixture:
    def __init__(self, base):
        self.base = base
        self.old = base / 'old-frontend'
        self.new = base / 'dist'
        self.sql = base / 'migrations'
        self.old.mkdir()
        self.new.mkdir()
        self.sql.mkdir()
        (self.old / 'assets').mkdir()
        (self.new / 'assets').mkdir()
        self.injection = '<style id="hide-public-version">.version{display:none}</style>'
        self.brand = '<script>window.brand="AG STUDIO";</script>'
        (self.old / 'index.html').write_text('<head><title>AG_&gt;</title><link rel="icon" href="/old.svg">' + self.injection + self.brand + '</head><body></body>')
        (self.old / 'old.svg').write_text('official brand fixture')
        (self.old / 'assets/old-123.js').write_text('old asset')
        (self.old / 'manifest.json').write_text('old stale manifest')
        (self.new / 'index.html').write_text('<head><title>Upstream</title></head><body><script src="/assets/new-456.js"></script></body>')
        (self.new / 'assets/new-456.js').write_text('new asset')
        (self.sql / '001_init.sql').write_bytes(b'\nSELECT 1;\n')
        self.binary = base / 'binary'
        self.binary.write_bytes(b'fake-new-binary')
        self.controller = base / 'tested-controller.py'
        self.controller.write_text('# fake; never execute\n')
        self.old_binary = base / 'old-binary'
        self.old_binary.write_bytes(b'fake-old-binary')
        self.manifest = {'schema': 1, 'binary_sha256': m.digest(self.binary),
                         'frontend_files': m.tree_hashes(self.new), 'migration_files': m.tree_hashes(self.sql),
                         'embedded_migrations': m.migration_hashes(self.sql),
                         'controller_sha256': m.digest(self.controller), 'new_commit': NEW, 'build_identity': {'version':'0.2.19','commit':NEW}}
        self.manifest_path = base / 'package.json'
        self.manifest_path.write_text(json.dumps(self.manifest))
        self.baseline_path = base / 'baseline.json'
        self.baseline = {'source_commit': OLD_COMMIT, 'version':'0.2.19', 'binary_sha256':OLD_SHA,
                         'frontend_pointer':'releases/old', 'frontend_files':m.tree_hashes(self.old),
                         'configuration_hashes':{name:'a'*64 for name in ('.env','run.sh','launchd-run.sh','deployment.json')}}
        self.baseline_path.write_text(json.dumps(self.baseline))
        self.args = m.parser().parse_args(['--root', str(base / 'task'), '--package-json', str(self.manifest_path),
                                          '--binary', str(self.binary), '--frontend', str(self.new), '--migrations', str(self.sql),
                                          '--new-commit', NEW, '--controller', str(self.controller), '--baseline-json', str(self.baseline_path), '--public-base', 'https://public.invalid'])


class FakeCanary(m.Canary):
    """In-memory host/command layer; all adapter orchestration remains real."""
    def __init__(self, fixture, fault=None):
        super().__init__(fixture.args)
        self.f = fixture
        self.fault = fault
        self.commands = []
        self.queries = []
        self.reads = []
        self.next_id = 100
        self.blocked = False
        self.order_count = '4|5'
        self.fake_ledger = [{'filename': '001_init.sql', 'checksum': fixture.manifest['embedded_migrations']['001_init.sql'], 'applied_at': '2026-11-01T00:00:00Z'}]
        self.fake_atlas = [{'version': 'baseline', 'hash': 'same'}]
        self.live = fixture.base / 'fake-live'

    def preflight(self):
        # Explicitly bypass ONLY real host identity/port discovery, not package,
        # migration, frontend, runtime, evidence, or controller sequencing checks.
        self.load_baseline()
        self.new_version = '0.2.19'
        self.package = json.loads(self.f.manifest_path.read_text())
        self.binary, self.frontend = self.f.binary, self.f.new
        self.migrations, self.controller = self.f.sql, self.f.controller
        self.verify_package()
        self.root.mkdir(mode=0o700)
        self.created = True
        self.production.update(DATABASE_HOST='production.invalid', DATABASE_PORT='5432',
                               DATABASE_USER='prod_user', DATABASE_PASSWORD='NEVER-PRINT-ME', DATABASE_DBNAME='prod_db')
        self.new_binary, self.old_binary = self.root / 'new-sub2api', self.root / 'old-sub2api'
        self.controller_copy = self.root / 'controller.py'
        self.new_binary.write_bytes(self.f.binary.read_bytes())
        self.old_binary.write_bytes(self.f.old_binary.read_bytes())
        self.controller_copy.write_bytes(self.f.controller.read_bytes())
        self.baseline = self.check_baseline()
        self.merged = m.merge_frontend(self.f.old, self.f.new, self.root / 'merged-frontend')

    def check_baseline(self):
        if self.fault == 'baseline_drift' and self.stage == 'controller_prepare':
            raise m.Failure('production baseline changed')
        return {'frontend_path': str(self.f.old), 'frontend_files': m.tree_hashes(self.f.old)}

    @staticmethod
    def port_free(port, wait=0):
        pass

    def run(self, args, env=None, timeout=120, input_text=None, allowed=(0,), include_stderr=False):
        args = [str(x) for x in args]
        self.commands.append((args, timeout, dict(env or self.env)))
        assert timeout > 0
        cmd = Path(args[0]).name
        if cmd == 'pg_dump':
            if self.fault == 'dump':
                raise m.Failure('fake dump failure')
            Path(args[-1]).write_bytes(b'fake-protected-pg-dump')
        elif cmd == 'pg_restore' and '--list' in args:
            return 'TABLE DATA public schema_migrations\n'
        elif cmd == 'pg_restore' and self.fault == 'restore':
            raise m.Failure('fake restore failure')
        elif cmd == 'initdb':
            (self.root / 'postgres').mkdir()
        elif cmd == 'redis-cli':
            return 'PONG\n'
        elif cmd == 'sandbox-exec' and '-c' in args:
            rows = [{'connected': True, 'port': m.DBPORT, 'errno': 0},
                    {'connected': True, 'port': m.REDISPORT, 'errno': 0},
                    {'connected': False, 'port': 8080, 'errno': errno.EPERM},
                    {'connected': False, 'port': 443, 'errno': errno.EPERM}]
            if self.fault == 'egress_allowed':
                rows[-1]['connected'] = True
            if self.fault == 'egress_refused_not_denied':
                rows[-1]['errno'] = errno.ECONNREFUSED
            return json.dumps(rows)
        elif cmd == 'sandbox-exec' and '--version' in args:
            commit = NEW if 'new-sub2api' in args[-2] else OLD_COMMIT
            return 'Sub2API 0.2.19 (commit: %s, built: fixture)' % commit
        elif 'prepare' in args:
            assert self.root.joinpath('canary-result.json').exists()
            assert all(p.poll() is not None for p in self.children)
            evidence = json.loads((self.root / 'evidence.json').read_text())
            assert evidence['reviewed'] is True
            assert evidence['database_backup']['verified'] is True
            assert evidence['canary']['sha256'] == m.digest(self.root / 'canary-result.json')
            if self.fault == 'controller_timeout':
                raise m.Failure('fake controller timeout')
            return json.dumps({'state': 'prepared', 'attempt': str(self.live / 'deploy-controller/attempts/fake-attempt')})
        if cmd == 'pg_ctl' and 'stop' in args and self.fault == 'cleanup':
            raise m.Failure('fake cleanup failed')
        return ''

    def spawn(self, args, env, label, cwd=None):
        proc = FakeProcess()
        if label == 'new' and self.fault == 'startup_checksum':
            proc.returncode = 1
        self.children.append(proc)
        self.current = label
        return proc

    def sql(self, query, db='canary', user='canary_admin'):
        self.queries.append(query)
        if query == m.LEDGER_QUERY:
            result = list(self.fake_ledger)
            if self.fault == 'new_sql':
                result = []
            if self.fault == 'checksum':
                result = [dict(self.fake_ledger[0], checksum='0' * 64)]
            if self.fault == 'ledger_drift' and hasattr(self, 'ledger'):
                result = [dict(self.fake_ledger[0], applied_at='changed')]
            return json.dumps(result)
        if query == m.ATLAS_QUERY:
            return json.dumps(self.fake_atlas)
        if 'rolsuper' in query:
            return 'true'
        if 'RETURNING id' in query:
            self.next_id += 1
            return str(self.next_id)
        if query.startswith('INSERT INTO user_subscriptions'):
            self.blocked = True
        if 'UPDATE security_secrets' in query:
            assert "key='jwt_secret'" in query and self.jwt in query and db == 'canary'
            return '0' if self.fault == 'jwt_secret_missing' else '1'
        if 'SELECT value FROM settings' in query:
            return 'PRIVATE-RESTORED-ADMIN-KEY'
        if 'count(*) FROM payment_orders' in query:
            return '6|7' if self.fault == 'order_created' and self.blocked else self.order_count
        return ''

    def http(self, path, admin=False, token=False):
        self.reads.append((self.current, path, admin, token))
        if path == '/health':
            return 200, {'status': 'ok'}
        if path == '/api/v1/admin/system/version':
            return 200, {'data': {'version': '0.2.19'}}
        if path == '/api/v1/admin/accounts' or (path == '/api/v1/payment/config' and not token):
            return (200 if self.fault == 'auth' else 401), {}
        if path.startswith('/api/v1/payment/subscription-checkout?'):
            if self.blocked:
                return 409, {'reason': 'WRONG' if self.fault == 'same_platform' else 'SUBSCRIPTION_PLATFORM_ALREADY_ACTIVE'}
            return 200, {'code': 0, 'data': {'total_cents': 130500, 'next_amount': 1000 if self.fault == 'capped_quote' else 1305}}
        return 200, {'code': 0}


class Harness(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='payment-canary-offline-')
        self.addCleanup(self.temp.cleanup)
        self.f = Fixture(Path(self.temp.name).resolve())

    def worker(self, fault=None):
        return FakeCanary(self.f, fault)

    def test_success_is_prepare_only_after_cleanup(self):
        worker = self.worker()
        result = worker.execute()
        self.assertEqual('prepared_only', result['state'])
        self.assertFalse(result['activation_performed'])
        controller = [args for args, _, _ in worker.commands if 'prepare' in args]
        self.assertEqual(1, len(controller))
        self.assertEqual('prepare', controller[0][3])
        self.assertTrue(all(proc.terminated for proc in worker.children))
        self.assertEqual(['new', 'old'], [label for label, path, *_ in worker.reads if path == '/health'])
        self.assertTrue(any('same-platform' not in q and "'active'" in q and 'user_subscriptions' in q for q in worker.queries))
        serialized = (worker.root / 'evidence.json').read_text() + (worker.root / 'canary-result.json').read_text()
        for secret in ('NEVER-PRINT-ME', 'PRIVATE-RESTORED-ADMIN-KEY', worker.password, worker.jwt):
            self.assertNotIn(secret, serialized)
        self.assertFalse(any('schema_migrations SET' in q or 'INSERT INTO schema_migrations' in q for q in worker.queries))
        for _, _, env in worker.commands:
            if env.get('PGPASSWORD') == 'NEVER-PRINT-ME':
                self.assertEqual(1, sum(x[2].get('PGPASSWORD') == 'NEVER-PRINT-ME' for x in worker.commands))

    def test_each_fault_blocks_controller_and_success_evidence(self):
        for fault in ('dump', 'restore', 'egress_allowed', 'egress_refused_not_denied', 'new_sql', 'checksum',
                      'startup_checksum', 'ledger_drift', 'auth', 'same_platform', 'capped_quote', 'order_created', 'cleanup',
                      'jwt_secret_missing'):
            with self.subTest(fault=fault):
                with tempfile.TemporaryDirectory(prefix='payment-fault-') as temp:
                    f = Fixture(Path(temp).resolve())
                    worker = FakeCanary(f, fault)
                    with self.assertRaises(m.Failure):
                        worker.execute()
                    self.assertFalse(any('prepare' in args for args, _, _ in worker.commands))
                    self.assertFalse((worker.root / 'evidence.json').exists())
                    self.assertFalse((worker.root / 'canary-result.json').exists())
                    self.assertTrue(all(proc.poll() is not None for proc in worker.children))
                    if worker.pg_attempted:
                        self.assertTrue(any('stop' in args and args[0] == 'pg_ctl' for args, _, _ in worker.commands))

    def test_real_baseline_inventory_rejects_each_live_drift(self):
        live = self.f.base / 'baseline-live'
        (live / 'bin').mkdir(parents=True)
        (live / 'frontend/releases').mkdir(parents=True)
        shutil.copytree(self.f.old, live / 'frontend/releases/old')
        (live / 'bin/sub2api').write_bytes(b'fake-old-binary')
        pointer = live / 'frontend/current'
        self.f.baseline['frontend_pointer'] = str(Path('releases') / 'old')
        try:
            pointer.symlink_to(self.f.baseline['frontend_pointer'], target_is_directory=True)
        except OSError:
            if sys.platform == 'win32':
                self.skipTest('directory symlink needs Windows developer mode or privilege; exercised in Linux CI')
            raise
        for name in self.f.baseline['configuration_hashes']:
            (live / name).write_text('protected-' + name)
            self.f.baseline['configuration_hashes'][name] = m.digest(live / name)
        self.f.baseline_path.write_text(json.dumps(self.f.baseline))
        worker = m.Canary(self.f.args)
        worker.live = live
        worker.load_baseline()
        baseline = worker.check_baseline()
        self.assertEqual(self.f.baseline['frontend_files'], baseline['frontend_files'])
        for relative in ('bin/sub2api', '.env', 'frontend/releases/old/index.html'):
            with self.subTest(relative=relative):
                path = live / relative
                original = path.read_bytes()
                path.write_bytes(b'changed')
                with self.assertRaises(m.Failure):
                    worker.check_baseline()
                path.write_bytes(original)
        extra = live / 'frontend/releases/old/extra-untracked.txt'
        extra.write_text('untracked drift')
        with self.assertRaises(m.Failure):
            worker.check_baseline()
        extra.unlink()
        (live / 'deploy-controller').mkdir()
        (live / 'deploy-controller/active.json').write_text('{}')
        with self.assertRaisesRegex(m.Failure, 'active deploy lock'):
            worker.check_baseline()

    def test_poller_cli_contract_accepts_explicit_and_default_application_root(self):
        arguments = ['--root', str(self.f.base / 'contract-task'), '--package-json', str(self.f.manifest_path),
                     '--binary', str(self.f.binary), '--frontend', str(self.f.new), '--migrations', str(self.f.sql),
                     '--new-commit', NEW, '--controller', str(self.f.controller),
                     '--baseline-json', str(self.f.baseline_path), '--public-base', 'https://public.invalid']
        explicit = self.f.base / 'explicit-application'
        parsed = m.parser().parse_args(arguments + ['--app-root', str(explicit), '--env-file', str(self.f.base / '.env'), '--deadline-seconds', '900'])
        self.assertEqual(explicit, m.Canary(parsed).live)
        default = m.parser().parse_args(arguments)
        self.assertEqual(Path.home() / '.local/share/sub2api', m.Canary(default).live)

    def test_generic_baseline_validation_rejects_missing_identity_hashes_and_extra_paths(self):
        self.assertEqual(OLD_COMMIT, m.read_baseline(self.f.baseline_path)['source_commit'])
        mutations = (
            {'source_commit': 'short'}, {'version': ''}, {'binary_sha256': 'bad'},
            {'frontend_pointer': ''}, {'frontend_files': {'../index.html':'a'*64}},
            {'frontend_files': {'index.html': 'bad'}}, {'commit':'d'*40},
            {'configuration_hashes': {'.env':'a'*64}},
            {'configuration_hashes': dict(self.f.baseline['configuration_hashes'], **{'not-protected':'a'*64})})
        for mutation in mutations:
            with self.subTest(mutation=mutation), self.assertRaises(m.Failure):
                self.f.baseline_path.write_text(json.dumps(dict(self.f.baseline, **mutation)))
                m.read_baseline(self.f.baseline_path)

    def test_safe_environment_loader_only_reads_connection_keys_and_never_expands(self):
        envfile = self.f.base / '.env'
        envfile.write_text("export DATABASE_HOST=127.0.0.1 # local\nDATABASE_PORT=5432\nDATABASE_USER='user name'\nDATABASE_PASSWORD='literal$(never-run)$HOME`ignored`'\nDATABASE_DBNAME=app\nDATABASE_SSLMODE=require\nJWT_SECRET=never-export\nOTHER=$(also-never-run)\n")
        values = m.load_env_file(envfile)
        self.assertEqual(m.DB_ENV_KEYS, set(values))
        self.assertEqual('user name', values['DATABASE_USER'])
        self.assertEqual('literal$(never-run)$HOME`ignored`', values['DATABASE_PASSWORD'])
        self.assertNotIn('JWT_SECRET', values)
        envfile.write_text("DATABASE_PASSWORD=literal#hash # comment\nDATABASE_USER='quoted # user' # trailing\n")
        self.assertEqual({'DATABASE_PASSWORD':'literal#hash','DATABASE_USER':'quoted # user'}, m.load_env_file(envfile))
        for raw in ("DATABASE_PASSWORD='broken", 'DATABASE_USER=one two', 'DATABASE_PORT=5432\nDATABASE_PORT=6432'):
            with self.subTest(raw=raw), self.assertRaises(m.Failure):
                envfile.write_text(raw)
                m.load_env_file(envfile)

    def test_cached_fonts_preserved_root_resources_not_inherited(self):
        (self.f.old / 'fonts').mkdir()
        (self.f.old / 'fonts' / 'old.woff2').write_bytes(b'cached-font')
        destination = self.f.base / 'merged-fonts'
        m.merge_frontend(self.f.old, self.f.new, destination)
        self.assertEqual(b'cached-font', (destination / 'fonts/old.woff2').read_bytes())
        self.assertFalse((destination / 'old.svg').exists())

    def test_baseline_drift_blocks_prepare(self):
        worker = self.worker('baseline_drift')
        with self.assertRaises(m.Failure):
            worker.execute()
        self.assertFalse(any('prepare' in args for args, _, _ in worker.commands))
        self.assertFalse((worker.root / 'evidence.json').exists())

    def test_controller_timeout_keeps_uncertain_receipt_no_retry(self):
        worker = self.worker('controller_timeout')
        with self.assertRaises(m.Failure):
            worker.execute()
        receipt = json.loads((worker.root / 'prepare-receipt.json').read_text())
        self.assertEqual('unknown_or_failed', receipt['state'])
        self.assertTrue(receipt['do_not_retry_or_clear_lock'])
        self.assertEqual(1, sum('prepare' in args for args, _, _ in worker.commands))

    def test_only_250_known_checksum_pair_is_accepted(self):
        a, b = sorted(m.HISTORICAL_250)
        m.check_migrations([{'filename': '250_openclaw_provider_descriptions_live.sql', 'checksum': a}], {'250_openclaw_provider_descriptions_live.sql': b})
        for name in ('246_example.sql', '252_example.sql', '250_other.sql'):
            with self.subTest(name=name), self.assertRaises(m.Failure):
                m.check_migrations([{'filename': name, 'checksum': a}], {name: b})
        with self.assertRaises(m.Failure):
            m.check_migrations([{'filename': '250_openclaw_provider_descriptions_live.sql', 'checksum': a}], {'250_openclaw_provider_descriptions_live.sql': '0' * 64})

    def test_sql_hash_preserves_internal_crlf_like_go_runner(self):
        (self.f.sql / '001_init.sql').write_bytes(b' \r\nSELECT 1;\r\nSELECT 2;\r\n')
        expected = hashlib.sha256(b'SELECT 1;\r\nSELECT 2;').hexdigest()
        self.assertEqual(expected, m.migration_hashes(self.f.sql)['001_init.sql'])

    def test_package_tamper_is_rejected_before_root(self):
        self.f.binary.write_bytes(b'tampered')
        worker = self.worker()
        with self.assertRaises(m.Failure):
            worker.execute()
        self.assertFalse(worker.root.exists())
        self.assertEqual([], worker.commands)

    def test_frontend_uses_source_index_and_preserves_cached_assets(self):
        destination = self.f.base / 'merged'
        m.merge_frontend(self.f.old, self.f.new, destination)
        index = (destination / 'index.html').read_text()
        self.assertNotIn(self.f.injection, index)
        self.assertNotIn(self.f.brand, index)
        self.assertIn('<title>Upstream</title>', index)
        self.assertNotIn('href="/old.svg"', index)
        self.assertEqual((self.f.new / 'index.html').read_bytes(), (destination / 'index.html').read_bytes())
        self.assertTrue((destination / 'assets/old-123.js').is_file())
        self.assertTrue((destination / 'assets/new-456.js').is_file())
        self.assertFalse((destination / 'manifest.json').exists())

    def test_frontend_without_historical_hiding_injection_preserves_actual_baseline(self):
        path = self.f.old / 'index.html'
        path.write_text('<head><title>AG_&gt;</title><link rel="icon" href="/old.svg"></head><body></body>')
        destination = self.f.base / 'merged-without-injections'
        m.merge_frontend(self.f.old, self.f.new, destination)
        index = (destination / 'index.html').read_text()
        self.assertIn('<title>Upstream</title>', index)
        self.assertIn('/assets/new-456.js', index)
        self.assertNotIn('hide-public-version', index)
        self.assertTrue((destination / 'assets/old-123.js').is_file())

    def test_port_check_rejects_live_listener_and_accepts_closed_port(self):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            listener.listen(1)
            port = listener.getsockname()[1]
            with self.assertRaises(m.Failure):
                m.Canary.port_free(port, wait=0)
        m.Canary.port_free(port, wait=2)

    def test_frontend_colliding_cached_asset_fails(self):
        (self.f.new / 'assets/old-123.js').write_text('mutated same URL')
        with self.assertRaises(m.Failure):
            m.merge_frontend(self.f.old, self.f.new, self.f.base / 'merged')

    def test_environment_never_inherits_production_jwt_or_database(self):
        with mock.patch.dict(os.environ, {'DATABASE_PASSWORD': 'prod-pass', 'JWT_SECRET': 'prod-jwt', 'HTTP_PROXY': 'prod-proxy'}):
            worker = self.worker()
            worker.root.mkdir()
            env = worker.app_env(worker.root / 'data')
            self.assertEqual('127.0.0.1', env['DATABASE_HOST'])
            self.assertEqual(str(m.DBPORT), env['DATABASE_PORT'])
            self.assertEqual(worker.jwt, env['JWT_SECRET'])
            self.assertEqual(worker.password, env['DATABASE_PASSWORD'])
            self.assertNotIn('HTTP_PROXY', env)
            self.assertEqual('false', env['AUTO_SETUP'])

    def test_jwt_matches_resolved_token_fingerprint(self):
        token = m.fixture_jwt(12, 'test@example.invalid', 'hash', 'new-canary-secret')
        claims = json.loads(base64.urlsafe_b64decode(token.split('.')[1] + '=='))
        fingerprint = int.from_bytes(hashlib.sha256(b'test@example.invalid\nhash').digest()[:8], 'big') & 0x7fffffffffffffff
        self.assertEqual(fingerprint, claims['token_version'])
        self.assertEqual('user', claims['role'])

    def test_http_rejects_mutating_and_gateway_routes_without_network(self):
        worker = self.worker()
        for route in ('/api/v1/payment/orders', '/api/v1/payment/sessions/1/reconcile',
                      '/api/v1/payment/public/orders/verify', '/api/v1/admin/payment/config'):
            with self.assertRaises(m.Failure):
                m.Canary.http(worker, route)

    def test_sql_transport_is_fixed_to_disposable_and_stdin(self):
        worker = m.Canary(self.f.args)
        with mock.patch.object(worker, 'run', return_value='result') as run:
            self.assertEqual('result', worker.sql('SELECT 1;'))
        args, kwargs = run.call_args
        self.assertIn(str(m.DBPORT), args[0])
        self.assertIn('127.0.0.1', args[0])
        self.assertNotIn('SELECT 1;', args[0])
        self.assertEqual('SELECT 1;', kwargs['input_text'])
        self.assertEqual(worker.admin_password, kwargs['env']['PGPASSWORD'])

    def test_process_launches_are_bounded_windowless_and_redacted(self):
        worker = m.Canary(self.f.args)
        worker.root.mkdir()
        with mock.patch.object(m.subprocess, 'run', side_effect=subprocess.TimeoutExpired('secret-command', 1)):
            with self.assertRaises(m.Failure) as exc:
                worker.run(['something', 'a-secret'], timeout=1)
        self.assertNotIn('secret', str(exc.exception))
        with mock.patch.object(m.subprocess, 'Popen', return_value=FakeProcess()) as popen:
            worker.spawn(['something'], worker.env, 'owned')
            if os.name == 'nt':
                self.assertEqual(subprocess.CREATE_NO_WINDOW, popen.call_args.kwargs['creationflags'])
        for log in worker.logs:
            log.close()

    def test_source_python39_and_no_activation_commands(self):
        source = SCRIPT.read_text(encoding='utf-8')
        ast.parse(source, feature_version=(3, 9))
        calls = [node.func.attr for node in ast.walk(ast.parse(source)) if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)]
        self.assertNotIn('is_relative_to', calls)
        self.assertNotIn('removeprefix', calls)
        tree = ast.parse(source)
        for node in ast.walk(tree):
            if isinstance(node, ast.List) and node.elts:
                strings = [item.value for item in node.elts if isinstance(item, ast.Constant) and isinstance(item.value, str)]
                self.assertFalse({'ssh', 'scp', 'launchctl', 'activate', 'arm', 'killall', 'pkill'} & set(strings))

    @unittest.skipIf(sys.platform == 'darwin', 'actual macOS CLI must never be run by offline harness')
    def test_cli_refuses_non_mac_without_creating_root(self):
        command = [sys.executable, '-I', str(SCRIPT), '--root', str(self.f.base / 'refuse'),
                   '--package-json', str(self.f.manifest_path), '--binary', str(self.f.binary),
                   '--frontend', str(self.f.new), '--migrations', str(self.f.sql), '--new-commit', NEW,
                   '--controller', str(self.f.controller), '--baseline-json', str(self.f.baseline_path), '--public-base', 'https://public.invalid']
        result = subprocess.run(command, stdin=subprocess.DEVNULL, capture_output=True, text=True,
                                timeout=8, **m.windowless())
        self.assertEqual(1, result.returncode)
        self.assertIn('requires macOS', result.stderr)
        self.assertNotIn('Traceback', result.stderr)
        self.assertFalse((self.f.base / 'refuse').exists())
        if os.name == 'nt':
            import ctypes
            self.assertEqual(0, ctypes.windll.kernel32.GetConsoleWindow())


if __name__ == '__main__':
    unittest.main(verbosity=2)
