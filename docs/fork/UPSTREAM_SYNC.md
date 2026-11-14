# Syncing with upstream (Wei-Shaw/sub2api)

Goal: take upstream fixes and features without losing or silently overriding the fork customizations listed in
[CUSTOMIZATIONS.md](CUSTOMIZATIONS.md).

## Procedure

```bash
git remote add upstream https://github.com/Wei-Shaw/sub2api.git   # once
git fetch upstream --tags
git switch -c sync/upstream-<tag> origin/main
git merge --no-ff <tag>            # e.g. v0.2.20; never rebase main
```

Resolve conflicts with these rules, then open a PR. CI runs `fork-guard` and must be green before merging.

1. **Hook-point files** (the table in CUSTOMIZATIONS.md): keep upstream's change and re-apply the fork hook. Never
   resolve one of these files with `--theirs` or `--ours` wholesale.
2. **Fork-owned files**: keep ours. Upstream does not have them, so conflicts are rare; if upstream adds a file with
   the same name, rename upstream's.
3. **Generated ent code (`backend/ent/**`, except `backend/ent/schema/`)**: do not hand-merge. Resolve
   `backend/ent/schema/*.go` (keep both sides' fields), then regenerate:
   ```bash
   cd backend && go generate ./ent
   ```
4. **Migrations**: never edit or renumber a migration already applied in production. The runner keys on the full
   file name and verifies checksums.
   - The fork's own series currently runs 250–263, while upstream v0.2.21 is at 250. Upstream will add its own files numbered
     250 and up with different names; they coexist with ours and run on the next deploy. Review them for overlap with
     the fork's columns, such as `groups`/`users` columns of the same name, before merging.
   - New fork migrations use `9NN_fork_<name>.sql`, so fork numbers never collide with upstream's.
   - Commit migrations with LF line endings (enforced by `.gitattributes`). The checksum production recorded is the
     LF one.
5. **Snapshot version** (`apiKeyAuthSnapshotVersion`): if both sides bumped it, take the higher number plus one, and
   keep every field from both sides.

## Before merging the sync PR

```bash
tools/fork-guard.sh          # invariants + Go guard tests + frontend guard specs
cd backend && make test-unit   # full suite (CI also runs it)
```

If `fork-guard` fails, the sync dropped or changed a customization. Fix the merge rather than the guard. Change the
guard only when a customization is intentionally retired, and update CUSTOMIZATIONS.md in the same PR.

## v0.2.21 migration rollout

Pause the host puller before merging this update; leave the running gateway in place.
This release includes the upstream Gemini image-generation backfill, a concurrent
usage-cost index, MCP OAuth tables, and nullable 12-hour price / zero-default usage
columns. Review the Gemini backfill against current group policy before applying it.
The existing fork SQL and its recorded checksums remain unchanged.

Build `backend/cmd/migrate` from the reviewed commit. It uses standard PostgreSQL
`PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, and `PGSSLMODE` environment
variables; keep these values outside Git. Running without `--apply` only lists pending
files. `--apply --expect <comma-separated-filenames>` requires an exact pending-file
match and calls the application's existing migration runner, including its checksum
checks, advisory lock, transactions and concurrent-index handling.

First restore a fresh backup into an isolated database, rehearse the exact migration
list, and run both old and new application canaries against the upgraded schema.
Retain the verified backup. Apply the same reviewed list to production with bounded
lock waits, then resume the unchanged automatic deployment policy. Its standard
restored-copy canary must now see no pending migrations and pass before activation.
Application rollback restores the previous binary/frontend while retaining the
backward-compatible additions; do not restore a stale database over live writes.
