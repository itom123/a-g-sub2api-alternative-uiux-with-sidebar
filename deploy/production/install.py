#!/usr/bin/env python3
"""Install the reviewed outbound-only production poller as a user launch agent.

No application restart, Git checkout, credentials, DNS, or production SQL changes
are made by installation. --enable starts polling only after a verified baseline
has been adopted. Pause/resume preserves in-flight controller recovery.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
import uuid

HERE = Path(__file__).resolve().parent
LABEL = 'local.sub2api.github-pull'


def load_poller():
    spec = importlib.util.spec_from_file_location('production_poller', HERE / 'poller.py')
    loaded = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = loaded
    spec.loader.exec_module(loaded)
    return loaded


def launch_agent(python, current, config, logs):
    return {'Label': LABEL, 'ProgramArguments': [str(python), '-I', str(current / 'poller.py'), '--config', str(config)],
            'RunAtLoad': True, 'StartInterval': 300, 'ProcessType': 'Background',
            'StandardOutPath': str(logs / 'poller.stdout.log'), 'StandardErrorPath': str(logs / 'poller.stderr.log'),
            'EnvironmentVariables': {'PATH': '/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin'},
            'ThrottleInterval': 60, 'ExitTimeOut': 30}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('install', 'pause', 'resume', 'status'))
    parser.add_argument('--app-root', default=str(Path.home() / '.local/share/sub2api'))
    parser.add_argument('--repository', default='itom123/a-g-sub2api-alternative-uiux-with-sidebar')
    parser.add_argument('--public-base', default='https://api.getag.org')
    parser.add_argument('--local-base', default='http://127.0.0.1:8080')
    parser.add_argument('--expected-commit')
    parser.add_argument('--expected-binary-sha256')
    parser.add_argument('--enable', action='store_true')
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    os.umask(0o077)
    poller = load_poller()
    poller.require(sys.platform == 'darwin', 'installation-requires-macos')
    app = Path(args.app_root).expanduser().resolve(strict=True)
    poller.require(app == (Path.home() / '.local/share/sub2api').resolve(strict=True),
                   'controller-requires-standard-app-root')
    state = app / 'auto-deploy'
    config_path = state / 'config.json'
    plist_path = Path.home() / 'Library/LaunchAgents' / (LABEL + '.plist')
    if args.command == 'status':
        result = {'installed': config_path.is_file(), 'paused': (state / 'PAUSED').exists()}
        if (state / 'status.json').is_file():
            result['last_poll'] = poller.read_json(state / 'status.json')
        print(json.dumps(result, sort_keys=True))
        return 0
    if args.command in ('pause', 'resume'):
        poller.require(config_path.is_file(), 'poller-not-installed')
        marker = state / 'PAUSED'
        if args.command == 'pause':
            marker.touch(mode=0o600, exist_ok=True)
        elif marker.exists():
            marker.unlink()
        print(json.dumps({'paused': marker.exists()}))
        return 0
    poller.require(not state.exists(), 'poller-already-installed; update through GitHub or inspect existing state')
    poller.require(not plist_path.exists(), 'launch-agent-already-exists')
    poller.require(not (app / 'deploy-controller/active.json').exists(), 'deployment-attempt-already-active')
    poller.require(re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', args.repository), 'invalid-repository')
    poller.require(re.fullmatch(r'[0-9a-f]{40}', args.expected_commit or ''), 'expected-commit-required')
    poller.require(re.fullmatch(r'[0-9a-f]{64}', args.expected_binary_sha256 or ''), 'expected-binary-sha256-required')
    poller.require(args.public_base.startswith('https://') and args.local_base.startswith('http://127.0.0.1:'), 'invalid-probe-base')
    installed = poller.baseline(app)
    poller.require(installed['source_commit'] == args.expected_commit, 'bootstrap-commit-mismatch')
    poller.require(installed['binary_sha256'] == args.expected_binary_sha256, 'bootstrap-binary-mismatch')
    tools_hashes = poller.inventory(HERE)
    tools_hashes = {name: checksum for name, checksum in tools_hashes.items() if '/' not in name and name.endswith('.py')}
    poller.require({'poller.py', 'github_api.py', 'controller.py', 'prepare_canary.py', 'activate_prepared.py'} <= set(tools_hashes), 'bootstrap-tooling-incomplete')
    version = hashlib.sha256(json.dumps(tools_hashes, sort_keys=True).encode()).hexdigest()[:16]
    current = state / 'current'
    # Preserve the operator's stable interpreter link (e.g. Homebrew/bin),
    # rather than pinning its resolved Cellar version through brew cleanup.
    agent = launch_agent(Path(sys.executable).absolute(), current, config_path, state / 'logs')
    if args.dry_run:
        print(json.dumps({'dry_run': True, 'source_commit': installed['source_commit'],
                          'binary_sha256': installed['binary_sha256'], 'frontend_files': len(installed['frontend_files']),
                          'tool_files': len(tools_hashes), 'label': LABEL, 'interval_seconds': 300, 'enabled': args.enable}))
        return 0
    state.mkdir(mode=0o700)
    (state / 'logs').mkdir(mode=0o700)
    (state / 'packages').mkdir(mode=0o700)
    bootstrap = state / ('bootstrap-' + version)
    bootstrap.mkdir(mode=0o700)
    for name, checksum in tools_hashes.items():
        shutil.copyfile(HERE / name, bootstrap / name)
        (bootstrap / name).chmod(0o600)
        poller.require(poller.digest(bootstrap / name) == checksum, 'bootstrap-copy-changed')
    os.symlink(bootstrap, current, target_is_directory=True)
    installed.update(schema=1, repository=args.repository, bootstrap=True, tooling_hashes=tools_hashes)
    poller.save(state / 'installed.json', installed)
    poller.save(config_path, {'schema': 1, 'repository': args.repository, 'app_root': str(app),
                             'public_base': args.public_base.rstrip('/'), 'local_base': args.local_base.rstrip('/'),
                             'enabled': True})
    if not args.enable:
        (state / 'PAUSED').touch(mode=0o600)
    poller.save(state / 'installation.json', {'schema': 1, 'baseline_commit': installed['source_commit'],
                                             'baseline_binary_sha256': installed['binary_sha256'],
                                             'tooling_hashes': tools_hashes, 'label': LABEL, 'interval_seconds': 300})
    poller.require(not plist_path.exists(), 'launch-agent-already-exists')
    temporary = plist_path.with_name(plist_path.name + '.' + uuid.uuid4().hex + '.tmp')
    temporary.write_bytes(plistlib.dumps(agent))
    temporary.chmod(0o600)
    os.replace(temporary, plist_path)
    result = subprocess.run(['/bin/launchctl', 'bootstrap', 'gui/' + str(os.getuid()), str(plist_path)],
                            stdin=subprocess.DEVNULL, capture_output=True, timeout=20)
    poller.require(result.returncode == 0, 'launch-agent-bootstrap-failed; inspect installed state before retry')
    print(json.dumps({'installed': True, 'paused': not args.enable, 'label': LABEL, 'interval_seconds': 300}))
    return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as exc:
        # Environment, command output and config contents are never logged.
        print(json.dumps({'installed': False, 'error_type': type(exc).__name__,
                          'reason': str(exc) if isinstance(exc, RuntimeError) else 'inspect-private-installation-state'}))
        sys.exit(1)
