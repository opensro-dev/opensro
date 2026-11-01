# Reviews and merges

- **review** A PR needs two non-author approvals at its exact head and finished, green CI. A pushed change needs fresh
  approval of what changed.
- **review** A skipped check is not a pass. Read every comment and review before merging.
- **review** A review covers blast radius (search every caller), native evidence, edge cases, what was run, and a
  verdict. Say what actually ran, and quote failures.
- **review** Never force-push a PR branch. Merge `main` into it.
- **review** Contributor code from a fork is read first. It runs in CI, and runs locally only from a reviewed commit on
  a branch of this repository. Check whether a PR comes from a fork before running anything.
- **review** A check result proves only the commit it ran on. Re-run it after a reorder or a rebase.
- **review** Do not use `git stash` in a worktree: the stash list is shared across worktrees. Test an old commit in its
  own worktree, not by checking old paths out over your tree.
- **review** Do not edit a worktree while a build or test runs from it.
- **review** A wire or handshake change is checked against every client of that endpoint, including monitors and tools.
- **review** A new non-native feature needs the owner's wish first, even when it is default-off and clean.
- **review** Public text states policy only: no incident details, server addresses or security specifics, and no text
  from players' reports.
