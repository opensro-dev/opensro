# Pet drops: native equipment tables plus DB factor

Port-only, behind `SRO_ATTACK_PET_EXPANSION`. The original shared
level/country equipment tables are generated into
`loot/.generated/equipment.json`; this server does not query an original MSSQL
drop table during kills.

Each actual admitted character-equipment drop independently has a chance to
produce one additional collar, harness or charm of **the same degree**. The
factor is the SQLite authority DB's `meta` row `petGearDropFactor`:

| Value | Meaning |
| --- | --- |
| `0` | No pet equipment drops |
| `0.10` | Default: about 10 pet items per 100 normal equipment drops |
| `0.25` | About 25 per 100 |
| `1` | One pet item per normal equipment drop (live test setting) |

This is a fractional multiplier relative to normal loot, not an independent
per-kill chance. It must be finite and between 0 and 1. Missing row defaults
to 0.10; malformed/out-of-range DB settings refuse shard startup with an
error. The value is read once at startup; restart the shard after changing it.

Normal rank/unique tables, level-gap admission, ordinary loot cap and beta
equipment rates all participate before the factor. Gold, consumables, NPC
stock and unsupported equipment degrees do not produce pet gear. The original
equipment remains on the ground too. Pet blue count, tier eligibility and roll
floors still use the design JSON; NPC shop protection remains active.

## Safe DB edit

Stop the backend first. From the server module:

```powershell
go run ./cmd/tools/sro-pet-loot -authority-dir <actual-shard-authority-directory>
go run ./cmd/tools/sro-pet-loot -authority-dir <actual-shard-authority-directory> -factor 0.10
```

The tool takes the normal store lock and changes only this meta setting. It
never edits character records. No new DB table or schema migration is needed.
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
still apply. Catalogue rows for unimplemented blues remain ineligible. Total
effective cooldown reduction from equipment and passives remains capped at
80%; the pet inventory displays the effective total.

Stop the backend before changing DB configuration, then restart it:

```sql
SELECT * FROM pet_affix_rules WHERE affix_group = 'Cooldown';
UPDATE pet_affix_rules SET maximum = 50, roll_power = 3 WHERE code = 77;
```

Changes affect future monster drops only. Saved item rolls are never rerolled.
Invalid ranges, non-finite values, negative weights and invalid powers refuse
startup.

## Base equipment stats

Pet gear has flat physical/magical attack, physical/magical defense, hit and
parry bonuses. Collars provide attacks/hit, harnesses defenses/parry, and
charms smaller bonuses to all six. They are summed before percentage
modifiers, and the pet Information page reflects the worn equipment. Only
actual blue options provide damage/defense percentages and cooldown reduction;
the old implicit percentage bonuses and drag instructions are no longer used.

The shared design exporter authors degree-scaled base stats into both server
and client data. The old workbook implicit-percentage columns are legacy and
no longer consumed. Item IDs, ownership, blue caps and stored blue values
remain.

## Elemental caps and individual skill blues (2026-10-24)

The highest fire/cold rolls now cap at **1,000 damage**, lightning at **2,000
damage**, and critical chance at **30%**. Lower affix tiers retain smaller
ranges; higher degrees still unlock stronger affix tiers. These are per-blue
caps, not a new total elemental damage cap.

All twenty skill families now have a named **+1–5 skill levels** blue on
**Collar, Harness and Charm**, starting at D2. For example, `+5 to Savage Rend
(Wolf)` boosts learned Savage Rend books I–III, including the next-rank
preview. It does not learn a skill or satisfy its natural-rank prerequisites.
The matching species is required. All individual skills share the `OneSkill`
roll group: at most one such blue per item, stacking across three items (up to
+15 for the same family). Existing whole-tree bonuses still stack. Legacy
nonsequential trees retain their old behavior.

The new stable codes are `100 + first-book ID` (101,104,…158), with `skillId`
in canonical `pet-design.json`. The old planned generic codes72–74 remain
reserved. New code rows are seeded on startup. Existing database policies are
deliberately not overwritten: apply these changes to an existing shard while
it is stopped, after backing up its authority database:

```sql
BEGIN;
UPDATE pet_affix_rules SET maximum = 30 WHERE code = 11;
UPDATE pet_affix_rules SET maximum = 1000 WHERE code IN (15,19);
UPDATE pet_affix_rules SET maximum = 2000 WHERE code = 23;
COMMIT;
```

`minimum`, `selection_weight` and `roll_power` remain configurable in the same
table. New individual-skill rows default to1–5, weight20 and power3 (high
rolls are rarer); keep their maximum at5, which is also enforced when applying
the bonus. Setting weight0 disables future rolls. No old item is rerolled and
no NPC sells these items.

Tests cover all20 families on all3 slots, wrong species, unlearned books,
stacking to+15, named client tooltips and updated default caps. New randomized
drops have not yet been collected live in this pass.

All20 new `pet_affix_rules` rows are present with maximum5.

Attack-speed follow-up: the highest `Speed` tier (code50, D7+) now rolls up
to100% on all3equipment slots. Lower tiers retain5–10% and11–15%. For an
existing shard, back up and stop it, then apply:

```sql
UPDATE pet_affix_rules SET maximum = 100 WHERE code = 50;
```

This is a per-item roll limit, not a combined speed cap: +100% doubles attack
rate before other bonuses. Saved item rolls remain unchanged. Companion/action
tests pass.

Life-steal follow-up: highest `Leech` tier (code30, D8+) now rolls up to50% on
Collar/Harness. Lower tiers retain2–3% and4–6%. It heals the pet from its
damage, as before. Existing DBs: back up, then set `UPDATE pet_affix_rules SET
maximum = 50 WHERE code = 30;` and restart the shard to reload rules. Saved
item rolls are unchanged.

## Per-degree elemental caps (supersedes the earlier four-tier caps)

| Degree | Fire / Cold maximum | Lightning maximum |
|---|---:|---:|
| D1 |100|100|
| D2 |213|338|
| D3 |325|575|
| D4 |438|813|
| D5 |550|1050|
| D6 |663|1288|
| D7 |775|1525|
| D8 |888|1763|
| D9 |1000|2000|

Caps interpolate linearly between D1 and D9, rounded to whole damage. Each new
degree unlocks its affix tier; lower tiers remain in the eligible roll pool.
Existing low-roll bias and slot restrictions (Collar/Harness) remain. Saved
items retain their rolls, including older items above a new degree cap.

Existing codes12–23 remain assigned to D1/D3/D5/D7. Their new maximums must be
updated in existing databases (future rolls only):

```sql
BEGIN;
UPDATE pet_affix_rules SET maximum=100 WHERE code IN (12,16,20);
UPDATE pet_affix_rules SET maximum=325 WHERE code IN (13,17);
UPDATE pet_affix_rules SET maximum=550 WHERE code IN (14,18);
UPDATE pet_affix_rules SET maximum=775 WHERE code IN (15,19);
UPDATE pet_affix_rules SET maximum=575 WHERE code=21;
UPDATE pet_affix_rules SET maximum=1050 WHERE code=22;
UPDATE pet_affix_rules SET maximum=1525 WHERE code=23;
COMMIT;
```

Codes200–214 supply the missing D2/D4/D6/D8/D9 tiers and seed automatically.
Their maximums, weights and roll powers use the same editable
`pet_affix_rules` table.

Verification: all27 degree/element combinations tested through actual gear
rolls on both allowed slots, including lower tiers; server tests and22 focused
client tests pass. No new drops have been collected live in this pass.

## Per-degree poison caps (2026-10-24)

Poison follows the same degree progression, from300 atD1 to9000 atD9, with the
existing four-second poison description and behavior retained.

| Degree | Maximum poison roll |
|---|---:|
|D1|300|
|D2|1388|
|D3|2475|
|D4|3563|
|D5|4650|
|D6|5738|
|D7|6825|
|D8|7913|
|D9|9000|

These are linear, rounded per-blue caps. Lower tiers remain eligible at higher
degrees, and Collar/Harness eligibility is unchanged. Existing items are not
rerolled. For an existing DB, back up before updating its four old poison
rules, then restart the shard:

```sql
BEGIN;
UPDATE pet_affix_rules SET maximum=300 WHERE code=24;
UPDATE pet_affix_rules SET maximum=2475 WHERE code=25;
UPDATE pet_affix_rules SET maximum=4650 WHERE code=26;
UPDATE pet_affix_rules SET maximum=6825 WHERE code=27;
COMMIT;
```

Codes215–219 seed the missing degree tiers. All nine rule caps remain editable
through `pet_affix_rules`; the roll bias and weights are preserved. Tests
cover actual maximum rolls at all nine degrees on both slots, alongside the
other elements. [Gear spell procs](ATTACK-PET-SYSTEM.md#gear-spell-procs) documents the new paired
attack/when-struck spell blues and degree-scaled damage.
