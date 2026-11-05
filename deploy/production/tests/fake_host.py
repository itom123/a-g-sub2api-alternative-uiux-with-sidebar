#!/usr/bin/env python3
"""Egress-free fake wrapper/listener; fixtures must be under an isolated .fake-host root.

No socket, privilege, launchctl, real executable, app path, or database access.
The fake wrapper models mtime+size detection and stop/wait/start replacement.
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


def listener(root):
    artifact = dc.read_json(root / "bin/sub2api")
    binary = root / "bin/sub2api"
    dc.save(root / "runtime.json", {"pid": os.getpid(), "token": dc.process_token(os.getpid()),
                                   "build": artifact["build"],
                                   "signature": [binary.stat().st_mtime_ns, binary.stat().st_size]})
    while not (root / "stop-listener").exists():
        time.sleep(0.02)
    time.sleep(0.03)  # model graceful shutdown without changing production services


def wrapper(root):
    child = None
    last = None
    try:
        while not (root / "stop-wrapper").exists():
            binary = root / "bin/sub2api"
            signature = (binary.stat().st_mtime_ns, binary.stat().st_size)
            if child is None or signature != last:
                last = signature
                if child is not None:
                    time.sleep(0.08)  # model existing wrapper's 2s debounce
                    (root / "stop-listener").touch()
                    child.wait(timeout=4)
                    (root / "stop-listener").unlink()
                with (root / "listener.log").open("ab", buffering=0) as log:
                    child = subprocess.Popen([sys.executable, "-I", str(HERE / "fake_host.py"),
                                              "listener", "--root", str(root)],
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
    args = p.parse_args()
    root = Path(args.root).resolve()
    if not (root / ".fake-host").is_file():
        raise SystemExit("isolated fake-host marker required")
    (wrapper if args.command == "wrapper" else listener)(root)
