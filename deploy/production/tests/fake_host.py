#!/usr/bin/env python3
"""Egress-free fake wrapper/listener; fixtures must be under an isolated .fake-host root.

No socket, privilege, launchctl, real executable, app path, or database access.
The fake wrapper models mtime+size detection and stop/wait/start replacement.
Each listener starts from one read of the binary taken after the debounce, as
exec binds one file, so swaps during the debounce coalesce into a single start
and a listener whose signature matches the installed binary is never replaced
until the binary changes again.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("controller", HERE.parent / "controller.py")
dc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dc)


def signature(binary):
    info = binary.stat()
    return [info.st_mtime_ns, info.st_size]


def image(binary):
    """Read build and signature from one open file, so they always describe the same binary."""
    with binary.open("rb") as handle:
        info = os.fstat(handle.fileno())
        content = handle.read()
    try:
        build = json.loads(content)["build"]
    except (ValueError, KeyError, TypeError):
        build = None  # not a runnable fake binary
    return {"build": build, "signature": [info.st_mtime_ns, info.st_size]}


def listener(root, loaded):
    if loaded["build"] is None:
        raise SystemExit("installed fake binary is not runnable")  # never becomes ready
    dc.save(root / "runtime.json", {"pid": os.getpid(), "token": dc.process_token(os.getpid()),
                                   "build": loaded["build"], "signature": loaded["signature"]})
    while not (root / "stop-listener").exists():
        time.sleep(0.02)
    time.sleep(0.03)  # model graceful shutdown without changing production services


def wrapper(root):
    binary = root / "bin/sub2api"
    child = None
    running = None  # signature of the binary the current listener was started from
    try:
        while not (root / "stop-wrapper").exists():
            if child is None or signature(binary) != running:
                if child is not None:
                    time.sleep(0.08)  # model existing wrapper's 2s debounce
                    (root / "stop-listener").touch()
                    child.wait(timeout=4)
                    (root / "stop-listener").unlink()
                # Read the binary only now, not when the change was noticed: a listener
                # started from a stale reading would run the newest binary yet be replaced
                # again moments after the controller had accepted it as stable.
                loaded = image(binary)
                running = loaded["signature"]
                with (root / "listener.log").open("ab", buffering=0) as log:
                    child = subprocess.Popen([sys.executable, "-I", str(HERE / "fake_host.py"),
                                              "listener", "--root", str(root),
                                              "--image", json.dumps(loaded)],
                                             stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                             **dc.windowless_process_options())
                dc.save(root / "listener-record.json", {"pid": child.pid,
                                                       "token": dc.process_token(child.pid)})
            time.sleep(0.02)
    finally:
        if child is not None:
            (root / "stop-listener").touch()
            try:
                child.wait(timeout=4)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=3)


if __name__ == "__main__":
    dc.record_test_console()
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("command", choices=("wrapper", "listener"))
    p.add_argument("--root", required=True)
    p.add_argument("--image", help="listener only: the build and signature the wrapper read")
    args = p.parse_args()
    root = Path(args.root).resolve()
    if not (root / ".fake-host").is_file():
        raise SystemExit("isolated fake-host marker required")
    if args.command == "wrapper":
        wrapper(root)
    else:
        if not args.image:
            raise SystemExit("listener requires --image from the wrapper")
        listener(root, json.loads(args.image))
