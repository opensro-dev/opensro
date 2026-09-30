# AGENTS.md - asset pipeline, checks and task runner

Area rules for `scripts/`. The repository rules in
[`../AGENTS.md`](../AGENTS.md), including the id Software code style, apply
here too. `checks/check_source_file_size.mjs` is a good example of the style
in JavaScript.

## Rules

- Asset packs: every published path has exactly one owning group, defined in
  `build/assetPackOwnership.mjs`. A duplicate owner makes the client reject
  the whole pack index. A new publisher gets its membership from there and
  merges with `mergeAssetPackGroupUpdates`; never splice rows out of a group
  without rebuilding its packs.
- Other sessions run publishers too. Check `pnpm task assets:lock` before a
  rebuild.
- Text read from JMX files is CP949. Decode through the shared reader and
  fold ASCII case only. Empty `RN_RM_*` region banners are shipped data, not a
  bug.
- A new gate is registered in `tasks/checks.mjs` and added to the `source`
  pipeline when it needs no game data, so CI and the pre-push hook run it.
- Gates that grandfather existing debt use a ledger that only shrinks (see
  `checks/check_formatting.mjs`), never a count that can be traded.

## Verification

| Scope | Command |
| --- | --- |
| Scripts typecheck allowlist | `pnpm task check:scripts` |
| Formatting | `pnpm task check:format` |
| One outdoor region | `pnpm assets build world-outdoor -- --region=0x6a48 --force --jobs=1`, then `pnpm assets refresh outdoor` |
| Full asset build (about 42 minutes) | `pnpm assets build full` |
