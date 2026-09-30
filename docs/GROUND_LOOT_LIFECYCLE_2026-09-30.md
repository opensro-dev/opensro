# Ground loot lifecycle — BUG-047

The report describes earned items that remain visible but become impossible to
pick up after a delay. Monster kill publishers sent `0x30D7` without the scope
record that registers the item in each transport session. The registry later
expired the item, but the maintenance tick could not address its `0x36AB` despawn
to those sessions. Their next pickup therefore named an item absent from server
authority. The same omission suppressed the earlier `0x31E2` ownership release.

## Shared fix and caller map

All four affected publishers now use the existing `wire.DropBroadcastFrames`:

| Producer | Shared owner and affected callers |
| --- | --- |
| Single-target fatal hit | `skillcombat.go`; skill requests, basic attacks, repeat attacks and delayed projectile release |
| Area fatal hits | `skillarea.go`; area/chain victim settlement and delayed area release |
| Persistent attack fatal pulse | `skillperiodic.go`; linked periodic effects |
| Abnormal fatal damage | `monsterabnormal.go`; credited periodic damage and detonation settlement |

The helper carries the same wire bytes and an atomic visible-object admission.
`SendFrames`, `BroadcastObservedFrames`, and simulation-frame conversions preserve
that metadata. The session commits it with the reliable spawn batch, and
`simulation.Ticker.runHookList` can then route ownership and expiry to its viewers.

The rg audit also covered manual item/gold drops (`itemmove.go`), dropped body and
COS items (`bodystatus.go`, `cosground.go`), captured-monster essence
(`monstercapture.go`), and scene admission/re-entry (`GroundObjectListRows`). Those
already use the common publisher or supply the same scope admission. Pickup
grant/despawn and remainder paths already retain/remove scope correctly.

## Client and native contract

The client already handles `0x31E2` by releasing ownership while preserving the
item, and handles `0x36AB` by deleting the entity, motion state and pickup intent.
The client source needs no parallel expiry timer. Existing ground-item and
pickup-intent tests exercise these behaviors.

Raw v1.150 executable instructions at `0x777310–0x777366` read a four-byte GID,
resolve the object, and dispatch its removal through vtable offset `0x4c` with
argument zero. This was checked directly with PE bytes and Capstone because the
Binary Ninja MCP connection reset during inspection. No database names were
used as behavioral evidence and no unnamed functions were returned by MCP.

This fix does not establish or change a native item lifetime. The existing
180-second fixture lifetime and five-second sweep cadence remain server policy;
their correctness is separate from reliable delivery of an actual expiry.

## Regression evidence

`TestMonsterLootPublicationSurvivesOwnershipAndExpiry` uses real loopback
WebSocket sessions and the production transport bridge. It covers direct skill,
basic attack, delayed projectile, area, persistent and abnormal kills. It checks:

- the killer and observing peer receive the producer's original packets;
- every received loot spawn appears in both sessions' published-object sets;
- ownership release reaches both viewers on the maintenance tick;
- expiry reaches both viewers and removes both registry and session membership.

Before the fix, direct, area, persistent and abnormal cases each failed with
`received loot 300001 is absent from session 1 publication scope`. After the fix,
all six paths pass. The regression uses the production registry clock and
transport publication contract; it does not assert on source text or synthesize
replacement spawn metadata.

For a live retest, defeat monsters, leave some loot on the ground, and collect
other loot after the ownership reservation ends. Expired loot should disappear
for the killer and nearby observers. Visible, unexpired loot should remain
collectible subject to normal range, ownership and inventory restrictions.
