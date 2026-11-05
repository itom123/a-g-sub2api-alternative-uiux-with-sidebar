import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('activate_production', Path(__file__).resolve().parent.parent / 'activate_prepared.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class ActivationAdapterTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.attempt = self.root / 'attempt'
        self.attempt.mkdir()
        (self.root / 'controller.py').write_text('# fake never executed')
        (self.root / 'prepare-receipt.json').write_text(json.dumps({'state': 'prepared', 'attempt': str(self.attempt)}))
        (self.root / 'canary-result.json').write_text(json.dumps({'status': 'passed'}))

    def response(self, state, code=0):
        return mock.Mock(returncode=code, stdout=json.dumps({'state': state, 'attempt': str(self.attempt)}))

    def status(self, state):
        return {'attempt': str(self.attempt), 'state': {'state': state}, 'host_lock_held': state not in m.TERMINAL}

    def invoke(self, states, command_results=(), resume=False):
        argv = ['activate', '--canary-root', str(self.root)] + (['--resume'] if resume else [])
        with mock.patch.object(m.sys, 'argv', argv), mock.patch.object(m.subprocess, 'run', side_effect=command_results) as run, mock.patch.object(m, 'run_status', side_effect=[self.status(s) for s in states]), mock.patch.object(m.time, 'sleep'), mock.patch('sys.stdout', new_callable=io.StringIO):
            return m.main(), run

    def test_arms_once_activates_once_and_commits(self):
        def command(args, **kwargs):
            cmd = args[3]
            marker = self.root / ('activation-' + cmd + '-intent.json')
            self.assertTrue(marker.is_file(), 'durable intent must precede process launch')
            self.assertEqual(str(self.attempt), json.loads(marker.read_text())['attempt'])
            return self.response('armed' if cmd == 'arm' else 'scheduled')
        result, run = self.invoke(['prepared', 'scheduled', 'committed'], command)
        self.assertEqual(result, 0)
        self.assertEqual([c.args[0][3] for c in run.call_args_list], ['arm', 'activate'])
        self.assertEqual('committed', m.state_name(json.loads((self.root / 'activation-terminal.json').read_text())))

    def test_resume_and_default_detection_never_replay_armed_or_scheduled(self):
        for marker, resume in (('armed.json', False), ('activation.json', False), ('', True)):
            with self.subTest(marker=marker):
                (self.root / 'activation-terminal.json').unlink(missing_ok=True)
                if marker:
                    (self.attempt / marker).write_text('{}')
                result, run = self.invoke(['scheduled', 'committed'], resume=resume)
                self.assertEqual(result, 0)
                run.assert_not_called()
                if marker:
                    (self.attempt / marker).unlink()

    def test_lost_acknowledgement_intent_forces_follow_only(self):
        for command in ('arm', 'activate'):
            with self.subTest(command=command):
                (self.root / 'activation-terminal.json').unlink(missing_ok=True)
                marker = self.root / ('activation-' + command + '-intent.json')
                marker.write_text('{}')
                result, run = self.invoke(['arming', 'rolled_back'])
                self.assertEqual(result, 1)
                run.assert_not_called()
                marker.unlink()

    def test_timeout_after_arm_is_not_replayed_on_resume(self):
        with self.assertRaisesRegex(RuntimeError, 'never replay'):
            self.invoke(['prepared'], [subprocess.TimeoutExpired('arm', 55)])
        self.assertTrue((self.root / 'activation-arm-intent.json').exists())
        self.assertFalse((self.root / 'activation-activate-intent.json').exists())
        result, run = self.invoke(['armed', 'rolled_back'], resume=True)
        self.assertEqual(result, 1)
        run.assert_not_called()

    def test_arm_failure_never_activates(self):
        with self.assertRaisesRegex(RuntimeError, 'never replay'):
            self.invoke(['prepared'], [self.response('failed', 1)])
        self.assertTrue((self.root / 'activation-arm-intent.json').exists())
        self.assertFalse((self.root / 'activation-activate-intent.json').exists())

    def test_missing_arm_acknowledgement_never_activates(self):
        with self.assertRaisesRegex(RuntimeError, 'acknowledgement missing'):
            self.invoke(['prepared'], [self.response('unknown')])
        self.assertFalse((self.root / 'activation-activate-intent.json').exists())

    def test_saved_terminal_evidence_is_idempotent_and_never_overwritten(self):
        terminal = self.status('committed')
        saved = self.root / 'activation-terminal.json'
        saved.write_text(json.dumps(terminal))
        before = saved.read_bytes()
        result, run = self.invoke([])
        self.assertEqual(result, 0)
        run.assert_not_called()
        self.assertEqual(before, saved.read_bytes())

    def test_existing_terminal_controller_state_only_records_and_returns(self):
        result, run = self.invoke(['aborted_untouched'])
        self.assertEqual(result, 1)
        run.assert_not_called()

    def test_invalid_saved_terminal_or_missing_canary_fails_without_command(self):
        for bad in ({'attempt':'elsewhere','state':{'state':'committed'}}, self.status('scheduled')):
            with self.subTest(bad=bad):
                (self.root / 'activation-terminal.json').write_text(json.dumps(bad))
                with self.assertRaisesRegex(RuntimeError, 'evidence is invalid'):
                    self.invoke([])
        (self.root / 'activation-terminal.json').unlink()
        (self.root / 'canary-result.json').write_text('{"status":"failed"}')
        with self.assertRaisesRegex(RuntimeError, 'canary evidence missing'):
            self.invoke([])

    def test_evidence_is_create_only_complete_and_durable(self):
        path = self.root / 'evidence.json'
        m.save_once(path, {'state':'committed'})
        self.assertEqual({'state':'committed'}, json.loads(path.read_text()))
        with self.assertRaises(FileExistsError):
            m.save_once(path, {'state':'rolled_back'})
        self.assertEqual({'state':'committed'}, json.loads(path.read_text()))
        self.assertEqual([], list(self.root.glob('.evidence.json-*')))

    def test_wrong_command_acknowledgement_attempt_cannot_activate(self):
        wrong = mock.Mock(returncode=0, stdout=json.dumps({'state':'armed','attempt':'/wrong/attempt'}))
        with self.assertRaisesRegex(RuntimeError, 'attempt identity differs'):
            self.invoke(['prepared'], [wrong])
        self.assertFalse((self.root / 'activation-activate-intent.json').exists())

    def test_run_status_validates_exact_attempt_and_is_readonly(self):
        for code, attempt in ((1, self.attempt), (0, self.root / 'other')):
            result = mock.Mock(returncode=code, stdout=json.dumps({'attempt':str(attempt), 'state':{'state':'prepared'}}))
            with mock.patch.object(m.subprocess, 'run', return_value=result) as run, self.assertRaises(RuntimeError):
                m.run_status(self.root / 'controller.py', self.attempt)
            self.assertEqual('status', run.call_args.args[0][3])

    def test_follow_only_bounded_wait_never_mutates_prepared_attempt(self):
        (self.root / 'activation-arm-intent.json').write_text('{}')
        with mock.patch.object(m.time, 'monotonic', side_effect=[0, 1000]), self.assertRaisesRegex(RuntimeError, 'bounded status wait elapsed'):
            self.invoke(['prepared'])
        self.assertFalse((self.root / 'activation-activate-intent.json').exists())


if __name__ == '__main__':
    unittest.main()
