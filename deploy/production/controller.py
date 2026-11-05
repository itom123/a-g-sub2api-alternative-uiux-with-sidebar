#!/usr/bin/env python3
"""Detached, binary/frontend-only deployment. Run with python3 -I; --help is the interface.

Production requires macOS and a reviewed config/evidence file. There is no SSH,
service kickstart, database operation, shell evaluation, or archive extraction.
Fake mode uses a marked private host, real attempt child processes and no sockets.
"""
import argparse
import contextlib
import concurrent.futures
import hashlib
import gzip
from html.parser import HTMLParser
import http.client
import io
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import urlsplit
import uuid

SCRIPT = Path(__file__).resolve()
TERMINAL = {"committed", "rolled_back", "rollback_failed", "prepare_failed", "aborted_untouched"}
LIVE_TIMING = dict(activation=240, recovery=180, ack_delay=8, arm_wait=30,
                   arm_expiry=90, probe=45, interval=2, poll=0.25, stable=3)
FAKE_TIMING = dict(activation=12, recovery=10, ack_delay=0.25, arm_wait=10,
                   arm_expiry=20, probe=2, interval=0.04, poll=0.04, stable=2)


class Failure(Exception):
    pass


class NotReady(Failure):
    pass


def monotonic():
    # Python <3.10 on macOS reports process-relative monotonic(). Shared
    # deadlines need the kernel clock, identical in runner/watchdog/probes.
    if sys.platform == "darwin":
        return time.clock_gettime(time.CLOCK_MONOTONIC)
    return time.monotonic()


def windowless_process_options():
    # DETACHED_PROCESS ignores CREATE_NO_WINDOW. A no-console child already
    # survives its parent; keep POSIX session detachment separate below.
    return {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}


def record_test_console():
    audit = os.environ.get("SUB2API_TEST_CONSOLE_AUDIT")
    if os.name == "nt" and audit:
        import ctypes
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.GetConsoleWindow.restype = ctypes.c_void_p
        row = {"pid": os.getpid(), "argv": sys.argv, "console_hwnd": kernel.GetConsoleWindow() or 0}
        with Path(audit).open("a", encoding="utf-8") as output:
            output.write(json.dumps(row) + "\n")


def read_json(path):
    return json.loads(Path(path).read_text(encoding="utf-8"))


def sync_dir(path):
    if os.name != "nt":
        fd = os.open(path, os.O_RDONLY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def save(path, value):
    path = Path(path)
    temp = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    with temp.open("x", encoding="utf-8") as out:
        json.dump(value, out, sort_keys=True)
        out.write("\n")
        out.flush()
        os.fsync(out.fileno())
    # Windows readers briefly deny FILE_SHARE_DELETE. Keep the same prepared
    # bytes and deadline values while retrying only this bounded file publish.
    until = monotonic() + 0.5
    while True:
        try:
            os.replace(temp, path)
            break
        except PermissionError:
            if os.name != "nt" or monotonic() >= until:
                raise
            time.sleep(0.005)
    sync_dir(path.parent)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as src:
        for block in iter(lambda: src.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def tree_hashes(root):
    root = Path(root)
    if not root.is_dir() or root.is_symlink():
        raise Failure("frontend must be an ordinary directory")
    result = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise Failure("frontend contains a symlink or special file")
        if path.is_file():
            result[path.relative_to(root).as_posix()] = digest(path)
    if "index.html" not in result:
        raise Failure("frontend index.html missing")
    return result


def tree_digest(hashes):
    return hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest()


@contextlib.contextmanager
def gate(path, wait=5):
    """OS-owned locks are released even on SIGKILL, unlike stale mkdir locks."""
    with Path(path).open("a+b") as lock:
        if lock.tell() == 0:
            lock.write(b"0")
            lock.flush()
        until = monotonic() + wait
        while True:
            try:
                if os.name == "nt":
                    import msvcrt
                    lock.seek(0)
                    msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl
                    fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except (OSError, BlockingIOError):
                if monotonic() >= until:
                    raise Failure("lock wait expired")
                time.sleep(0.02)
        try:
            yield
        finally:
            if os.name == "nt":
                import msvcrt
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                import fcntl
                fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def process_token(pid):
    """PID birth identity, excluding exited processes and POSIX zombies."""
    try:
        if os.name == "nt":
            import ctypes
            from ctypes import wintypes
            kernel = ctypes.WinDLL("kernel32", use_last_error=True)
            kernel.OpenProcess.restype = wintypes.HANDLE
            kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
            kernel.GetProcessTimes.argtypes = [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4
            kernel.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
            kernel.CloseHandle.argtypes = [wintypes.HANDLE]
            handle = kernel.OpenProcess(0x1000, False, int(pid))
            if not handle:
                return None
            try:
                code = wintypes.DWORD()
                if not kernel.GetExitCodeProcess(handle, ctypes.byref(code)) or code.value != 259:
                    return None
                creation, exit_, kernel_, user = (wintypes.FILETIME() for _ in range(4))
                if not kernel.GetProcessTimes(handle, ctypes.byref(creation), ctypes.byref(exit_),
                                               ctypes.byref(kernel_), ctypes.byref(user)):
                    return None
                return str((creation.dwHighDateTime << 32) | creation.dwLowDateTime)
            finally:
                kernel.CloseHandle(handle)
        if sys.platform.startswith("linux"):
            data = Path(f"/proc/{int(pid)}/stat").read_text().rsplit(") ", 1)[1].split()
            return None if data[0] == "Z" else data[19]
        out = subprocess.run(["/bin/ps", "-p", str(int(pid)), "-o", "stat=", "-o", "lstart=", "-o", "command="],
                             capture_output=True, text=True, timeout=1, check=True).stdout.strip()
        fields = out.split(None, 1)
        return fields[1] if len(fields) == 2 and not fields[0].startswith("Z") else None
    except (OSError, ValueError, subprocess.SubprocessError):
        return None


def terminate_record(record):
    if not record.get("token") or process_token(record["pid"]) != record["token"]:
        return False
    if os.name == "nt":
        import ctypes
        from ctypes import wintypes
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        kernel.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
        kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        handle = kernel.OpenProcess(1 | 0x1000, False, record["pid"])
        if handle:
            try:
                # A second birth check narrows the PID-reuse interval before termination.
                if process_token(record["pid"]) == record["token"]:
                    kernel.TerminateProcess(handle, 70)
            finally:
                kernel.CloseHandle(handle)
    else:
        try:
            os.kill(record["pid"], signal.SIGKILL)
        except ProcessLookupError:
            pass
    return True


def clock_deadline(seconds):
    return {"wall": time.time() + seconds, "mono": monotonic() + seconds,
            "created_mono": monotonic()}


def remaining(deadline):
    now = monotonic()
    if now < deadline["created_mono"]:  # reboot: never extend an old attempt
        return 0
    return max(0, min(deadline["wall"] - time.time(), deadline["mono"] - now))


class Attempt:
    def __init__(self, path):
        self.path = Path(path).resolve()
        self.c = read_json(self.path / "config.json")
        self.root = Path(self.c["app_root"])
        self.base = self.path.parent.parent
        if self.path.parent != self.root / "deploy-controller" / "attempts":
            raise Failure("attempt path does not belong to its configured host")
        self.fake = self.c["fake"]
        if not self.fake and sys.platform != "darwin":
            raise Failure("live mode is macOS-only")
        if self.fake and not (self.root / ".fake-host").is_file():
            raise Failure("fake host marker missing")
        self.t = self.c["timing"]

    def event(self, name, **fields):
        data = json.dumps(dict(time=time.time(), event=name, **fields), sort_keys=True) + "\n"
        fd = os.open(self.path / "events.jsonl", os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
        try:
            os.write(fd, data.encode())
            os.fsync(fd)
        finally:
            os.close(fd)

    def state(self, name, **fields):
        save(self.path / "state.json", dict(state=name, time=time.time(), **fields))
        self.event(name, **fields)

    def register(self, role, pid=None, owner=None):
        pid = os.getpid() if pid is None else pid
        token = process_token(pid)
        if not token:
            raise Failure("cannot attest process birth")
        record = dict(pid=pid, token=token, role=role, owner=owner or role,
                      attempt=self.path.name, parent=os.getpid() if pid != os.getpid() else os.getppid())
        save(self.path / "processes" / f"{role}-{uuid.uuid4().hex}.json", record)
        return record

    def decision(self):
        p = self.path / "decision.json"
        return read_json(p) if p.exists() else None

    def decide(self, name, reason, **evidence):
        payload = dict(decision=name, reason=reason, time=time.time(),
                       deadline=clock_deadline(self.t["recovery"]), **evidence)
        candidate = self.path / "decisions" / (uuid.uuid4().hex + ".json")
        save(candidate, payload)
        try:
            os.link(candidate, self.path / "decision.json")
            sync_dir(self.path)
            self.event("decision_won", decision=name, reason=reason)
            return True
        except FileExistsError:
            self.event("decision_lost", requested=name)
            return False

    def release_lock(self):
        with gate(self.base / "ownership.lock"):
            lock = self.base / "active.json"
            if lock.exists() and read_json(lock).get("attempt") == str(self.path):
                lock.unlink()
                sync_dir(self.base)
            # Report the global lock separately: another attempt may own it.
            return {"host_lock_held": lock.exists(), "attempt_lock_held": False}

    def binary(self):
        return self.root / "bin" / "sub2api"

    def pointer(self):
        p = self.root / "frontend" / "current"
        if self.fake:
            return p.read_text(encoding="utf-8").strip()
        if not p.is_symlink():
            raise Failure("frontend/current must be a symlink")
        return os.readlink(p)

    def frontend_target(self, pointer):
        path = Path(pointer)
        return (self.root / "frontend" / path).resolve() if not path.is_absolute() else path.resolve()

    def swap_frontend(self, target):
        p = self.root / "frontend" / "current"
        temp = p.with_name(".deploy-link-" + uuid.uuid4().hex)
        if self.fake:
            with temp.open("x", encoding="utf-8") as out:
                out.write(str(target) + "\n")
                out.flush()
                os.fsync(out.fileno())
        else:
            os.symlink(str(target), temp)
        os.replace(temp, p)
        sync_dir(p.parent)

    def swap_binary(self, source, expected):
        source = Path(source)
        if source.is_symlink() or digest(source) != expected:
            raise Failure("binary snapshot hash mismatch")
        target = self.binary()
        temp = target.with_name(".deploy-bin-" + uuid.uuid4().hex)
        shutil.copy2(source, temp)
        os.chmod(temp, self.c["binary_mode"])
        # The existing wrapper uses second-granularity mtime + size. copy2 alone
        # can preserve both, so force a DIFFERENT integer second on every swap.
        stamp = max(time.time_ns(), target.stat().st_mtime_ns + 2_000_000_000)
        os.utime(temp, ns=(stamp, stamp))
        with temp.open("r+b") as fd:
            os.fsync(fd.fileno())
        os.replace(temp, target)
        sync_dir(target.parent)

    def hook(self, name):
        if not self.fake:
            return
        self.event("hook", checkpoint=name)
        save(self.path / ("hook-" + name + ".json"), {"pid": os.getpid()})
        faults = read_json(self.root / "faults.json")
        if name in faults.get("crash", []):
            os._exit(73)
        while name in read_json(self.root / "faults.json").get("pause", []):
            time.sleep(self.t["poll"])

    def probe(self, phase, frontend, deadline, owner):
        deadline_budget = remaining(deadline)
        timeout = min(self.t["probe"], deadline_budget)
        if timeout <= 0:
            raise Failure("probe deadline expired")
        ident = uuid.uuid4().hex
        output = self.path / "probe-results" / (ident + ".json")
        args = [sys.executable, "-I", str(SCRIPT), "_probe", "--attempt", str(self.path),
                "--phase", phase, "--owner", owner]
        if frontend:
            args.append("--frontend-check")
        with output.open("wb") as out:
            child = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=out,
                                     stderr=subprocess.STDOUT, **windowless_process_options())
            try:
                record = self.register("probe", child.pid, owner)
                try:
                    child.wait(timeout=timeout)
                except subprocess.TimeoutExpired:
                    terminate_record(record)
                    child.wait(timeout=3)
                    reason = "probe deadline expired" if deadline_budget <= self.t["probe"] else "probe timed out"
                    raise Failure(reason)
            finally:
                if child.poll() is None:
                    child.kill()  # an unreaped, direct Popen child only
                    child.wait(timeout=3)
        try:
            result = read_json(output)
        except (ValueError, OSError):
            raise Failure("probe did not return bounded JSON") from None
        if result.get("not_ready"):
            raise NotReady(result["error"])
        if not result.get("ok") or child.returncode != 0:
            raise Failure(result.get("error", "probe failed"))
        return result["identity"]

    def stable(self, phase, frontend, deadline, owner, identity=None):
        count = 0
        while remaining(deadline) > 0:
            try:
                current = self.probe(phase, frontend, deadline, owner)
                if identity is not None and current != identity:
                    raise Failure("listener identity changed during stability checks")
                identity = current
                count += 1
                self.event("stable_sample", phase=phase, frontend=frontend, count=count,
                           identity=identity)
                if count >= self.t["stable"]:
                    return identity
            except NotReady:
                count = 0
            except Failure:
                if owner != "recovery":
                    raise
                count = 0
                identity = None
            time.sleep(min(self.t["interval"], remaining(deadline)))
        raise Failure("readiness deadline expired")

    def fence_runner(self, deadline):
        def owned_records():
            records = [read_json(p) for p in (self.path / "processes").glob("*.json")]
            return [r for r in records if r.get("attempt") == self.path.name and
                    (r["role"] == "runner" or r.get("owner") == "runner")]
        until = monotonic() + min(5, remaining(deadline))
        # Stop producers first, then rescan their children. Late-starting probes
        # independently check the decision before executing any work.
        runners = [r for r in owned_records() if r["role"] == "runner"]
        for record in runners:
            terminate_record(record)
        while any(process_token(r["pid"]) == r["token"] for r in runners):
            if monotonic() >= until:
                raise Failure("runner fencing not confirmed")
            time.sleep(0.02)
        owned = owned_records()
        for record in owned:
            terminate_record(record)
        while any(process_token(r["pid"]) == r["token"] for r in owned):
            if monotonic() >= until:
                raise Failure("runner child fencing not confirmed")
            time.sleep(0.02)
        self.event("runner_fenced", pids=sorted(set(r["pid"] for r in owned)))

    def recover(self):
        decision = self.decision()
        if not decision or decision["decision"] != "rollback":
            return
        deadline = decision["deadline"]
        with gate(self.path / "recovery-supervisor.lock"):
            state = read_json(self.path / "state.json")["state"]
            if state == "rolled_back":
                self.release_lock()
                return
            if state == "rollback_failed":
                return
            # A restarted launchd watchdog first fences any orphaned recovery
            # worker/probe; it never grants a new deadline.
            self.fence_recovery()
            if not remaining(deadline):
                self.state("rollback_failed", error="recovery deadline expired", lock_held=True,
                           database_restored=False)
                return
            with (self.path / "recovery.log").open("ab", buffering=0) as log:
                child = subprocess.Popen([sys.executable, "-I", str(SCRIPT), "_recover",
                                          "--attempt", str(self.path)],
                                         stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                         **windowless_process_options())
                try:
                    self.register("recovery", child.pid, "recovery")
                    try:
                        child.wait(timeout=remaining(deadline))
                    except subprocess.TimeoutExpired:
                        self.fence_recovery()
                        child.wait(timeout=3)
                finally:
                    if child.poll() is None:
                        child.kill()
                        child.wait(timeout=3)
            state = read_json(self.path / "state.json")["state"]
            if state not in {"rolled_back", "rollback_failed"}:
                self.fence_recovery()
                self.state("rollback_failed", error="recovery worker exited or deadline expired",
                           lock_held=True, database_restored=False)

    def fence_recovery(self):
        until = monotonic() + 3
        records = [read_json(p) for p in (self.path / "processes").glob("*.json")]
        workers = [r for r in records if r.get("attempt") == self.path.name and r["role"] == "recovery"]
        for r in workers:
            terminate_record(r)
        while any(process_token(r["pid"]) == r["token"] for r in workers):
            if monotonic() >= until:
                raise Failure("recovery worker fencing not confirmed")
            time.sleep(0.02)
        records = [read_json(p) for p in (self.path / "processes").glob("*.json")]
        children = [r for r in records if r.get("attempt") == self.path.name and r.get("owner") == "recovery"]
        for r in children:
            terminate_record(r)
        while any(process_token(r["pid"]) == r["token"] for r in children):
            if monotonic() >= until:
                raise Failure("recovery probe fencing not confirmed")
            time.sleep(0.02)

    def restore(self):
        decision = self.decision()
        if not decision or decision["decision"] != "rollback":
            return
        deadline = decision["deadline"]
        self.register("recovery", owner="recovery")
        with gate(self.path / "recovery.lock", wait=min(5, remaining(deadline))):
            state = read_json(self.path / "state.json")["state"]
            if state == "rolled_back":
                self.release_lock()
                return
            if state == "rollback_failed":
                return  # manual intervention only; never silently extend recovery
            try:
                self.state("recovering", reason=decision["reason"])
                self.fence_runner(deadline)
                self.hook("before_restore")
                if not remaining(deadline):
                    raise Failure("recovery deadline expired")
                with gate(self.path / "mutation.lock", wait=min(5, remaining(deadline))):
                    old = self.path / "old-binary"
                    if digest(old) != self.c["old_binary_sha256"]:
                        raise Failure("old binary backup corrupted")
                    if digest(self.binary()) != self.c["old_binary_sha256"]:
                        self.swap_binary(old, self.c["old_binary_sha256"])
                    original = self.frontend_target(self.c["old_frontend_pointer"])
                    old_hashes = self.c["old_frontend_hashes"]
                    if not original.is_dir() or tree_hashes(original) != old_hashes:
                        original = self.root / "frontend" / "releases" / (self.path.name + "-recovered")
                        if not original.exists():
                            shutil.copytree(self.path / "old-frontend", original)
                        if tree_hashes(original) != old_hashes:
                            raise Failure("old frontend backup corrupted")
                    self.swap_frontend(self.c["old_frontend_pointer"] if
                                       original == self.frontend_target(self.c["old_frontend_pointer"])
                                       else original)
                    self.event("old_artifacts_restored")
                identity = self.stable("old", True, deadline, "recovery")
                self.state("rolled_back", identity=identity, database_restored=False)
                self.release_lock()
            except Exception as exc:
                self.state("rollback_failed", error=str(exc), database_restored=False,
                           lock_held=True)


PROTECTED_CONFIGURATION = frozenset(('.env', 'run.sh', 'launchd-run.sh', 'deployment.json'))


def verify_configuration(root, hashes, required=False):
    """Attest protected settings without copying, exposing or changing values."""
    if not isinstance(hashes, dict) or not set(hashes).issubset(PROTECTED_CONFIGURATION | {'frontend/Caddyfile'}):
        raise Failure("protected configuration inventory is invalid")
    if required and not PROTECTED_CONFIGURATION.issubset(hashes):
        raise Failure("protected configuration inventory is incomplete")
    for name, checksum in hashes.items():
        if not isinstance(checksum, str) or not re.fullmatch(r'[0-9a-f]{64}', checksum):
            raise Failure("protected configuration digest is invalid")
        path = root / name
        if not path.is_file() or any(part.is_symlink() for part in (path,) + tuple(path.parents)):
            raise Failure("protected configuration path is not ordinary")
        if digest(path) != checksum:
            raise Failure("protected configuration changed")


def verify_protected(a):
    verify_configuration(a.root, a.c.get('protected_configuration_hashes', {}), required=not a.fake)


def validate_evidence(evidence):
    if (evidence.get("reviewed") is not True or not evidence.get("reviewer") or
            evidence.get("rollback_compatible") is not True or
            evidence.get("database_backup", {}).get("verified") is not True or
            not evidence.get("database_backup", {}).get("reference") or
            evidence.get("canary", {}).get("eligible") is not True or
            not evidence.get("canary", {}).get("reference")):
        raise Failure("reviewed backup, rollback compatibility and canary evidence required")


def prepare(args):
    raw = read_json(args.config)
    root = Path(raw["app_root"]).expanduser().resolve()
    fake = bool(args.fake)
    if fake:
        if not (root / ".fake-host").is_file() or root == Path.home() / ".local/share/sub2api":
            raise Failure("fake mode requires an isolated marked host")
        if any(key in raw for key in ("runtime_probe", "runtime_probe_command", "public_base", "local_base")):
            raise Failure("fake mode rejects network/callback configuration")
    elif sys.platform != "darwin" or root != (Path.home() / ".local/share/sub2api").resolve():
        raise Failure("live mode is restricted to the current user's macOS app root")
    for directory in (root / "bin", root / "frontend", root / "frontend/releases"):
        if not directory.is_dir() or directory.is_symlink():
            raise Failure("host artifact directories must not be symlinks")
    installed_binary = root / "bin/sub2api"
    if installed_binary.is_symlink() or not installed_binary.is_file():
        raise Failure("installed bin/sub2api must be a real binary, not a symlink")
    if fake and (root / "frontend/current").is_symlink():
        raise Failure("fake frontend pointer must be an isolated text file")
    evidence = read_json(args.evidence)
    validate_evidence(evidence)
    protected = dict(evidence.get('configuration_hashes', {}))
    verify_configuration(root, protected, required=not fake)
    for build in (raw["old_build"], raw["new_build"]):
        if not isinstance(build, dict) or not build or not all(isinstance(v, str) and v for v in build.values()):
            raise Failure("exact old/new build dictionaries are required")
    if raw["old_build"] == raw["new_build"]:
        raise Failure("old and new runtime builds must differ")
    if not fake:
        probe = raw.get("runtime_probe", {})
        if probe.get("mode") != "binary_inode":
            raise Failure("runtime probe requires binary_inode attestation")
        if set(raw["old_build"]) != {"version", "commit"} or set(raw["new_build"]) != {"version", "commit"}:
            raise Failure("exact version and commit are required")
        raw.setdefault("local_base", "http://127.0.0.1:8080")
        raw.setdefault("public_base", "https://api.getag.org")
        local = urlsplit(raw["local_base"])
        public = urlsplit(raw["public_base"])
        if local.hostname not in {"127.0.0.1", "localhost", "::1"} or local.scheme != "http":
            raise Failure("local_base must be loopback HTTP")
        if public.scheme != "https":
            raise Failure("public_base must use HTTPS")
        for url in (local, public):
            if not url.hostname or url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
                raise Failure("probe base URLs must be credential-free origins")
        raw["listener_port"] = local.port or 80
    binary = Path(args.binary).resolve()
    frontend = Path(args.frontend).resolve()
    if binary.is_symlink() or not binary.is_file():
        raise Failure("staged binary must be a regular file")
    new_hashes = tree_hashes(frontend)
    base = root / "deploy-controller"
    attempts = base / "attempts"
    attempts.mkdir(parents=True, exist_ok=True, mode=0o700)
    name = time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + uuid.uuid4().hex
    path = attempts / name
    path.mkdir(mode=0o700)
    save(path / "lock-owner.json", {"attempt": str(path)})
    try:
        with gate(base / "ownership.lock"):
            os.link(path / "lock-owner.json", base / "active.json")
            sync_dir(base)
    except FileExistsError:
        shutil.rmtree(path)
        raise Failure("another immutable attempt holds the host lock") from None
    try:
        for directory in ("processes", "decisions", "probe-results", "jobs"):
            (path / directory).mkdir(mode=0o700)
        c = dict(raw, app_root=str(root), fake=fake,
                 protected_configuration_hashes=protected,
                 timing=dict(FAKE_TIMING if fake else LIVE_TIMING),
                 old_binary_sha256=digest(root / "bin/sub2api"),
                 new_binary_sha256=digest(binary), new_frontend_hashes=new_hashes,
                 binary_mode=stat.S_IMODE((root / "bin/sub2api").stat().st_mode),
                 evidence_sha256=digest(args.evidence),
                 new_frontend_target=str(root / "frontend/releases" / name))
        if fake:
            c["timing"].update(raw.get("fake_timing", {}))
        current = root / "frontend/current"
        c["old_frontend_pointer"] = current.read_text().strip() if fake else os.readlink(current)
        old_frontend = Path(c["old_frontend_pointer"])
        if not old_frontend.is_absolute():
            old_frontend = root / "frontend" / old_frontend
        if not old_frontend.resolve().is_relative_to(root / "frontend/releases"):
            raise Failure("frontend pointer escaped the host release directory")
        c["old_frontend_hashes"] = tree_hashes(old_frontend)
        shutil.copy2(root / "bin/sub2api", path / "old-binary")
        shutil.copy2(binary, path / "new-binary")
        shutil.copytree(old_frontend, path / "old-frontend")
        shutil.copytree(frontend, path / "new-frontend")
        shutil.copyfile(args.evidence, path / "evidence.json")
        if (digest(path / "old-binary") != c["old_binary_sha256"] or
                digest(path / "new-binary") != c["new_binary_sha256"] or
                tree_hashes(path / "old-frontend") != c["old_frontend_hashes"] or
                tree_hashes(path / "new-frontend") != new_hashes or
                digest(path / "evidence.json") != c["evidence_sha256"]):
            raise Failure("input changed while taking immutable snapshots")
        save(path / "config.json", c)
        a = Attempt(path)
        a.state("preparing")
        identity = a.probe("old", True, clock_deadline(c["timing"]["probe"] + 1), "prepare")
        save(path / "baseline.json", identity)
        save(path / "manifest.json", {"config_sha256": digest(path / "config.json"),
                                      "evidence_sha256": c["evidence_sha256"],
                                      "baseline_sha256": digest(path / "baseline.json")})
        if digest(path / "old-binary") == c["new_binary_sha256"]:
            raise Failure("old and new binaries must differ")
        verify_manifest(a)
        a.state("prepared")
        return {"attempt": str(path), "state": "prepared", "fake": fake}
    except Exception as exc:
        save(path / "state.json", {"state": "prepare_failed", "error": str(exc)})
        with gate(base / "ownership.lock"):
            if read_json(base / "active.json")["attempt"] == str(path):
                (base / "active.json").unlink()
                sync_dir(base)
        raise


def verify_manifest(a):
    m = read_json(a.path / "manifest.json")
    if any(digest(a.path / name) != m[key] for name, key in
           (("config.json", "config_sha256"), ("evidence.json", "evidence_sha256"),
            ("baseline.json", "baseline_sha256"))):
        raise Failure("immutable attempt manifest changed")


def launchd_job(a, role):
    return dict(Label=f"local.sub2api.deploy.{a.path.name}.{role}",
                ProgramArguments=[sys.executable, "-I", str(SCRIPT), role, "--attempt", str(a.path)],
                RunAtLoad=True, KeepAlive={"SuccessfulExit": False}, ThrottleInterval=1,
                WorkingDirectory=str(a.path), StandardOutPath=str(a.path / f"{role}.log"),
                StandardErrorPath=str(a.path / f"{role}.log"), ProcessType="Background")


def watchdog_alive(a):
    records = [read_json(p) for p in (a.path / "processes").glob("*.json")]
    return any(record.get("attempt") == a.path.name and record.get("role") == "watchdog"
               and record.get("token") and process_token(record["pid"]) == record["token"]
               for record in records)


def ensure_watchdog(a):
    """Restore only recovery supervision, preserving every existing deadline."""
    verify_manifest(a)
    with gate(a.path / "watchdog-supervisor.lock"):
        state = read_json(a.path / "state.json")["state"]
        if state in TERMINAL:
            ownership = {}
            if state in {"committed", "rolled_back"}:
                # A crash can persist successful completion before releasing its
                # host lock. Finish only that bookkeeping, never replay work or
                # revive a runner after its durable decision.
                with gate(a.path / "mutation.lock"):
                    state = read_json(a.path / "state.json")["state"]
                    expected = {"committed": "committed", "rolled_back": "rollback"}.get(state)
                    if expected is None:
                        raise Failure("successful terminal state changed before ownership cleanup")
                    decision = a.decision()
                    if not isinstance(decision, dict) or decision.get("decision") != expected:
                        raise Failure("successful terminal state lacks its matching durable decision")
                    ownership = a.release_lock()
            return {"attempt": str(a.path), "state": state, "watchdog_started": False, **ownership}
        active = a.base / "active.json"
        if not active.exists() or read_json(active).get("attempt") != str(a.path):
            raise Failure("recovery supervision requires this attempt's active host lock")
        armed = a.path / "armed.json"
        if not armed.exists():
            # A crash before arm's durable write cannot have activated anything.
            # Hold the same mutation lock as arm/activate through administrative
            # closure, so a late arm cannot resurrect this prepared attempt.
            with gate(a.path / "mutation.lock"):
                if not armed.exists():
                    if (a.path / "activation.json").exists() or a.decision():
                        raise Failure("unarmed attempt has inconsistent activation evidence")
                    return {"attempt": str(a.path), **close_untouched(a), "watchdog_started": False}
        remaining(read_json(armed)["expiry"])  # Validate the original deadline; never replace it.
        if not a.fake:
            plist = a.path / "jobs/watchdog.plist"
            if not plist.exists():
                # Arm writes armed.json before its launchd job. The absent job
                # is recoverable only while there has been no activation.
                with gate(a.path / "mutation.lock"):
                    if (a.path / "activation.json").exists():
                        raise Failure("activated attempt is missing its frozen watchdog job")
                    a.decide("rollback", "interrupted startup before recovery supervision")
                    return {"attempt": str(a.path), **close_untouched(a), "watchdog_started": False}
            if plist.is_symlink() or not plist.is_file():
                raise Failure("watchdog job must be an ordinary frozen file")
            with plist.open("rb") as stream:
                job = plistlib.load(stream)
            if job != launchd_job(a, "watchdog"):
                raise Failure("frozen watchdog job differs from the exact supervised command")
            domain = f"gui/{os.getuid()}"
            target = domain + "/" + job["Label"]
            existing = subprocess.run(["/bin/launchctl", "print", target],
                                      stdin=subprocess.DEVNULL, capture_output=True, timeout=10)
            if existing.returncode == 0:
                return {"attempt": str(a.path), "state": state, "watchdog_started": False,
                        "supervisor_loaded": True}
            # A still-running attested watchdog needs no competing process even
            # if its launchd registration vanished. Poller can restore it later.
            if watchdog_alive(a):
                return {"attempt": str(a.path), "state": state, "watchdog_started": False,
                        "watchdog_alive": True}
            subprocess.run(["/bin/launchctl", "bootstrap", domain, str(plist)],
                           stdin=subprocess.DEVNULL, capture_output=True, timeout=10, check=True)
        else:
            if watchdog_alive(a):
                return {"attempt": str(a.path), "state": state, "watchdog_started": False,
                        "watchdog_alive": True}
            detached(a, "watchdog")
        a.event("recovery_supervision_restored", runner_restarted=False, deadlines_reset=False)
        return {"attempt": str(a.path), "state": state, "watchdog_started": True,
                "runner_restarted": False, "deadlines_reset": False}


def detached(a, role):
    args = [sys.executable, "-I", str(SCRIPT), role, "--attempt", str(a.path)]
    if a.fake:
        with (a.path / f"{role}.log").open("ab", buffering=0) as log:
            kwargs = {"start_new_session": True} if os.name != "nt" else windowless_process_options()
            child = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=log, stderr=log, **kwargs)
            a.register(role, child.pid, role)
        return
    job = launchd_job(a, role)
    plist = a.path / "jobs" / f"{role}.plist"
    with plist.open("xb") as out:
        plistlib.dump(job, out)
        out.flush()
        os.fsync(out.fileno())
    sync_dir(plist.parent)
    subprocess.run(["/bin/launchctl", "bootstrap", f"gui/{os.getuid()}", str(plist)],
                   capture_output=True, timeout=10, check=True)


def arm(a):
    verify_manifest(a)
    with gate(a.path / "mutation.lock"):
        if (a.path / "armed.json").exists() or a.decision():
            raise Failure("attempt already armed or decided; never re-arm")
        if read_json(a.path / "state.json")["state"] != "prepared":
            raise Failure("attempt is not prepared")
        save(a.path / "armed.json", {"expiry": clock_deadline(a.t["arm_expiry"])})
        a.state("arming")
    try:
        detached(a, "watchdog")  # watchdog is supervised before a runner can mutate
        detached(a, "runner")
        deadline = clock_deadline(a.t["arm_wait"])
        while remaining(deadline):
            if a.decision():
                raise Failure("attempt aborted during startup")
            if all((a.path / (role + "-ack.json")).exists() for role in ("runner", "watchdog")):
                for role in ("runner", "watchdog"):
                    record = read_json(a.path / (role + "-ack.json"))
                    if process_token(record["pid"]) != record["token"]:
                        raise Failure("startup process died before acknowledgement")
                with gate(a.path / "mutation.lock"):
                    if a.decision():
                        raise Failure("startup was fenced before readiness")
                    a.state("armed")
                    save(a.path / "ready.json", {"time": time.time()})
                return {"attempt": str(a.path), "state": "armed", "activation_requires": "activate"}
            time.sleep(a.t["poll"])
        raise Failure("startup acknowledgement timed out")
    except Exception:
        a.decide("rollback", "startup failed")
        a.recover()
        raise


def close_untouched(a):
    verify_manifest(a)
    verify_protected(a)
    if (a.path / "activation.json").exists():
        raise Failure("cannot close an attempt that was activated")
    deadline = clock_deadline(a.t["probe"] + 10)
    a.fence_runner(deadline)
    a.fence_recovery()
    if digest(a.binary()) != a.c["old_binary_sha256"] or a.pointer() != a.c["old_frontend_pointer"]:
        raise Failure("untouched release baseline differs")
    if tree_hashes(a.frontend_target(a.pointer())) != a.c["old_frontend_hashes"]:
        raise Failure("untouched frontend bytes differ")
    # Closing a never-activated attempt is not a release acceptance. Verify the
    # running old inode/health and saved tree without requiring the failed public
    # asset gate to recover before releasing an administrative attempt lock.
    identity = a.probe("old", False, deadline, "prepare")
    a.state("aborted_untouched", identity=identity, database_restored=False)
    a.release_lock()
    return {"state": "aborted_untouched", "activated": False}


def activate(a):
    verify_manifest(a)
    with gate(a.path / "mutation.lock"):
        if a.decision() or not (a.path / "ready.json").exists():
            raise Failure("attempt must have two startup acknowledgements and no decision")
        if (a.path / "activation.json").exists():
            raise Failure("activation already scheduled; deadlines cannot be extended")
        if not remaining(read_json(a.path / "armed.json")["expiry"]):
            raise Failure("armed attempt expired")
        for role in ("runner", "watchdog"):
            r = read_json(a.path / (role + "-ack.json"))
            if process_token(r["pid"]) != r["token"]:
                raise Failure("supervised process is no longer alive")
        schedule = {"not_before": clock_deadline(a.t["ack_delay"]),
                    "deadline": clock_deadline(a.t["ack_delay"] + a.t["activation"])}
        save(a.path / "activation.json", schedule)
        a.state("scheduled", not_before=schedule["not_before"]["wall"],
                activation_deadline=schedule["deadline"]["wall"])
    return {"attempt": str(a.path), "state": "scheduled", **schedule,
            "ack_delay_seconds": a.t["ack_delay"]}


def runner(a):
    verify_manifest(a)
    if a.decision():
        return
    record = a.register("runner")
    # A supervised restart may ACK again, but MUST NOT replay any interrupted swap.
    try:
        (a.path / "runner-once").mkdir()
    except FileExistsError:
        a.decide("rollback", "runner restarted")
        return
    save(a.path / "runner-ack.json", record)
    try:
        while not (a.path / "activation.json").exists():
            if a.decision():
                return
            time.sleep(a.t["poll"])
        schedule = read_json(a.path / "activation.json")
        while remaining(schedule["not_before"]):
            if a.decision():
                return
            time.sleep(a.t["poll"])
        deadline = schedule["deadline"]
        a.hook("before_backend_swap")
        # A final attested old runtime check prevents preparing against one
        # listener and activating after it has been replaced out of band.
        if a.probe("old", True, deadline, "runner") != read_json(a.path / "baseline.json"):
            raise Failure("old listener changed after prepare")
        with gate(a.path / "mutation.lock"):
            if a.decision() or not remaining(deadline):
                return
            verify_protected(a)
            # Recheck the old state immediately before the first mutation.
            if (digest(a.binary()) != a.c["old_binary_sha256"] or
                    a.pointer() != a.c["old_frontend_pointer"] or
                    tree_hashes(a.frontend_target(a.pointer())) != a.c["old_frontend_hashes"]):
                raise Failure("host drifted after prepare")
            a.state("activating_backend")
            a.swap_binary(a.path / "new-binary", a.c["new_binary_sha256"])
            a.event("backend_swapped")
        a.hook("after_backend_swap")
        identity = a.stable("new", False, deadline, "runner")
        a.hook("before_frontend_swap")
        with gate(a.path / "mutation.lock"):
            if a.decision() or not remaining(deadline):
                return
            if tree_hashes(a.path / "new-frontend") != a.c["new_frontend_hashes"]:
                raise Failure("new frontend snapshot corrupted")
            target = Path(a.c["new_frontend_target"])
            shutil.copytree(a.path / "new-frontend", target)
            if tree_hashes(target) != a.c["new_frontend_hashes"]:
                raise Failure("published frontend hash mismatch")
            a.swap_frontend(target)
            a.state("activating_frontend")
        a.hook("after_frontend_swap")
        a.stable("new", True, deadline, "runner", identity)
        a.hook("before_commit")
        with gate(a.path / "mutation.lock"):
            if not remaining(deadline):
                raise Failure("activation deadline expired before commit")
            verify_protected(a)
            if a.decide("committed", "stable backend, auth boundary and frontend checks", identity=identity):
                a.hook("after_commit_decision")
                a.state("committed", identity=identity)
                a.release_lock()
    except Exception as exc:
        a.event("runner_failure", error=str(exc))
        a.decide("rollback", str(exc))


def watchdog(a):
    record = a.register("watchdog")
    save(a.path / "watchdog-ack.json", record)
    while True:
        decision = a.decision()
        if decision:
            if decision["decision"] == "rollback":
                a.recover()
            else:
                # Commit's atomic decision is authoritative even if the runner died
                # between that decision and the cosmetic state/lock update.
                with gate(a.path / "mutation.lock"):
                    a.state("committed", recovered_decision=True, identity=decision.get("identity"))
                    a.release_lock()
            return
        armed = read_json(a.path / "armed.json")
        activation = a.path / "activation.json"
        ack = a.path / "runner-ack.json"
        if activation.exists():
            if not remaining(read_json(activation)["deadline"]):
                a.decide("rollback", "activation deadline expired")
        elif not remaining(armed["expiry"]):
            a.decide("rollback", "armed attempt expired without activation")
        if ack.exists():
            r = read_json(ack)
            if process_token(r["pid"]) != r["token"]:
                a.decide("rollback", "attempt runner exited before decision")
        time.sleep(a.t["poll"])


class AssetRefs(HTMLParser):
    def __init__(self):
        super().__init__()
        self.paths = set()

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        value = attrs.get("src") if tag == "script" else attrs.get("href") if tag == "link" else None
        if value:
            path = urlsplit(value).path.lstrip("/")
            if path.startswith("assets/") and ".." not in Path(path).parts:
                self.paths.add(path)


def http_get(base, path, expected, limit=32 * 1024 * 1024, headers=None):
    url = urlsplit(base)
    cls = http.client.HTTPSConnection if url.scheme == "https" else http.client.HTTPConnection
    connection = cls(url.hostname, url.port, timeout=8)
    try:
        request_headers = {"Cache-Control": "no-cache", "Accept-Encoding": "gzip"}
        request_headers.update(headers or {})
        connection.request("GET", path, headers=request_headers)
        response = connection.getresponse()
        if response.status != expected:
            if path == "/health" and response.status in {502, 503, 504}:
                raise NotReady("health route warming: HTTP " + str(response.status))
            raise Failure("HTTP status mismatch for " + path + ": " + str(response.status))
        data = response.read(limit + 1)
        if response.getheader("Content-Encoding", "").lower() == "gzip":
            with gzip.GzipFile(fileobj=io.BytesIO(data)) as compressed:
                data = compressed.read(limit + 1)
        if len(data) > limit:
            raise Failure("HTTP response exceeded bounded size")
        return data
    except (OSError, http.client.HTTPException) as exc:
        if path == "/health":
            raise NotReady("health route reconnecting: " + type(exc).__name__) from None
        raise Failure("HTTP probe transport failure: " + type(exc).__name__) from None
    finally:
        connection.close()


def json_field(value, dotted):
    for part in dotted.split("."):
        if not isinstance(value, dict) or part not in value:
            raise Failure("authenticated runtime endpoint omitted a configured field")
        value = value[part]
    return value


def listener_pid(a):
    output = subprocess.run(["/usr/sbin/lsof", "-nP", f"-iTCP:{a.c['listener_port']}",
                             "-sTCP:LISTEN", "-t"], capture_output=True, text=True,
                            check=False, timeout=2)
    pids = set(int(v) for v in output.stdout.split())
    if len(pids) != 1:
        raise NotReady("waiting for one replacement listener")
    return pids.pop()


def live_identity(a, phase):
    try:
        pid = listener_pid(a)
        token = process_token(pid)
        if not token:
            raise NotReady("listener birth identity unavailable")
        binary = a.binary()
        signature = (binary.stat().st_ino, binary.stat().st_size, binary.stat().st_mtime_ns)
        version = subprocess.run([str(binary), "--version"], stdin=subprocess.DEVNULL,
                                 capture_output=True, text=True, timeout=2, check=True)
        match = re.search(r"Sub2API ([^\s]+) \(commit: ([^,]+),", version.stdout + version.stderr)
        if not match:
            raise Failure("installed binary omitted exact build identity")
        build = {"version": match.group(1), "commit": match.group(2)}
        if listener_pid(a) != pid or process_token(pid) != token:
            raise NotReady("listener changed around binary attestation")
        # The version belongs to the exact inode mapped by the listening process,
        # not just an unrelated staged --version command or an old healthy PID.
        text = subprocess.run(["/usr/sbin/lsof", "-a", "-p", str(pid), "-d", "txt", "-Ffin"],
                              capture_output=True, text=True, check=True, timeout=2).stdout
        inode = None
        matched = False
        for line in text.splitlines():
            if line.startswith("f"):
                inode = None
            elif line.startswith("i"):
                inode = int(line[1:])
            elif line.startswith("n") and line[1:] in {str(a.binary()), str(a.binary().resolve())} and inode == a.binary().stat().st_ino:
                matched = True
        if not matched or signature != (binary.stat().st_ino, binary.stat().st_size, binary.stat().st_mtime_ns):
            raise NotReady("listener executable is not the stable installed binary inode")
        return {"pid": pid, "token": token, "build": build}
    except subprocess.SubprocessError:
        raise NotReady("listener inspection unavailable") from None


def probe_body(a, phase, frontend):
    verify_protected(a)
    hashes = a.c[phase + "_frontend_hashes"]
    expected_build = a.c[phase + "_build"]
    expected_binary = a.c[phase + "_binary_sha256"]
    if a.fake:
        faults = read_json(a.root / "faults.json")
        if phase in faults.get("hang_probe", []):
            time.sleep(3600)  # bounded by the independent parent/watchdog
        try:
            runtime = read_json(a.root / "runtime.json")
        except (OSError, ValueError):
            raise NotReady("fake listener is restarting") from None
        if process_token(runtime["pid"]) != runtime["token"]:
            raise NotReady("fake listener process exited")
        sig = [a.binary().stat().st_mtime_ns, a.binary().stat().st_size]
        if runtime["signature"] != sig:
            raise NotReady("waiting for wrapper replacement listener")
        identity = {k: runtime[k] for k in ("pid", "token", "build")}
    else:
        faults = {}
        identity = live_identity(a, phase)
    if identity["build"] != expected_build:
        if phase == "new" and identity["build"] == a.c["old_build"]:
            raise NotReady("waiting for new runtime build")
        raise Failure("exact runtime build mismatch")
    if phase == "new":
        old = read_json(a.path / "baseline.json")
        if (identity["pid"], identity["token"]) == (old["pid"], old["token"]):
            raise NotReady("listener PID/birth identity was not replaced")
    if digest(a.binary()) != expected_binary:
        raise Failure("installed binary hash mismatch")
    if a.fake:
        if time.time() < faults.get("health_warming_until", {}).get(phase, 0):
            raise NotReady("fake edge health cache warming")
        for check in ("local_health", "public_health", "auth_boundary"):
            if phase in faults.get(check, []):
                raise Failure("fake " + check + " failure")
    else:
        http_get(a.c["local_base"], "/health", 200)
        http_get(a.c["public_base"], "/health", 200)
        http_get(a.c["public_base"], "/api/v1/admin/accounts", 401)
    if frontend:
        target = a.frontend_target(a.pointer())
        if tree_hashes(target) != hashes:
            raise Failure("installed frontend tree hash mismatch")
        index = (target / "index.html").read_bytes()
        refs = AssetRefs()
        refs.feed(index.decode("utf-8"))
        if not refs.paths or not refs.paths.issubset(hashes):
            raise Failure("expected index asset references are missing")
        if a.fake:
            if phase in faults.get("frontend", []):
                raise Failure("fake frontend response mismatch")
        else:
            index_response = http_get(a.c["public_base"], "/", 200)
            if hashlib.sha256(index_response).hexdigest() != hashes["index.html"]:
                raise Failure("public index hash mismatch")
            def check_asset(path):
                body = http_get(a.c["public_base"], "/" + path, 200)
                if hashlib.sha256(body).hexdigest() != hashes[path]:
                    raise Failure("public asset hash mismatch")
            with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
                list(pool.map(check_asset, sorted(refs.paths)))
    if process_token(identity["pid"]) != identity["token"]:
        raise NotReady("listener exited during probe")
    verify_protected(a)
    return identity


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    p = commands.add_parser("prepare", help="snapshot artifacts/evidence and verify old runtime; no activation")
    for name in ("config", "evidence", "binary", "frontend"):
        p.add_argument("--" + name, required=True)
    p.add_argument("--fake", action="store_true", help="marked isolated host only; no network or callbacks")
    for name in ("status", "arm", "activate", "runner", "watchdog", "ensure-watchdog", "rollback", "close-untouched", "_probe", "_recover"):
        p = commands.add_parser(name)
        p.add_argument("--attempt", required=True)
        if name == "rollback":
            p.add_argument("--reason", default="operator requested rollback")
        if name == "_probe":
            p.add_argument("--phase", choices=("old", "new"), required=True)
            p.add_argument("--owner", required=True)
            p.add_argument("--frontend-check", action="store_true")
    args = parser.parse_args()
    if args.command == "prepare":
        return prepare(args)
    a = Attempt(args.attempt)
    if args.command == "status":
        return {"attempt": str(a.path), "state": read_json(a.path / "state.json"),
                "decision": a.decision(), "host_lock_held": (a.base / "active.json").exists(),
                "logs": str(a.path / "events.jsonl")}
    if args.command == "ensure-watchdog":
        return ensure_watchdog(a)
    if args.command == "arm":
        return arm(a)
    if args.command == "activate":
        return activate(a)
    if args.command == "close-untouched":
        return close_untouched(a)
    if args.command == "runner":
        runner(a)
    elif args.command == "watchdog":
        watchdog(a)
    elif args.command == "rollback":
        verify_manifest(a)
        a.decide("rollback", args.reason)
        a.recover()
        return {"state": read_json(a.path / "state.json"), "decision": a.decision()}
    elif args.command == "_recover":
        a.restore()
    elif args.command == "_probe":
        os.environ["DEPLOY_OWNER"] = args.owner
        a.register("probe", owner=args.owner)  # closes parent-killed-before-registration gap
        decision = a.decision()
        if args.owner == "runner" and decision:
            return {"ok": False, "error": "runner probe fenced by immutable decision"}
        elif args.owner == "recovery" and (not decision or not remaining(decision["deadline"])):
            return {"ok": False, "error": "recovery probe fenced by deadline"}
        try:
            return {"ok": True, "identity": probe_body(a, args.phase, args.frontend_check)}
        except NotReady as exc:
            return {"ok": False, "not_ready": True, "error": str(exc)}
        except Exception as exc:
            return {"ok": False, "error": str(exc)}
    return None


if __name__ == "__main__":
    os.umask(0o077)
    record_test_console()
    try:
        result = main()
        if result is not None:
            print(json.dumps(result, sort_keys=True), flush=True)
    except Exception as exc:
        print(json.dumps({"ok": False, "error": str(exc)}), flush=True)
        sys.exit(1)
