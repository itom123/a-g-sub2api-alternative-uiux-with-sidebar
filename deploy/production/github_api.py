#!/usr/bin/env python3
"""Small GitHub client shared by the production publisher and host puller.

Credentials are used only in request headers and are never persisted. Public
release downloads need no token. All candidate checks fail closed.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import urllib.error
import urllib.parse
import urllib.request
import uuid


class GitHubError(RuntimeError):
    pass


SHA = re.compile(r"[0-9a-f]{40}\Z")
REPOSITORY = re.compile(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+\Z")
REQUIRED_WORKFLOWS = (
    ("CI", "backend-ci.yml"),
    ("Fork guard", "fork-guard.yml"),
    ("Security Scan", "security-scan.yml"),
)
DOWNLOAD_HOSTS = frozenset(("github.com", "api.github.com", "objects.githubusercontent.com",
                            "release-assets.githubusercontent.com"))


def require_sha(value):
    if not isinstance(value, str) or not SHA.fullmatch(value):
        raise GitHubError("Expected a full lowercase commit SHA")
    return value


class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        parsed = urllib.parse.urlsplit(newurl)
        if parsed.scheme != "https" or parsed.hostname not in DOWNLOAD_HOSTS or parsed.username or parsed.password:
            raise GitHubError("Refused an untrusted download redirect")
        result = super().redirect_request(req, fp, code, msg, headers, newurl)
        if result is not None and parsed.hostname != urllib.parse.urlsplit(req.full_url).hostname:
            result.remove_header("Authorization")
        return result


class GitHub:
    def __init__(self, repo, token=None, api_base="https://api.github.com"):
        if not isinstance(repo, str) or not REPOSITORY.fullmatch(repo) or any(p in (".", "..") for p in repo.split("/")):
            raise GitHubError("Invalid GitHub repository")
        self.repo = repo
        self.token = os.environ.get("GITHUB_TOKEN", "") if token is None else token
        self.api_base = api_base.rstrip("/")
        self.opener = urllib.request.build_opener(SafeRedirect())

    @property
    def prefix(self):
        return "/repos/" + self.repo

    def request(self, path, *, method="GET", data=None, raw=None, content_type=None):
        if path.startswith("/"):
            url = self.api_base + path
        else:
            parsed = urllib.parse.urlsplit(path)
            if (parsed.scheme != "https" or parsed.hostname not in ("api.github.com", "uploads.github.com")
                    or parsed.username or parsed.password or parsed.fragment):
                raise GitHubError("Refused an untrusted GitHub API URL")
            url = path
        headers = {"Accept": "application/vnd.github+json", "User-Agent": "sub2api-production",
                   "X-GitHub-Api-Version": "2022-11-28"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        if raw is not None:
            body = raw
            headers["Content-Type"] = content_type or "application/octet-stream"
        elif data is not None:
            body = json.dumps(data, sort_keys=True).encode("utf-8")
            headers["Content-Type"] = "application/json"
        else:
            body = None
        req = urllib.request.Request(url, data=body, headers=headers, method=method)
        try:
            with self.opener.open(req, timeout=30) as response:
                result = response.read(8 * 1024 * 1024 + 1)
                if len(result) > 8 * 1024 * 1024:
                    raise GitHubError("GitHub JSON response exceeded bounds")
                return json.loads(result) if result else None
        except urllib.error.HTTPError as exc:
            error = GitHubError("GitHub API returned HTTP " + str(exc.code))
            error.status = exc.code
            raise error from None
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as exc:
            raise GitHubError("GitHub request failed: " + type(exc).__name__) from None

    def main_sha(self):
        result = self.request(self.prefix + "/branches/main")
        try:
            return require_sha(result["commit"]["sha"])
        except (TypeError, KeyError):
            raise GitHubError("Invalid main branch response") from None

    def gate(self, sha):
        require_sha(sha)
        checks = []
        for name, filename in REQUIRED_WORKFLOWS:
            query = urllib.parse.urlencode({"branch": "main", "event": "push", "head_sha": sha, "per_page": 100})
            result = self.request(self.prefix + "/actions/workflows/" + filename + "/runs?" + query)
            if not isinstance(result, dict) or not isinstance(result.get("workflow_runs"), list):
                raise GitHubError("Invalid workflow runs response")
            runs = [r for r in result["workflow_runs"] if r.get("head_sha") == sha]
            if not runs:
                checks.append({"workflow": name, "path": ".github/workflows/" + filename,
                               "ready": False, "reason": "missing", "status": "missing"})
                continue
            # Do not select an older successful run over a newer failure or an
            # in-progress rerun of the same SHA.
            run = max(runs, key=lambda r: (int(r.get("run_number", 0)), int(r.get("id", 0)), int(r.get("run_attempt", 1))))
            origin = (run.get("head_repository") or {}).get("full_name", "")
            valid_origin = origin.lower() == self.repo.lower()
            path = run.get("path", "").split("@", 1)[0]
            valid_context = (valid_origin and run.get("event") == "push" and run.get("head_branch") == "main"
                             and path == ".github/workflows/" + filename)
            ready = valid_context and run.get("status") == "completed" and run.get("conclusion") == "success"
            checks.append({"workflow": name, "path": path, "run_id": run.get("id"),
                           "run_attempt": run.get("run_attempt", 1), "status": run.get("status"),
                           "conclusion": run.get("conclusion"), "ready": ready,
                           "reason": "success" if ready else ("untrusted_context" if not valid_context else "not_successful")})
        ready = all(check["ready"] for check in checks)
        return {"sha": sha, "ready": ready, "reason": "all_checks_passed" if ready else "required_checks_not_successful", "checks": checks}

    def resolve_tag(self, sha):
        require_sha(sha)
        result = self.request(self.prefix + "/git/ref/tags/production-" + sha)
        obj = result.get("object") if isinstance(result, dict) else None
        for _ in range(5):
            if not isinstance(obj, dict):
                raise GitHubError("Invalid production tag response")
            current = require_sha(obj.get("sha"))
            if obj.get("type") == "commit":
                return current
            if obj.get("type") != "tag":
                raise GitHubError("Production tag does not resolve to a commit")
            result = self.request(self.prefix + "/git/tags/" + current)
            obj = result.get("object") if isinstance(result, dict) else None
        raise GitHubError("Production tag nesting exceeded bounds")

    def is_descendant(self, base, sha):
        require_sha(base)
        require_sha(sha)
        result = self.request(self.prefix + "/compare/" + base + "..." + sha)
        if not isinstance(result, dict):
            raise GitHubError("Invalid commit comparison response")
        merge_base = result.get("merge_base_commit") or {}
        return result.get("status") in ("ahead", "identical") and merge_base.get("sha") == base

    def release(self, sha):
        require_sha(sha)
        try:
            release = self.request(self.prefix + "/releases/tags/production-" + sha)
        except GitHubError as exc:
            if getattr(exc, "status", None) == 404:
                return None
            raise
        if not isinstance(release, dict) or release.get("tag_name") != "production-" + sha:
            raise GitHubError("Invalid production release response")
        if release.get("draft"):
            return None
        if self.resolve_tag(sha) != sha:
            raise GitHubError("Production release tag points to another commit")
        if not isinstance(release.get("assets"), list):
            raise GitHubError("Production release asset list is invalid")
        return release

    def download(self, url, destination, expected_sha256=None, max_bytes=512 * 1024 * 1024):
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme != "https" or parsed.hostname not in DOWNLOAD_HOSTS or parsed.username or parsed.password or parsed.fragment:
            raise GitHubError("Refused an untrusted download URL")
        if expected_sha256 is not None and not re.fullmatch(r"[0-9a-f]{64}", expected_sha256):
            raise GitHubError("Invalid expected SHA256")
        path = Path(destination)
        if path.exists() or path.is_symlink():
            raise GitHubError("Download destination already exists")
        headers = {"User-Agent": "sub2api-production"}
        # Browser release asset URLs are intentionally anonymous. API asset
        # downloads may need a read token; redirects remove it on host changes.
        if parsed.hostname == "api.github.com":
            headers["Accept"] = "application/octet-stream"
            if self.token:
                headers["Authorization"] = "Bearer " + self.token
        req = urllib.request.Request(url, headers=headers)
        # Download into a unique adjacent temporary file. A killed poller may
        # leave this orphan, but never leaves a partial final asset that the next
        # polling cycle would mistake for a completed cached download.
        temporary = path.with_name("." + path.name + "." + uuid.uuid4().hex + ".download")
        created = False
        total = 0
        checksum = hashlib.sha256()
        try:
            with self.opener.open(req, timeout=120) as response:
                length = response.headers.get("Content-Length")
                if length is not None and int(length) > max_bytes:
                    raise GitHubError("Download exceeded size bound")
                with temporary.open("xb") as output:
                    created = True
                    temporary.chmod(0o600)
                    while True:
                        block = response.read(1024 * 1024)
                        if not block:
                            break
                        total += len(block)
                        if total > max_bytes:
                            raise GitHubError("Download exceeded size bound")
                        checksum.update(block)
                        output.write(block)
                    output.flush()
                    os.fsync(output.fileno())
            actual = checksum.hexdigest()
            if expected_sha256 is not None and actual != expected_sha256:
                raise GitHubError("Downloaded SHA256 does not match")
            # Hard-link publication is atomic and refuses to overwrite an
            # existing destination on both macOS and Windows.
            os.link(temporary, path)
            temporary.unlink()
            created = False
            return {"path": str(path), "sha256": actual, "size": total}
        except Exception as exc:
            if created:
                temporary.unlink(missing_ok=True)
            if isinstance(exc, GitHubError):
                raise
            raise GitHubError("Download failed: " + type(exc).__name__) from None
