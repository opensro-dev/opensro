# Event So-ok Survival Arena

Port-only, off by default: `SRO_SOOK_SURVIVAL_ARENA=on`. Uses the imported
Colosseum in an isolated layer for each entrant. No PK2 changes. This release
requires matched Agent/GameWorld/client protocol **17**, store **42**, layout 12.

## Release and deployment requirement

Land this feature with the next coordinated protocol/store release, not as an
independent patch to a protocol-16/store-41 deployment. The **protocol 17 and
store 42 requirements apply to every upgraded shard, even with the arena
flag off**. The flag controls gameplay, not release compatibility or migration.
Old browser tabs must reload. Do not relax the version checks to avoid this.

The release coordinator must:

1. Back up the authority/account databases and stop their writers before the
   schema-41 to schema-42 upgrade. Migration is one-way for the old binary;
   rollback requires the pre-upgrade backup and matched previous binaries,
   and cannot retain writes made after migration.
2. Deploy matched Agent, GameWorld and client plus matching asset/server-data
   bundles. Keep the arena off until this deployment is healthy.
3. For a shard opting in, build assets with `SRO_SOOK_SURVIVAL_ARENA=on` and
   the pinned Colosseum import described below, then enable the GameWorld's
   `SRO_SOOK_SURVIVAL_ARENA` (ops Nomad var `sook_survival_arena`). Production
   activation is an ops change, not something this PR enables automatically.

The independent retained-self-buff visual fix is excluded from this feature
and is reviewed separately. Ordinary crow-effect lifetimes are unchanged;
only the arena's own one-shot cues use its cold-model admission policy.

Talk to Event So-ok and choose **Survival Arena (5 stages, EXP/SP x5)**. There are
three entries per character per UTC day. The same off-by-default server flag
gates entry, eggs/modifiers, chest claims and silk redemption; saved items remain
stored when it is off. Login recovery still returns stranded characters safely. Enter alive, without a job suit,
stall/exchange/storage interaction or another event return anchor.

## Five stages

Each stage has a budget of **15 waves** with the standard five-minute setting.
Shortened operator timers may reduce that budget further. Clearing a wave starts the next Pandora
immediately, so clearing the budget finishes the stage sooner. If monsters
remain, timed reinforcements still arrive. Five minutes remains the spawning
cutoff; survivors must then be cleared. Decisions can extend the total event.

| Stage | Monster levels relative to player | Average monsters per Pandora | Total chest items |
| --- | --- | ---: | ---: |
| 1 | −8 to −5 | 2.8 | 3–7 |
| 2 | −6 to −3 | 4.95 | 8–15 |
| 3 | −3 to 0 | 8.1 | 18–28 |
| 4 | −1 to +2 | 11.25 | 32–45 |
| 5 | +1 to +4 | 14.4 | 50–70 |

Level 1 is the floor. References, including uniques, must fit their stage's
band; an unavailable unique is skipped. The safety cap is 200 living
monsters per private arena. Waves reinforce survivors every 15 seconds;
clearing a wave early starts the next Pandora without waiting. Its windup
is 1.2 seconds. Stage I adds one monster per wave to the original carried-density pattern
(3/3/3/3/2; 42 normal wave slots over 15 waves). Stages II–V are unchanged;
stage V keeps 15/14/15/14/14 (216 slots), before uniques, ambushes or cap truncation.
The opening has a visible **20-second countdown**. Three in
ten normal spawns immediately aggro; the others keep native sight aggression.

At each stage end: **End of stage N — kill all remaining monsters!** Spawning
stops and old ground eggs are removed. Fresh eggs continue every eight seconds
while you clear the remaining monsters.
A remaining-monster notice counts down from ten to zero. After one minute,
survivors reacquire the entrant every ten seconds.
Once all are dead, the chest opens for **five minutes**. Continue closes it
immediately and starts a visible **15-second countdown**, then the next wave.
Stage V offers extraction only. Decision timeout safely claims and leaves.
Combat, power deadlines and negative states pause during the choice/countdown.
Skill reuse freezes during the reward decision, then runs normally during the
15-second preparation countdown. You can cast self-buffs before the next wave;
those buffs and their new cooldowns are preserved when combat starts. The ordinary death return still waits five seconds.

## Cave encounters

The arena can use 30 regular Qin-Shi Tomb variants, level 81–100, with their
original levels and combat skills. The ten families are Tomb Soldier (81–83),
Qin-Shi Tombstone (81–84), Tomb Archer (84–86), Tombstone Ghost (86–88), Royal
Soldier (88–90), Tomb Bug Ghost (90–92), Snake Slave (91–95), Snake Woman
(92–97), Snake Demon (94–99), and Snake Man (97–100).

Stage bounds still win: a level-90 entrant can meet only level 82–94 monsters.
Traps, egos, quest copies and tomb-chain bosses are excluded. Existing installed
Donwhang cave monsters, such as Work Earth Ghost (62/64), Soldier Earth Ghost
(65/66) and Nachal (69/70), already enter the pool when their levels fit.

With the arena enabled and the public tomb disabled, only cave combat references
and skills are loaded: no cave nests, trigger chains or teleport doors are
activated. Client assets must be baked with `SRO_SOOK_SURVIVAL_ARENA=on`; the
model, animation, effect and skill builders include the matching combat overlay
without enabling cave teleport data. Rebuild/publish assets before restarting
the updated arena server; old packs cannot draw these additional encounters.

## Rewards

Ordinary equipment, consumable, gold-heap and quest-item floor drops are
suppressed for the exact arena lease. EXP/SP and pet EXP are **5×** native.
Eggs and Pandora spawners remain. Eggs appear every eight seconds (formerly
15), during combat and clearing; they do not accumulate during reward decisions. Leftover ground eggs and Pandora props are removed at stage end
and again before the next stage starts.

Each completed stage replaces the previous preview with a fresh chest in the range shown above.
The final chest holds **50–70 newly generated items**; earlier-stage items are not retained.
The native-style window shows **32 items per page**, with item tooltips and
Previous/Next page buttons. Bundles include:

- Native equipment with plus values, blues, and Seal of Star/Moon/Sun variants.
  Stages I–II cannot roll Sun gear (including pet gear). Stage I cannot roll Moon either. Approximate per-gear Star/Moon/Sun rates
  are **2/0/0%, 3/0.25/0%, 4/0.5/0.05%, 5/0.75/0.1%, 6/1/0.25%** across
  stages I–V, quantized to the native 15-bit sample space. There are no guaranteed seals.
  Existing saved rewards stay unchanged.
- Pet collars/harnesses/charms, including sealed versions, with existing blue rules.
- **Silk scrolls**, each redeemable for 10 real Item Mall silk. Use one from the
  bag; consumption and wallet credit commit atomically. Scrolls stack to20;
  each use consumes one scroll for10silk. Reward stack ranges by stage are
  **1–3 / 1–5 / 1–10 / 1–15 / 1–20**. They remain saved while the experimental flag is off; enabling it is required
  to redeem or claim. A stage provides at least one voucher.
- Native HP, MP and vigor recovery grains, in stacks of **50**. Regular HP/MP
  potions and universal pills are excluded from new rewards; saved rewards and
  carried items are not rewritten.
- Guaranteed kill-bracket gold, separate from chest items; no ordinary gold heaps.

Death forfeits the unclaimed chest, but preserves carried items and pays kill
gold once. Combat disconnect forfeits; a checkpoint reconnect preserves its
chest and may resume without another entry. Graceful server shutdown preserves
earned rewards. Full bags retain a saved entitlement at So-ok: close its window,
make space, select So-ok and choose the arena option again. Claims are atomic;
partial grants, repeated decisions and stale run IDs cannot duplicate rewards.

## Eggs

KABUM uses the bear-roar shockwave. Fire Pit is a stationary 30m field lasting
60 seconds: non-uniques lose **3% maximum HP per second**. Other powers include
2–10× damage, 30-second unkillable, unlimited MP, unstoppable and **500% of actual HP damage reflected** (you still take damage).
**Fleetfoot** grants +200% movement speed (3× normal) for 30 seconds.
Fleetfoot and Chain Reaction each have a distinct, simple HUD icon.
**Chain Reaction** lasts 30 seconds: each basic attack or direct-damage skill
copies its actual HP damage to up to ten other enemies within 10m of the
original target. An area strike copies its first damaged target once; secondary
hits and damage-over-time do not recursively copy. Overkill is excluded.
Bad eggs aggro all monsters or summon ten more, subject to the population cap.
They are private props, cannot enter inventories, and expire with their run.

## Operator settings

| Setting | Default | Bounds | Purpose |
| --- | ---: | --- | --- |
| `SRO_SOOK_CHECKPOINT_SECONDS` | 300 | 30–300 | Maximum spawning duration; also sets wave budget. 30 is accelerated testing only |
| `SRO_SOOK_CHEST_CAPACITY` | 200 | 1–200 | Reward item rows; keep at least 70 for full rewards |

Both are live tunables (`game/tunables`, Nomad vars `sook_checkpoint_seconds`
and `sook_chest_capacity`): the staff console can change them without a
restart, and a value outside its bounds or not a number stops the boot rather
than being clamped. Bundle counts, stage bands, decision time,
countdowns and pacing are in `game/sookarena` and `action/sookarena_rewards.go`.
Native loot, blue and seal property generators remain shared. Pet blue maxima
come from the existing pet-affix DB settings. There is no 10× arena floor-loot
multiplier. Optional difficulty modifiers and endless stages are not included.

See [map import](SURVIVAL-ARENA-MAP.md) and
[implementation notes](SOOK-SURVIVAL-ARENA-IMPLEMENTATION.md) for evidence/deployment.
Egg selection favors **Chain Reaction** and **Damage Surge**: each has about
11.82% of egg rolls, 30% more likely than the former equal 9.09% share. The
overall egg cadence is unchanged. KABUM has about 11.03% of egg rolls
(+30% relative to its previous share); Ambush has about 5.94% (−30%). Eggs spawned during the remaining-monster
cleanup are removed immediately when the last monster dies, before the reward
window opens; no old eggs remain during the decision or next-stage countdown.

## Cave asset scope

Arena encounters intentionally reuse Qin-Shi cave actors and combat skills.
An arena-enabled bake therefore includes the committed tomb combat overlays
and their degree-9 compound-branch stand-ins, even when `SRO_QINSHI_TOMB=off`.
This is an asset-resolution choice for those encounters, not permission to
open the cave world. The tomb's teleport links still follow its own flag.

| Tomb flag | Arena flag | Tomb combat assets / branch fallback | Tomb teleport links |
| --- | --- | --- | --- |
| off | off | off | off |
| off | on | on, for arena encounters | off |
| on | off | on | on |
| on | on | on | on |

## Troubleshooting assets

The eight buff PNGs are original, nonempty source artwork and are copied by
one normal publisher; they do not prove the Colosseum is installed. Verify
the importer completed, region `0x63ec` was published, and client/server data
match. Missing venue/navigation data refuses admission. No retail arena
models or textures are shipped in this repository.

## Asset build from the original v1.150 client

The baseline v1.150 client does not include the event's Colosseum sector.
Operators must additionally supply their own legally obtained **Silkroad
Online v1.696** client (Media.pk2 `SV.T` build 696). Put its unchanged
`Map.pk2`, `Media.pk2`, `Data.pk2` and `Particles.pk2` in `C:/SRO-v1.696`
or another folder passed as `--client-root`. No retail arena files are
committed or downloaded by this project.

Run `--help` to see the required version, folder layout and exact SHA-256
hashes from `scripts/survival_archive_pins.json`. The importer verifies all
four archives before parsing or writing anything; another version, regional
build or patched archive is rejected. It uses the existing Alexandria safe
link/catalog merges and normal converters, with v1.150 as the base. Neither
client's source files are modified or executed.

After preparing the original v1.150 extraction with the documented Python
asset-tool environment, run from the repository root (PowerShell):

```powershell
python scripts/import_survival_colosseum.py --help
$env:SRO_SURVIVAL_COLOSSEUM = "on"
python scripts/import_survival_colosseum.py --client-root "C:/SRO-v1.696" --native-root "C:/SRO-v150" --variant-root "C:/SRO-survival-build"
$env:SRO_GAME_ROOT = "C:/SRO-survival-build"
$env:SRO_SOOK_SURVIVAL_ARENA = "on"
pnpm assets build full
pnpm task build server-game-data
pnpm assets ledger
```

Generated output stays ignored. The generated tree follows the usual
shared-root resolver; use its documented `SRO_GENERATED_ROOT` override for
an isolated build. Package and publish through the normal release workflow,
with matching server data and client packs. Merely compiling the server does
not rebuild or publish assets. See [the map import guide](SURVIVAL-ARENA-MAP.md)
for import contents, conflict guards and focused verification.
