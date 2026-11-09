import hashlib
import importlib.util
import io
import json
import subprocess
import sys
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('tested_production_poller', ROOT / 'poller.py')
poller = importlib.util.module_from_spec(spec)
spec.loader.exec_module(poller)
OLD, NEW = '1' * 40, '2' * 40
REPO = 'owner/repository'


# A fresh -I process runs copied poller source as a script, so its own code is
# not imported/cached. Poller() still uses its real dynamic github_api loader.
RESTART_PROOF = r"""
import json
from pathlib import Path
import sys
import types
from unittest import mock

script, app = map(Path, sys.argv[1:3])
mode = sys.argv[3]
runtime = types.ModuleType('restarted_poller')
runtime.__file__ = str(script)
exec(compile(script.read_bytes(), str(script), 'exec'), runtime.__dict__)
tools = script.parent
manifest = runtime.read_json(tools.parent / 'manifest.json')
worker = runtime.Poller({'schema': 1, 'repository': 'owner/repository',
                         'app_root': str(app), 'enabled': True})
outcome = 'imported'
if mode == 'resume':
    installed = runtime.read_json(worker.installed_path)

    def link(target, name, **kwargs):
        Path(name).write_text(str(target))

    with mock.patch.object(runtime, 'baseline', return_value=installed), \
            mock.patch.object(worker, 'controller_status', return_value={
                'attempt': installed['attempt'], 'state': {'state': 'committed'}, 'host_lock_held': False}), \
            mock.patch.object(runtime.os, 'symlink', side_effect=link), \
            mock.patch.object(worker.api, 'request', side_effect=AssertionError('network forbidden')):
        outcome = worker.once()['state']
print(json.dumps({'isolated': sys.flags.isolated, 'dont_write_bytecode': sys.dont_write_bytecode,
    'tool_files_unchanged': runtime.inventory(tools) == manifest['tools_files'],
    'bytecode_paths': [path.relative_to(tools).as_posix() for path in tools.rglob('*.pyc')],
    'result_state': outcome}, sort_keys=True))
"""


def sha(value):
    return hashlib.sha256(value).hexdigest()


def package_fixture(root, extra=None, mutate=None, runtime_tools=False):
    files = {'bin/sub2api': b'binary', 'frontend/index.html': b'<html>new</html>', 'migrations/001.sql': b'SELECT 1;'}
    if runtime_tools:
        for path in ROOT.glob('*.py'):
            files['tools/' + path.name] = path.read_bytes()
    else:
        for name in ('poller.py', 'github_api.py', 'controller.py', 'prepare_canary.py', 'activate_prepared.py', 'install.py'):
            files['tools/' + name] = ('# ' + name).encode()
    manifest = {'schema': 1, 'repository': REPO, 'platform': 'darwin/arm64', 'new_commit': NEW,
                'source_tree_sha256': '3' * 64, 'binary_sha256': sha(files['bin/sub2api']),
                'controller_sha256': sha(files['tools/controller.py']),
                'build_identity': {'version': '0.2.19', 'commit': NEW, 'goos': 'darwin', 'goarch': 'arm64', 'source_tree_sha256': '3' * 64}}
    for field, prefix in (('frontend_files', 'frontend/'), ('migration_files', 'migrations/'), ('tools_files', 'tools/')):
        manifest[field] = {name[len(prefix):]: sha(content) for name, content in files.items() if name.startswith(prefix)}
    manifest['embedded_migrations'] = dict(manifest['migration_files'])
    if mutate:
        mutate(manifest)
    files['manifest.json'] = json.dumps(manifest).encode()
    if extra:
        files.update(extra)
    archive = root / 'package.tar.gz'
    with tarfile.open(archive, 'w:gz') as stream:
        for name, content in files.items():
            info = tarfile.TarInfo(name)
            info.size = len(content)
            stream.addfile(info, io.BytesIO(content))
    deployment = {'schema': 1, 'repository': REPO, 'new_commit': NEW, 'platform': 'darwin/arm64',
                  'archive_name': 'sub2api-production-' + NEW + '-darwin-arm64.tar.gz',
                  'archive_sha256': poller.digest(archive), 'manifest_sha256': sha(files['manifest.json']),
                  'archive_size': archive.stat().st_size,
                  'source_tree_sha256': manifest['source_tree_sha256'], 'version': '0.2.19'}
    return archive, deployment


class ArchiveTests(unittest.TestCase):
    def test_valid_package_is_fully_verified(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root)
            manifest = poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)
            self.assertEqual(poller.digest(root / 'payload/bin/sub2api'), manifest['binary_sha256'])

    def test_unsafe_names_are_rejected_before_extraction(self):
        for name in ('../escape', '/absolute', r'frontend\escape', 'tools/../escape', 'bin/sub2api:stream'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive, deployment = package_fixture(root, {name: b'bad'})
                with self.assertRaisesRegex(poller.Failure, 'unsafe-archive-member'):
                    poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)
                self.assertFalse((root / 'payload').exists())

    def test_case_collision_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root, {'FRONTEND/index.html': b'collision'})
            with self.assertRaisesRegex(poller.Failure, 'duplicate-archive-member'):
                poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)

    def test_symlink_and_hardlink_rejected(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive, deployment = package_fixture(root)
                with tarfile.open(root / 'link.tar', 'w:gz') as stream:
                    member = tarfile.TarInfo('manifest.json')
                    member.type, member.linkname = kind, '/outside'
                    stream.addfile(member)
                deployment['archive_sha256'] = poller.digest(root / 'link.tar')
                deployment['archive_size'] = (root / 'link.tar').stat().st_size
                with self.assertRaisesRegex(poller.Failure, 'unsafe-archive-member'):
                    poller.extract_verified(root / 'link.tar', root / 'payload', deployment, REPO, NEW)

    def test_extra_unmanifested_file_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root, {'tools/unreviewed.py': b'bad'})
            with self.assertRaisesRegex(poller.Failure, 'package-inventory-mismatch'):
                poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)

    def test_archive_hash_and_platform_rejected(self):
        for field, value, error in (('archive_sha256', '0' * 64, 'archive-hash-mismatch'),
                                    ('platform', 'linux/amd64', 'wrong-package-platform')):
            with self.subTest(field=field), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive, deployment = package_fixture(root)
                deployment[field] = value
                with self.assertRaisesRegex(poller.Failure, error):
                    poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)

    def test_oversized_members_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root)
            with mock.patch.object(poller, 'MAX_EXTRACTED', 1), self.assertRaisesRegex(poller.Failure, 'archive-size-limit'):
                poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)

    def test_size_and_source_identity_are_required(self):
        for mutation, error in ((lambda m: m.update(source_tree_sha256=None), 'wrong-source-tree'),
                                (lambda m: m['build_identity'].update(source_tree_sha256='4' * 64), 'wrong-source-tree'),
                                (lambda m: m['tools_files'].pop('github_api.py'), 'incomplete-package'),
                                (lambda m: m['tools_files'].pop('install.py'), 'incomplete-package')):
            with self.subTest(error=error), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                archive, deployment = package_fixture(root, mutate=mutation)
                with self.assertRaisesRegex(poller.Failure, error):
                    poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root)
            deployment['archive_size'] += 1
            with self.assertRaisesRegex(poller.Failure, 'archive-size-mismatch'):
                poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)

    def test_wrong_controller_hash_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root, mutate=lambda m: m.update(controller_sha256='0' * 64))
            with self.assertRaisesRegex(poller.Failure, 'controller-hash-conflict'):
                poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)

    def test_payload_hash_failure_never_publishes_partial_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root, {'frontend/index.html': b'changed after manifest'})
            with self.assertRaisesRegex(poller.Failure, 'package-file-hash-mismatch'):
                poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)
            self.assertFalse((root / 'payload').exists())
            self.assertEqual(list(root.glob('*.extract')), [])

    def test_interrupted_publication_can_extract_again_without_manual_cleanup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive, deployment = package_fixture(root)
            with mock.patch.object(poller.os, 'rename', side_effect=OSError('simulated interruption')):
                with self.assertRaises(OSError):
                    poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)
            self.assertFalse((root / 'payload').exists())
            manifest = poller.extract_verified(archive, root / 'payload', deployment, REPO, NEW)
            self.assertEqual(poller.digest(root / 'payload/bin/sub2api'), manifest['binary_sha256'])


class FakeAPI:
    def __init__(self, main=NEW):
        self.main = main
        self.calls = []
        self.green = True
        self.descendant = True

    def main_sha(self):
        self.calls.append('main')
        return self.main

    def gate(self, sha):
        self.calls.append(('gate', sha))
        return {'ready': self.green}

    def is_descendant(self, base, sha):
        return self.descendant


class PollerStateTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.app = Path(self.temporary.name) / 'app'
        self.app.mkdir()
        self.state = self.app / 'auto-deploy'
        self.state.mkdir()
        self.installed = {'version': '0.2.19', 'source_commit': OLD, 'binary_sha256': 'a' * 64,
                          'frontend_pointer': 'releases/old', 'frontend_files': {'index.html': 'b' * 64},
                          'configuration_hashes': {'.env': 'c' * 64}}
        poller.save(self.state / 'installed.json', self.installed)
        self.api = FakeAPI()
        self.worker = poller.Poller({'schema': 1, 'repository': REPO, 'app_root': str(self.app),
                                     'public_base': 'https://example.test', 'enabled': True}, self.api, clock=lambda: 1000)

    def tearDown(self):
        self.temporary.cleanup()

    def test_unchanged_main_uses_only_one_request(self):
        self.api.main = OLD
        result = self.worker.once()
        self.assertEqual(result['state'], 'up-to-date')
        self.assertEqual(self.api.calls, ['main'])

    def test_controller_status_uses_versioned_controller_name(self):
        root = self.app / 'canary'
        root.mkdir()
        poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': '/owned/attempt'})
        with mock.patch.object(poller, 'cli', return_value={'state': {'state': 'prepared'}, 'host_lock_held': True}) as command:
            self.worker.controller_status({'canary_root': str(root)})
        command.assert_called_once_with(root / 'controller.py', ['status', '--attempt', '/owned/attempt'])

    def test_failed_checks_never_stage_and_back_off(self):
        self.api.green = False
        with mock.patch.object(self.worker, 'stage') as stage:
            with self.assertRaisesRegex(poller.Failure, 'checks-not-green'):
                self.worker.once()
            stage.assert_not_called()
        self.api.calls.clear()
        self.assertEqual(self.worker.once()['state'], 'candidate-blocked')
        self.assertEqual(self.api.calls, ['main'])

    def test_rewind_never_reaches_staging(self):
        self.api.descendant = False
        with mock.patch.object(self.worker, 'stage') as stage:
            with self.assertRaisesRegex(poller.Failure, 'candidate-not-descendant'):
                self.worker.once()
            stage.assert_not_called()

    def test_pause_prevents_new_network_requests(self):
        (self.state / 'PAUSED').touch()
        self.assertEqual(self.worker.once()['state'], 'paused')
        self.assertEqual(self.api.calls, [])

    def test_pause_does_not_interrupt_existing_recovery(self):
        (self.state / 'PAUSED').touch()
        self.worker.operation(phase='activation_started', sha=NEW)
        with mock.patch.object(self.worker, 'finish', return_value={'state': 'committed'}) as finish:
            self.assertEqual(self.worker.once()['state'], 'committed')
            finish.assert_called_once()

    def test_uncertain_activation_only_reads_status(self):
        self.worker.operation(phase='activation_started', sha=NEW)
        with mock.patch.object(self.worker, 'finish', return_value={'state': 'activation-pending'}) as finish, \
                mock.patch.object(poller, 'cli', side_effect=AssertionError('must never replay')):
            self.assertEqual(self.worker.once()['state'], 'activation-pending')
            finish.assert_called_once()

    def test_prepared_superseded_candidate_closes_untouched(self):
        root = self.app / 'canary'
        root.mkdir()
        poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': '/owned/attempt'})
        self.worker.operation(phase='prepared', sha=NEW, canary_root=str(root))
        self.api.main = '3' * 40
        with mock.patch.object(poller, 'cli', return_value={}) as command:
            with self.assertRaisesRegex(poller.Failure, 'superseded-candidate'):
                self.worker.once()
            self.assertEqual(command.call_args.args[1], ['close-untouched', '--attempt', '/owned/attempt'])
            self.assertEqual(command.call_args.args[0], root / 'controller.py')
        self.assertEqual(poller.read_json(self.state / 'operation.json')['phase'], 'aborted_untouched')

    def test_failed_canary_does_not_retry_same_sha(self):
        root = self.app / 'canary'
        root.mkdir()
        poller.save(root / 'failure.json', {'status': 'failed'})
        self.worker.operation(phase='canary_started', sha=NEW, canary_root=str(root))
        self.assertEqual(self.worker.once()['state'], 'canary-failed')
        self.assertEqual(self.worker.once()['state'], 'candidate-blocked')

    def _fail_canary_on_every_attempt(self, stage, home, logs):
        """Run the real cli() with a canary that fails; record each log path it opened."""
        def run(argv, **kwargs):
            args = argv[3:]
            root = Path(args[args.index('--root') + 1])
            root.mkdir()
            poller.save(root / 'failure.json', {'status': 'failed'})
            logs.append(Path(kwargs['stdout'].name))
            return subprocess.CompletedProcess(argv, 1)
        return mock.patch.multiple(poller.subprocess, run=mock.DEFAULT), run

    def test_failed_canary_in_the_same_poll_keeps_the_daily_backoff(self):
        stage = self.state / 'packages' / NEW
        stage.mkdir(parents=True)
        home = Path(self.temporary.name) / 'owned-home'
        home.mkdir()
        logs = []
        patch, run = self._fail_canary_on_every_attempt(stage, home, logs)
        with mock.patch.object(poller.Path, 'home', return_value=home), \
                mock.patch.object(self.worker, 'stage', return_value=(stage, stage / 'payload', {}, {'archive_sha256': 'd' * 64})), \
                mock.patch.object(poller, 'verify_baseline', return_value=self.installed), \
                patch as mocked:
            mocked['run'].side_effect = run
            with self.assertRaisesRegex(poller.Failure, 'protected-command-failed'):
                self.worker.once()
        operation = poller.read_json(self.worker.operation_path)
        self.assertEqual(operation['phase'], 'canary_failed')
        self.assertEqual(operation['retry_after'], 1000 + 86400)
        self.assertEqual(operation['detail'], 'protected-command-failed')

    def test_retry_after_failed_canary_writes_fresh_logs(self):
        stage = self.state / 'packages' / NEW
        stage.mkdir(parents=True)
        home = Path(self.temporary.name) / 'owned-home'
        home.mkdir()
        now = [1000]
        self.worker.clock = lambda: now[0]
        logs = []
        patch, run = self._fail_canary_on_every_attempt(stage, home, logs)
        with mock.patch.object(poller.Path, 'home', return_value=home), \
                mock.patch.object(self.worker, 'stage', return_value=(stage, stage / 'payload', {}, {'archive_sha256': 'd' * 64})), \
                mock.patch.object(poller, 'verify_baseline', return_value=self.installed), \
                patch as mocked:
            mocked['run'].side_effect = run
            with self.assertRaisesRegex(poller.Failure, 'protected-command-failed'):
                self.worker.once()
            now[0] += 86400 + 1
            # A retry of the same candidate must run its canary again, not trip over the
            # previous attempt's log file.
            with self.assertRaisesRegex(poller.Failure, 'protected-command-failed'):
                self.worker.once()
        self.assertEqual(len(logs), 2)
        self.assertNotEqual(logs[0], logs[1])
        self.assertTrue(all(log.parent == stage and log.is_file() for log in logs))

    def test_pause_closes_prepared_attempt_without_activation(self):
        root = self.app / 'canary'
        root.mkdir()
        poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': '/owned/attempt'})
        self.worker.operation(phase='prepared', sha=NEW, canary_root=str(root))
        (self.state / 'PAUSED').touch()
        with mock.patch.object(poller, 'cli', return_value={}) as command:
            self.assertEqual(self.worker.once()['state'], 'paused')
            self.assertEqual(command.call_args.args[1][0], 'close-untouched')
            self.assertEqual(command.call_args.args[0], root / 'controller.py')
        self.assertEqual(self.api.calls, [])

    def test_crash_before_arm_cannot_hold_prepared_lock_forever(self):
        root = self.app / 'canary'
        root.mkdir()
        poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': '/owned/attempt'})
        self.worker.operation(phase='activation_started', activation_started_at=0, sha=NEW, canary_root=str(root))
        with mock.patch.object(self.worker, 'controller_status', return_value={'state': {'state': 'prepared'}, 'host_lock_held': True}), \
                mock.patch.object(poller, 'cli', return_value={}) as command:
            self.assertEqual(self.worker.once()['state'], 'aborted_untouched')
            self.assertEqual(command.call_args.args[1][0], 'close-untouched')

    def test_lost_supervisor_restores_only_watchdog_and_reconciles_terminal(self):
        root = self.app / 'canary'
        root.mkdir()
        poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': '/owned/attempt'})
        self.worker.operation(phase='activation_started', activation_started_at=900, sha=NEW, canary_root=str(root))
        with mock.patch.object(self.worker, 'controller_status', side_effect=[
                {'state': {'state': 'activating'}, 'host_lock_held': True},
                {'state': {'state': 'rolled_back'}, 'host_lock_held': False}]), \
                mock.patch.object(poller, 'cli', return_value={}) as command:
            self.assertEqual(self.worker.once()['state'], 'rolled_back')
            command.assert_called_once_with(root / 'controller.py', ['ensure-watchdog', '--attempt', '/owned/attempt'], timeout=90)
        self.assertEqual(self.api.calls, [])

    def terminal_operation(self):
        root = self.app / 'canary'
        root.mkdir(exist_ok=True)
        poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': '/owned/attempt'})
        operation = self.worker.operation(phase='activation_pending', sha=NEW, canary_root=str(root))
        return root, operation

    def test_successful_terminal_missing_lock_field_cannot_complete(self):
        root, operation = self.terminal_operation()
        installed = self.worker.installed_path.read_bytes()
        for terminal in ('committed', 'rolled_back'):
            with self.subTest(terminal=terminal):
                status = {'state': {'state': terminal}}
                with mock.patch.object(self.worker, 'controller_status', return_value=status) as read, \
                        mock.patch.object(poller, 'cli', return_value={}) as command, \
                        mock.patch.object(poller, 'baseline') as baseline, \
                        mock.patch.object(poller.os, 'symlink') as switch:
                    self.assertEqual(self.worker.finish(operation)['state'], 'activation-pending')
                    self.assertEqual(read.call_count, 2)
                    command.assert_called_once_with(root / 'controller.py',
                        ['ensure-watchdog', '--attempt', '/owned/attempt'], timeout=90)
                    baseline.assert_not_called()
                    switch.assert_not_called()
                self.assertEqual(self.worker.installed_path.read_bytes(), installed)
                self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'activation_pending')

    def test_terminal_lock_not_strictly_false_or_settlement_error_stays_pending(self):
        root, operation = self.terminal_operation()
        installed = self.worker.installed_path.read_bytes()
        for terminal in ('committed', 'rolled_back'):
            for held in (True, None, 0, 'false'):
                for failed in (False, True):
                    with self.subTest(terminal=terminal, held=held, failed=failed):
                        status = {'state': {'state': terminal}, 'host_lock_held': held}
                        error = poller.Failure('matching durable decision missing') if failed else None
                        with mock.patch.object(self.worker, 'controller_status', return_value=status), \
                                mock.patch.object(poller, 'cli', side_effect=error, return_value={}) as command, \
                                mock.patch.object(poller, 'baseline') as baseline, \
                                mock.patch.object(poller.os, 'symlink') as switch:
                            self.assertEqual(self.worker.finish(operation)['state'], 'activation-pending')
                            command.assert_called_once()
                            baseline.assert_not_called()
                            switch.assert_not_called()
                        self.assertEqual(self.worker.installed_path.read_bytes(), installed)
                        self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'activation_pending')

    def test_rolled_back_settles_owned_lock_before_terminal_receipt(self):
        root, operation = self.terminal_operation()
        installed = self.worker.installed_path.read_bytes()
        with mock.patch.object(self.worker, 'controller_status', side_effect=[
                {'state': {'state': 'rolled_back'}, 'host_lock_held': True},
                {'state': {'state': 'rolled_back'}, 'host_lock_held': False}]), \
                mock.patch.object(poller, 'cli', return_value={}) as command, \
                mock.patch.object(poller, 'baseline') as baseline, \
                mock.patch.object(poller.os, 'symlink') as switch:
            self.assertEqual(self.worker.finish(operation)['state'], 'rolled_back')
            command.assert_called_once_with(root / 'controller.py',
                ['ensure-watchdog', '--attempt', '/owned/attempt'], timeout=90)
            baseline.assert_not_called()
            switch.assert_not_called()
        self.assertEqual(self.worker.installed_path.read_bytes(), installed)
        self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'rolled_back')

    def test_other_failed_terminals_need_cleanup_proof_and_failed_rollback_keeps_lock(self):
        _, operation = self.terminal_operation()
        installed = self.worker.installed_path.read_bytes()
        for terminal in ('prepare_failed', 'aborted_untouched'):
            with self.subTest(terminal=terminal):
                with mock.patch.object(self.worker, 'controller_status', return_value={'state': {'state': terminal}}), \
                        mock.patch.object(poller, 'cli') as command:
                    self.assertEqual(self.worker.finish(operation)['state'], 'activation-pending')
                    command.assert_not_called()
                self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'activation_pending')
        with mock.patch.object(self.worker, 'controller_status', return_value={
                'state': {'state': 'rollback_failed'}, 'host_lock_held': True}), \
                mock.patch.object(poller, 'cli') as command:
            self.assertEqual(self.worker.finish(operation)['state'], 'rollback_failed')
            command.assert_not_called()
        self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'rollback_failed')
        self.assertEqual(self.worker.installed_path.read_bytes(), installed)

    def test_drift_in_each_protected_baseline_field_is_rejected(self):
        for field in ('binary_sha256', 'frontend_pointer', 'frontend_files', 'configuration_hashes'):
            with self.subTest(field=field):
                current = dict(self.installed)
                current[field] = None
                with mock.patch.object(poller, 'baseline', return_value=current):
                    with self.assertRaisesRegex(poller.Failure, 'installed-baseline-drift:' + field):
                        poller.verify_baseline(self.app, self.installed)

    def runtime_package(self):
        stage = self.state / 'packages' / NEW
        stage.mkdir(parents=True)
        archive, deployment = package_fixture(stage, runtime_tools=True)
        package = stage / 'payload'
        manifest = poller.extract_verified(archive, package, deployment, REPO, NEW)
        return package, manifest, deployment

    def restart_from_copied_tools(self, package, mode):
        # Deliberately omit -B and ignore Python environment variables with -I:
        # the copied runtime itself must keep the verified package immutable.
        options = {'creationflags': subprocess.CREATE_NO_WINDOW} if sys.platform == 'win32' else {}
        result = subprocess.run([sys.executable, '-I', '-c', RESTART_PROOF,
                                 str(package / 'tools/poller.py'), str(self.app), mode],
                                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=20, **options)
        self.assertEqual(result.returncode, 0, result.stderr)
        proof = json.loads(result.stdout)
        self.assertEqual(proof['isolated'], 1)
        self.assertIs(proof['dont_write_bytecode'], True)
        self.assertIs(proof['tool_files_unchanged'], True)
        self.assertEqual(proof['bytecode_paths'], [])
        self.assertFalse((package / 'tools/__pycache__').exists())
        return proof

    def test_isolated_runtime_import_preserves_verified_tool_inventory(self):
        package, _, _ = self.runtime_package()
        self.assertEqual(self.restart_from_copied_tools(package, 'import')['result_state'], 'imported')

    def test_restart_after_tool_switch_before_operation_commit_reconciles(self):
        package, manifest, deployment = self.runtime_package()
        root = self.app / 'canary'
        root.mkdir()
        attempt = self.app / 'deploy-controller/attempts/candidate'
        attempt.mkdir(parents=True)
        poller.save(root / 'baseline.json', self.installed)
        poller.save(attempt / 'config.json', {'new_frontend_hashes': manifest['frontend_files']})
        installed_new = dict(self.installed, source_commit=NEW, binary_sha256=manifest['binary_sha256'],
                             frontend_pointer='releases/new', frontend_files=manifest['frontend_files'])
        operation = self.worker.operation(phase='activation_pending', sha=NEW, canary_root=str(root),
                                           package_root=str(package), archive_sha256=deployment['archive_sha256'])

        def link(target, name, **kwargs):
            Path(name).write_text(str(target))

        with mock.patch.object(poller, 'baseline', return_value=installed_new), \
                mock.patch.object(self.worker, 'controller_status', return_value={
                    'attempt': str(attempt), 'state': {'state': 'committed'}, 'host_lock_held': False}), \
                mock.patch.object(poller.os, 'symlink', side_effect=link), \
                mock.patch.object(self.worker, 'operation', side_effect=RuntimeError('simulated crash after switch')):
            with self.assertRaisesRegex(RuntimeError, 'simulated crash after switch'):
                self.worker.finish(operation)
        self.assertEqual((self.state / 'current').read_text(), str(package / 'tools'))
        self.assertEqual(poller.read_json(self.state / 'installed.json')['source_commit'], NEW)
        self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'activation_pending')
        self.assertEqual(self.restart_from_copied_tools(package, 'resume')['result_state'], 'committed')
        self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'committed')
        self.assertEqual(poller.inventory(package / 'tools'), manifest['tools_files'])

    def test_full_candidate_settles_lock_then_commits_receipt_and_updates_tools_once(self):
        stage = self.state / 'packages' / NEW
        stage.mkdir(parents=True)
        archive, deployment = package_fixture(stage)
        package = stage / 'payload'
        manifest = poller.extract_verified(archive, package, deployment, REPO, NEW)
        home = Path(self.temporary.name) / 'owned-home'
        home.mkdir()
        attempt = self.app / 'deploy-controller/attempts/candidate'
        attempt.mkdir(parents=True)
        poller.save(attempt / 'config.json', {'new_frontend_hashes': manifest['frontend_files']})
        installed_new = dict(self.installed, source_commit=NEW,
                             binary_sha256=manifest['binary_sha256'],
                             frontend_pointer='releases/new', frontend_files=manifest['frontend_files'])
        commands = []
        canary = poller.module('poller_contract_canary', ROOT / 'prepare_canary.py')

        def command(script, args, **kwargs):
            commands.append(Path(script).name)
            if Path(script).name == 'prepare_canary.py':
                parsed = canary.parser().parse_args(args)
                self.assertEqual(parsed.app_root, str(self.app))
                self.assertEqual(parsed.new_commit, NEW)
                self.assertEqual(parsed.controller, str(package / 'tools/controller.py'))
                root = Path(parsed.root)
                root.mkdir()
                poller.save(root / 'prepare-receipt.json', {'state': 'prepared', 'attempt': str(attempt)})
                poller.save(root / 'baseline.json', self.installed)
            elif Path(script).name == 'activate_prepared.py':
                self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'activation_started')
            elif Path(script).name == 'controller.py' and args == ['ensure-watchdog', '--attempt', str(attempt)]:
                self.assertEqual(poller.read_json(self.worker.installed_path)['source_commit'], OLD)
                self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'activation_started')
                self.assertFalse((self.state / 'current').exists())
            else:
                self.fail('unexpected controller mutation ' + repr(args))

        def fake_link(target, name, **kwargs):
            # Exercise publication/receipts on Windows without symlink privilege.
            self.assertEqual(Path(target), package / 'tools')
            Path(name).write_text(str(target))

        with mock.patch.object(poller.Path, 'home', return_value=home), \
                mock.patch.object(self.worker, 'stage', return_value=(stage, package, manifest, deployment)), \
                mock.patch.object(poller, 'verify_baseline', return_value=self.installed), \
                mock.patch.object(poller, 'baseline', return_value=installed_new), \
                mock.patch.object(poller, 'cli', side_effect=command), \
                mock.patch.object(self.worker, 'controller_status', side_effect=[
                    {'attempt': str(attempt), 'state': {'state': 'committed'}, 'host_lock_held': True},
                    {'attempt': str(attempt), 'state': {'state': 'committed'}, 'host_lock_held': False}]), \
                mock.patch.object(poller.os, 'symlink', side_effect=fake_link):
            self.assertEqual(self.worker.once()['state'], 'committed')
            self.api.main = NEW
            self.assertEqual(self.worker.once()['state'], 'up-to-date')
        self.assertEqual(commands, ['prepare_canary.py', 'activate_prepared.py', 'controller.py'])
        adopted = poller.read_json(self.state / 'installed.json')
        self.assertEqual(adopted['source_commit'], NEW)
        self.assertEqual(adopted['raw_frontend_files'], manifest['frontend_files'])
        self.assertEqual(adopted['configuration_hashes'], self.installed['configuration_hashes'])
        self.assertEqual((self.state / 'current').read_text(), str(package / 'tools'))
        self.assertEqual(poller.read_json(self.worker.operation_path)['phase'], 'committed')


if __name__ == '__main__':
    unittest.main()
