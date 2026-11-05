import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path
import plistlib
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("tested_production_install", ROOT / "install.py")
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)
poller = installer.load_poller()
SHA, BINARY = "a" * 40, "b" * 64


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.home = self.root / "home"
        self.app = self.home / ".local/share/sub2api"
        self.app.mkdir(parents=True)
        self.agent_dir = self.home / "Library/LaunchAgents"
        self.agent_dir.mkdir(parents=True)
        self.tools = self.root / "tools"
        self.tools.mkdir()
        for name in ("poller.py", "github_api.py", "controller.py", "prepare_canary.py", "activate_prepared.py", "install.py"):
            (self.tools / name).write_text("# frozen " + name + "\n", encoding="utf-8")
        (self.tools / "README.md").write_text("documentation", encoding="utf-8")
        self.state = self.app / "auto-deploy"
        self.plist = self.agent_dir / (installer.LABEL + ".plist")
        self.baseline = {"version": "0.2.19", "source_commit": SHA, "binary_sha256": BINARY,
                         "frontend_pointer": "releases/old", "frontend_files": {"index.html": "c" * 64},
                         "configuration_hashes": {".env": "d" * 64}}
        self.stack = contextlib.ExitStack()
        self.addCleanup(self.stack.close)
        self.stack.enter_context(mock.patch.object(installer.Path, "home", return_value=self.home))
        self.stack.enter_context(mock.patch.object(installer, "HERE", self.tools))
        self.stack.enter_context(mock.patch.object(installer, "load_poller", return_value=poller))
        self.stack.enter_context(mock.patch.object(installer.sys, "platform", "darwin"))
        self.stack.enter_context(mock.patch.object(installer.os, "umask"))
        self.stack.enter_context(mock.patch.object(installer.os, "getuid", create=True, return_value=501))
        self.baseline_call = self.stack.enter_context(mock.patch.object(poller, "baseline", side_effect=lambda app: copy.deepcopy(self.baseline)))
        self.launch = self.stack.enter_context(mock.patch.object(installer.subprocess, "run", return_value=mock.Mock(returncode=0)))
        # A Windows test should not require native symbolic-link privileges.
        self.symlink = self.stack.enter_context(mock.patch.object(installer.os, "symlink"))

    def args(self, *extra):
        return ["install.py", "install", "--expected-commit", SHA,
                "--expected-binary-sha256", BINARY, *extra]

    def invoke(self, arguments):
        output = io.StringIO()
        with mock.patch.object(sys, "argv", arguments), contextlib.redirect_stdout(output):
            result = installer.main()
        return result, json.loads(output.getvalue())

    def snapshot(self):
        return {path.relative_to(self.root).as_posix(): path.read_bytes() if path.is_file() else None
                for path in self.root.rglob("*")}

    def test_rejects_non_macos_before_any_writes(self):
        before = self.snapshot()
        with mock.patch.object(installer.sys, "platform", "win32"), self.assertRaisesRegex(poller.Failure, "requires-macos"):
            self.invoke(self.args())
        self.assertEqual(before, self.snapshot())
        self.launch.assert_not_called()

    def test_requires_standard_app_root(self):
        alternate = self.home / "alternate"
        alternate.mkdir()
        before = self.snapshot()
        with self.assertRaisesRegex(poller.Failure, "standard-app-root"):
            self.invoke(self.args("--app-root", str(alternate)))
        self.assertEqual(before, self.snapshot())
        self.baseline_call.assert_not_called()
        self.launch.assert_not_called()

    def test_baseline_mismatch_refuses_all_writes(self):
        for field, value, message in (("source_commit", "e" * 40, "commit-mismatch"),
                                       ("binary_sha256", "f" * 64, "binary-mismatch")):
            with self.subTest(field=field):
                before = self.snapshot()
                original = self.baseline[field]
                self.baseline[field] = value
                with self.assertRaisesRegex(poller.Failure, message):
                    self.invoke(self.args())
                self.baseline[field] = original
                self.assertEqual(before, self.snapshot())
        self.launch.assert_not_called()
        self.symlink.assert_not_called()

    def test_active_controller_attempt_refuses_adoption(self):
        active = self.app / "deploy-controller/active.json"
        active.parent.mkdir()
        active.write_text("{}", encoding="utf-8")
        before = self.snapshot()
        with self.assertRaisesRegex(poller.Failure, "already-active"):
            self.invoke(self.args())
        self.assertEqual(before, self.snapshot())
        self.baseline_call.assert_not_called()
        self.launch.assert_not_called()

    def test_dry_run_does_not_write_or_launch(self):
        before = self.snapshot()
        code, result = self.invoke(self.args("--dry-run"))
        self.assertEqual(code, 0)
        self.assertTrue(result["dry_run"])
        self.assertFalse(result["enabled"])
        self.assertEqual(result["source_commit"], SHA)
        self.assertEqual(before, self.snapshot())
        self.launch.assert_not_called()
        self.symlink.assert_not_called()

    def assert_installed(self, enabled):
        args = self.args("--enable") if enabled else self.args()
        code, result = self.invoke(args)
        self.assertEqual(code, 0)
        self.assertTrue(result["installed"])
        self.assertEqual(result["paused"], not enabled)
        self.assertEqual((self.state / "PAUSED").exists(), not enabled)
        receipt = poller.read_json(self.state / "installed.json")
        self.assertEqual(receipt["source_commit"], SHA)
        self.assertEqual(receipt["binary_sha256"], BINARY)
        self.assertTrue(receipt["bootstrap"])
        bootstrap = next(self.state.glob("bootstrap-*"))
        self.assertEqual({p.name for p in bootstrap.iterdir()}, set(receipt["tooling_hashes"]))
        self.symlink.assert_called_once_with(bootstrap, self.state / "current", target_is_directory=True)
        config = poller.read_json(self.state / "config.json")
        self.assertEqual(config["app_root"], str(self.app.resolve()))
        self.assertTrue(config["enabled"])
        agent = plistlib.loads(self.plist.read_bytes())
        self.assertEqual(agent["Label"], installer.LABEL)
        self.assertEqual(agent["StartInterval"], 300)
        self.assertTrue(agent["RunAtLoad"])
        self.assertEqual(agent["ProgramArguments"], [str(Path(sys.executable).absolute()), "-I",
                         str(self.state / "current/poller.py"), "--config", str(self.state / "config.json")])
        self.assertNotIn("GITHUB_TOKEN", agent["EnvironmentVariables"])
        self.launch.assert_called_once_with(["/bin/launchctl", "bootstrap", "gui/501", str(self.plist)],
                                             stdin=installer.subprocess.DEVNULL, capture_output=True, timeout=20)

    def test_default_install_is_paused_with_recorded_receipt_and_launch_agent(self):
        self.assert_installed(False)

    def test_enable_install_is_unpaused(self):
        self.assert_installed(True)

    def test_existing_install_or_launch_agent_is_preserved(self):
        for existing in (self.state, self.plist):
            with self.subTest(existing=existing):
                if existing == self.state:
                    existing.mkdir()
                else:
                    existing.write_bytes(b"original plist")
                before = self.snapshot()
                with self.assertRaisesRegex(poller.Failure, "already-installed|already-exists"):
                    self.invoke(self.args())
                self.assertEqual(before, self.snapshot())
                existing.rmdir() if existing.is_dir() else existing.unlink()
        self.launch.assert_not_called()

    def test_pause_resume_and_status_do_not_launch_or_rewrite_receipt(self):
        self.state.mkdir()
        poller.save(self.state / "config.json", {"schema": 1})
        poller.save(self.state / "status.json", {"state": "up-to-date", "commit": SHA})
        receipt = self.state / "installed.json"
        poller.save(receipt, self.baseline)
        original = receipt.read_bytes()
        _, paused = self.invoke(["install.py", "pause"])
        self.assertTrue(paused["paused"])
        _, status = self.invoke(["install.py", "status"])
        self.assertTrue(status["installed"])
        self.assertTrue(status["paused"])
        self.assertEqual(status["last_poll"]["state"], "up-to-date")
        _, resumed = self.invoke(["install.py", "resume"])
        self.assertFalse(resumed["paused"])
        self.assertEqual(receipt.read_bytes(), original)
        self.launch.assert_not_called()

    def test_pause_resume_require_existing_install(self):
        for command in ("pause", "resume"):
            with self.subTest(command=command), self.assertRaisesRegex(poller.Failure, "not-installed"):
                self.invoke(["install.py", command])
        self.assertFalse(self.state.exists())
        self.launch.assert_not_called()

    def test_status_before_install_is_read_only(self):
        before = self.snapshot()
        _, status = self.invoke(["install.py", "status"])
        self.assertEqual(status, {"installed": False, "paused": False})
        self.assertEqual(before, self.snapshot())
        self.launch.assert_not_called()


if __name__ == "__main__":
    unittest.main()
