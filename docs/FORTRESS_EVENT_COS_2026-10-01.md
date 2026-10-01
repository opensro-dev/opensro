# Fortress, event and COS extension — 2026-10-01

Status: implementation in progress on `codex/fortress-event-cos`, based on
`1f452a2` after the 2026-10-02 rebase. This is not a completion or production-release claim. The user
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
- Mount now relocates the rider through the shared movement store to the
  admitted vehicle position before attaching it (`4FC643`), persists that
  position and publishes the correction followed by ride and speed state.
- Persistent-pet cancellation (`756C` / `B56C`) is registered on the normal
  authenticated action hub and reachable through native options binding 16.
  It checks ownership and the strict planar range below 100 (`511B40`),
  retains HP/MP, level, experience, name and inventory, clears the summoned
  state, retires pending pickup and follower work, and publishes ordinary
  despawn. Retained dead pets can also be cancelled; follow and pickup still
  require living pets. This does not yet provide summoner-item resummoning.
- Fortress begin/end publication is now idempotent and ordered with entering
  players' state seeds. Client `7E2100` disables flags by XOR, so duplicate
  end messages reactivated war. The browser already preserves this native
  rule; the server emitter must publish each transition exactly once.

## 2026-10-02 riding and hunger continuation

- Ordinary riding horses (COS band 1) now enter the summon, private record,
  spawn, movement, reconnect, ride-toggle and peer-visibility paths. Their
  record omits the death word and their spawn omits owner-name/GID tails.
  Client command ownership comes from the owner-private companion record.
- Consumable vehicle summoning binds the rider automatically. New actor
  restoration binds saved usable vehicles before bootstrap and initializes
  their movement speeds; repeated admission of a resident actor preserves a
  parked transport. Dismount retires band-1 horses; band-2 transports remain.
- Reference column 72 supplies ride permission. Mounts reject the shared
  committed-action/cast lock, posture and distance failures. Both native
  request composers return the same category-14 refusal. Summoning rejects
  trailing request bytes, battle, blocked posture and the shared action lock.
- Pet food consumes Param1, recovers HGP and sends v1.150 `3508/4`. The client
  composes native target tails for pet potions, cures, food and revival, with
  explicit selection required when several eligible records exist.
- Online attack-pet hunger retains fractional float32 drain. Reference
  parameter 4 (column 113) gives minutes per one percent; shipped wolves use
  five minutes. HGP below 3000 applies the native factor through the existing
  parameter keeper. Defensive combat stats use it now; the independent pet
  attack producer remains outstanding. Starvation uses shared death cleanup.
  Cancellation/horse retirement clear transient commands, abnormal state and
  hunger clocks so a new actor cannot inherit them.

Native checks: server `4D5E00` hunger, `4D60A0` eight affected parameters,
`6A602E` rate derivation, `49D240` food, `4FB2C0`/`4FA430` automatic mounting,
`4EC750` horse retirement, `483F40` reference permission. Server `482840`
was misleadingly labelled as an attack-pet test: the raw mask checks band 1.
It is now `CGObj_IsRidingHorseCOS`; snapshot 327 was saved and the label read
back. Client snapshot 264 retains the inspected hunger gauge label.

Pet EXP has been traced, **not implemented**. `410110` uses the pet's own
level and no player mastery-gap factor; `4D6370` caps positive EXP below the
next level when the pet has reached its owner's level. `4D6210`/`4EFD10`
follow the authored next-reference chain, restore vitals/HGP, and publish a
reference change before EXP feedback. Do not credit player EXP to a pet or
pretend a level counter implements this lifecycle.

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
| Mount relocation | Server `4FC643` | Move owner to vehicle before binding the ride actor |
| Dismount | Server `4FC6D0` | Detach ride actor and restore movement parameters |
| Pet cancellation admission | Server `511A60` | Owner state, owned record, strict planar range, pet combat-target refusal |
| Persistent-item toggle | Server `4E8FC0` | Existing summoned record cancels; dormant item submits resummon |
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

- Complete the remaining COS panel combat/name command presentation.
  Cancellation transport and basic retirement are implemented, but native
  pet-combat refusal must be connected when independent attack AI exists.
- Pet-owned attack/AI, including owner target rules, independent stats and
  movement. Mounted rider attacks do not establish attack-pet parity.
- Pet naming, inventory growth, experience
  distribution and owner movement tethers mapped above. Online hunger and
  feeding are now implemented; this is not complete pet progression.
- Finish job/world-specific mount and summon admission and level-suffixed
  transport reference resolution. Riding horses, automatic binding, reconnect,
  distance, posture, action locks and horse retirement are implemented; concurrent item-owned companions are now implemented (see below).
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

## Item-owned summoning and persistence completed on 2026-10-02

The single-record dependency identified earlier in this investigation is now
resolved. `Character.ActiveCOS` owns the consumable vehicle/legacy migration
record. Each persistent summoner's `InventoryRow.Summon` owns its companion.
Attack and pickup pets have separate bounded owner-derived GID ranges, so a
vehicle, attack pet and pickup pet coexist without replacing one another.
Commands, feeding/cures, abnormal effects, monster targets, movement and peer
visibility resolve the selected companion; dormant records never own a live
GID. Presentation is wired through the production GameWorld service.

Native `493100` is `CGItemCOSSummoner_Use` (an earlier database name incorrectly
called it a record reader). First use creates the retained record without
consuming the summoner. Use again dismisses the actor; subsequent use preserves
HP/MP, name, level, EXP, hunger, command state, bag and rentals. Explicit cancel,
reconnect, ordinary return/portal and GM relocation use the same ownership.
Native `4FA430` restores only alive summoned records; dead and expired pickup
records remain on their item. Legacy pets migrate only when their matching
summoner slot exists; no missing item is fabricated.

`492D20`/`492D40` now drive the complete item-body contract: 1 absent, 2 alive
summoned, 3 alive dormant, 4 dead, followed by reference/name, pickup remaining
lease and rental jobs (kinds 0/5). The client binds `3158` records to the owned
slot and applies `3645` state/lease deltas without discarding retained data.
Names and statistics are preserved; this does not add the separate pet-naming
command or pet experience producer.

All item copies carry detached retained state through inventory, warehouse,
COS bags, drops/pickup, merchant buyback and database snapshots. A summoner
cannot split/stack or leave a container while its actor is live. The latter is
a recorded ownership inference: allowing an active actor to cross ownership
would leave commands and world visibility attached to the wrong character.
Refused inventory operations and failed warehouse commits retain the original
record. No new SQL strings accept client values; the store retains its existing
parameterized persistence boundary.

Pickup leases use absolute deadlines, including offline time. `49D8F0` targets
an existing pickup summoner and renews it from max(expiry, now), using the
reference's minute value. The lease is saved and projected into native remaining
seconds; expiry dismisses only that actor. Drag/click-carry renewal and revival
select the actual summoner slot. A revived live corpse receives ordered vitals
and LIFE-alive publication; a dormant revival never publishes a stale GID that
may now identify a different pet. Retirement/death invalidates prepared monster
casts before any family GID can be reused.

Native evidence for this continuation includes server `4E8F20`, `4E8FC0`,
`4FCEF0`, `4FA430`, `492D20`, `492D40`, `493100`, `49D8F0`; client `59C2E0`,
`59C290`, `54FC80`, `6961B0`, `7654B0`, `830EC0`. Exposed helper labels were
saved and read back through Binary Ninja's existing database connection:
server snapshot 331, client snapshot 268. Characterdata column 67 is recorded
as the inferred bag-capacity source and tested against shipped companion rows.

Release protocol is now 5 on both client and server. Authority schema is 15;
table layout remains 5. The offline upgrade validates and backs up either
schema 13/layout 4 (adding the existing account tables) or schema 14/layout 5
(preserving those tables and their contents). Runtime startup never upgrades
implicitly. Publish the client/server pair together through coordinated release
admission; an older binary cannot read the new retained records. No live
release or database upgrade was performed in this continuation.

This finishes the item-owned summon/persistence slice, not the separately
listed attack AI, naming, progression, fortress or event work above.

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

### 2026-10-02 continuation

The final source run passed all 12 tasks in 111.4 seconds with the licensed
server projection configured, including vet, lint, tests and the race subset.
The final client run passed all 11 gates in 108.9 seconds. Focused pickup,
cancellation, mount and action/movement browser-runtime tests passed all 12
cases. These are source-level tests, not a live-browser parity claim.

Earlier attempts failed with the missing server projection, a truncated
ownership JSON from the rebase (`Expected double-quoted property name`),
nullable assertions in the new cancellation test, and stale generated NPC/
attachment packs. The projection, ownership map and assertions were corrected;
the concurrent asset rebuild completed before the final client run. No test
was removed or weakened to bypass these failures. No production deployment.

### Riding/hunger validation on 2026-10-02

Final source pipeline: all 12 tasks passed in 67.9 seconds, including server
vet, lint, tests, race subset and vulnerability checks. The full client check
passed all 11 gates in 121.7 seconds. Focused Go tests cover the shipped horse
item, automatic binding, both mount request paths, reconnect speed, retirement
and GM body-status inheritance. The client COS suite passed all eight tests,
including horse ownership without a public owner tail. Hunger/feeding tests
cover fractional drain, feeding recovery, the hungry-stat threshold and shared
starvation cleanup.

Earlier source attempts failed with `frames = [B5BD 3158 30D7 B4B5 376F],
want [B5BD 3158 30D7]` and `the summon itself must still succeed`. The first
fixture now exercises automatic mounting before parking; the second uses a
transport rather than a rabbit with a fabricated transport type. The new
client test initially failed `Property 'band' does not exist` on the narrow
active-COS view; it now asserts that view's GID contract. Final reruns passed
after those corrections. No live-browser validation, full asset build or
production deployment was performed for this continuation.

### Summoning/persistence validation on 2026-10-02

Final `pnpm check source`: all 12 tasks passed in 159.2 seconds. The server
gate included tidy, gofmt, vet, lint, tests, race subset, vulnerability scan
and release-contract checks. Final full client check: all 11 gates passed in
147.7 seconds. The upgrade CLI compiled after its final help-text correction.

Coverage includes all 13 shipped attack/pickup summoners, first use, item-use
and explicit dismissal, resummon, restart restoration, concurrent family
visibility, online/offline lease expiry, renewal, revival, stale-GID item
selection, prepared-cast retirement, actual pet-bag deposit/withdrawal,
detached ground snapshots, and warehouse commit failure plus database reopen.
Schema-13 and schema-14 offline upgrades validate preserved records and their
backups; schema-14 account tables are retained rather than recreated. Client
tests cover native item-body states, 3158 binding, 3645 state/time deltas and
explicit inventory targets for renewal/revival.

Earlier attempts reported `const cosSummonerNoRecord is unused (unused)`,
`resolve server game-data projection: identify server game-data artifact`,
`undefined: fmt`, and `itemUsePetExtension ... is not used`; these were fixed
by using the existing state constant, configuring the licensed-data projection,
and correcting the new imports/dispatch. The client initially reported
`Property 'source' does not exist on type 'GameplayCommand'` and later
`tests/runtime/cos-item-use.test.mjs (9)` type errors. The intent is now narrowed
before its callbacks, and fixtures carry complete inventory records. An
expired-renewal test initially used an invalid bag row, which correctly caused
`summon refused`; it now retains an authored potion row. Final reruns passed
without weakening the authority checks or removing tests.

No live-browser parity run, full asset rebuild, production database upgrade,
or deployment was performed. The implementation remains on the isolated
`codex/fortress-event-cos` branch until merged/released.
