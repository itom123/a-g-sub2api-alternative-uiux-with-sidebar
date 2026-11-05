import hashlib
import io
import json
from pathlib import Path
import struct
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from github_api import GitHubError
from package_release import build, create_package, digest, inventory, save
from publish_release import publish, read_package

SHA = "a" * 40
BASE = "b" * 40
REPO = "owner/sub2api"


class PackageFixture:
    def __init__(self, root):
        self.root = root
        self.source = root / "source"
        version = self.source / "backend/cmd/server/VERSION"
        version.parent.mkdir(parents=True)
        version.write_text("0.2.19\n", encoding="utf-8")
        migrations = self.source / "backend/migrations"
        migrations.mkdir()
        (migrations / "001_test.sql").write_bytes(b"\r\nSELECT 1;\r\n")
        tools = self.source / "deploy/production"
        tools.mkdir(parents=True)
        for name in ("controller.py", "prepare_canary.py", "activate_prepared.py", "github_api.py", "poller.py", "install.py"):
            (tools / name).write_text("# tool: " + name + "\n", encoding="utf-8")
        (tools / "tests").mkdir()
        (tools / "tests/test_private.py").write_text("# exclude me\n", encoding="utf-8")
        self.frontend = root / "frontend"
        (self.frontend / "assets").mkdir(parents=True)
        (self.frontend / "index.html").write_bytes(b'<script src="/assets/app.js"></script>')
        (self.frontend / "assets/app.js").write_bytes(b"console.log('sub2api');")
        self.binary = root / "binary"
        self.binary.write_bytes(b"\xcf\xfa\xed\xfe" + struct.pack("<I", 0x0100000C) + b"binary-" + SHA.encode()
                                + b"".join(p.read_bytes() for p in sorted(self.frontend.rglob("*")) if p.is_file()))

    def build(self, name="output"):
        output = self.root / name
        result = create_package(self.source, self.binary, self.frontend, output, REPO, SHA,
                                "2026-11-05T00:00:00Z", inventory(self.source))
        return output, result


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.fixture = PackageFixture(Path(self.temporary.name))

    def test_complete_manifest_binds_one_source_and_all_payloads(self):
        output, deployment = self.fixture.build()
        manifest = json.loads((output / "payload/manifest.json").read_text())
        self.assertEqual(deployment["archive_sha256"], digest(output / deployment["archive_name"]))
        self.assertEqual(deployment["manifest_sha256"], digest(output / "payload/manifest.json"))
        self.assertEqual(manifest["new_commit"], SHA)
        self.assertEqual(manifest["platform"], "darwin/arm64")
        self.assertEqual(manifest["frontend_files"], inventory(self.fixture.frontend))
        self.assertEqual(manifest["embedded_migrations"]["001_test.sql"], hashlib.sha256(b"SELECT 1;").hexdigest())
        self.assertEqual(manifest["controller_sha256"], manifest["tools_files"]["controller.py"])
        self.assertIn("poller.py", manifest["tools_files"])
        self.assertFalse(any("tests" in name for name in manifest["tools_files"]))
        self.assertFalse(any(key.startswith("old_") for key in manifest))

    def test_identical_input_produces_identical_archives(self):
        output1, first = self.fixture.build("first")
        output2, second = self.fixture.build("second")
        self.assertEqual(first, second)
        self.assertEqual((output1 / first["archive_name"]).read_bytes(), (output2 / second["archive_name"]).read_bytes())

    def test_archive_contains_only_regular_bounded_relative_payload(self):
        output, deployment = self.fixture.build()
        with tarfile.open(output / deployment["archive_name"], "r:gz") as archive:
            members = archive.getmembers()
        self.assertTrue(all(m.isfile() and not m.name.startswith("/") and ".." not in Path(m.name).parts for m in members))
        self.assertIn("bin/sub2api", {m.name for m in members})
        self.assertTrue(all(m.mtime == 0 and m.uid == 0 and m.gid == 0 for m in members))
        binary = next(m for m in members if m.name == "bin/sub2api")
        self.assertEqual(binary.mode, 0o755)

    def test_wrong_cpu_architecture_rejected_even_for_macho_binary(self):
        data = self.fixture.binary.read_bytes()
        self.fixture.binary.write_bytes(data[:4] + struct.pack("<I", 0x01000007) + data[8:])
        with self.assertRaisesRegex(ValueError, "Mach-O"):
            self.fixture.build()

    def test_binary_missing_source_commit_rejected(self):
        self.fixture.binary.write_bytes(self.fixture.binary.read_bytes().replace(SHA.encode(), BASE.encode()))
        with self.assertRaisesRegex(ValueError, "commit"):
            self.fixture.build()

    def test_different_embedded_frontend_rejected(self):
        (self.fixture.frontend / "assets/app.js").write_bytes(b"changed after embed")
        with self.assertRaisesRegex(ValueError, "Embedded frontend"):
            self.fixture.build()

    def test_package_does_not_overwrite_existing_output(self):
        self.fixture.build()
        with self.assertRaisesRegex(ValueError, "must be new"):
            self.fixture.build()

    def test_missing_controller_rejected(self):
        (self.fixture.source / "deploy/production/controller.py").unlink()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.fixture.build()

    def test_untracked_tool_or_migration_never_enters_package(self):
        for name in ("deploy/production/untracked.py", "backend/migrations/999_untracked.sql"):
            with self.subTest(name=name):
                source_files = inventory(self.fixture.source)
                untracked = self.fixture.source / name
                untracked.write_text("untracked source", encoding="utf-8")
                try:
                    with self.assertRaisesRegex(ValueError, "not bound to tracked source"):
                        create_package(self.fixture.source, self.fixture.binary, self.fixture.frontend,
                                       self.fixture.root / "untracked-output", REPO, SHA,
                                       "2026-11-05T00:00:00Z", source_files)
                finally:
                    untracked.unlink()

    def test_source_file_change_after_inventory_is_rejected(self):
        source_files = inventory(self.fixture.source)
        (self.fixture.source / "deploy/production/poller.py").write_text("modified", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "not bound to tracked source"):
            create_package(self.fixture.source, self.fixture.binary, self.fixture.frontend,
                           self.fixture.root / "modified-output", REPO, SHA,
                           "2026-11-05T00:00:00Z", source_files)

    def test_build_refuses_untracked_source_before_starting_dependencies(self):
        arguments = mock.Mock(source_root=str(self.fixture.source), sha=SHA)
        with mock.patch("package_release.run", side_effect=[SHA, "?? deploy/production/untracked.py"]) as command, \
                mock.patch("package_release.subprocess.run") as dependencies:
            with self.assertRaisesRegex(ValueError, "source must be clean"):
                build(arguments)
        self.assertEqual(command.call_args.args[0], ["git", "status", "--porcelain=v1", "--untracked-files=normal"])
        dependencies.assert_not_called()

    def test_missing_installer_rejected(self):
        (self.fixture.source / "deploy/production/install.py").unlink()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.fixture.build()

    def test_missing_puller_rejected(self):
        (self.fixture.source / "deploy/production/poller.py").unlink()
        with self.assertRaisesRegex(ValueError, "incomplete"):
            self.fixture.build()

    def rewrite_archive(self, output, deployment, change):
        archive = output / deployment["archive_name"]
        members = []
        with tarfile.open(archive, "r:gz") as bundle:
            for member in bundle.getmembers():
                members.append((member, bundle.extractfile(member).read()))
        with tarfile.open(archive, "w:gz") as bundle:
            for member, content in change(members):
                member.size = len(content)
                bundle.addfile(member, io.BytesIO(content))
        deployment.update(archive_sha256=digest(archive), archive_size=archive.stat().st_size)
        (output / "deployment.json").unlink()
        save(output / "deployment.json", deployment)

    def test_publisher_verifies_inner_file_checksums_after_outer_checksums(self):
        output, deployment = self.fixture.build()
        def change(members):
            return [(member, b"modified payload" if member.name == "frontend/assets/app.js" else content)
                    for member, content in members]
        self.rewrite_archive(output, deployment, change)
        with self.assertRaisesRegex(GitHubError, "file-hash-mismatch"):
            read_package(output, REPO, SHA)

    def test_publisher_rejects_unlisted_archive_member(self):
        output, deployment = self.fixture.build()
        def change(members):
            members.append((tarfile.TarInfo("tools/unlisted.py"), b"# extra file"))
            return members
        self.rewrite_archive(output, deployment, change)
        with self.assertRaisesRegex(GitHubError, "inventory-mismatch"):
            read_package(output, REPO, SHA)

    def test_publisher_detects_tampered_archive(self):
        output, deployment = self.fixture.build()
        with (output / deployment["archive_name"]).open("ab") as stream:
            stream.write(b"tampered")
        with self.assertRaisesRegex(GitHubError, "checksum"):
            read_package(output, REPO, SHA)


class PublishClient:
    repo = REPO
    prefix = "/repos/" + REPO

    def __init__(self, assets, existing=False, draft=False):
        self.paths = assets
        self.calls = []
        self.ready = True
        self.current = SHA
        self.release_data = {"id": 10, "tag_name": "production-" + SHA, "target_commitish": SHA,
                             "draft": draft, "prerelease": True,
                             "upload_url": "https://uploads.github.com/repos/owner/sub2api/releases/10/assets{?name,label}",
                             "assets": []}
        if existing:
            self.release_data["assets"] = [self.asset(name, path) for name, path in assets.items()]
        self.exists = existing

    def asset(self, name, path):
        return {"name": name, "state": "uploaded", "size": path.stat().st_size,
                "digest": "sha256:" + digest(path)}

    def main_sha(self):
        return self.current

    def gate(self, sha):
        return {"ready": self.ready}

    def resolve_tag(self, sha):
        return SHA

    def request(self, path, method="GET", data=None, raw=None, content_type=None):
        self.calls.append((method, path, data))
        if method == "GET":
            if not self.exists:
                error = GitHubError("not found")
                error.status = 404
                raise error
            return self.release_data
        if method == "POST" and path == self.prefix + "/releases":
            self.exists = True
            self.release_data.update(data)
            return self.release_data
        if method == "POST" and path.startswith("https://uploads.github.com/"):
            import urllib.parse
            name = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)["name"][0]
            self.assert_upload_bytes(name, raw)
            asset = self.asset(name, self.paths[name])
            self.release_data["assets"].append(asset)
            return asset
        if method == "PATCH":
            self.release_data.update(data)
            return self.release_data
        raise AssertionError(path)

    def assert_upload_bytes(self, name, raw):
        if self.paths[name].read_bytes() != raw:
            raise AssertionError("Uploaded bytes differ")


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.fixture = PackageFixture(Path(self.temporary.name))
        self.output, self.deployment = self.fixture.build()
        self.assets = {"deployment.json": self.output / "deployment.json",
                       self.deployment["archive_name"]: self.output / self.deployment["archive_name"]}

    def test_first_publish_creates_draft_uploads_then_publishes(self):
        client = PublishClient(self.assets)
        result = publish(client, self.output, SHA)
        self.assertTrue(result["published"])
        self.assertEqual(client.calls[1][2]["draft"], True)
        self.assertEqual([call[0] for call in client.calls], ["GET", "POST", "POST", "POST", "PATCH"])
        self.assertFalse(client.release_data["draft"])
        self.assertEqual(client.release_data["make_latest"], "false")

    def test_existing_identical_release_is_read_only_noop(self):
        client = PublishClient(self.assets, existing=True)
        self.assertTrue(publish(client, self.output, SHA)["published"])
        self.assertEqual([call[0] for call in client.calls], ["GET"])

    def test_changed_existing_asset_is_never_overwritten(self):
        client = PublishClient(self.assets, existing=True)
        client.release_data["assets"][0]["digest"] = "sha256:" + "0" * 64
        with self.assertRaisesRegex(GitHubError, "never overwrite"):
            publish(client, self.output, SHA)
        self.assertEqual([call[0] for call in client.calls], ["GET"])

    def test_failed_gate_or_superseded_main_never_creates_release(self):
        for ready, current in ((False, SHA), (True, BASE)):
            client = PublishClient(self.assets)
            client.ready, client.current = ready, current
            with self.subTest(ready=ready, current=current), self.assertRaises(GitHubError):
                publish(client, self.output, SHA)
            self.assertEqual(client.calls, [])

    def test_existing_wrong_tag_is_rejected_before_release_writes(self):
        client = PublishClient(self.assets)
        client.resolve_tag = lambda sha: BASE
        with self.assertRaisesRegex(GitHubError, "tag already points"):
            publish(client, self.output, SHA)
        self.assertEqual(client.calls, [])

    def test_gates_rechecked_before_draft_becomes_visible(self):
        client = PublishClient(self.assets)
        results = iter([True, False])
        client.gate = lambda sha: {"ready": next(results)}
        with self.assertRaisesRegex(GitHubError, "before publication"):
            publish(client, self.output, SHA)
        self.assertEqual([call[0] for call in client.calls], ["GET", "POST", "POST", "POST"])
        self.assertTrue(client.release_data["draft"])

    def test_partial_draft_can_resume_but_published_release_cannot(self):
        for draft in (True, False):
            client = PublishClient(self.assets, existing=True, draft=draft)
            client.release_data["assets"].pop()
            if draft:
                self.assertTrue(publish(client, self.output, SHA)["published"])
                self.assertEqual([call[0] for call in client.calls], ["GET", "POST", "PATCH"])
            else:
                with self.assertRaisesRegex(GitHubError, "never mutate"):
                    publish(client, self.output, SHA)


if __name__ == "__main__":
    unittest.main()
