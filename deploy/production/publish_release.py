#!/usr/bin/env python3
"""Publish one already-built production package without rebuilding its code."""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import tarfile
import tempfile
import urllib.parse

from github_api import GitHub, GitHubError, require_sha
from package_release import digest
from poller import Failure, MAX_ARCHIVE, extract_verified


def read_package(directory, repository, sha):
    directory = Path(directory)
    metadata = directory / "deployment.json"
    if metadata.is_symlink() or not metadata.is_file() or metadata.stat().st_size > 8 * 1024 * 1024:
        raise GitHubError("Deployment metadata must be a regular file")
    deployment = json.loads(metadata.read_text(encoding="utf-8"))
    expected_name = "sub2api-production-" + sha + "-darwin-arm64.tar.gz"
    if (deployment.get("schema") != 1 or deployment.get("repository") != repository
            or deployment.get("new_commit") != sha or deployment.get("platform") != "darwin/arm64"
            or deployment.get("archive_name") != expected_name):
        raise GitHubError("Deployment metadata identity does not match")
    for name in ("archive_sha256", "manifest_sha256", "source_tree_sha256"):
        if not re.fullmatch(r"[0-9a-f]{64}", deployment.get(name, "")):
            raise GitHubError("Deployment metadata checksum is invalid")
    archive = directory / expected_name
    if archive.is_symlink() or not archive.is_file() or digest(archive) != deployment["archive_sha256"]:
        raise GitHubError("Production archive checksum does not match")
    if archive.stat().st_size != deployment.get("archive_size") or archive.stat().st_size > MAX_ARCHIVE:
        raise GitHubError("Production archive size does not match")
    # The publisher and puller share the same full package verifier, so the
    # exact payload accepted for publication is also what the host can accept.
    try:
        with tempfile.TemporaryDirectory(prefix="sub2api-package-verify-") as temporary:
            extract_verified(archive, Path(temporary) / "payload", deployment, repository, sha)
    except Failure as exc:
        raise GitHubError("Production package verification failed: " + str(exc)) from None
    return deployment, {expected_name: archive, "deployment.json": metadata}


def existing_release(client, sha):
    try:
        return client.request(client.prefix + "/releases/tags/production-" + sha)
    except GitHubError as exc:
        if getattr(exc, "status", None) == 404:
            return None
        raise


def verify_asset(client, asset, path):
    if asset.get("state") != "uploaded" or asset.get("size") != path.stat().st_size:
        raise GitHubError("Existing release asset size or state differs")
    expected = digest(path)
    declared = asset.get("digest")
    if declared:
        if declared != "sha256:" + expected:
            raise GitHubError("Existing release asset checksum differs; never overwrite")
        return
    url = asset.get("url") or asset.get("browser_download_url")
    if not isinstance(url, str):
        raise GitHubError("Existing release asset URL is missing")
    with tempfile.TemporaryDirectory(prefix="sub2api-asset-verify-") as temporary:
        client.download(url, Path(temporary) / "asset", expected_sha256=expected,
                        max_bytes=path.stat().st_size)


def publish(client, directory, sha):
    sha = require_sha(sha)
    deployment, assets = read_package(directory, client.repo, sha)
    if client.main_sha() != sha:
        raise GitHubError("Refused to publish a superseded main commit")
    gate = client.gate(sha)
    if not gate["ready"]:
        raise GitHubError("Required checks did not all pass for this commit")
    tag = "production-" + sha
    # A pre-existing tag is immutable input: never publish against a tag that
    # already names another source commit, even when its draft target matches.
    try:
        tag_target = client.resolve_tag(sha)
    except GitHubError as exc:
        if getattr(exc, "status", None) != 404:
            raise
    else:
        if tag_target != sha:
            raise GitHubError("Production tag already points to another commit")
    release = existing_release(client, sha)
    if release is None:
        release = client.request(client.prefix + "/releases", method="POST", data={
            "tag_name": tag, "target_commitish": sha, "name": "Production " + sha[:12],
            "body": "Exact-commit Sub2API darwin/arm64 deployment package. Source: " + sha,
            "draft": True, "prerelease": True, "make_latest": "false"})
    if not isinstance(release, dict) or release.get("tag_name") != tag or not isinstance(release.get("id"), int):
        raise GitHubError("Invalid production release response")
    if release.get("draft"):
        if release.get("target_commitish") != sha:
            raise GitHubError("Existing draft release has another target")
    elif client.resolve_tag(sha) != sha:
        raise GitHubError("Existing production tag points to another commit")
    actual_assets = release.get("assets", [])
    if not isinstance(actual_assets, list):
        raise GitHubError("Invalid release asset response")
    names = [a.get("name") for a in actual_assets]
    if len(names) != len(set(names)) or not set(names).issubset(assets):
        raise GitHubError("Release contains duplicate or unexpected assets")
    for name, path in assets.items():
        found = [a for a in actual_assets if a.get("name") == name]
        if found:
            verify_asset(client, found[0], path)
            continue
        if not release.get("draft"):
            raise GitHubError("Published release is incomplete; never mutate it")
        upload = release.get("upload_url", "").split("{", 1)[0]
        if not upload:
            raise GitHubError("Release upload URL is missing")
        uploaded = client.request(upload + "?" + urllib.parse.urlencode({"name": name}), method="POST",
                                  raw=path.read_bytes(), content_type="application/json" if name.endswith(".json") else "application/gzip")
        verify_asset(client, uploaded, path)
    if release.get("draft"):
        if client.main_sha() != sha or not client.gate(sha)["ready"]:
            raise GitHubError("Main or its required checks changed before publication")
        release = client.request(client.prefix + "/releases/" + str(release["id"]), method="PATCH",
                                 data={"draft": False, "prerelease": True, "make_latest": "false"})
    if release.get("draft") or client.resolve_tag(sha) != sha:
        raise GitHubError("Published release identity did not verify")
    return {"published": True, "tag": tag, "release_id": release["id"],
            "new_commit": sha, "archive_sha256": deployment["archive_sha256"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--package", required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(publish(GitHub(args.repository), args.package, args.sha), sort_keys=True))
    except (GitHubError, ValueError, OSError, tarfile.TarError) as exc:
        raise SystemExit("Production publication failed: " + str(exc)) from None


if __name__ == "__main__":
    main()
