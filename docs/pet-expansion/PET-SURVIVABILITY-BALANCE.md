# Pet survivability balance — 2026-10-24

Port-only attack-pet expansion. Native behavior with the expansion disabled stays unchanged.

## Targets

Against a same-level party giant, a full-health, ungeared pet without defensive
skills should die in approximately: Wolf 2 ordinary hits, Bear 3, Penguin/Crow 1.
These are balancing benchmarks, not fixed damage overrides. Critical hits, other
monster attacks, magical damage, shields, passives and rolls can change the count.

The retained `TestPetPartyGiantSurvival` uses real retail default attacks at their
normal midpoint roll, native rarity 0x14 scaling and actual COS defenses. It
checks all four species at levels 10,20,40,60,80,90. All 24 cases pass.

| Level | Bear HP | Wolf HP | Penguin/Crow HP |
|---|---:|---:|---:|
|1|20|16|10|
|10|185|165|100|
|20|245|235|160|
|40|390|350|230|
|60|1025|900|590|
|80|5000|4500|3000|
|90|11000|8100|4800|

Intermediate levels interpolate. The current retail-cap curve stops at level90;
levels above90 retain that endpoint and require a separate higher-cap balance pass.
Bear defense is100% of native reference defense, Wolf70%, Penguin/Crow40%.
Existing role-specific attack multipliers and all learned skills are unchanged.

## Gear

The requested growth mainly affects survivability. Each valid item adds an
explicit white maximum-HP stat: degree ×100/3 percent. Bonuses add across the
three correctly matched, level-eligible slots. A full unsealed D9 set gives
+900%; the cap is +2250% maximum HP, which only seals reach (3 × D9 Seal of
Sun: 300% × 2.5).

A full D1 set gives2× base HP; D2 gives3×; ...; D9 gives10×. Existing flat defense
and blues still work, so this is an HP cap, not a hard cap on effective survival
with defensive skills. No new global attack multiplier was added.

Equipping increases capacity without free healing. Removing gear clamps HP;
revival, summoning, growth, restoration and potions use the same effective maximum.
Clone hatch HP snapshots source HP once, without applying equipment twice.
The item tooltip shows the HP bonus in white; owned pet gauges use the new curve
and equipped HP when Resolve expires.

## Editing

Edit `docs/pet-expansion/pet-design.json`:

- `pets[].healthByLevel`: strictly ascending level/HP anchors.
- `pets[].defensePercent`, `magicDefensePercent`: native defense multipliers.
- `gear[].items[].baseStats.maxHPPercent`: per-item HP bonus.

Run `python scripts/tools/pet_design_export.py`, then the action, companion and
enterworld tests plus the client check. Generated copies are not edited by hand.
No persisted field or schema change is needed. Existing gear adopts the new base
stat automatically. Existing over-cap pet HP clamps on restore/summon.
