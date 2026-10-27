# Crow rework — support kit

**Current verification (2026-10-23):** Stormbrood replaces Exposing Mark
(49–51). Latest tuning: Mana Spring restores **10% actual damage as MP at
every rank**. Tailwind stacks and grants speed in safe zones, including nearby
party in III. Death burst remains **500% normal clone attack**, using the shared damage calculation. All five crow rows have owned feather particle animations; see
latest section. Live ten-summon cap and full ten-minute expiry passed: ten
eggs hatched, all ten clones coexisted, cast11 was refused without charge
loss, then all ten expired with exactly one wind burst each
(600.072–600.077s). The owned translucent spiral wind effect was observed in
the browser and softened after visual review. All12 client gates, full
action/companion/domain tests and scoped Go lint pass. Detailed current
acceptance and evidence are in the final section below.




Implemented on `feat/attack-pet-crow-rework`, based on penguin merge commit
`e40e506b`.

## Agreed behavior

Tailwind replaces Swift Wings in passive row four, preserving book IDs55–57,
learned ranks, and the existing10/40/90 unlocks.

- I: gain one movement-speed stack after every5 seconds without dealing or
  taking damage. Maximum10. Any damaging hit removes all stacks and starts a
  fresh five-second wait. Standing still also charges; movement is not
  required.
- II: at10 stacks, the next damaging hit dealt OR received attempts a2-second
  stun on that enemy, then consumes all stacks. User explicitly confirmed both
  directions. Native resistance and unique-monster immunity remain in force.
- III: the owner and nearby party members within20 m receive independent
  counters. One member taking/dealing damage does not consume another's
  stacks.

Existing expansion eligibility applies: living nearby summoned crow, slotted
learned book, supported normal world, no town/job/free-battle benefit. Stuns
are PvE; damage against players still removes travel stacks. Direct
physical/magical hits, later skill stages, credited effect hits and periodic
damage reset stacks. Misses, blocks and fully absorbed zero-damage hits do
not. Crow attacks do not count as the owner's attacks for this passive.

Movement is now 5% per stack in every book/rank: ten stacks grant 50%. Books
II/III add the stun and party scope; existing rank prerequisites remain.
Multiple crows contribute the strongest speed bonus, never multiplied grants.
Unsummon, death, separation, slot/book change, party departure and logout
remove grants. Charges are transient and never saved to the character
database.

## Code and tuning

- Canonical values/descriptions: `apps/server/internal/game/companion/petdesign.json`.
  Run `python scripts/tools/pet_design_export.py` after editing it; it
  regenerates the client module, and `pnpm task check:pet-design` keeps it matched.
- `apps/server/internal/game/action/pettailwind.go` owns grants, timing,
  range, damage consumption, native stun resolution and movement publication.
  Constants: five-second period, ten-stack cap; stun duration2 seconds; party
  radius20 m.
- Movement reaches the actual world mover and native0x376F observer speed
  pair. It preserves roots/slows and combines with existing native movement
  modifiers.
- Private0x3FB7 packet: recipient u32, book u8, stacks u8, movement-percent
  x10 u16. The client validates the complete frame before updating state.
  Owner and party HUD show their own stacks, movement bonus and full-stack
  stun readiness. The pet tree also shows the owner's stack count. HUD follows
  the existing attack pet expansion display toggle.
- No new animations or feather attack rework yet. The remaining crow skills
  are unchanged apart from Mana Spring, documented below.

Dated verification logs from the development sessions are kept outside the repository.


## Wind-burst damage correction — 2026-10-25

The burst now prepares its strike through the same species/equipment scaling as
a normal clone attack, then multiplies attack power by five. The former manual
strike setup skipped the crow profile. A regression compares both paths with
the same combat roll for bare, D1, D9 and D9 Sun collars. Each native impact
rounds separately; the comparison allows at most four rounding points per
impact when multiplying by five. Book II still produces no death burst.

Native yellow damage text (3128) and target HP refresh (33A6) now accompany the
burst. Feedback routes through the surviving crow because the clone leaves
observer scope during its death tick.

Live acceptance: one naturally expiring clone dealt **19,780** damage to a
training dummy. The yellow damage notification matched the exact HP decrease
(99,766,786 to 99,747,006), with one burst after 600.191 seconds. Normal clone
hits were 3,707–4,025, plus one critical hit of 7,838. Independent random rolls
vary; the deterministic regression verifies 500% with the same roll and only
integer-rounding tolerance. Full server tests, scoped vet and lint passed.
Temporary test characters, gear and GM access were restored afterward.
