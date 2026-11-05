# GitHub → UK-01-M2 production deployment

The repository is the source of application code, raw frontend output and
deployment tooling. The Mac pulls a complete immutable package; it does not use
an old checkout or build from a dirty working directory.

## Release contract

1. A push to this repository's `main` must complete **CI**, **Fork guard** and
   **Security Scan** successfully for the same full commit SHA. A newer failed,
   pending or rerun check blocks an older successful result.
2. `production-package.yml` builds a clean checkout with frozen pnpm dependencies,
   readonly Go modules, CGO disabled and the `embed` tag for darwin/arm64. The raw
   frontend and the frontend embedded in the backend must match. The archive
   records source, binary, migration and tool inventories. Tar and gzip timestamps
   are fixed; the build date comes from the source commit.
3. Publication rechecks main and all gates, uploads both assets into a draft, then
   publishes `production-<40-character-SHA>` as a prerelease without changing the
   latest normal release. Existing assets with different bytes are never replaced.
4. Every 300 seconds `local.sub2api.github-pull` checks main. An unchanged SHA uses
   one API request. A new candidate must descend from the installed SHA, have green
   gates and an exact matching release tag. Downloads and extraction publish only
   fully verified files and directories. Failed candidates back off.
5. The host checks the installed binary, complete frontend and protected config
   fingerprints. It restores a private database copy, runs the frozen candidate
   and its business canaries, checks the migration ledger, cleans up its temporary
   runtime and prepares one controller attempt. It rechecks main and gates before
   activation. Config and credentials stay on the host.
6. The controller supervises binary/frontend switching, exact runtime identity,
   health, authentication boundaries and public asset hashes. Failed activation
   restores the previous binary and frontend pointer. The database is never
   restored over production. A committed attempt updates the installed receipt
   and switches the puller's tools to the verified package tools.

The existing release workflow remains the normal release channel. This dedicated
production workflow neither edits VERSION nor invokes the old private deployment
scripts or any OpenClaw deployment.

## Frontend and database policy

`index.html` and all current frontend files come from the GitHub build unchanged.
Only previous `assets/` and `fonts/` URLs are retained for cached clients. Reusing a
cached URL with different bytes blocks deployment. No host HTML, scripts, title,
favicon or other root resources are inherited. Raw and merged inventories are
recorded separately. Historical assets are retained without automatic pruning.

Automatic deployment currently requires **no new or changed database migrations**.
The canary verifies the restored ledger before startup and that startup leaves it
unchanged. Only the exact documented historical checksum pair for
`250_openclaw_provider_descriptions_live.sql` is accepted. A new migration requires
a separately reviewed migration rollout; do not edit the ledger or bypass the
gate to make an application package deploy.

## One-time host bootstrap

Run as the same macOS user that owns the existing service, with its GUI launchd
domain available. The controller supports the standard
`~/.local/share/sub2api` app root. Keep the selected Python 3.12+ interpreter
available across host maintenance. PostgreSQL client/server tools, Redis and
the existing service configuration must already be installed. No GitHub write
token or self-hosted GitHub runner is needed on the Mac for this public repository.

Copy the reviewed `deploy/production/*.py` files from a verified source archive
into a private bootstrap folder outside the app root. First attest the baseline:

```sh
python3 -I /absolute/bootstrap/install.py install --dry-run \
  --expected-commit <audited-full-live-sha> \
  --expected-binary-sha256 <audited-live-binary-sha256>
```

Then use the same command without `--dry-run`. Installation adopts that exact
baseline, records tool hashes and installs a **paused** launch agent. It does not
restart the application or run production SQL. Existing installer state, launch
agents or an active controller attempt cause refusal instead of overwriting them.
Use `--enable` only when enabling automatic future deployments is intended.

For the baseline audited on 2026-11-05, the live source SHA was
`73f2ba00f7de78af7502a35c0e8a35f731853c65` and binary SHA256 was
`163c94ea05f2e5e9a20093d6d090b20998b72e3f7cf2fb3bb183ccc5b2d1311f`.
Recheck these on the host before adoption; these historical values are not
hardcoded rollback identities. Every subsequent attempt uses its own host baseline.

## Operation and failure inspection

```sh
python3 -I "$HOME/.local/share/sub2api/auto-deploy/current/install.py" status
python3 -I "$HOME/.local/share/sub2api/auto-deploy/current/install.py" pause
python3 -I "$HOME/.local/share/sub2api/auto-deploy/current/install.py" resume
```

Pause prevents a new activation and safely closes an untouched prepared attempt.
An already armed or activated attempt continues controller recovery. `status`
reports the pause marker and last polling result. Private logs and receipts are
under `auto-deploy/`; private database canary copies and evidence are under
`~/.local/share/sub2api-auto-canaries/`. Never copy their database dumps, runtime
tokens, environment values or raw private logs into the repository or issue text.

`operation.json` records durable intent before mutating subprocesses. An uncertain
activation is followed through its existing controller status; arm and activation
are never replayed. After logout or reboot, the next poll restores a missing
recovery watchdog from the attempt's frozen launchd job, with the same deadlines.
It does not restart the runner or grant a new activation window. A prepared
attempt that never starts activation is closed after the bounded adapter window.
An interrupted canary or unknown prepare receipt stops for host inspection; do
not delete its ownership lock or blindly rerun the attempt. Recovery waits for
the service owner's GUI launchd domain to become available after login.

Rollback failure and immutable-baseline drift block further automatic deployment.
Inspect the attempt's state and recovery records, establish the live binary,
frontend pointer and exact listener identity, then repair through the controller's
documented commands. Do not manually clear `active.json` to bypass a failure.
After a successful automatic rollback, fix the source on GitHub and use a new
descendant commit; force-pushing an older commit is rejected by the host.

Database dumps, retained packages and historic assets are intentionally not
automatically pruned by this first rollout. Review disk usage during normal host
maintenance and retain all material needed by an active attempt.

## Verification

The CI `production-deployment` job runs the stdlib Python suite, including release
gates, immutable publication, bounded extraction, canary contracts, activation
intent and controller fault recovery. Frontend theme acceptance remains in CI.

```sh
python -m unittest discover -s deploy/production/tests -p 'test_*.py' -v
```

On the owner's Windows workstation, **all tests, builds, Playwright and dependency
jobs must be wrapped by the workspace's `scripts/video/tools/safe_run.py`**.
That workstation safeguard is not required on GitHub-hosted runners.

A manual production workflow dispatch defaults to `dry_run=true`: build and
validate only, without a production release. GitHub requires this workflow to
exist on the default branch before its first manual dispatch. A local green
suite proves the implementation; only a successful GitHub run and host receipt
prove the corresponding publication and production installation.
