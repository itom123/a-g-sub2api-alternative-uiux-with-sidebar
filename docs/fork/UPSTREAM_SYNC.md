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
   - The fork's own series currently runs 250–263, while upstream is at 249. Upstream will add its own files numbered
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
