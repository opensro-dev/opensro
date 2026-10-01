# Abnormal mechanics audit — 2026-10-01

This audit compares the v1.150 client and v1.188 server within the
v1.150 feature set. Work is isolated on `codex/abnormal-mechanics-audit` in
`temp/worktrees/abnormal-status-levels`; Claude owns `claude/gameplay-fixes`.
The earlier tooltip correction is merged as #78. This audit is not deployed.

## Implemented corrections

| Area | Native evidence and resulting behavior | Regression coverage |
| --- | --- | --- |
| Element resistance | `5909FB` reads Electric Shock at keeper `1D`; `590B39` reads Burn at `1E`. Player and monster adapters now preserve keeper order. | Six different resistance values expose accidental swaps. |
| Player movement | `4A45D0`/`4A4A90` apply Frostbite/Slow after the other speed contributions. Live mover and `376F` publication agree. | Haste, berserk, both statuses, expiry and wire values. |
| Stop and cancellation | `4AA340` stops movement before skill retirement. `59FF80` and `5A0100` select different effects. Registry retirement now uses those predicates and existing teardown. | Freeze/Sleep/Stun/Root moving actors; selected/all retirement metadata. |
| Monster movement and reach | Status changes retime the retained ground path without replacing its surface ownership. Myopia keeper `B7` affects pursuit and both player/COS attack admission. | Curved terrain, in-flight continuity, far player and pet targets. |
| Character reference identity | COS now use their own RefObjChar defenses, maxima, body radius, resistance and speed. Character TID4 was being dropped by the loader and is now retained. | All four COS bands and independent reference parameters. |
| COS callbacks | Periodic damage, drain, effective maxima, stop/speed and death now mutate the pet. Fatal transitions dismount once, clear statuses and make revival eligible. | Burn/Poison/Bleeding/Panic/Combustion, nonlethal poison, mounted motion and repeated death ticks. |
| COS first sight | Late observers receive the live abnormal mask and dead-life baseline after the pet spawn. | Packet ordering, payload ownership, ordinary correction does not replay first-sight state. |
| Pet potions | `49D240` calls immediate recovery (`4A86A0`), with target HP/MP reduction and Zombie reversal. Player potion queuing does not apply to pets. | HP/MP/vigor families with and without Zombie. |
| Player potion queue | `49A5B0`, `49A0C0`, `49A110`, `49A510`: absolute potions apply one fifth immediately and queue four pulses; HP and MP queues are independent. Percentage potions trim existing queues. | Minimum/maximum pulse, integer truncation, queue order, HP/MP admission asymmetry, overflow trimming. |
| Potion lifecycle | `4A7042` installs a one-second character timer; `4AB690` keeps elapsed debt while dispatching once per update. Existing resident binding/ticking owns pulses; death, disconnect and replacement binding retire them. | Public ItemUse followed by TickHook, exact boundary, duplicate tick, cure between pulses and teardown. |
| Reuse timing | `64C700` scales the eligible type-two action portion by keeper `8C`, then adds the unscaled nonnegative reuse remainder. `64C1A0` has an additional common recovery gate for phase `80` and unchained skills. | Frostbite/Slow, own/shared groups, changing skill group, equality, chain exemption and stable admitted deadlines. |
| Monster action timing | `5A1A40` now receives live keeper `8C`; monster attack/self-effect/summon plans use effective reuse. | Existing attack/chain/projectile tests and shared arithmetic vectors. |
| COS cast lifecycle | Positive-time monster attacks retain a COS recipient in the existing preparation queue. Release revalidates pet identity/life and keeps individual impact records; no overflowing aggregate damage sum. Both instant and delayed actions finalize. | Delayed two-impact release, token/target identity, death, unsummoning, changed reference/slot, attacker death and finalization. |
| Time bomb | `59B412`/`59B465` serialize the original authored damage word even when HP debit is smaller. | Overkill packet damage 300 versus actual debit 7. |
| Wall status context | `590680` receives the wall's optional `pw` mask, not the hit's blocked bit. A magical wall suppresses ordinary rolls unless the attack carries Stun; any wall context excludes Stun itself (`591E7C`). | Crystal/Fire Wall integration and absent/zero/physical/magical/combined mask cases. |
| Hit retirement | `58F491` releases Root only for nonzero magical damage, including imbue. `5939D8` gates Sleep/Stun retirement on the skill execution selector independently of final damage. Time Bomb protects Sleep/Stun, not Root. | Player, monster and COS physical/magical/mixed impacts; imbue and selector cases. |
| Periodic damage publication | `52A240` rejects self-source/dead-victim hits. `52A33D` privately notifies a credited player source, including COS victims through `52A1E0`/`52A940`; `52A38E`/`52A3FD` also notify player victims regardless of source credit. | Missing/dead sources, private routing, raw overkill damage, self-hit and fatal-slot guards. |
| Complete status-slot lifecycle | All 23 occupied slots retain replacement/source rules and retire through the common block. Fear/Confusion notify living monster AI on natural expiry, not cure/death. | Every slot: equal/stronger replacement, source departure, expiry equality, cure, death and no resurrection on later ticks. |

The player potion queue is `internal/game/item/recovery`; resident integration
is `action/potionrecovery.go`. There is no new timer goroutine or unwired
runtime hook. Both public and private vitals use the existing wire owners.

## Timing distinctions that must be preserved

Do not multiply every duration by action speed. Persistent cast release
(`5835F1`), projectile release (`585CA2`) and instant release (`586CE3`)
compare elapsed time with authored casting time. Chain continuation
(`4AEC32..4AECD8`) also reads authored duration and its latency budget.
Reuse-manager timing and AI command-result timing are separate consumers.

The browser already implements native status animation rates in
`engine/foundation/animation/status-presentation.ts` (`85C590`): Frostbite
uses 0.5 and Slow uses 0.75. Slow's animation rate is not the reciprocal of
server keeper 125%. Do not replace this native distinction with a shared
heuristic multiplier.

`4A9D00` schedules the 1.5-second thaw motion transition. This alone does not
prove a 1.5-second command lock: `4B0EA0` rejects motion 8/10h/12h and checks
the status mask; `58D8F0` checks mask `4041`. The port retains these status
admission rules without inventing a thaw command lock. The v1.150 browser
already derives the relevant animation changes from the abnormal mask;
the later server's opcode `3200` is not introduced into that protocol.

Mounted movement uses the COS's own immobilizing mask and speed keeper.
The native client composer at `692CB0` checks mount identity and reference
capability before sending tag 2; this is not evidence that the rider and
COS share a status block. The existing mounted attack route remains in
`action/coscommand.go`; this audit does not claim a reconstruction of every
server-side COS command or unmounted pet command.

`CGObjMob` inherits `4A6850` and `4A8770`: native monsters have a keeper-backed
MP accessor and resource callback. `4C382A` initializes MP from RefObjChar.
All 5,986 enabled monster rows in the published v1.150 character tables have
MP zero. This data constraint explains the current zero-MP monster adapter;
it is not a claim that the native monster class lacks an MP field.

## Native labels and feature mapping

Every newly exposed unnamed helper is inspected before naming. Save uses
`save_auto_snapshot`; verification reads the saved `PE/symbols` key. Never
use `BinaryView.save` on a database path.

- Server snapshots 285–286: 60 initial search/cooldown/container labels.
- Snapshot 287 corrects `49A5B0` to
  `CGItemExpendable_ApplyOrQueuePotionRecovery` (saved and verified).
- Snapshots 288–290: potion queues, timer/alchemy helpers and linked monster
  death helpers (`4C2700`, `4C2740`, `4C2730`).
- Snapshots 291–294: 53 timer-search helpers and three callback-vector helpers;
  all label names were read back from the saved symbol table.
- Snapshot 295: ten NPC damage-map insertion/allocation/iterator/exception
  helpers, each saved and verified. Go maps and the existing contribution
  owner replace native tree allocation; labels are not evidence of parity.
- Snapshot 296 saves and verifies `CGObjChar_ProcessNormalHit_ExceptionHandler`
  (`AB0C71`).
- Snapshot 298 saves the siege database-result owner (`6232C0`) and 20
  associated tax, guild, logging, time and teleport helpers.
- Snapshot 299 saves `CGObjMob_SetLinkedDeathPartner` (`4C2710`). Snapshot
  300 verifies the Root magical-lane and Sleep/Stun selector comments.
- Snapshot 301 saves 53 additional inspected inventory, COS lifecycle,
  record/container and exception helpers. All 53 names were read back from
  `PE/symbols`; none of the names exposed by that trace remain unnamed.
- Snapshot 302 saves and verifies seven incidental byte-search matches:
  native list destruction, a socket connection owner, two reference-table
  allocators and their registration, a scrap-reward allocator, and the
  Flag World carrier/buff owner. These map to container/transport/catalog
  infrastructure or the separate event-world subsystem, not new status slots.
- Client snapshots 242–243 save the status-decoration and target-interaction
  exception handlers. Existing `74FCD0`, `6935F0` and `877D80` labels were
  checked when their old unnamed references appeared in repository notes.

The incidental helpers map to existing `item/inventory` storage/stack owners,
`action/itemmove.go`, `costransfer.go`, `coscontainer.go`, `itemuse_cos.go`,
the persisted character/COS records, and existing item transaction/publication
owners. Go values, slices and maps replace native record pools and STL
containers; exception handlers do not require a second gameplay subsystem.
This mapping identifies ownership, not a proof of every inventory operation.

The linked-death setter's producer is `DoSummonSkillOfUnique` (`596E50`).
Its linked branches select `MOB_SD_HAROERIS[_DROP]` and `MOB_SD_SETH[_DROP]`,
which are absent from the published v1.150 character/skill rows. They are
trimmed by the repository's version scope, not grafted onto ordinary summons.

Stoneboard/Treasurebox/Kisaeng NPCs and art do exist in the data, but the
specific native quest registrations (`QNO_TQ_STONEBOARD_4`,
`QET_TQ_TREASUREBOX1..4`, `SN_QEV_CH_EVENT_KISAENG_070919` and `_081201`)
are absent from both published `questdata.txt` and the compiled quest catalog.
NPC presence alone must not activate an unauthored seasonal quest. The
existing quest catalog remains the registration owner. Likewise, the incidental
siege DB callback is not a claim that full fortress gameplay is implemented.

The user subsequently expanded the implementation scope to these fortress,
event and COS systems. The data/version observations above remain evidence,
but are no longer a reason to leave applicable v1.150 behavior unimplemented.
The active extension is tracked in
[`FORTRESS_EVENT_COS_2026-10-01.md`](FORTRESS_EVENT_COS_2026-10-01.md).

## Verification completed

`pnpm check source` passed all 12 tasks after the final COS source-echo change
(61.8 seconds), including server tidy, gofmt, vet, lint, tests, race subset,
govulncheck and release-contract checks. Earlier full source runs passed after
the potion, cooldown, wall/hit-context and victim-echo changes.

Focused tests passed for potion lifecycle, cooldowns, status retirement,
chains/projectiles, monster/COS attacks, independent COS parameters and
movement, periodic routing, and the 23-slot lifecycle matrix.

Client runtime unit tests passed: `status-presentation.test.mjs`,
`abnormal-snapshot.test.mjs`, and `abnormal-tooltip.test.mjs` (16 tests).
The existing client already consumes the corrected masks, speed values,
damage echoes and snapshots; no client source change was required here.

No authenticated browser session, full asset rebuild or production deployment
validation is claimed. The fixes need a server release before public bug
reports can be called live or closed on the basis of these changes.

Native quirks already checked: poison cannot kill; both Panic and Combustion
drain MP; burn/poison tick at fixed 2000 ms; equality does not tick or expire;
Zombie does not reverse skill healing; Frostbite/Slow share a movement source.
