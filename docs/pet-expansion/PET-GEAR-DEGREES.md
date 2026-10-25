# Pet gear degree balance

Owner-approved change, 2026-10-15. Port-only; the existing
`SRO_ATTACK_PET_EXPANSION` flag still controls pet gear.

| Degree | Pet/monster level band | Maximum blues | Minimum roll percentile |
| --- | --- | --- | --- |
| D1 | 1–7 | 1 | 0% |
| D2 | 8–15 | 2 | 6.25% |
| D3 | 16–23 | 2 | 12.5% |
| D4 | 24–31 | 3 | 18.75% |
| D5 | 32–41 | 3 | 25% |
| D6 | 42–51 | 4 | 31.25% |
| D7 | 52–63 | 4 | 37.5% |
| D8 | 64–75 | 5 | 43.75% |
| D9 | 76+ | 6 | 50% |

These are the character equipment degree thresholds through D9. Pet drops now
inherit the degree of actual normal character-equipment loot, including
lower-degree items a higher-level monster can roll. Uniques use their normal
equipment tables; there is no additional pet-only degree promotion.
See `PET-LOOT-DB-CONFIG.md` for the DB chance multiplier. White pet items may have zero
blues. Magic drops roll one or two, subject to the degree cap. Rare drops roll
three through six, also subject to that cap. Six-blue weight is available only
on D9. A group may appear only once on an item, and only implemented affixes roll.

Higher degrees unlock the existing stronger affix tiers and improve rolls
within each eligible tier. The integer roll floor is
`affixMin + floor((affixMax - affixMin) * percentile)`; the maximum remains the
affix tier's authored maximum. For example, a 10–30% affix rolls 10–30% at
D1 if eligible, and 20–30% at D9. A D9 item can still select a lower affix
tier; D9 does not guarantee six blues or perfect rolls.

## Source and compatibility

Edit [`petdesign.json`](../../apps/server/internal/game/companion/petdesign.json): `gear` owns equipment requirements, maximum blues,
six-blue weights and roll floors; `rules` owns the global six-blue ceiling.
Legacy design drop chances/rank multipliers no longer control runtime drops.
Run `python scripts/tools/pet_design_export.py` to regenerate the client
module; the server and the asset pipeline read the file itself. Do not hand-edit
generated files.

Worn gear now stores six options. Existing five-option saves load with an
empty sixth slot; existing items retain their options and are not rerolled.
Do not roll back to a five-option server after issuing six-option items:
older code would discard the sixth option when loading them.

All27 slot/degree icons from `PET-ITEM-ICONS.md` are included. No extra icon
per blue count is needed: the degree selects the art, while the tooltip
shows actual options and the new per-degree cap.

## Verification

- Degree boundaries and unique promotion, all nine caps, rare/magic rolls
  for every slot and low/middle/high random draws.
- Roll values remain inside the degree-adjusted affix range.
- Old five-option JSON loads; six-option JSON roundtrips.
- Six blues survive actual equip/remove commands and client state decoding.
- All27 exported icons are distinct32x32 RGBA images.

These checks are not a
claim that every random affix combination has been tested in live combat.
