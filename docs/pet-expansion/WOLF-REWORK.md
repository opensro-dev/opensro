# Wolf kit

Port-only, behind `SRO_ATTACK_PET_EXPANSION` (server, default off) and the
client's Experimental "Attack-pet training" option. Ordinary-world PvE only:
no effect in towns, PvP, job suits, free battle or instances. Not a native
claim. Values are starting balance, not final.

The wolf keeps two manual active rows and three passive rows, with books at
pet levels 10/40/90 and 10 ranks per book; a book needs the previous one
mastered. Skill IDs 1..15, learned ranks, slots and gear are unchanged from the
earlier wolf tree, so existing characters keep their progress.

## Skills

Ranges are rank 1..10. Actives in a row share one cooldown across books.

| Row / IDs | Skill | I | II | III |
| --- | --- | --- | --- | --- |
| Active I / 1..3 | Hunting Howl | wolf +10..100% attack speed, 8 s | wolf and owner +120..300% | wolf, owner and party within 20 m +320..500% |
| Active II / 4..6 | Savage Rend | 160..250% hit, 6 s bleed | 220..310%, cleave 3 within 4 m | 280..370%, cleave 5 within 6 m, +25% vs bleeding |
| Passive I / 7..9 | Execute | wolf hits execute below 5..11.3% HP | owner hits too (12..18.3%) | every 7th wolf or owner attack sweeps 10 m (19..25.3%) |
| Passive II / 10..12 | Bloodscent | up to 5 stacks, +1..2% damage each | 8 stacks, +2..3%; owner pool | 10 stacks, +3..4% and +0.5% crit each (crit cap 5) |
| Passive III / 13..15 | Blood Hunt | 4 wolf hits mark prey; Rend +50..100% | owner hits charge too; +75..125% | +100..150%; 50% splashes up to 5 bleeding enemies within 6 m |

Rules:

- **Hunting Howl**: manual, no target, 25 s base recharge. The wolf stops and
  holds 1.4 s for its howl. Party members are snapshotted at cast (20 m, same
  plane); a player receives only the strongest live howl, never a product.
  Attack speed follows the existing action-interval rules.
- **Savage Rend**: manual and targeted (a monster is required), 12 s base
  recharge, normal approach and range. Bleed: 6 s, a tick every 2 s, 2x pet
  level per tick (4x in II/III); a reapplication refreshes it. Cleave respects
  line of sight and the normal hostile population.
- **Execute**: thresholds include the +5 point bonus against bleeding targets
  after proficiency 5; HP must be strictly below. Uniques are immune; players
  and companions are never executed. Kills go through the normal credited
  hit, death, reward and loot doors, so each settles once. Wolf and owner count
  attack actions separately (one multi-hit or area action counts once); sweeps,
  damage over time and procs never count or chain. The owner side needs a
  living summoned wolf within 20 m; losing it, a book change, death or
  unsummon clears progress.
- **Bloodscent**: one stack per committed damaging attack action on a monster;
  6 s, refreshed by the next. Wolf and owner pools are separate; the owner's
  bonus is a stat-sheet grant withdrawn as soon as eligibility ends.
- **Blood Hunt**: four landed damaging actions on the same monster prepare one
  mark (a multi-hit or area action adds one, on its first damaged victim); the
  mark lasts 8 s and moves with the prey. A landed Rend on marked prey consumes
  it for the bonus (a share of the Rend's committed direct damage on that prey).
  III's splash uses the credited hit door, with no extra cast, bleed, count or
  recursion.
- Hits on players never build stacks, charges or Execute progress.

Counters reach the owner in the port-only private packet 0x3FB6 (17 bytes:
pet GID, bear and owner counters, wolf and owner Execute steps, wolf and owner
Bloodscent stacks, hunt charges, hunt target GID). The client still accepts
the earlier 8/10/12-byte forms.

## Visuals

The wolf has its own look and never uses the bear's roar or shield models.
Every visual is a retail effect program, listed in one table
(`apps/client-next/src/engine/foundation/animation/wolf-effects.ts`), plus two
clips authored at model admission from the retail wolf skeletons
(`foundation/animation/bear-roar.ts`). No asset files are added. The server
signals each visual with a COS action emote (0x324B) on the object it belongs
to (`apps/server/internal/game/action/petwolfvisual.go`); gameplay never reads
these signals.

| Signal | Sent on | Motion | Effects |
| --- | --- | --- | --- |
| 1..3 Hunting Howl | wolf | `pet-howl`, 1.6 s: paws planted, chest dips, neck and head throw the muzzle at the sky, jaw pulses, ears fold | red flush; beast-howl ring; II/III war cry on the owner; wolf shout |
| 4..6 Savage Rend | wolf, with the cast at its windup (on the hit for instant casts) | `pet-rend` replaces attack1 for that cast on attack1's timing: crouch, lunge, jaws snap at 45 %, head-shaking tear | wolf strike, blood spray, II/III slash arc |
| 0xF0 executed | victim | - | blood death burst and blood-flower spikes |
| 0xF1 Execute III sweep | wolf or owner | - | blood-flower eruptions in rings at 5 m and 10 m; light shake |
| 0xF2 Blood Hunt consumed | prey | - | blood-touch strike and heavy blood spatter |
| 0xF3 Blood Hunt III splash | each bleeding enemy hit | - | wide blood spatter |
| 0xF4 Bloodscent full | wolf or owner | - | crimson spiral and pooled red glow |

Actions 0xF0 and above are port-only and never select an emote clip.

## Tests

- `companion/wolf_test.go`: tiers, scaling and thresholds.
- `action/petwolf_test.go`, `petwolfkit_test.go`, `petwolfhunt_test.go`: owner
  tier gates, exactly-once kill credit, independent seventh-attack sweeps, 10 m
  and HP boundaries, no recursion, eligibility cleanup, howl targets and expiry,
  Rend cleave and bleed, stack and mark lifecycles, player hits building nothing,
  and every visual signal sent once to viewers and owner.
- `client-next/tests/runtime/wolf-effects.test.mjs`: every effect part is
  published, compiles, is not a bear model; the sweep reaches 10 m.
- `client-next/tests/runtime/wolf-howl.test.mjs`: both clips on all four wolf
  rigs (planted feet, muzzle to the sky without rearing, head shake, neutral
  recovery) and emote routing.

## Open

- Balance: every value above needs gameplay tuning.
- Live acceptance of the Execute III sweep with several victims, Blood Hunt's
  bonus on surviving prey and its splash, and Bloodscent at full stacks; their
  visuals were checked on the client and their signals by server tests.


## Functional verification

All three books of each wolf row have recorded functional coverage, including
Execute sweeps, Blood Hunt damage on surviving prey and splash onto nearby
bleeding enemies. See [the pet skill acceptance matrix](ALL-PET-SKILL-ACCEPTANCE.md)
for case evidence and the limits of rank and visual coverage.
