# Pet drops: native equipment tables plus DB factor

Port-only, behind `SRO_ATTACK_PET_EXPANSION`. The original shared level/country
equipment tables are generated into `loot/.generated/equipment.json`; this
server does not query an original MSSQL drop table during kills.

Each actual admitted character-equipment drop independently has a chance to
produce one additional collar, harness or charm of **the same degree**.
The factor is the SQLite authority DB's `meta` row `petGearDropFactor`:

| Value | Meaning |
| --- | --- |
| `0` | No pet equipment drops |
| `0.10` | Default: about 10 pet items per 100 normal equipment drops |
| `0.25` | About 25 per 100 |
| `1` | One pet item per normal equipment drop (live test setting) |

This is a fractional multiplier relative to normal loot, not an independent
per-kill chance. It must be finite and between 0 and 1. Missing row defaults to
0.10; malformed/out-of-range DB settings refuse shard startup with an error.
The value is read once at startup; restart the shard after changing it.

Normal rank/unique tables, level-gap admission, ordinary loot cap and beta
equipment rates all participate before the factor. Gold, consumables, NPC
stock and unsupported equipment degrees do not produce pet gear. The original
equipment remains on the ground too. Pet blue count, tier eligibility and
roll floors still use the design workbook; NPC shop protection remains active.

## Safe DB edit

Stop the backend first. From the server module:

```powershell
go run ./cmd/tools/sro-pet-loot -authority-dir <actual-shard-authority-directory>
go run ./cmd/tools/sro-pet-loot -authority-dir <actual-shard-authority-directory> -factor 0.10
```

The tool takes the normal store lock and changes only this meta setting.
It never edits character records. No new DB table or schema migration is needed.
For an existing SQLite DB editor, the equivalent stopped-shard operation is:

```sql
INSERT INTO meta(key, value) VALUES('petGearDropFactor', '0.10')
ON CONFLICT(key) DO UPDATE SET value=excluded.value;
```

Restart with the new binary. Do not edit the generated loot JSON or the old
workbook drop-per-kill column to tune this rate; neither is a live DB setting.
To end a boosted test, restore this single factor rather than restoring an
entire DB backup and losing legitimate test pickups/progression.

## Blue option settings

The same authority database now owns `pet_affix_rules`, seeded once from the
design catalogue. Existing rows are never overwritten on restart.

| Column | Controls |
| --- | --- |
| `code`, `name`, `affix_group` | Stable identity and explanatory labels; leave unchanged |
| `minimum`, `maximum` | Integer roll limits for that blue tier |
| `selection_weight` | Relative chance to select this blue among eligible tiers; 0 disables it |
| `roll_power` | Value bias: 1 is uniform, 3 favors low values, larger values make top rolls rarer; allowed 1–20 |

Cooldown tier 3 (`code=77`, of Timelessness) defaults to maximum **50%** and
roll power **3**. This is the maximum on one blue, not a guaranteed roll.
Existing degree floors still raise the minimum result on higher degrees.
Eligibility by degree/slot, implemented status and one-blue-per-group rules
still apply. Catalogue rows for unimplemented blues remain ineligible.
Total effective cooldown reduction from equipment and passives remains capped
at 80%; the pet inventory displays the effective total.

Stop the backend before changing DB configuration, then restart it:

```sql
SELECT * FROM pet_affix_rules WHERE affix_group = 'Cooldown';
UPDATE pet_affix_rules SET maximum = 50, roll_power = 3 WHERE code = 77;
```

Changes affect future monster drops only. Saved item rolls are never rerolled.
Invalid ranges, non-finite values, negative weights and invalid powers refuse
startup. Refresh the DBeaver connection to see the new table after deployment.

## Base equipment stats

Pet gear has flat physical/magical attack, physical/magical defense, hit and
parry bonuses. Collars provide attacks/hit, harnesses defenses/parry, and charms
smaller bonuses to all six. They are summed before percentage modifiers, and
the pet Information page reflects the worn equipment. Only actual blue options
provide damage/defense percentages and cooldown reduction; the old implicit
percentage bonuses and drag instructions are no longer used.

The shared design exporter authors degree-scaled base stats into both server
and client data. The old workbook implicit-percentage columns are legacy and no
longer consumed. Item IDs, ownership, blue caps and stored blue values remain.
