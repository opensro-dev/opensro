# Auto-potion parity follow-up

Native evidence: v1.150 client SHA256
`375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a`;
v1.188 server SHA256
`bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`,
restricted to the v1.150 item catalog. Binary Ninja MCP was used to inspect
both decompilation and instructions. This is bounded verification, not an
exhaustive equivalence proof of every reachable native feature.

## Completed dependency integrations

| Behavior | Native evidence | Port and verification |
| --- | --- | --- |
| Visible owner/visitor stall prevents automatic item use | Client 561E57..561E6B | Existing stall state gates the attempt without cancelling its retry timer. Separate stall network is allowed. Auto-potion transition tests. |
| Authored item cooldown | Client 80B870 parser, 755E40 receipt, 565400 icon | Scan all 20 description pairs; last COOLTIME declaration wins. Category 18 uses authored group byte or item reference. Inventory and quickslot icons retain the timer after the final unit is spent. |
| Fortress repair target admission | Server 49CC80 | Existing war, fortress holder, guild-member role, structure HP/state, world and range owners. Target refusals leave the stack untouched. |
| Three repair grades | Items 19234/19609/19610; skills 20507/20508/20509 | Complete dura/puls/heal/lnks/skc/ao/rpkt programs. 1/2/3 percent maximum-HP healing every 3000 ms for 12000 ms. Descriptors survive compact skill storage. |
| Persistent repair timing | Server 5830B0, 5A09F0 | Existing linked-pulse registry; one pulse per due update, no catch-up loop, pulse before strict duration expiry. Existing structure population owns HP and persistence. |
| Stop, damage, death and logout | Server persistent handler and existing effect retirement owners | Voluntary stop, damage interruption, source retirement, death, logout, world change and destroyed target stop subsequent healing. Paired instance teardown uses existing effect packets. |
| Repair use rejected by inner skill | Server 59B480:59B7A8, 49CC80 | Native targeted invocation returns success after a skill-handler refusal. Emit skill refusal then item success, consuming one kit without installing a heal. Target-admission failures still do not consume. |
| Repair delay row | Client 755E40, 6B1B30, 6FFE50, 6FD710 | Resolve item-owned skill duration on bootstrap and reference deltas. Bind the source effect even when it precedes the item receipt. Skill-only cancel request; token retirement, death, reset and expiry clear the row. |

The repair source uses the character effect registry for cancellation; the
linked-pulse registry owns timing; the existing fortress population owns
structure health. No parallel siege or stall authority was introduced.

## Validation on 2026-10-06

- `pnpm check source`: 13 tasks passed, including server format, vet, lint,
  tests, race checks and vulnerability gate (unchanged substeps may use stamps).
- `pnpm --filter @sro/client-next check`: all 11 gates passed.
- `SRO_REQUIRE_GAME_DATA=1 go test ./internal/game/action -run TestStructureRepair -count=1 -v`:
  all 21 scenarios passed with licensed game data present, no skipped cases.
- Skill descriptor, compact-store round-trip and authored cooldown tests passed.
- Production UI `tests/browser/game-options.test.mjs` passed against the
  isolated worktree's Vite server, including save, cancel and persistence.
- Fresh asset build, asset publication and server-data bundle completed.

Initial runs caught a dropped compact-store descriptor and missing generated
assets. Both were corrected before the final passing gates. The browser test
uses a deterministic production-renderer fixture; it is not a connected-player
native-versus-webport consumption capture.

## Retained exception and remaining scope

The user explicitly retained strongest usable HP/MP potion replacement on
exhaustion. Native 573390 replaces only with the same item reference.

Native does not wait for the entire preceding potion recovery sequence.
Reuse admission and queued recovery are separate; accepting another potion
while prior pulses remain is not by itself a parity defect.

The broad audit remains open. Client 561DAA also checks flag-war state
`CE883C != 0xFF` with the player's timed interaction byte at `+780`.
The flag-war feature/lifecycle has no corresponding current port owner;
this follow-up does not manufacture a flag to pretend that branch is closed.
A complete native lifecycle equivalence proof and connected-player comparison
also remain outside the verification above.
