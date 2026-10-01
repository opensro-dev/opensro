# Fortress, event and COS extension — 2026-10-01

Status: implementation in progress on `codex/fortress-event-cos`, based on
`7d0eb0e`. This is not a completion or production-release claim. The user
explicitly expanded the abnormal-status audit to include these systems.
The v1.150 content boundary still applies; the later server supplies rules.

## Implemented paths

- The client drop manager now submits native `769E/08` automatic pickup.
  Category, reservation, party item sharing, public-drop settings, nearest
  selection, pending commands and retry deadlines have one simulation owner.
- The server routes automatic and manual pet pickup through the same live
  position, ownership, container and grant authority. Arrival, death, target
  loss, cancellation and timeout all retire the native command acknowledgement.
- Automatic `B06D` grants and `B69E` command results have separate lifetimes
  from manual inventory requests. Neither a grant nor a command refusal may
  release an unrelated manual transaction.
- Follow (`769E/09`) cancels pending pickup and resumes the existing pet
  follower. Its native options binding (index 17) now reaches gameplay.
- Ride toggle (`74B5`) and the existing mount command (`769E/0B`) share one
  authority. Native mount range is 30 units in 3D, after rejecting incompatible
  coordinate planes (`4FC5D9`). Dismount parks the transport
  at its live position and restores the rider's movement parameters. Options
  binding 15 now reaches the ride command.
- Fortress begin/end publication is now idempotent and ordered with entering
  players' state seeds. Client `7E2100` disables flags by XOR, so duplicate
  end messages reactivated war. The browser already preserves this native
  rule; the server emitter must publish each transition exactly once.

## Native evidence

Claims were checked against raw instructions, including the original scalar
constant for mount range; database names alone were not treated as proof.

| Owner | Binary/address | Relevant rule |
| --- | --- | --- |
| Drop admission | Client `77D7D0` | Quest exclusion and ownership/category flags |
| Candidate selection | Client `77DBA0` | Nearest eligible drop inside 500 units |
| Automatic request | Client `77DFA0` | Cash-pet bit `80` enables pickup; `769E/08` |
| Pickup completion | Client `77DD10` | Terminal classes and 1/3/5-second retries |
| Pet panel commands | Client `6A2350` | Attack, follow, summon cancel and ride paths |
| Ride request | Client `6FFB40` | `74B5`: state byte, COS GID |
| Ride response | Client `777F60` | `B4B5`: success/rider/state/COS or failure byte |
| Mount authority | Server `4FC4D0` | Identity/state gates and inclusive 30-unit 3D range |
| Dismount | Server `4FC6D0` | Detach ride actor and restore movement parameters |
| Pickup AI | Server `55AB70`, `55AC20` | Approach then common inventory transaction |
| Target failure | Server `55ADF0` | Failure serializer, not an item-grant function |
| Pickup receipt | Server `559310`, `4E9480` | Transaction feedback and item receipt |
| Pet target admission | Server `528F40` | Owner-mediated target legality |
| Fortress request | Client `703130` | `71E1` request family, per-selector layouts |
| Fortress response | Client `754A40` | `B1E1` manager response family |
| Fortress DB results | Server `6232C0` | Tax, treasury, guild and structures |
| Flag carrier | Server `647D40` | Team buff and carrier lifetime |
| Flag event notices | Client `4B2530` | `30A9` event, gate and carrier messages |

Client snapshots 244–246 persist corrected pickup/configuration/composer
labels. Server snapshots 303–305 persist the inspected inventory/state/COS
helpers; snapshot 305 was read back to verify
`CAIState_PICKITEM_SendTargetFailure`. In particular, the old sound-manager
names at client `77DFA0` and `77E310` were incorrect.

Server snapshot 306 saves the inspected COS manager helpers, including summon
family admission (`4FCEF0`), summon cancellation (`4FAAB0`), database completion
(`4FAF60`), naming (`4FB610`), satiety transitions (`4FB8C0`), inventory growth
(`4FC960`), experience (`4FCB00`, `4FCDA0`) and movement tethers (`4FD1D0`).
Snapshot 307 saves `CGObjCOS_DispatchOwnedCommand` (`4D2200`): attack enters
the pet AI through event `19`, follow through `17`, and pickup through `1C`.
All 27 COS manager labels and the command-dispatch comment were read back
from the saved snapshots. These labels establish evidence, not implementation.

Client snapshot 248 saves the fortress request composer. Snapshot 250 saves
and verifies `CPSMission_OnCosSummonCancelB56C` (`778340`): cancellation uses
`756C` plus a GID; success is `[1]`, failure is `[2,error]` with notice category
`0C`. Despawn retires the scene object independently. Cancellation still needs
the durable summoning-item lifecycle; merely hiding the object is incomplete.

Client snapshot 251 saves and verifies the exposed fortress flag/list/alliance
and crest helpers, the COS record reader, the item magic-option reader and
the UTF-16 string reader. The flag helper's OR/XOR distinction is saved as
an instruction comment and was verified by reading the snapshot back.

## Outstanding implementation

- Complete COS panel command presentation and summon cancellation/cleanup.
- Pet-owned attack/AI, including owner target rules, independent stats and
  movement. Mounted rider attacks do not establish attack-pet parity.
- Pet summon/resummon persistence, naming, satiety transitions, inventory
  growth, experience distribution and owner movement tethers mapped above.
- Finish mount admission and native owner-to-vehicle relocation (`4FC643`)
  through movement authority. Range and coordinate-plane checks alone do
  not establish complete ride parity.
- Finer server pickup refusal classes, reservation-refresh reacquisition,
  and any remaining native local busy-state branches.
- Fortress manager requests/UI, persistent ownership/tax/treasury, guild
  authorization, registration, structures and war lifecycle. The current
  `game/siege` seed emitter is not this implementation.
- Flag-event world authority and client presentation, including carrier
  replacement, buffs, death/drop/leave and round-end cleanup.
- Reconcile seasonal quest registrations with the v1.150 catalog through
  the existing quest owner. Do not activate later-version content merely
  because the later server contains a registration.

## Verification log

Focused Go COS/pickup/mount/codec tests passed. Client pickup integration and
ground/notice tests passed (28 tests); the final focused COS suite passed six
tests, including concurrent manual/automatic receipt handling and ride commands.
`verify:quick` and `verify:test-types` passed.

The first `pnpm check source` run failed its server test gate with
`resolve server game-data projection: identify server game-data artifact:
The system cannot find the path specified.` Eleven other tasks passed. The
rerun with the explicit licensed-data projection passed all 12 source tasks
in 129.1 seconds, including the server checks. The final full client check
passed all 11 gates in 133.0 seconds. After the coordinate-plane mount guard,
the focused COS ride/command/pickup Go tests passed again with `-count=1`.
No browser, full asset build,
deployment or fortress/event gameplay validation has been performed here.

The subsequent fortress publication fix passed `go test -race
./internal/game/siege -count=1`, including duplicate transitions from both
initial states and concurrent begin/end callers replayed with native XOR
semantics. The final source rerun passed all 12 tasks in 76.6 seconds. The
existing client fortress/crest/music suites passed all 20 tests; they include
the native XOR end arm, relation colors, owner deltas and music transition
ordering. Those checks do not establish complete fortress gameplay.
