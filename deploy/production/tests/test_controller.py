#!/usr/bin/env python3
"""Run: python3 -I tools/test_deploy_controller.py -v (no third-party packages).

All writes and subprocess fixtures stay under this tools directory. No HTTP or
SSH is used: fake probes have no URL/callback path and inspect fake-host files.
"""
import importlib.util
import json
import plistlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("controller", HERE.parent / "controller.py")
dc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dc)
OLD = {"version": "0.2.18", "commit": "old-exact-build"}
NEW = {"version": "0.2.19", "commit": "new-exact-build"}


def wait_until(check, timeout=20):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        try:
            value = check()
            if value:
                return value
        except (OSError, ValueError, KeyError):
            pass
        time.sleep(0.025)
    raise AssertionError("bounded wait expired")


class ProbeTimeoutTests(unittest.TestCase):
    def assert_probe_timeout(self, deadline_budget, expected_reason):
        with tempfile.TemporaryDirectory(prefix=".probe-timeout-test-", dir=HERE) as directory:
            attempt = object.__new__(dc.Attempt)
            attempt.path = Path(directory)
            (attempt.path / "probe-results").mkdir()
            attempt.t = {"probe": 2}
            record = {"pid": 12345, "token": "fake-birth-token"}
            child = mock.Mock(pid=record["pid"])
            child.wait.side_effect = [subprocess.TimeoutExpired("fake-probe", 0), 0]
            child.poll.return_value = 0
            deadline = {"test": "fixed-deadline"}
            with mock.patch.object(dc, "remaining", return_value=deadline_budget) as remaining, \
                    mock.patch.object(dc.subprocess, "Popen", return_value=child), \
                    mock.patch.object(attempt, "register", return_value=record) as register, \
                    mock.patch.object(dc, "terminate_record") as terminate:
                with self.assertRaises(dc.Failure) as raised:
                    attempt.probe("new", False, deadline, "runner")
            self.assertEqual(str(raised.exception), expected_reason)
            remaining.assert_called_once_with(deadline)
            register.assert_called_once_with("probe", record["pid"], "runner")
            terminate.assert_called_once_with(record)
            self.assertEqual(child.wait.call_args_list,
                             [mock.call(timeout=min(2, deadline_budget)), mock.call(timeout=3)])
            child.kill.assert_not_called()

    def test_probe_timeout_clipped_by_global_deadline(self):
        for deadline_budget in (0.5, 2):
            with self.subTest(deadline_budget=deadline_budget):
                self.assert_probe_timeout(deadline_budget, "probe deadline expired")

    def test_probe_timeout_with_independent_probe_budget(self):
        self.assert_probe_timeout(3, "probe timed out")


class ControllerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix=".fake-test-", dir=HERE))
        self.root = self.tmp / "host"
        self.root.mkdir()
        (self.root / ".fake-host").touch()
        for path in (self.root / "bin", self.root / "frontend/releases/old/assets",
                     self.tmp / "staged/assets"):
            path.mkdir(parents=True)
        dc.save(self.root / "bin/sub2api", {"build": OLD})
        (self.root / "frontend/current").write_text("releases/old\n")
        for directory, version in ((self.root / "frontend/releases/old", "old"),
                                   (self.tmp / "staged", "new")):
            (directory / "index.html").write_text('<html><script src="/assets/' + version + '.js"></script></html>')
            (directory / "assets" / (version + ".js")).write_text(version)
        dc.save(self.tmp / "new-binary", {"build": NEW})
        dc.save(self.tmp / "config.json", {"app_root": str(self.root), "old_build": OLD, "new_build": NEW})
        dc.save(self.tmp / "evidence.json", {"reviewed": True, "reviewer": "test-owner",
                                           "rollback_compatible": True,
                                           "database_backup": {"verified": True, "reference": "test-only-db-backup"},
                                           "canary": {"eligible": True, "reference": "test-only-canary"}})
        protected = {}
        for name in dc.PROTECTED_CONFIGURATION:
            (self.root / name).write_text('fake-protected-' + name)
            protected[name] = dc.digest(self.root / name)
        evidence = dc.read_json(self.tmp / "evidence.json")
        evidence['configuration_hashes'] = protected
        dc.save(self.tmp / "evidence.json", evidence)
        self.faults()
        with (self.tmp / "wrapper.log").open("ab", buffering=0) as log:
            self.wrapper = subprocess.Popen([sys.executable, "-I", str(HERE / "fake_host.py"),
                                             "wrapper", "--root", str(self.root)],
                                            stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                            **dc.windowless_process_options())
        wait_until(lambda: (self.root / "runtime.json").exists())
        self.attempt = None
        self.extra_children = []

    def tearDown(self):
        if self.attempt:
            processes = self.attempt.path / "processes"
            for file in processes.glob("*.json"):
                try:
                    dc.terminate_record(dc.read_json(file))
                except OSError:
                    pass
        for child in self.extra_children:
            if child.poll() is None:
                child.kill()
            child.wait(timeout=5)
        (self.root / "stop-wrapper").touch()
        try:
            self.wrapper.wait(timeout=6)
        except subprocess.TimeoutExpired:
            self.wrapper.kill()
            self.wrapper.wait(timeout=4)
        if (self.root / "listener-record.json").exists():
            dc.terminate_record(dc.read_json(self.root / "listener-record.json"))
        # Windows may hold Popen image/log handles briefly after termination.
        until = time.monotonic() + 5
        while True:
            try:
                shutil.rmtree(self.tmp)
                break
            except PermissionError:
                if time.monotonic() >= until:
                    raise
                time.sleep(0.05)

    def faults(self, **changes):
        dc.save(self.root / "faults.json", changes)

    def cli(self, *args, success=True):
        ident = str(time.time_ns())
        stdout_path = self.tmp / ("cli-" + ident + ".stdout.log")
        stderr_path = self.tmp / ("cli-" + ident + ".stderr.log")
        with stdout_path.open("wb") as out, stderr_path.open("wb") as err:
            result = subprocess.run([sys.executable, "-I", str(HERE.parent / "controller.py"), *args],
                                    stdin=subprocess.DEVNULL, stdout=out, stderr=err, timeout=20,
                                    **dc.windowless_process_options())
        stdout, stderr = stdout_path.read_text(), stderr_path.read_text()
        if success:
            self.assertEqual(result.returncode, 0, stdout + stderr)
        else:
            self.assertNotEqual(result.returncode, 0)
        return json.loads(stdout) if stdout.strip() else None

    def prepare(self):
        data = self.cli("prepare", "--fake", "--config", str(self.tmp / "config.json"),
                        "--evidence", str(self.tmp / "evidence.json"),
                        "--binary", str(self.tmp / "new-binary"), "--frontend", str(self.tmp / "staged"))
        self.attempt = dc.Attempt(data["attempt"])
        return self.attempt

    def arm_activate(self):
        self.prepare()
        self.cli("arm", "--attempt", str(self.attempt.path))
        return self.cli("activate", "--attempt", str(self.attempt.path))

    def terminal(self):
        def completed():
            state = self.read_state()
            if state not in dc.TERMINAL:
                return None
            # Durable terminal state precedes ownership cleanup for crash recovery.
            if state != "rollback_failed":
                # Share the writer's gate so a Windows observer cannot deny unlink.
                with dc.gate(self.attempt.base / "ownership.lock"):
                    active = self.attempt.base / "active.json"
                    if active.exists() and dc.read_json(active).get("attempt") == str(self.attempt.path):
                        return None
            return state
        return wait_until(completed)

    def read_state(self):
        return dc.read_json(self.attempt.path / "state.json")["state"]

    def attempt_failure_context(self):
        sections = []
        paths = [self.attempt.path / name for name in
                 ("state.json", "decision.json", "events.jsonl", "recovery.log", "runner.log", "watchdog.log")]
        paths.extend((self.tmp / "wrapper.log", self.root / "listener.log"))
        probes = list((self.attempt.path / "probe-results").glob("*.json"))
        probes.sort(key=lambda path: path.stat().st_mtime_ns)
        paths.extend(probes[-5:])
        for path in paths:
            try:
                contents = path.read_text(encoding="utf-8", errors="replace")
            except OSError as error:
                contents = type(error).__name__
            sections.append(str(path.relative_to(self.tmp)) + ":\n" + contents[-12000:])
        return "\n".join(sections)

    def events(self):
        return [json.loads(v) for v in (self.attempt.path / "events.jsonl").read_text().splitlines()]

    def assert_old(self):
        self.assertEqual(dc.digest(self.root / "bin/sub2api"), self.attempt.c["old_binary_sha256"])
        self.assertEqual(dc.tree_hashes(self.attempt.frontend_target(self.attempt.pointer())),
                         self.attempt.c["old_frontend_hashes"])
        runtime = dc.read_json(self.root / "runtime.json")
        self.assertEqual(runtime["build"], OLD)
        self.assertEqual(dc.process_token(runtime["pid"]), runtime["token"])
        self.assertFalse((self.attempt.base / "active.json").exists())

    def test_ensure_watchdog_closes_never_armed_interrupted_adapter(self):
        self.prepare()
        result = self.cli("ensure-watchdog", "--attempt", str(self.attempt.path))
        self.assertEqual(result["state"], "aborted_untouched")
        self.assertFalse((self.attempt.path / "armed.json").exists())
        self.assertFalse((self.attempt.path / "activation.json").exists())
        self.assert_old()

    def terminal_before_lock_cleanup(self, state, decision=None):
        self.prepare()
        dc.save(self.attempt.path / "armed.json", {"expiry": dc.clock_deadline(-1)})
        dc.save(self.attempt.path / "activation.json",
                {"not_before": dc.clock_deadline(-2), "deadline": dc.clock_deadline(-1)})
        if decision is not None:
            self.attempt.decide(decision, "interrupted terminal ownership cleanup")
        # Model a crash after durable terminal publication, before lock release.
        self.attempt.state(state)
        return {name: (self.attempt.path / name).read_bytes()
                for name in ("state.json", "decision.json", "armed.json", "activation.json")
                if (self.attempt.path / name).exists()}

    def assert_terminal_settles_without_replay(self, state, decision):
        before = self.terminal_before_lock_cleanup(state, decision)
        binary = dc.digest(self.attempt.binary())
        frontend_pointer = self.attempt.pointer()
        frontend = dc.tree_hashes(self.attempt.frontend_target(frontend_pointer))
        processes = {path.name: path.read_bytes()
                     for path in (self.attempt.path / "processes").glob("*.json")}
        with mock.patch.object(dc.subprocess, "Popen") as popen, \
                mock.patch.object(dc.subprocess, "run") as run:
            for _ in range(2):
                result = dc.ensure_watchdog(self.attempt)
                self.assertEqual(result["state"], state)
                self.assertFalse(result["watchdog_started"])
                self.assertFalse(result["host_lock_held"])
                self.assertFalse(result["attempt_lock_held"])
        popen.assert_not_called()
        run.assert_not_called()
        self.assertFalse((self.attempt.base / "active.json").exists())
        for name, contents in before.items():
            self.assertEqual((self.attempt.path / name).read_bytes(), contents)
        self.assertEqual(dc.digest(self.attempt.binary()), binary)
        self.assertEqual(self.attempt.pointer(), frontend_pointer)
        self.assertEqual(dc.tree_hashes(self.attempt.frontend_target(self.attempt.pointer())), frontend)
        self.assertEqual({path.name: path.read_bytes()
                          for path in (self.attempt.path / "processes").glob("*.json")}, processes)
        self.assertFalse((self.attempt.path / "runner-ack.json").exists())
        self.assertFalse((self.attempt.path / "watchdog-ack.json").exists())

    def test_ensure_watchdog_settles_committed_terminal_ownership_idempotently(self):
        self.assert_terminal_settles_without_replay("committed", "committed")

    def test_ensure_watchdog_settles_rolled_back_terminal_ownership_idempotently(self):
        self.assert_terminal_settles_without_replay("rolled_back", "rollback")

    def test_ensure_watchdog_terminal_cleanup_preserves_foreign_ownership(self):
        self.terminal_before_lock_cleanup("rolled_back", "rollback")
        active = self.attempt.base / "active.json"
        dc.save(active, {"attempt": str(self.attempt.path.with_name("another-attempt"))})
        before = active.read_bytes()
        with mock.patch.object(dc.subprocess, "Popen") as popen, \
                mock.patch.object(dc.subprocess, "run") as run:
            result = dc.ensure_watchdog(self.attempt)
        self.assertFalse(result["watchdog_started"])
        self.assertTrue(result["host_lock_held"])
        self.assertFalse(result["attempt_lock_held"])
        self.assertEqual(active.read_bytes(), before)
        popen.assert_not_called()
        run.assert_not_called()

    def assert_terminal_evidence_rejected(self, state, decision=None):
        before = self.terminal_before_lock_cleanup(state, decision)
        active = self.attempt.base / "active.json"
        ownership = active.read_bytes()
        with mock.patch.object(dc.subprocess, "Popen") as popen, \
                mock.patch.object(dc.subprocess, "run") as run, \
                self.assertRaisesRegex(dc.Failure, "^successful terminal state lacks its matching durable decision$"):
            dc.ensure_watchdog(self.attempt)
        self.assertEqual(active.read_bytes(), ownership)
        for name, contents in before.items():
            self.assertEqual((self.attempt.path / name).read_bytes(), contents)
        popen.assert_not_called()
        run.assert_not_called()

    def test_ensure_watchdog_terminal_missing_decision_retains_ownership(self):
        self.assert_terminal_evidence_rejected("committed")

    def test_ensure_watchdog_terminal_conflicting_decision_retains_ownership(self):
        self.assert_terminal_evidence_rejected("rolled_back", "committed")

    def test_ensure_watchdog_failed_rollback_retains_ownership(self):
        self.terminal_before_lock_cleanup("rollback_failed", "rollback")
        active = self.attempt.base / "active.json"
        before = active.read_bytes()
        with mock.patch.object(dc.subprocess, "Popen") as popen, \
                mock.patch.object(dc.subprocess, "run") as run:
            result = dc.ensure_watchdog(self.attempt)
        self.assertEqual(result["state"], "rollback_failed")
        self.assertFalse(result["watchdog_started"])
        self.assertEqual(active.read_bytes(), before)
        popen.assert_not_called()
        run.assert_not_called()

    def test_ensure_watchdog_unproven_terminal_states_retain_ownership(self):
        self.prepare()
        active = self.attempt.base / "active.json"
        before = active.read_bytes()
        for state in ("prepare_failed", "aborted_untouched"):
            with self.subTest(state=state):
                self.attempt.state(state)
                with mock.patch.object(dc.subprocess, "Popen") as popen, \
                        mock.patch.object(dc.subprocess, "run") as run:
                    result = dc.ensure_watchdog(self.attempt)
                self.assertEqual(result["state"], state)
                self.assertFalse(result["watchdog_started"])
                self.assertEqual(active.read_bytes(), before)
                popen.assert_not_called()
                run.assert_not_called()

    def test_ensure_watchdog_replaces_only_dead_watchdog_and_preserves_expiry(self):
        self.prepare()
        self.cli("arm", "--attempt", str(self.attempt.path))
        expiry = (self.attempt.path / "armed.json").read_bytes()
        runner = dc.read_json(self.attempt.path / "runner-ack.json")
        original = dc.read_json(self.attempt.path / "watchdog-ack.json")
        dc.terminate_record(original)
        wait_until(lambda: dc.process_token(original["pid"]) != original["token"])
        result = self.cli("ensure-watchdog", "--attempt", str(self.attempt.path))
        self.assertTrue(result["watchdog_started"])
        replacement = wait_until(lambda: (record if (record := dc.read_json(self.attempt.path / "watchdog-ack.json"))["pid"] != original["pid"] else None))
        self.assertEqual(expiry, (self.attempt.path / "armed.json").read_bytes())
        self.assertEqual(runner, dc.read_json(self.attempt.path / "runner-ack.json"))
        again = self.cli("ensure-watchdog", "--attempt", str(self.attempt.path))
        self.assertFalse(again["watchdog_started"])
        self.assertEqual(replacement, dc.read_json(self.attempt.path / "watchdog-ack.json"))
        self.assertFalse((self.attempt.path / "activation.json").exists())
        dc.terminate_record(runner)
        self.assertEqual("rolled_back", self.terminal())
        self.assert_old()

    def test_ensure_watchdog_recovers_after_both_supervisors_die_mid_swap(self):
        self.faults(pause=["after_backend_swap"])
        self.arm_activate()
        wait_until(lambda: (self.attempt.path / "hook-after_backend_swap.json").exists())
        schedule = (self.attempt.path / "activation.json").read_bytes()
        watchdog = dc.read_json(self.attempt.path / "watchdog-ack.json")
        runner = dc.read_json(self.attempt.path / "runner-ack.json")
        dc.terminate_record(watchdog)
        wait_until(lambda: dc.process_token(watchdog["pid"]) != watchdog["token"])
        dc.terminate_record(runner)
        wait_until(lambda: dc.process_token(runner["pid"]) != runner["token"])
        self.cli("ensure-watchdog", "--attempt", str(self.attempt.path))
        self.assertEqual("rolled_back", self.terminal())
        self.assertEqual(schedule, (self.attempt.path / "activation.json").read_bytes())
        self.assertEqual(runner, dc.read_json(self.attempt.path / "runner-ack.json"))
        self.assert_old()

    def test_ensure_watchdog_does_not_extend_expired_recovery_or_replay_runner(self):
        self.prepare()
        dc.save(self.attempt.path / "armed.json", {"expiry":dc.clock_deadline(-1)})
        self.attempt.decide("rollback", "old recovery")
        decision = dc.read_json(self.attempt.path / "decision.json")
        decision["deadline"] = dc.clock_deadline(-1)
        dc.save(self.attempt.path / "decision.json", decision)
        before = (self.attempt.path / "decision.json").read_bytes()
        self.cli("ensure-watchdog", "--attempt", str(self.attempt.path))
        self.assertEqual("rollback_failed", self.terminal())
        self.assertEqual(before, (self.attempt.path / "decision.json").read_bytes())
        self.assertFalse((self.attempt.path / "runner-ack.json").exists())
        self.assertTrue((self.attempt.base / "active.json").exists())

    def test_ensure_watchdog_live_bootstraps_only_exact_missing_frozen_job(self):
        self.prepare()
        dc.save(self.attempt.path / "armed.json", {"expiry":dc.clock_deadline(30)})
        job = dc.launchd_job(self.attempt, "watchdog")
        plist = self.attempt.path / "jobs/watchdog.plist"
        with plist.open("wb") as stream:
            plistlib.dump(job, stream)
        self.attempt.fake = False
        with mock.patch.object(dc.os, "getuid", return_value=501, create=True), mock.patch.object(dc.subprocess, "run", side_effect=[mock.Mock(returncode=1), mock.Mock(returncode=0)]) as run:
            result = dc.ensure_watchdog(self.attempt)
        self.assertTrue(result["watchdog_started"])
        self.assertEqual([call.args[0][1] for call in run.call_args_list], ["print", "bootstrap"])
        self.assertEqual(str(plist), run.call_args_list[1].args[0][-1])
        with mock.patch.object(dc.os, "getuid", return_value=501, create=True), mock.patch.object(dc.subprocess, "run", return_value=mock.Mock(returncode=0)) as run:
            self.assertFalse(dc.ensure_watchdog(self.attempt)["watchdog_started"])
        self.assertEqual(1, run.call_count)
        job["ProgramArguments"][3] = "runner"
        with plist.open("wb") as stream:
            plistlib.dump(job, stream)
        with mock.patch.object(dc.subprocess, "run") as run, self.assertRaisesRegex(dc.Failure, "exact supervised command"):
            dc.ensure_watchdog(self.attempt)
        run.assert_not_called()
        self.attempt.fake = True

    def test_ensure_watchdog_missing_startup_job_closes_without_activation(self):
        self.prepare()
        dc.save(self.attempt.path / "armed.json", {"expiry":dc.clock_deadline(30)})
        self.attempt.fake = False
        # Real close probes are separately covered. Confirm missing plist chooses
        # only the fenced non-activated closure path, not launchd or runner.
        with mock.patch.object(dc, "close_untouched", return_value={"state":"aborted_untouched","activated":False}) as close, mock.patch.object(dc.subprocess, "run") as run:
            result = dc.ensure_watchdog(self.attempt)
        close.assert_called_once_with(self.attempt)
        run.assert_not_called()
        self.assertEqual("rollback", self.attempt.decision()["decision"])
        self.assertFalse(result["watchdog_started"])
        self.attempt.fake = True

    def test_prepare_rejects_protected_configuration_drift(self):
        (self.root / '.env').write_text('out of band drift')
        self.cli("prepare", "--fake", "--config", str(self.tmp / "config.json"),
                 "--evidence", str(self.tmp / "evidence.json"),
                 "--binary", str(self.tmp / "new-binary"), "--frontend", str(self.tmp / "staged"), success=False)
        self.assertFalse((self.root / 'deploy-controller/active.json').exists())

    def test_configuration_drift_before_commit_rolls_artifacts_back_and_holds_lock(self):
        self.faults(pause=['before_commit'])
        self.arm_activate()
        wait_until(lambda: (self.attempt.path / 'hook-before_commit.json').exists())
        (self.root / '.env').write_text('out of band drift')
        self.faults()
        self.assertEqual('rollback_failed', self.terminal())
        self.assertEqual(dc.digest(self.root / 'bin/sub2api'), self.attempt.c['old_binary_sha256'])
        self.assertEqual(dc.tree_hashes(self.attempt.frontend_target(self.attempt.pointer())), self.attempt.c['old_frontend_hashes'])
        self.assertEqual('out of band drift', (self.root / '.env').read_text())
        self.assertEqual('rollback', self.attempt.decision()['decision'])
        self.assertTrue((self.attempt.base / 'active.json').exists())

    def test_protected_configuration_names_hashes_and_link_paths_are_restricted(self):
        with self.assertRaises(dc.Failure):
            dc.verify_configuration(self.root, {'../escape':'a'*64})
        with self.assertRaises(dc.Failure):
            dc.verify_configuration(self.root, {'.env':'bad'})
        with self.assertRaises(dc.Failure):
            dc.verify_configuration(self.root, {}, required=True)

    def test_deadline_is_shared_across_process_lifetimes(self):
        deadline = dc.clock_deadline(10)
        time.sleep(0.4)
        script = ('import importlib.util,json,sys; '
                  's=importlib.util.spec_from_file_location("dc",sys.argv[1]); '
                  'm=importlib.util.module_from_spec(s); s.loader.exec_module(m); '
                  'print(m.remaining(json.loads(sys.argv[2])))')
        output = self.tmp / "cross-process-clock.log"
        with output.open("wb") as log:
            result = subprocess.run([sys.executable, "-I", "-c", script, str(dc.SCRIPT), json.dumps(deadline)],
                                    stdin=subprocess.DEVNULL, stdout=log, stderr=log, timeout=5,
                                    **dc.windowless_process_options())
        self.assertEqual(result.returncode, 0, output.read_text())
        remaining = float(output.read_text())
        self.assertGreater(remaining, 5)
        self.assertLess(remaining, 10)

    def test_close_untouched_verifies_without_replaying_activation(self):
        self.prepare()
        self.cli("close-untouched", "--attempt", str(self.attempt.path))
        self.assertEqual(self.read_state(), "aborted_untouched")
        self.assertFalse((self.attempt.path / "activation.json").exists())
        self.assert_old()

    def test_success_backend_first_stable_committed(self):
        self.arm_activate()
        self.assertEqual(self.terminal(), "committed")
        events = self.events()
        backend_verified = next(i for i, v in enumerate(events) if v["event"] == "stable_sample"
                                and v["phase"] == "new" and not v["frontend"]
                                and v["count"] == self.attempt.t["stable"])
        frontend_swapped = next(i for i, v in enumerate(events) if v["event"] == "activating_frontend")
        self.assertLess(backend_verified, frontend_swapped)
        self.assertEqual(dc.read_json(self.root / "runtime.json")["build"], NEW)
        self.assertEqual(self.attempt.decision()["decision"], "committed")
        self.assertFalse((self.attempt.base / "active.json").exists())
        self.assertGreaterEqual(sum(v["event"] == "stable_sample" for v in events), 4)

    def test_failed_health_rolls_back_without_frontend_activation(self):
        self.faults(local_health=["new"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()
        self.assertNotIn("activating_frontend", [v["event"] for v in self.events()])

    def test_health_http_status_classification_does_not_weaken_auth_gate(self):
        from unittest.mock import patch
        class Response:
            status = 503
        class Connection:
            def __init__(self, *args, **kwargs): pass
            def request(self, *args, **kwargs): pass
            def getresponse(self): return Response()
            def close(self): pass
        with patch.object(dc.http.client, "HTTPConnection", Connection):
            with self.assertRaises(dc.NotReady): dc.http_get("http://localhost", "/health", 200)
            with self.assertRaises(dc.Failure) as error:
                dc.http_get("http://localhost", "/api/v1/admin/accounts", 401)
            self.assertNotIsInstance(error.exception, dc.NotReady)

    def test_transient_edge_health_cache_requires_stable_recovery(self):
        self.prepare()
        self.faults(health_warming_until={"new": time.time() + 1.5})
        self.cli("arm", "--attempt", str(self.attempt.path))
        self.cli("activate", "--attempt", str(self.attempt.path))
        self.assertEqual(self.terminal(), "committed")
        samples = [v for v in self.events() if v["event"] == "stable_sample" and v["phase"] == "new"]
        self.assertGreaterEqual(len(samples), 4)

    def test_permanent_edge_health_warming_rolls_back_at_deadline(self):
        config = dc.read_json(self.tmp / "config.json")
        config["fake_timing"] = {"activation": 2}
        dc.save(self.tmp / "config.json", config)
        self.faults(health_warming_until={"new": time.time() + 60})
        self.arm_activate()
        state = self.terminal()
        self.assertEqual(state, "rolled_back", self.attempt_failure_context() if state != "rolled_back" else None)
        self.assert_old()
        self.assertIn("deadline", self.attempt.decision()["reason"])

    def test_public_health_failure_rolls_back(self):
        self.faults(public_health=["new"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()

    def test_auth_boundary_failure_rolls_back(self):
        self.faults(auth_boundary=["new"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()

    def test_hanging_probe_bounded_and_fenced(self):
        self.faults(hang_probe=["new"])
        started = time.monotonic()
        self.arm_activate()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assertLess(time.monotonic() - started, 15)
        self.assert_old()
        records = [dc.read_json(p) for p in (self.attempt.path / "processes").glob("*.json")]
        self.assertTrue(any(r["role"] == "probe" and r["owner"] == "runner" for r in records))
        self.assertTrue(all(dc.process_token(r["pid"]) != r["token"] for r in records
                            if r["owner"] == "runner"))

    def killed_runner_at(self, hook):
        self.faults(pause=[hook])
        self.arm_activate()
        wait_until(lambda: (self.attempt.path / ("hook-" + hook + ".json")).exists())
        r = dc.read_json(self.attempt.path / "runner-ack.json")
        self.assertTrue(dc.terminate_record(r))
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()
        events = [v["event"] for v in self.events()]
        self.assertLess(events.index("runner_fenced"), events.index("old_artifacts_restored"))

    def test_runner_killed_before_backend_swap(self):
        self.killed_runner_at("before_backend_swap")

    def test_runner_killed_after_backend_swap(self):
        self.killed_runner_at("after_backend_swap")

    def test_runner_killed_before_frontend_swap(self):
        self.killed_runner_at("before_frontend_swap")

    def test_runner_killed_after_frontend_swap(self):
        self.killed_runner_at("after_frontend_swap")

    def test_frontend_content_failure_restores_both(self):
        self.faults(frontend=["new"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()
        self.assertIn("activating_frontend", [v["event"] for v in self.events()])

    def test_failed_rollback_holds_host_lock_and_never_restores_database(self):
        self.prepare()
        self.faults(pause=["after_backend_swap"])
        self.cli("arm", "--attempt", str(self.attempt.path))
        self.cli("activate", "--attempt", str(self.attempt.path))
        wait_until(lambda: (self.attempt.path / "hook-after_backend_swap.json").exists())
        self.faults(local_health=["new", "old"])
        self.assertEqual(self.terminal(), "rollback_failed")
        state = dc.read_json(self.attempt.path / "state.json")
        self.assertFalse(state["database_restored"])
        self.assertTrue(state["lock_held"])
        self.assertTrue((self.attempt.base / "active.json").exists())
        self.assertEqual(dc.digest(self.root / "bin/sub2api"), self.attempt.c["old_binary_sha256"])

    def test_startup_ack_and_activation_delay(self):
        self.prepare()
        old_stat = self.attempt.binary().stat()
        self.cli("activate", "--attempt", str(self.attempt.path), success=False)
        self.cli("arm", "--attempt", str(self.attempt.path))
        self.assertTrue((self.attempt.path / "runner-ack.json").is_file())
        self.assertTrue((self.attempt.path / "watchdog-ack.json").is_file())
        self.assertEqual(self.attempt.binary().stat().st_mtime_ns, old_stat.st_mtime_ns)
        data = self.cli("activate", "--attempt", str(self.attempt.path))
        self.assertGreater(data["not_before"]["wall"], time.time())
        self.assertEqual(dc.digest(self.attempt.binary()), self.attempt.c["old_binary_sha256"])
        self.assertEqual(self.terminal(), "committed")
        self.cli("activate", "--attempt", str(self.attempt.path), success=False)

    def test_global_lock_and_immutable_snapshots(self):
        self.prepare()
        self.cli("prepare", "--fake", "--config", str(self.tmp / "config.json"),
                 "--evidence", str(self.tmp / "evidence.json"),
                 "--binary", str(self.tmp / "new-binary"), "--frontend", str(self.tmp / "staged"), success=False)
        (self.tmp / "new-binary").write_text("modified staging")
        (self.tmp / "staged/index.html").write_text("modified staging")
        self.cli("arm", "--attempt", str(self.attempt.path))
        self.cli("activate", "--attempt", str(self.attempt.path))
        self.assertEqual(self.terminal(), "committed")
        self.assertEqual(dc.digest(self.attempt.binary()), self.attempt.c["new_binary_sha256"])

    def test_same_size_preserved_mtime_swap_forces_wrapper_detection(self):
        self.prepare()
        binary = self.attempt.binary()
        source = self.attempt.path / "same-size"
        source.write_bytes(b"X" * binary.stat().st_size)
        old_time = binary.stat().st_mtime_ns
        os.utime(source, ns=(old_time, old_time))
        self.attempt.swap_binary(source, dc.digest(source))
        self.assertEqual(binary.stat().st_size, source.stat().st_size)
        self.assertNotEqual(int(old_time / 1e9), int(binary.stat().st_mtime))

    def test_atomic_decision_race_exactly_one_winner(self):
        self.prepare()
        # Processes race the same link creation; neither can overwrite its winner.
        script = ('import importlib.util,sys; from pathlib import Path; '
                  's=importlib.util.spec_from_file_location("dc",sys.argv[1]); '
                  'm=importlib.util.module_from_spec(s); s.loader.exec_module(m); '
                  'a=m.Attempt(sys.argv[2]); print(a.decide(sys.argv[3],"race"))')
        children = []
        outputs = []
        for index, decision in enumerate(("committed", "rollback") * 4):
            output = self.tmp / ("race-%d.log" % index)
            with output.open("wb") as log:
                child = subprocess.Popen([sys.executable, "-I", "-c", script, str(dc.SCRIPT),
                                          str(self.attempt.path), decision],
                                         stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                         **dc.windowless_process_options())
            children.append(child)
            self.extra_children.append(child)
            outputs.append(output)
        for child in children:
            self.assertEqual(child.wait(timeout=10), 0)
        results = [output.read_text().strip() for output in outputs]
        self.assertEqual(results.count("True"), 1, results)
        self.assertIn(self.attempt.decision()["decision"], ("committed", "rollback"))
        before = (self.attempt.path / "decision.json").read_bytes()
        self.assertFalse(self.attempt.decide("rollback", "late loser"))
        self.assertEqual((self.attempt.path / "decision.json").read_bytes(), before)

    def test_activation_deadline_rolls_back_paused_runner(self):
        config = dc.read_json(self.tmp / "config.json")
        config["fake_timing"] = {"activation": 2}
        dc.save(self.tmp / "config.json", config)
        self.faults(pause=["after_backend_swap"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()
        self.assertEqual(self.attempt.decision()["reason"], "activation deadline expired")

    def test_fence_does_not_kill_unrelated_or_pid_reused_process(self):
        self.prepare()
        with (self.tmp / "fence-child.log").open("wb") as log:
            child = subprocess.Popen([sys.executable, "-I", "-c", "import time; time.sleep(30)"],
                                     stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                     **dc.windowless_process_options())
        self.extra_children.append(child)
        # Forged PID with a mismatched birth token must never be signaled.
        dc.save(self.attempt.path / "processes/old-pid.json",
                {"pid": child.pid, "token": "not-the-process-birth", "role": "runner",
                 "owner": "runner", "attempt": self.attempt.path.name})
        self.attempt.decide("rollback", "fence identity test")
        self.attempt.recover()
        self.assertEqual(self.read_state(), "rolled_back")
        self.assertIsNone(child.poll())
        self.assert_old()

    def test_reviewed_evidence_required(self):
        evidence = dc.read_json(self.tmp / "evidence.json")
        evidence["canary"]["eligible"] = False
        dc.save(self.tmp / "evidence.json", evidence)
        self.cli("prepare", "--fake", "--config", str(self.tmp / "config.json"),
                 "--evidence", str(self.tmp / "evidence.json"),
                 "--binary", str(self.tmp / "new-binary"), "--frontend", str(self.tmp / "staged"), success=False)
        self.assertFalse((self.root / "deploy-controller/active.json").exists())

    def test_fake_rejects_network_or_callback_configuration(self):
        config = dc.read_json(self.tmp / "config.json")
        config["public_base"] = "https://example.com"
        dc.save(self.tmp / "config.json", config)
        self.cli("prepare", "--fake", "--config", str(self.tmp / "config.json"),
                 "--evidence", str(self.tmp / "evidence.json"),
                 "--binary", str(self.tmp / "new-binary"), "--frontend", str(self.tmp / "staged"), success=False)

    def test_wrong_exact_runtime_build_rejected(self):
        self.prepare()
        self.faults(pause=["after_backend_swap"])
        self.cli("arm", "--attempt", str(self.attempt.path))
        self.cli("activate", "--attempt", str(self.attempt.path))
        wait_until(lambda: (self.attempt.path / "hook-after_backend_swap.json").exists())
        wait_until(lambda: dc.read_json(self.root / "runtime.json")["build"] == NEW)
        runtime = dc.read_json(self.root / "runtime.json")
        runtime["build"] = {"version": "0.2.19", "commit": "wrong-commit"}
        dc.save(self.root / "runtime.json", runtime)
        self.faults()
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()

    def test_corrupt_old_binary_backup_fails_closed(self):
        self.faults(pause=["after_backend_swap"])
        self.arm_activate()
        wait_until(lambda: (self.attempt.path / "hook-after_backend_swap.json").exists())
        (self.attempt.path / "old-binary").write_text("corrupt")
        self.cli("rollback", "--attempt", str(self.attempt.path))
        self.assertEqual(self.read_state(), "rollback_failed")
        self.assertTrue((self.attempt.base / "active.json").exists())

    def test_recovery_deadline_kills_hanging_restore_worker(self):
        config = dc.read_json(self.tmp / "config.json")
        config["fake_timing"] = {"recovery": 2}
        dc.save(self.tmp / "config.json", config)
        self.faults(local_health=["new"], pause=["before_restore"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "rollback_failed")
        self.assertTrue((self.attempt.base / "active.json").exists())
        records = [dc.read_json(p) for p in (self.attempt.path / "processes").glob("*.json")]
        self.assertTrue(all(dc.process_token(r["pid"]) != r["token"] for r in records
                            if r.get("owner") == "recovery"))

    def test_runner_crash_after_commit_decision_is_not_rolled_back(self):
        self.faults(crash=["after_commit_decision"])
        self.arm_activate()
        self.assertEqual(self.terminal(), "committed")
        self.assertEqual(self.attempt.decision()["decision"], "committed")
        state = dc.read_json(self.attempt.path / "state.json")
        self.assertEqual(state["identity"]["build"], NEW)
        self.assertTrue(state["recovered_decision"])
        self.assertFalse((self.attempt.base / "active.json").exists())

    def test_runner_restart_never_replays_swap(self):
        self.faults(pause=["after_backend_swap"])
        self.arm_activate()
        wait_until(lambda: (self.attempt.path / "hook-after_backend_swap.json").exists())
        self.cli("runner", "--attempt", str(self.attempt.path))
        self.assertEqual(self.terminal(), "rolled_back")
        self.assertEqual(sum(v["event"] == "backend_swapped" for v in self.events()), 1)
        self.assert_old()

    def test_armed_without_activation_expires(self):
        config = dc.read_json(self.tmp / "config.json")
        config["fake_timing"] = {"arm_expiry": 2}
        dc.save(self.tmp / "config.json", config)
        self.prepare()
        self.cli("arm", "--attempt", str(self.attempt.path))
        self.assertEqual(self.terminal(), "rolled_back")
        self.assertEqual(self.attempt.decision()["reason"], "armed attempt expired without activation")
        self.assert_old()

    def test_immutable_config_tampering_rejected(self):
        self.prepare()
        config = dc.read_json(self.attempt.path / "config.json")
        config["new_build"] = OLD
        dc.save(self.attempt.path / "config.json", config)
        self.cli("arm", "--attempt", str(self.attempt.path), success=False)
        self.assertEqual(self.read_state(), "prepared")

    def test_old_frontend_backup_used_if_release_drifted(self):
        self.faults(pause=["after_frontend_swap"])
        self.arm_activate()
        wait_until(lambda: (self.attempt.path / "hook-after_frontend_swap.json").exists())
        (self.root / "frontend/releases/old/index.html").write_text("old release drift")
        self.cli("rollback", "--attempt", str(self.attempt.path))
        self.assertEqual(self.terminal(), "rolled_back")
        self.assert_old()
        self.assertTrue(self.attempt.pointer().endswith("-recovered"))


class TerminalWaitTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix=".terminal-wait-test-", dir=HERE)
        self.addCleanup(directory.cleanup)
        self.base = Path(directory.name)
        self.subject = ControllerTests(methodName="runTest")
        self.subject.attempt = mock.Mock(base=self.base, path=self.base / "attempt")
        self.active = self.base / "active.json"

    def test_released_terminal_states_wait_for_ownership_cleanup(self):
        for state in sorted(dc.TERMINAL - {"rollback_failed"}):
            with self.subTest(state=state):
                self.subject.read_state = mock.Mock(return_value=state)
                dc.save(self.active, {"attempt": str(self.subject.attempt.path)})

                def release_after_first_poll(_):
                    self.active.unlink()

                with mock.patch.object(time, "sleep", side_effect=release_after_first_poll) as sleep:
                    self.assertEqual(self.subject.terminal(), state)
                sleep.assert_called_once_with(0.025)
                self.assertFalse(self.active.exists())

    def test_rollback_failed_returns_with_ownership_retained(self):
        self.subject.read_state = mock.Mock(return_value="rollback_failed")
        dc.save(self.active, {"attempt": str(self.subject.attempt.path)})
        with mock.patch.object(time, "sleep") as sleep:
            self.assertEqual(self.subject.terminal(), "rollback_failed")
        sleep.assert_not_called()
        self.assertTrue(self.active.exists())

    def test_terminal_does_not_wait_for_another_attempts_ownership(self):
        self.subject.read_state = mock.Mock(return_value="rolled_back")
        dc.save(self.active, {"attempt": str(self.base / "another-attempt")})
        with mock.patch.object(time, "sleep") as sleep:
            self.assertEqual(self.subject.terminal(), "rolled_back")
        sleep.assert_not_called()
        self.assertTrue(self.active.exists())

    def test_terminal_observes_ownership_under_the_writer_gate(self):
        self.subject.read_state = mock.Mock(return_value="rolled_back")
        dc.save(self.active, {"attempt": str(self.base / "another-attempt")})
        gate_held = {"value": False}
        original_read_json = dc.read_json

        def read_while_locked(path):
            self.assertTrue(gate_held["value"], "ownership reads must share the cleanup writer's gate")
            return original_read_json(path)

        with mock.patch.object(dc, "gate") as acquire, \
                mock.patch.object(dc, "read_json", side_effect=read_while_locked):
            acquire.return_value.__enter__.side_effect = lambda: gate_held.update(value=True)
            acquire.return_value.__exit__.side_effect = lambda *_: gate_held.update(value=False)
            self.assertEqual(self.subject.terminal(), "rolled_back")
        acquire.assert_called_once_with(self.base / "ownership.lock")
        self.assertFalse(gate_held["value"])


if __name__ == "__main__":
    dc.record_test_console()
    unittest.main()
