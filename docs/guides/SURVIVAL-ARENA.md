# Survival Arena — player guide

A private, five-stage survival challenge. Fight waves, collect temporary egg
power-ups, then decide whether to take your reward chest or risk another stage.
Available only on servers that enable the experimental Survival Arena event.

## Entering

1. Prepare your equipment, skills and recovery supplies. Leave space in your bag.
2. Talk to **Event So-ok** and choose **Survival Arena (5 stages, EXP/SP x5)**.
3. You enter your own arena. A **20-second countdown** gives you time to prepare.

You have **three entries per character per UTC day**. Enter alive, without a
job suit, and finish any stall, exchange or storage interaction first. Other
players have separate runs and cannot take your monsters or eggs.

## Fighting the stages

Each stage has **15 waves**. Kill the monsters to advance through them. Clearing a wave quickly
brings the next one sooner; leaving monsters alive still brings timed
reinforcements. Spawning lasts at most **five minutes per stage**, and may end
earlier when you clear its wave budget. Then defeat every remaining monster.

| Stage | Monster levels compared with yours | Fresh chest items |
| --- | --- | --- |
| 1 | 8 to 5 levels lower | 3–7 |
| 2 | 6 to 3 levels lower | 8–15 |
| 3 | 3 levels lower to your level | 18–28 |
| 4 | 1 level lower to 2 higher | 32–45 |
| 5 | 1 to 4 levels higher | 50–70 |

Regular Qin-Shi Tomb creatures can appear when their levels fit your stage.
Their natural levels range from 81 to 100; a level-90 character meets only
those within the stage bands above.

Average monsters per wave through stages 1–5: approximately **2.8, 4.95,
8.1, 11.25 and 14.4 monsters per wave**, carrying fractions across waves.
There can be up to **200 living monsters**
in your arena. Some attack immediately; others notice you as you approach.
After spawning stops, remaining monsters start hunting you after one minute,
with their pursuit refreshed every ten seconds. The last ten monsters are
counted down in notices.

EXP, skill EXP and pet EXP are **5×** their normal amounts inside the event.
Ordinary monster loot goes into the reward system instead of covering the floor.

## Eggs: useful powers and dangerous surprises

Walk over an egg to activate its effect. Temporary buffs appear beside your
character portrait. Eggs keep appearing while you clear the remaining monsters,
but are removed at stage transitions; they cannot be saved in your inventory.

| Power | What it does |
| --- | --- |
| Damage Surge | Randomly multiplies damage by 2–10 for 30 seconds. |
| Unstoppable | Temporarily protects against control effects. |
| Unkillable | Prevents death for 30 seconds. |
| Unlimited MP | Temporarily removes MP costs. |
| KABUMMMMMM | A roar shockwave destroys nearby event monsters. |
| Fire Pit | Burns a 30 m area for one minute, damaging non-unique monsters for 3% of maximum HP each second. |
| Mirror Shell | Reflects 500% of actual HP damage taken for 30 seconds. You still take damage. |
| Chain Reaction | For 30 seconds, direct attacks copy damage to up to ten additional monsters within 10 m of the target. |
| Fleetfoot | Adds 200% movement speed for 30 seconds. |
| Alarm | Makes every monster in your arena attack you. |
| Ambush | Spawns ten additional monsters, subject to the arena's population cap. |

## Take the chest or continue?

After all monsters are dead, a reward window opens. You have **five minutes**
to decide. Use **Previous/Next** to inspect every page; hover items for details.

- **Take your rewards and leave** to keep the currently displayed chest.
- **Continue** to risk the chest for the next stage. Clearing that stage
  replaces this preview with a completely new reward roll. The window closes
  immediately and a **15-second countdown** starts. You can cast preparation
  buffs during this countdown.
- Stage 5 offers extraction only. If the decision timer expires, the run ends
  and attempts to deliver the earned rewards.

Each stage rolls a fresh chest of the size shown above. Earlier previews are
replaced, not carried over or added together. Rewards can include character
and pet equipment, recovery grains in stacks of 50, and silk scrolls.

Approximate chance per gear roll (character and pet gear):

| Stage | Star | Moon | Sun |
| --- | ---: | ---: | ---: |
| 1 | 2% | Not available | Not available |
| 2 | 3% | 0.25% | Not available |
| 3 | 4% | 0.5% | 0.05% |
| 4 | 5% | 0.75% | 0.1% |
| 5 | 6% | 1% | 0.25% |

The remaining rolls are ordinary gear. Sealed rewards are never
promised. Each silk scroll grants **10 Item Mall silk** when used; redemption
requires the event to remain enabled. Kill-based gold is paid separately.

**Death loses the unclaimed chest.** Your carried gear and items remain yours,
and you return after five seconds. Disconnecting during combat also forfeits
the chest; reconnecting at a saved checkpoint preserves it. A graceful server
shutdown preserves earned rewards.

If your bag is full, the saved chest remains claimable. Make room, then talk to
So-ok again and choose the arena option. Do not discard your own equipment to
make space during combat.

For hosting and configuration, see the separate
[operator guide](../features/SOOK-SURVIVAL-ARENA.md).

Egg selection favors **Chain Reaction** and **Damage Surge**: each has about
11.82% of egg rolls, 30% more likely than the former equal 9.09% share. The
overall egg cadence is unchanged. KABUM has about 11.03% of egg rolls
(+30% relative to its previous share); Ambush has about 5.94% (−30%). Eggs spawned during the remaining-monster
cleanup are removed immediately when the last monster dies, before the reward
window opens; no old eggs remain during the decision or next-stage countdown.
