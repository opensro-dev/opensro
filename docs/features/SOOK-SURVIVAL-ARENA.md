# Event So-ok Survival Arena

Port-only, off by default: `SRO_SOOK_SURVIVAL_ARENA=on`. Uses the imported
Colosseum in an isolated layer for each entrant. No PK2 changes. This release
requires matched Agent/GameWorld/client protocol **17**, store **42**, layout 12.

Talk to Event So-ok and choose **Survival Arena (5 stages, EXP/SP x5)**. There are
three entries per character per UTC day. The same off-by-default server flag
gates entry, eggs/modifiers, chest claims and silk redemption; saved items remain
stored when it is off. Login recovery still returns stranded characters safely. Enter alive, without a job suit,
stall/exchange/storage interaction or another event return anchor.

## Five stages

Each stage has a wave budget matching its spawning timer: 19 waves with the
standard five-minute setting. Clearing a wave starts the next Pandora
immediately, so clearing the budget finishes the stage sooner. If monsters
remain, timed reinforcements still arrive. Five minutes remains the spawning
cutoff; survivors must then be cleared. Decisions can extend the total event.

| Stage | Monster levels relative to player | Average monsters per Pandora | Total chest items |
| --- | --- | ---: | ---: |
| 1 | −6 to −3 | 1.8 | 3–7 |
| 2 | −4 to −1 | 3.6 | 8–15 |
| 3 | −1 to +2 | 7.2 | 18–28 |
| 4 | +1 to +4 | 14.4 | 32–45 |
| 5 | +3 to +6 | 28.8 | 50–70 |

Level 1 is the floor. References, including uniques, must fit their stage's
band; an unavailable unique is skipped. The safety cap is 200 living
monsters per private arena. Waves reinforce survivors every 15 seconds;
clearing a wave early starts the next Pandora without waiting. Its windup
is 1.2 seconds. Wave budgets are 40% smaller than the former 3/6/12/24/48
schedule, with fractional monsters carried between waves (stage I: 2/2/2/2/1).
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

## Rewards

Ordinary equipment, consumable, gold-heap and quest-item floor drops are
suppressed for the exact arena lease. EXP/SP and pet EXP are **5×** native.
Eggs and Pandora spawners remain. Eggs appear every eight seconds (formerly
15), during combat and clearing; they do not accumulate during reward decisions. Leftover ground eggs and Pandora props are removed at stage end
and again before the next stage starts.

Each completed stage grows the accumulated chest to the range shown above.
The final chest holds **50–70 items total**, including retained earlier rewards.
The native-style window shows **32 items per page**, with item tooltips and
Previous/Next page buttons. Bundles include:

- Native equipment with plus values, blues, and Seal of Star/Moon/Sun variants.
  Stages I–II cannot roll Sun gear (including pet gear). Sun chance per gear
  roll is 0.1% in III, 0.5% in IV and 1% in V, with no guaranteed Sun drops.
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
| `SRO_SOOK_CHEST_CAPACITY` | 200 | 1–200 | Accumulated item rows; keep at least 70 for full rewards |

Malformed settings retain defaults. Bundle counts, stage bands, decision time,
countdowns and pacing are in `game/sookarena` and `action/sookarena_rewards.go`.
Native loot, blue and seal property generators remain shared. Pet blue maxima
come from the existing pet-affix DB settings. There is no 10× arena floor-loot
multiplier. Optional difficulty modifiers and endless stages are not included.

See [map import](SURVIVAL-ARENA-MAP.md) and
[Claude handoff](SOOK-SURVIVAL-ARENA-CLAUDE-HANDOFF.md) for evidence/deployment.