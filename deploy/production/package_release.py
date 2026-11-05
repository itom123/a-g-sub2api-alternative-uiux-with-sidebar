#!/usr/bin/env python3
"""Build an exact GitHub main commit into a content-addressed Mac package."""
from __future__ import annotations

import argparse
import datetime as dt
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import struct
import tarfile

from github_api import GitHubError, REPOSITORY, require_sha


def digest(path):
    result = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def canonical(data):
    return json.dumps(data, sort_keys=True, separators=(",", ":")).encode("utf-8")


def inventory(root):
    root = Path(root)
    result = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ValueError("Package input contains a link or special file")
        if path.is_file():
            result[path.relative_to(root).as_posix()] = digest(path)
    return result


def save(path, data):
    with Path(path).open("x", encoding="utf-8", newline="\n") as output:
        json.dump(data, output, sort_keys=True, indent=2)
        output.write("\n")


def run(args, cwd):
    return subprocess.run(args, cwd=cwd, check=True, stdin=subprocess.DEVNULL,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.decode("utf-8", "replace").strip()


def source_inventory(root):
    names = run(["git", "ls-tree", "-r", "--name-only", "HEAD"], root).splitlines()
    result = {}
    for name in names:
        path = root / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("Tracked source contains a link, submodule or missing file")
        result[name] = digest(path)
    return result


def create_package(source, binary, frontend, output, repository, sha, build_date, source_files):
    require_sha(sha)
    if not REPOSITORY.fullmatch(repository):
        raise ValueError("Invalid repository")
    source, binary, frontend, output = map(Path, (source, binary, frontend, output))
    if output.exists():
        raise ValueError("Package output directory must be new")
    header = binary.read_bytes()[:8] if binary.is_file() and not binary.is_symlink() else b""
    if len(header) != 8 or header[:4] != b"\xcf\xfa\xed\xfe" or struct.unpack("<I", header[4:])[0] != 0x0100000C:
        raise ValueError("Expected a regular darwin/arm64 Mach-O binary")
    binary_bytes = binary.read_bytes()
    if sha.encode("ascii") not in binary_bytes:
        raise ValueError("Binary is missing its exact source commit")
    version = (source / "backend/cmd/server/VERSION").read_text().strip()
    if not version or any(c.isspace() for c in version):
        raise ValueError("Invalid application version")
    frontend_files = inventory(frontend)
    if "index.html" not in frontend_files:
        raise ValueError("Built frontend index is missing")
    for name in frontend_files:
        if (frontend / name).read_bytes() not in binary_bytes:
            raise ValueError("Embedded frontend differs from packaged frontend")
    tools = source / "deploy/production"
    tool_paths = sorted(tools.glob("*.py"))
    required = {"controller.py", "prepare_canary.py", "activate_prepared.py", "github_api.py", "poller.py", "install.py"}
    if not required.issubset({p.name for p in tool_paths}):
        raise ValueError("Production tools are incomplete")
    if any(p.is_symlink() for p in tool_paths):
        raise ValueError("Production tools contain a link")
    migrations = source / "backend/migrations"
    migration_paths = sorted(migrations.glob("*.sql"))
    if not migration_paths or any(p.is_symlink() for p in migration_paths):
        raise ValueError("Migration inventory is empty or contains a link")
    for path in tool_paths + migration_paths:
        name = path.relative_to(source).as_posix()
        if source_files.get(name) != digest(path):
            raise ValueError("Package input is not bound to tracked source: " + name)
    output.mkdir(parents=True)
    payload = output / "payload"
    (payload / "bin").mkdir(parents=True)
    shutil.copyfile(binary, payload / "bin/sub2api")
    shutil.copytree(frontend, payload / "frontend")
    (payload / "migrations").mkdir()
    (payload / "tools").mkdir()
    for path in migration_paths:
        shutil.copyfile(path, payload / "migrations" / path.name)
    for path in tool_paths:
        shutil.copyfile(path, payload / "tools" / path.name)
    migration_files = inventory(payload / "migrations")
    embedded = {}
    for path in sorted((payload / "migrations").glob("*.sql")):
        text = path.read_bytes().decode("utf-8").strip()
        if text:
            embedded[path.name] = hashlib.sha256(text.encode("utf-8")).hexdigest()
    tree_hash = hashlib.sha256(canonical(source_files)).hexdigest()
    identity = {"version": version, "commit": sha, "date": build_date, "build_type": "source",
                "goos": "darwin", "goarch": "arm64", "cgo_enabled": "0", "tags": ["embed"],
                "source_tree_sha256": tree_hash}
    manifest = {"schema": 1, "repository": repository, "platform": "darwin/arm64", "new_commit": sha,
                "binary_sha256": digest(payload / "bin/sub2api"), "frontend_files": frontend_files,
                "migration_files": migration_files, "embedded_migrations": embedded,
                "tools_files": inventory(payload / "tools"),
                "controller_sha256": digest(payload / "tools/controller.py"),
                "source_tree_sha256": tree_hash, "build_identity": identity}
    save(payload / "manifest.json", manifest)
    archive_name = "sub2api-production-" + sha + "-darwin-arm64.tar.gz"
    archive = output / archive_name
    with archive.open("xb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as bundle:
            for path in sorted(payload.rglob("*")):
                if not path.is_file():
                    continue
                data = path.read_bytes()
                info = tarfile.TarInfo(path.relative_to(payload).as_posix())
                info.size, info.mtime, info.uid, info.gid = len(data), 0, 0, 0
                info.mode = 0o755 if info.name == "bin/sub2api" else 0o644
                bundle.addfile(info, io.BytesIO(data))
    deployment = {"schema": 1, "repository": repository, "new_commit": sha, "platform": "darwin/arm64",
                  "archive_name": archive_name, "archive_sha256": digest(archive), "archive_size": archive.stat().st_size,
                  "manifest_sha256": digest(payload / "manifest.json"), "source_tree_sha256": tree_hash, "version": version}
    save(output / "deployment.json", deployment)
    return deployment


def build(args):
    root = Path(args.source_root).resolve()
    sha = require_sha(args.sha)
    if run(["git", "rev-parse", "HEAD"], root) != sha:
        raise ValueError("Checkout does not match the requested source SHA")
    if run(["git", "status", "--porcelain=v1", "--untracked-files=normal"], root):
        raise ValueError("Tracked source must be clean")
    files = source_inventory(root)
    commit_date = run(["git", "show", "-s", "--format=%cI", "HEAD"], root)
    build_date = dt.datetime.fromisoformat(commit_date).astimezone(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    version = (root / "backend/cmd/server/VERSION").read_text().strip()
    env = dict(os.environ, GOOS="darwin", GOARCH="arm64", CGO_ENABLED="0", GOWORK="off")
    pnpm = shutil.which("pnpm")
    if not pnpm:
        raise ValueError("pnpm executable is unavailable")
    subprocess.run([pnpm, "install", "--frozen-lockfile"], cwd=root / "frontend", env=env, check=True)
    subprocess.run([pnpm, "run", "build"], cwd=root / "frontend", env=env, check=True)
    temporary = root / "backend" / (".production-binary-" + sha)
    if temporary.exists():
        raise ValueError("Temporary package binary already exists")
    try:
        ldflags = "-s -w -X main.Version=" + version + " -X main.Commit=" + sha + " -X main.Date=" + build_date + " -X main.BuildType=source"
        subprocess.run(["go", "build", "-mod=readonly", "-tags=embed", "-trimpath", "-buildvcs=false", "-ldflags=" + ldflags,
                        "-o", str(temporary), "./cmd/server"], cwd=root / "backend", env=env, check=True)
        if source_inventory(root) != files:
            raise ValueError("Build changed tracked source")
        return create_package(root, temporary, root / "backend/internal/web/dist", Path(args.output).resolve(),
                              args.repository, sha, build_date, files)
    finally:
        temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    command = commands.add_parser("build")
    command.add_argument("--sha", required=True)
    command.add_argument("--repository", required=True)
    command.add_argument("--output", required=True)
    command.add_argument("--source-root", default=str(Path(__file__).resolve().parents[2]))
    args = parser.parse_args()
    try:
        print(json.dumps(build(args), sort_keys=True))
    except (ValueError, GitHubError, subprocess.CalledProcessError) as exc:
        raise SystemExit("Production package failed: " + str(exc)) from None


if __name__ == "__main__":
    main()
