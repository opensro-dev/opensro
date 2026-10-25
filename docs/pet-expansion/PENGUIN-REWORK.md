# Penguin frost caster

This branch replaces the penguin's five skill rows. It preserves IDs 31–45,
learned ranks, gear and the three passive slots. It uses the existing
`SRO_ATTACK_PET_EXPANSION=on` gate; off retains native companion behavior. The
canonical tuning source is [petdesign.json](../../apps/server/internal/game/companion/petdesign.json), `treeSkills`.
The older `skills` array is historical, not the live three-book tree.

## Books

Books unlock at pet levels 10 / 40 / 90 and retain the existing rank and
previous-book requirements. The two actives can be dragged onto the skill bar.
They share cooldowns with the other books in their own row.

| Row | Book I | Book II adds | Book III adds |
| --- | --- | --- | --- |
| Ice Lance | 15 m cast; 120–300% magic damage by rank; 40% frostbite chance; 6 s cooldown | Pierces two other enemies in a forward line | +50% damage against targets already frostbitten before the hit |
| Blizzard | Targeted 5 m storm; four hits, one second apart; 40–100% magic damage per hit; 20 s cooldown; at most eight targets per pulse | 7 m radius; frostbite slow | Final pulse freezes enemies present at all four pulse samples |
| Shatter | +10–20% damage against frozen/frostbitten enemies | Owner attacks also gain the bonus | Frozen kills burst for 50% of the killing action's damage, hitting up to five other enemies within 5 m |
| Arcane Resonance | Penguin magic actions build up to five stacks, lasting 8 s; +2–5% magic attack per stack | Owner magic actions build an independent pool; +3–6% per stack | +4–7% per stack; at five penguin stacks, the next active repeats once and consumes those stacks |
| Frozen Hourglass | Penguin magic hits build five charges; the next active consumes them and recovers 2 s of that row's cooldown | Owner magic actions also build the shared charges | Spending charges also extends that spell's frostbite/slow by 2 s |

Damage percentages multiply the pet's native magical attack action. Gear,
resistance, block and normal combat formulas still apply. Freeze/frostbite use
the existing status engine, including resistance and native durations.
Hourglass's recovery is currently a fixed 2 s at every rank; its books add
owner charging and duration extension. Balance remains provisional.

## Combat and lifecycle rules

- Ordinary-world PvE only: no player targets, job/free-battle or
  instanced-world bonuses. The living owner and penguin must remain within 20
  m, outside town.
- The penguin approaches only to its 15 m casting reach. Actions have a 600 ms
  windup. It retains its retail model and attack pose. Small original ice-bolt
  meshes fly from the penguin; Blizzard uses an original snowflake seal with
  spiralling falling crystals. Sounds still reuse published frost WAVs.
- One successful action grants one counter increment, regardless of victims or
  multiple impacts. Storm repeat pulses and echoed spells grant no charges.
- A full Resonance pool is spent only on the penguin's next active. The owner
  pool grants magic attack; it does not duplicate owner spells.
- Echoes use the normal damage/reward path without a second cooldown or cost.
  The original action starts the next charge cycle at one.
- Hourglass recovery floors at the current time. The existing equipment
  cooldown-reduction cap still applies before this flat recovery.
- A storm snapshots its center and damage row, checks population/range/line of
  sight on each pulse, and admits at most eight monsters per pulse. At most
  four storms can exist per pet, including echoes. Late ticks do not replay
  missed damage in a burst.
- Native damage/status/reward owners commit all HP changes. Shatter's credited
  secondary damage cannot recursively trigger another Shatter.
- Resonance/Hourglass progress and storms are session state; they are not
  saved to the database. Separation, death, unsummoning, logout or eligibility
  loss clear them. Refunding the passive clears its progress; the highest
  learned book is always active. No schema migration.

## Client contract and resources

Penguin-only `0x3FB6` counter payloads append three bytes to the existing
17-byte payload: penguin Resonance, owner Resonance and Hourglass (all 0–5).
Other pets retain their previous wire shape. Malformed counters are rejected
atomically. Skill and owner buff icons display counters and a ready highlight.

Native emote signals D0–D2 identify Lance; D3–D5 identify Blizzard; D6 is
Shatter. Blizzard signals only the primary target, so an area hit draws one
stationary field rather than one overlapping storm per victim. The existing
bounded effect lane consumes these target-anchored signals. No PK2 files are
modified. `/assets/effects/penguin-ice-shard` and `penguin-frost-sigil` are
generated meshes/textures owned by the client; wizard frost WAVs remain
reused.

Dated verification logs from the development sessions are kept outside the repository.
