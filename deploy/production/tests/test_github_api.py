import hashlib
import io
import os
from pathlib import Path
import sys
import tempfile
import unittest
import urllib.request
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from github_api import GitHub, GitHubError, REQUIRED_WORKFLOWS, SafeRedirect

SHA = "a" * 40
BASE = "b" * 40
REPO = "owner/sub2api"


def successful_run(filename, **changes):
    result = {"id": 2, "run_number": 2, "run_attempt": 1, "path": ".github/workflows/" + filename,
              "head_sha": SHA, "head_repository": {"full_name": REPO}, "head_branch": "main",
              "event": "push", "status": "completed", "conclusion": "success"}
    result.update(changes)
    return result


class GateClient(GitHub):
    def __init__(self, overrides=None):
        super().__init__(REPO, token="")
        self.overrides = overrides or {}

    def request(self, path, **kwargs):
        for _, filename in REQUIRED_WORKFLOWS:
            if "/" + filename + "/runs?" in path:
                return {"workflow_runs": self.overrides.get(filename, [successful_run(filename)])}
        raise AssertionError(path)


class Response(io.BytesIO):
    def __init__(self, data, length=None):
        super().__init__(data)
        self.headers = {} if length is None else {"Content-Length": str(length)}


class DownloadOpener:
    def __init__(self, data, length=None):
        self.data, self.length, self.requests = data, length, []

    def open(self, request, timeout):
        self.requests.append(request)
        return Response(self.data, self.length)


class GitHubGateTests(unittest.TestCase):
    def test_all_required_exact_commit_checks_pass(self):
        result = GateClient().gate(SHA)
        self.assertTrue(result["ready"])
        self.assertEqual(len(result["checks"]), 3)

    def test_newer_failed_run_cannot_be_replaced_by_old_success(self):
        old = successful_run("backend-ci.yml", id=1, run_number=1)
        new = successful_run("backend-ci.yml", conclusion="failure")
        result = GateClient({"backend-ci.yml": [old, new]}).gate(SHA)
        self.assertFalse(result["ready"])
        self.assertEqual(result["checks"][0]["run_id"], 2)

    def test_in_progress_latest_rerun_blocks_earlier_attempt(self):
        first = successful_run("backend-ci.yml")
        rerun = successful_run("backend-ci.yml", run_attempt=2, status="in_progress", conclusion=None)
        result = GateClient({"backend-ci.yml": [first, rerun]}).gate(SHA)
        self.assertFalse(result["ready"])
        self.assertEqual(result["checks"][0]["run_attempt"], 2)

    def test_pending_missing_or_failed_gate_blocks(self):
        for rows in ([], [successful_run("security-scan.yml", status="queued", conclusion=None)],
                     [successful_run("security-scan.yml", conclusion="failure")]):
            with self.subTest(rows=rows):
                self.assertFalse(GateClient({"security-scan.yml": rows}).gate(SHA)["ready"])

    def test_wrong_commit_does_not_count(self):
        self.assertFalse(GateClient({"backend-ci.yml": [successful_run("backend-ci.yml", head_sha=BASE)]}).gate(SHA)["ready"])

    def test_fork_pr_branch_and_workflow_path_rejected(self):
        for changes in ({"head_repository": {"full_name": "attacker/sub2api"}},
                        {"event": "pull_request"}, {"head_branch": "feature"},
                        {"path": ".github/workflows/not-ci.yml"}):
            with self.subTest(changes=changes):
                client = GateClient({"backend-ci.yml": [successful_run("backend-ci.yml", **changes)]})
                self.assertFalse(client.gate(SHA)["ready"])

    def test_full_sha_required(self):
        with self.assertRaises(GitHubError):
            GateClient().gate("aaaa")

    def test_ancestry_rejects_rewind_divergence_and_wrong_merge_base(self):
        client = GitHub(REPO, token="")
        for status, merge, expected in (("ahead", BASE, True), ("identical", BASE, True),
                                         ("behind", BASE, False), ("diverged", BASE, False),
                                         ("ahead", SHA, False)):
            with self.subTest(status=status, merge=merge), patch.object(client, "request", return_value={"status": status, "merge_base_commit": {"sha": merge}}):
                self.assertEqual(client.is_descendant(BASE, SHA), expected)

    def test_annotated_release_tag_is_resolved(self):
        client = GitHub(REPO, token="")
        with patch.object(client, "request", side_effect=[{"object": {"type": "tag", "sha": BASE}},
                                                         {"object": {"type": "commit", "sha": SHA}}]):
            self.assertEqual(client.resolve_tag(SHA), SHA)

    def test_release_refuses_tag_pointing_to_other_commit(self):
        client = GitHub(REPO, token="")
        with patch.object(client, "request", return_value={"tag_name": "production-" + SHA, "draft": False, "assets": []}), \
                patch.object(client, "resolve_tag", return_value=BASE):
            with self.assertRaises(GitHubError):
                client.release(SHA)

    def test_draft_not_visible_to_puller(self):
        client = GitHub(REPO, token="")
        with patch.object(client, "request", return_value={"tag_name": "production-" + SHA, "draft": True}):
            self.assertIsNone(client.release(SHA))


class DownloadTests(unittest.TestCase):
    def test_anonymous_browser_download_and_checksum(self):
        client = GitHub(REPO, token="secret-test-value")
        client.opener = DownloadOpener(b"package", 7)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "package"
            result = client.download("https://github.com/owner/sub2api/releases/download/tag/package", path,
                                     hashlib.sha256(b"package").hexdigest())
            self.assertEqual(result["size"], 7)
            self.assertEqual(path.read_bytes(), b"package")
        self.assertFalse(client.opener.requests[0].has_header("Authorization"))

    def test_final_download_is_only_visible_after_verification(self):
        client = GitHub(REPO, token="")
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "package"
            class CheckingResponse(Response):
                def read(self, size=-1):
                    if path.exists():
                        raise AssertionError("Final package was visible before verification")
                    return super().read(size)
            class CheckingOpener:
                def open(self, request, timeout):
                    return CheckingResponse(b"package")
            client.opener = CheckingOpener()
            result = client.download("https://github.com/package", path,
                                     hashlib.sha256(b"package").hexdigest())
            self.assertEqual(result["size"], 7)
            self.assertEqual(list(Path(temporary).iterdir()), [path])

    def test_abandoned_partial_temporary_file_does_not_block_retry(self):
        client = GitHub(REPO, token="")
        client.opener = DownloadOpener(b"complete")
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "package"
            orphan = Path(temporary) / ".package.interrupted.download"
            orphan.write_bytes(b"partial")
            client.download("https://github.com/package", path,
                            hashlib.sha256(b"complete").hexdigest())
            self.assertEqual(path.read_bytes(), b"complete")
            self.assertEqual(orphan.read_bytes(), b"partial")

    def test_bad_checksum_and_oversize_leave_no_file(self):
        for limit, expected, length in ((100, "0" * 64, None), (3, None, None), (3, None, 7)):
            client = GitHub(REPO, token="")
            client.opener = DownloadOpener(b"package", length)
            with self.subTest(limit=limit, expected=expected), tempfile.TemporaryDirectory() as temporary:
                path = Path(temporary) / "package"
                with self.assertRaises(GitHubError):
                    client.download("https://github.com/owner/repo/package", path, expected, limit)
                self.assertFalse(path.exists())

    def test_existing_destination_is_never_overwritten(self):
        client = GitHub(REPO, token="")
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "existing"
            path.write_bytes(b"keep")
            with self.assertRaises(GitHubError):
                client.download("https://github.com/package", path)
            self.assertEqual(path.read_bytes(), b"keep")

    def test_untrusted_download_url_rejected(self):
        for url in ("http://github.com/a", "https://attacker.example/a", "https://github.com@attacker.example/a",
                    "https://user:password@github.com/a"):
            with self.subTest(url=url), self.assertRaises(GitHubError):
                GitHub(REPO, token="").download(url, "/unused")

    def test_redirect_strips_token_on_host_change(self):
        request = urllib.request.Request("https://api.github.com/repos/owner/repo/releases/assets/1",
                                         headers={"Authorization": "Bearer test-only"})
        redirected = SafeRedirect().redirect_request(request, None, 302, "Found", {},
                                                       "https://release-assets.githubusercontent.com/signed")
        self.assertFalse(redirected.has_header("Authorization"))

    def test_untrusted_redirect_is_rejected(self):
        request = urllib.request.Request("https://github.com/a")
        with self.assertRaises(GitHubError):
            SafeRedirect().redirect_request(request, None, 302, "Found", {}, "https://attacker.example/a")

    def test_environment_token_not_persisted(self):
        with patch.dict(os.environ, {"GITHUB_TOKEN": "test-only"}):
            client = GitHub(REPO)
        self.assertEqual(client.token, "test-only")


if __name__ == "__main__":
    unittest.main()
