#!/usr/bin/env python3
"""Activate one immutable attempt; resume only by following its existing status.

Every mutating command has a durable create-only intent marker before launch.
A failed/uncertain command is never replayed, including after adapter crashes.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

TERMINAL = {'committed', 'rolled_back', 'rollback_failed', 'prepare_failed', 'aborted_untouched'}


def windowless():
    return {'creationflags': subprocess.CREATE_NO_WINDOW} if os.name == 'nt' else {}


def save_once(path, value):
    # Publish complete bytes atomically with a create-only hard link. A crash
    # cannot expose a partly written terminal receipt or permit intent replay.
    fd, temporary = tempfile.mkstemp(prefix='.' + path.name + '-', dir=str(path.parent))
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as output:
            json.dump(value, output, indent=2, sort_keys=True)
            output.write('\n')
            output.flush()
            os.fsync(output.fileno())
        Path(temporary).chmod(0o600)
        os.link(temporary, path)
        if os.name != 'nt':
            directory = os.open(str(path.parent), os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
    finally:
        Path(temporary).unlink(missing_ok=True)


def state_name(status):
    return status.get('state', {}).get('state')


def run_status(controller, attempt):
    result = subprocess.run([sys.executable, '-I', str(controller), 'status', '--attempt', str(attempt)], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=12, **windowless())
    if result.returncode:
        raise RuntimeError('status read failed; inspect attempt without replay')
    status = json.loads(result.stdout)
    if status.get('attempt') != str(attempt):
        raise RuntimeError('status attempt identity differs')
    return status


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--canary-root', required=True)
    parser.add_argument('--resume', action='store_true', help='follow existing status only; never issue arm or activate')
    parser.add_argument('--wait-seconds', type=int, default=450)
    args = parser.parse_args()
    if not 1 <= args.wait_seconds <= 900:
        raise RuntimeError('invalid bounded status wait')
    root = Path(args.canary_root).resolve(strict=True)
    controller = root / 'controller.py'
    receipt = json.loads((root / 'prepare-receipt.json').read_text())
    canary = json.loads((root / 'canary-result.json').read_text())
    if receipt.get('state') != 'prepared' or canary.get('status') != 'passed':
        raise RuntimeError('prepared canary evidence missing')
    attempt = Path(receipt['attempt'])
    if not attempt.is_absolute() or not attempt.is_dir():
        raise RuntimeError('prepared attempt path is invalid')
    terminal_path = root / 'activation-terminal.json'
    if terminal_path.exists():
        terminal = json.loads(terminal_path.read_text())
        if terminal.get('attempt') != str(attempt) or state_name(terminal) not in TERMINAL:
            raise RuntimeError('saved terminal evidence is invalid')
        print(json.dumps(terminal, sort_keys=True), flush=True)
        return 0 if state_name(terminal) == 'committed' else 1

    def mutate_once(command, timeout=55):
        save_once(root / ('activation-' + command + '-intent.json'),
                  {'command': command, 'attempt': str(attempt), 'started': time.time(), 'never_replay': True})
        try:
            result = subprocess.run([sys.executable, '-I', str(controller), command, '--attempt', str(attempt)], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=timeout, **windowless())
        except subprocess.TimeoutExpired:
            raise RuntimeError('controller command acknowledgement uncertain; resume status only, never replay') from None
        try:
            response = json.loads(result.stdout)
        except ValueError:
            response = {'state': 'unknown', 'valid_json': False}
        save_once(root / ('activation-' + command + '.json'), response)
        if result.returncode:
            raise RuntimeError('controller command failed; resume status only, never replay')
        if response.get('attempt') != str(attempt):
            raise RuntimeError('controller acknowledgement attempt identity differs; resume status only')
        return response

    status = run_status(controller, attempt)
    markers = ('activation-arm-intent.json', 'activation-activate-intent.json',
               'activation-arm.json', 'activation-activate.json')
    follow_only = args.resume or any((root / name).exists() for name in markers) or any(
        (attempt / name).exists() for name in ('armed.json', 'activation.json'))
    if not follow_only and state_name(status) not in TERMINAL:
        if state_name(status) != 'prepared':
            raise RuntimeError('attempt is not prepared; resume status only, never replay')
        state = mutate_once('arm')
        if state.get('state') != 'armed':
            raise RuntimeError('controller startup acknowledgement missing; resume status only')
        scheduled = mutate_once('activate')
        if scheduled.get('state') != 'scheduled':
            raise RuntimeError('activation acknowledgement missing; resume status only')
        print(json.dumps({'scheduled': True, 'attempt': str(attempt)}), flush=True)
    elif follow_only:
        print(json.dumps({'following_existing': True, 'attempt': str(attempt), 'do_not_replay': True}), flush=True)

    deadline = time.monotonic() + args.wait_seconds
    while True:
        if state_name(status) in TERMINAL:
            save_once(terminal_path, status)
            print(json.dumps(status, sort_keys=True), flush=True)
            return 0 if state_name(status) == 'committed' else 1
        if time.monotonic() >= deadline:
            raise RuntimeError('bounded status wait elapsed; watchdog owns recovery, resume status only')
        time.sleep(min(4, max(0, deadline - time.monotonic())))
        status = run_status(controller, attempt)


if __name__ == '__main__':
    os.umask(0o077)
    try:
        sys.exit(main())
    except Exception as exc:
        print(json.dumps({'committed': False, 'error': str(exc), 'do_not_replay': True}), flush=True)
        sys.exit(1)
