# Attack Pet System — website and operator manual

Reviewed **16 October 2026** against `feat/Attack-pet-expansion`. This is an
optional expansion, not a claim that the public server has enabled it or that
every advanced effect has passed live acceptance.

## Website integration

The ready-to-use player page is [site/attack-pet-system.html](site/attack-pet-system.html),
with [site/attack-pet-system.css](site/attack-pet-system.css). Local preview:
`http://127.0.0.1:5182/attack-pet-system.html` while the preview server runs.

The visual reference is the live [OpenSRO beta guide](https://opensro.online/guide):
palace masthead, dark background, left navigation, gold fittings and parchment
panels. The current live frontend uses Next.js and is not fully present in this
checkout. `apps/server/ops/site/index.html` is an older minimal landing page;
do not replace the live homepage with it.

To add the requested tab:

1. Copy both guide files beside each other into the website's static/public
   directory. Add **Attack Pet System** to the existing navigation, pointing
   to `/attack-pet-system.html`.
2. Optionally rewrite `/attack-pet-system` to that static page. A normal link
   needs no JavaScript; if a separate browser tab is desired, use
   `target="_blank" rel="noopener"`.
3. The masthead references the existing public
   `https://opensro.online/art/jangan-banner.webp`. On the same site, change
   this to `/art/jangan-banner.webp`. A gradient remains if art is unavailable.
   No retail assets or game binaries are bundled.
4. For native Next.js integration, keep the site's header/sidebar and port
   only `<main id="guide-content">` into its page component. Place the guide
   inside a `.pet-guide` wrapper and import its CSS, or map the sections to
   existing panel components. All stylesheet selectors are scoped to that
   class. Do not render a second copy of the site shell inside the first.
5. Reuse the real navigation links and selected-tab state. Verify desktop,
   narrow screens, keyboard focus, section anchors and FAQ expanders.

The public page needs no login, game API, framework dependency or JavaScript.
It documents roles, twenty skill families, progression, manual commands,
cooldowns, drops, gear, degrees and care. It is a reviewed snapshot, not a live
DB dashboard. Administrator SQL and machine paths stay in this manual.

## System overview

Server flag: `SRO_ATTACK_PET_EXPANSION=on`. Client setting:
**Experimental → Game → Attack-pet training**. Both must be enabled.
Open pet information with **Insert**.

### Bear: Iron Hide shields

Iron Hide now grants absorption separately from HP for **5 seconds**. Tier I
protects the Bear and its owner; Tier II also protects living party members
within **20 m of the owner**. Each recipient's shield scales from their own
maximum HP and the effective skill rank (including gear bonuses), capped at
100% of maximum HP. Shields do not stack: an existing stronger shield keeps
its amount and original expiry; an equal or stronger new cast replaces it.

Damage consumes the shield before HP, including damage over time. Healing
restores HP, never absorption. The pale segment beside HP shows the remaining
shield, with a cyan barrier around protected characters. At full HP the health
and shield portions scale together to fit the existing bar; actual HP and max HP
stay unchanged.

Tier III retains the party protection. On natural expiry, each protected
**player and the Bear** with shield remaining releases an **8 m** burst, dealing **50% of
that remaining shield** to monsters in range. Each explosion is centred on that recipient. Depleting a shield, death, unsummoning, logout, town or instance entry
cancel it without a burst. No player-versus-player splash damage is added.
Iron Hide cannot be cast in town and retains the second active row's shared
cooldown. Current balance uses the existing Iron Hide rank values; tuning is
still needed.

| Pet | Role | Active families | Passive families |
| --- | --- | --- | --- |
| Bear | High HP/defense; lower attack | Challenging Roar; Iron Hide | Unbroken Spirit; Guardian’s Bond; Crushing Paw |
| Wolf | Physical offense | Hunting Howl; Savage Rend | Execute; Bloodscent; Blood Hunt |
| Penguin | Ranged magic/control; lower HP | Ice Lance; Blizzard | Shatter; Arcane Resonance; Frozen Hourglass |
| Three Footed Crow | Owner support | Mend; Mark Prey | Carrion Feast; Swift Wings; Murder of Crows |

Bear **Guardian’s Bond** replaces Last Stand; learned book IDs and ranks are
retained. A living summoned bear must be within 12 m. This expansion applies to
ordinary monster combat, outside towns, jobs, PvP and instances.

| Book | Ranks 1 → 10 |
| --- | --- |
| I | Automatically revives the owner with 10% → 100% maximum HP and 10% → 100% of the latest death’s lost EXP. Shared revival cooldown falls linearly from 600 → 300 seconds. No damage transfer. |
| II | Adds owner damage transfer of 10% → 20%. Inherits mastered book I’s full HP/EXP recovery and 300-second revival cooldown. |
| III | Transfer rises from 20% → 30%; also protects and revives online party members in range. Inherits book I’s revival. |

Revival admits monster deaths only and targets at most ten levels above the
bear. The owner has priority, followed by party members in stable GID order.
One revival consumes the bear’s shared saved cooldown; resummoning or relogging
does not reset it. EXP recovery uses the existing resurrection/refund authority
and cannot exceed the latest recorded loss. MP and lost SP are not refunded.

Transfer applies after native damage links, shields and mana redirection. One
strongest eligible bear applies; bonds do not stack. Allies share a transfer
limit of 20% of bear maximum HP each second, and transfer leaves at least one HP.
Transferred costs do not charge hit counters, damage links or retribution.
Gold recovery pulses and native cleric healing/rebirth sounds mark confirmed
events without changing the bear’s pose. Balance remains subject to testing.

Bear **Crushing Paw** now retaliates after every ten landed physical or spell
impacts taken. Tier I counts hits on the bear; tier II adds a separate owner
counter. Tier III also rolls a 50% stun chance per other monster within 8 m of
the hit recipient. The tenth hit's attacker always receives the stun attempt;
native status resistance/immunity still applies. Duration scales with learned
rank as before. Shielded hits count; blocked hits and periodic damage do not.
This deals no damage, affects no players or unique monsters, and is disabled
in town. Counters are transient and reset on resummon/relog.

Each row has three successive books: I starts at pet level 10, II at 40, III at
90. Each has ten ranks; max the previous book before learning the next.
Rank requirements progress to levels 40/90/120 respectively. Free points are
`max(0, 1 + floor(petLevel / 2) + operatorBonusPoints - spentRanks)`.
Extra points on lab fixtures are not normal progression.

Drag learned active skills to normal numbered quickslots. The currently
summoned pet must know the skill; offensive commands need a valid target.
All books in a row share recharge, while the two active rows are independent.
Timed recharge uses countdowns/sweeps. Event recharge needs attacks, hits,
kills or spells rather than waiting. The highest learned passive book in each
row is always active automatically; older books are replaced and cannot be
manually selected. Their effects do not stack.

Skills, gear and recharge persist with the pet's summon item.
Recall the pet and leave combat before changing gear/training if refused.
Low HGP affects performance; a pet above its owner's level cannot normally
be summoned. Preserve those rules when testing.

### Equipment and cooldowns

All four species share the same slots and pet-level requirements:

| Slot | White base stats |
| --- | --- |
| Collar | Physical/magical attack, hit ratio |
| Harness | Physical/magical defense, parry ratio |
| Charm | Smaller bonuses to all six |

Percentage damage/defense, cooldown reduction, elemental additions and +skills
belong to actual blue options, not implicit base bonuses. Basic Info reflects
applicable equipment/passives alongside species scaling and HGP.

Effective cooldown reduction is **min(sum of equipment and passives, 80%)**.
A 30-second skill takes 15 seconds at 50%, 6 seconds at 80%, and still 6 seconds
if the uncapped sum is 100% or 150%. Counters round to integer events with a
minimum of one. Cooldown blues currently roll on harnesses/charms, not collars.
The total 80% ceiling is source policy, **not a DB setting**.

## Configuration map

| Control | Where | Apply with |
| --- | --- | --- |
| Pet drop chance relative to normal equipment | SQLite `meta.petGearDropFactor` | Stop shard, edit, restart |
| Blue min/max, relative weight, value bias | SQLite `pet_affix_rules` | Stop shard, edit, restart |
| General drop/EXP/gold/SoX rates and category mix | Deployment environment | Persist config, restart/redeploy |
| Degree magic/rare odds, blue caps/weights/floors | Canonical JSON `gear` | Export and rebuild matched data |
| Skill values, family progression, species profiles | Canonical JSON `treeSkills`, `pets` | Export and rebuild matched client/server |
| Flat item stats | Canonical JSON `gear[].items[].baseStats` | Export, rebuild matched data |
| Total 80% cooldown cap | Server `companion/gear.go` and client state validation | Source change, tests, matched deployment |

The authority directory comes from `SRO_AUTHORITY_STATE_DIR`, not the cluster
wrapper's `--state-dir`. Open `<authority-dir>/state.db` in your SQLite client.
Stop the shard and retain a backup before changing rates or upgrading schema.

## Pet drop factor

The server first resolves normal character-equipment drops using the existing
level/country/rank tables, level-gap admission, general rate and ordinary item
cap. Each eligible admitted piece independently gives a chance for one extra
pet piece **of the same degree**. The original item remains. Pet slot selection
is random among the three slots. Gold, consumables, NPC stock and unsupported
degrees do not create pet gear. NPCs must not sell these items.

`petGearDropFactor` is a fraction, not a percentage from 0 to 100:

| Value | Expected pet pieces per 100 eligible equipment drops |
| --- | --- |
| `0` | 0 |
| `0.05` | About 5 |
| `0.10` | About 10; default |
| `0.25` | About 25 |
| `0.50` | About 50 |
| `1` | 100; useful for temporary testing |

Example: 100 kills yielding 20 eligible equipment pieces at factor 0.10 give
about **2 pet pieces**, not 10. General gear rates affect the number of
opportunities too, but caps/admission prevent unlimited proportional scaling.

### Safe DB change

1. Stop the affected GameWorld gracefully and wait for it to exit. Make a
   consistent SQLite backup, and record the old setting.
2. In the correct stopped authority DB, run:

```sql
SELECT key, value FROM meta WHERE key = 'petGearDropFactor';
BEGIN;
INSERT INTO meta(key, value) VALUES('petGearDropFactor', '0.25')
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
COMMIT;
```

3. Restart and confirm the log reports
   `pet loot: DB factor 0.25 per normal equipment drop` and the transport is
   listening. Missing row defaults to 0.10; invalid/nonfinite/out-of-range
   factors refuse startup. Allowed range is 0..1.
4. Test real monster kills. Revert by restoring the old setting and restarting,
   **not by restoring the whole backup over new pickups/progression**.

Guarded CLI equivalent, from `apps/server`, while stopped:

```powershell
go run ./cmd/tools/sro-pet-loot -authority-dir '<actual-authority-directory>'
go run ./cmd/tools/sro-pet-loot -authority-dir '<actual-authority-directory>' -factor 0.25
```

The CLI uses the normal authority lock and changes only this config value.
The old workbook “Drop % per kill (General)” column is no longer the runtime
rate. Normal equipment tables are generated in
`internal/game/item/loot/.generated/equipment.json`; kills do not query an original
MSSQL drop table. See [PET-LOOT-DB-CONFIG.md](PET-LOOT-DB-CONFIG.md).

## Blue limits, selection weight and high-roll rarity

`pet_affix_rules` is seeded once; existing DB edits survive restarts. Keep
identity columns stable. Changes affect future drops, never reroll saved items.

| Column | Meaning |
| --- | --- |
| `minimum`, `maximum` | Inclusive integer limits for this tier |
| `selection_weight` | Relative chance among eligible tiers/groups; 0 disables the tier |
| `roll_power` | 1 = uniform values, 3 = low-biased; higher makes top values rarer; range 1..20 |

Weight controls **which blue** rolls. Power controls **how high its value** is.
Neither controls pet-drop frequency or the number of blues. Slot/degree,
implemented-status and one-option-per-group restrictions still apply.

```sql
SELECT code, name, affix_group, minimum, maximum, selection_weight, roll_power
FROM pet_affix_rules ORDER BY affix_group, code;

SELECT * FROM pet_affix_rules WHERE affix_group = 'Cooldown';

-- Stopped shard only; first record the old values.
BEGIN;
UPDATE pet_affix_rules
SET minimum = 13, maximum = 50, selection_weight = 40, roll_power = 3
WHERE code = 77;
COMMIT;

-- To disable just this tier instead:
-- UPDATE pet_affix_rules SET selection_weight = 0 WHERE code = 77;
```

Do not delete rows to disable them: missing known rows can be seeded again.
Do not rename `affix_group` expecting different runtime eligibility; the
compiled catalogue owns that behavior. Restart after editing.

The current value calculation is:

```text
low = minimum + floor((maximum - minimum) * degreeRollFloor)
value = min(maximum, low + floor(random^max(1, roll_power) * (maximum - low + 1)))
```

For D9's 50% floor and cooldown tier 13..50, the actual range is **31..50**.
Power 3 makes the top of that range rarer than a uniform roll. Increasing the
maximum can increase the floor too; max 50 does not mean equal chances of 1..50.

Validation rejects nonfinite values; min/max must be integers with
0 ≤ min ≤ max ≤ 1,000,000, weight within 0..1,000,000, power within 1..20.
Invalid DB policy refuses startup. Storage limits are not recommended balance
targets. The top cooldown tier is seeded with max 50 by
`cmd/services/sro-gameworld/petaffixes.go`, even though the design's legacy
catalogue value may differ. Existing DB rows take precedence over new defaults.

## Degree quality and blue counts

Baseline design values before item-find modifiers:

| Degree | Pet level | Max blues | Magic chance | Rare chance | Roll floor |
| --- | --- | --- | --- | --- | --- |
| D1 | 1 | 1 | 30% | 3% | 0% |
| D2 | 8 | 2 | 32% | 4% | 6.25% |
| D3 | 16 | 2 | 34% | 5% | 12.5% |
| D4 | 24 | 3 | 36% | 6% | 18.75% |
| D5 | 32 | 3 | 38% | 7% | 25% |
| D6 | 42 | 4 | 40% | 8% | 31.25% |
| D7 | 52 | 4 | 42% | 9% | 37.5% |
| D8 | 64 | 5 | 44% | 10% | 43.75% |
| D9 | 76 | 6 | 46% | 12% | 50% |

One draw selects rare first, then the additional magic band; the remainder is
plain. Magic gets one blue with a 40% chance of two, then the degree cap applies.
Rare chooses 3/4/5/6 using the degree's weights and is also capped. D1 rare
therefore still has at most one blue. Fewer eligible groups can reduce the count.

D9 weights for 3/4/5/6 are **40/38/22/15**, totaling 115; these are relative
weights, not percentages. D9 does not guarantee six blues or a perfect roll.

Edit the canonical JSON `gear` section for odds, weights, caps and floors. `Rules`
owns global max six and the magic-two-blue chance. `Gear` owns names, IDs and
requirements. Do not change saved identities or raise the six-option wire/save
ceiling casually. Never roll back to a five-option server after issuing six-blue
items: it can discard the sixth option.

After editing `apps/server/internal/game/companion/petdesign.json`, from the repo root:

```powershell
python scripts/tools/pet_design_export.py
```

This rewrites the design in its canonical layout and regenerates
`apps/client-next/src/engine/foundation/ui/pet-design-data.ts`. The server embeds
the design and the asset build reads it in place; there is no other copy.

Never hand-edit generated data. Rebuild/deploy matched client and server.
For skill balancing, `Skills` holds effect templates and `Skill families`
chooses the current sequential families and book levels. `Combat roles` holds
species HP/attack/defense profiles; `Pets` retains the companion catalogue.
Item definition/icon changes may also need the existing asset pipeline; DB-only
roll policy changes need only a shard restart. Flat stat formulas live in the
exporter, not the old implicit-percentage workbook columns. Legacy drop-per-kill
and pet rank/drop multipliers are not current runtime rate controls.

## General server rates: separate from the pet factor

Beta rate settings apply with `SRO_BETA_GROWTH=on`:

| Variable | Meaning / allowed range | Current lab |
| --- | --- | --- |
| `SRO_ATTACK_PET_EXPANSION` | `on` enables pet expansion | `on` |
| `SRO_BETA_GROWTH` | Enables beta progression/rates | `on` |
| `SRO_BETA_DROP_RATE` | General drop passes, integer 1..100 | `20` |
| `SRO_BETA_DROP_MIX` | `on`, `off`, or `family=rate,...` | `on` |
| `SRO_BETA_DROP_CAP` | Ordinary per-kill item cap, 0..1000; 0 = uncapped | `16` |
| `SRO_BETA_SKILL_EXP_RATE` | Skill EXP multiplier, integer ≥ 1 | `100` |
| `SRO_BETA_GOLD_RATE` | Gold multiplier, integer 1..10000 | `250` |
| `SRO_BETA_RARE_RATE` | Normal SoX chance multiplier, integer 1..1000 | `5` |

These are deployment values, not universal code defaults. For example, the
low-level growth function defaults gold to 50 while shard deployment settings
set 250. Invalid general overrides may fall back silently; verify effective
startup logs. A requested drop rate of 1000 is outside the current 1..100 range.

`DROP_MIX=on` keeps potion/cure/scroll/elixir/ammo/stone/tablet at 1× while
equipment and assigned rewards use the general beta rate. `off` uses the
uniform beta rate. A custom deployment example:

```powershell
$env:SRO_BETA_GROWTH = 'on'
$env:SRO_BETA_DROP_RATE = '30'
$env:SRO_BETA_DROP_MIX = 'potion=1,cure=1,scroll=1,elixir=2,ammo=1,stone=1,tablet=1'
$env:SRO_BETA_DROP_CAP = '16'
```

Shell assignments alone do not update a running Nomad allocation. Persist
values using the existing deployment workflow and reconcile/restart the job.
Nomad catalogue names are `beta_growth`, `beta_drop_rate`, `beta_drop_mix`,
`beta_drop_cap`, `beta_skill_exp_rate`, `beta_gold_rate`, `beta_rare_rate`.
Preserve the rest of the environment; do not replace it with this small example.

SoX rate applies to normal character equipment, **not pet magic/rare odds**.
Gold/EXP do not directly multiply the pet factor. Overall drops and the ordinary
cap affect pet opportunities; changing only the pet factor avoids flooding
normal equipment.

## Verification and maintenance

1. Save old settings and a consistent backup; change the smallest relevant row.
2. Restart and check effective configuration and transport readiness.
3. Test actual eligible kills, pickup, degree/blues, equip/remove and relog.
4. Use enough drops to assess probability; a single lucky kill proves little.
5. Use weak enemies and a level-eligible owner. For a 30s skill, check 50% → 15s,
   80% → 6s, and refusal of an early recast. HGP must not confound stat tests.
6. Restore test rates/fixtures without overwriting new character progress.
   Confirm original options and return the owner to town.
7. Update the static player page when defaults, unlocks or effects change.

Targeted regression commands, from `apps/server`:

```powershell
go test ./internal/game/companion ./internal/game/action ./internal/data/store ./internal/game/item/wire ./cmd/services/sro-gameworld
```

From `apps/client-next`:

```powershell
node --test tests/runtime/pet-training.test.mjs tests/runtime/pet-gear-tooltip.test.mjs
```

Live acceptance on 16 October confirmed legitimate D9 harness/charm 50% blues
display 80%, Iron Hide III produced exactly 6000ms and early recast was refused.
One 50% piece produced 15000ms; Roar's 18s became about 3600ms against weak
Small-eyed Ghosts. Original gear restored; owner full HP in Hotan. This is not
blanket acceptance of every passive or affix.

### Source map

Server paths relative to `apps/server`:

- `internal/game/companion/{training,tiers,gear,design}.go`: skills and modifiers.
- `internal/game/action/{pettraining,pettrained,petgeardrop,monsterloot}.go`:
  live commands, effects, combat stats and loot admission.
- `internal/data/store/{petlootrules,petaffixes}.go`: DB config and validation.
- `cmd/services/sro-gameworld/petaffixes.go`: seeds and loads blue policy.
- `internal/game/progression/{growth,dropmix}.go`: general rates.
- `internal/config/shardsettings/shardsettings.go`: deployment catalogue.

Client: `src/engine/foundation/ui/{item-tooltip,pet-training}.ts`, runtime
`ui/ui.ts` and simulation `world/gameplay/pet-training/pet-training.ts`.
The workbook may retain planned/legacy templates; they are not automatically
implemented merely because a row exists.

### Unbroken Spirit (replaces Thick Hide, same saved skill IDs22–24)

Bear-only passive: each landed monster impact grants one Resolve stack, maximum10,
and refreshes15s expiry. Blocked hits and periodic damage grant none; shielded hits
count. Each stack raises maximumHP and fills only the new capacity. At skill ranks
1–10, HP per stack is I:1–2%, II:2–3%, III:3–4%. No static defense bonus remains.
II/III at full stacks resist new Slow/Frostbite and knockback (not stun, freeze or
knockdown). III spends full stacks only on a successfully admitted Roar or Iron
Hide: +50% radius/target capacity (rounded down), or +50% shield capacity. Normal
cooldown and one-second Roar windup remain. Consumption/expiry clamps currentHP;
it never deals damage or kills. Unsummon/relog/town/invalid passive clears stacks.
Resolve is transient and never changes saved ranks, equipment or database schema.
Subtle model growth (+12% at full), amber ground ring and brief full-stack pulse.

## The design lives in one JSON source

[`apps/server/internal/game/companion/petdesign.json`](../../apps/server/internal/game/companion/petdesign.json)
is the one source of every number: pets, skills, rows, cooldowns, gear degrees, affixes (blues),
procs. Edit it, then run

```
py scripts/tools/pet_design_export.py
```

which validates it, rewrites it in its canonical layout and generates the client
module (`apps/client-next/src/engine/foundation/ui/pet-design-data.ts`). The server
embeds the file and the asset build reads it in place; `pnpm task check:pet-design`
fails when the client module no longer matches. Only rows whose
**Status** is `Implemented` can be learned or rolled; the engine notes column says
what each rank does in code. Rebuild game data after changing gear rows.

## Wire

Wire: state push 0x3FB1; command = normal COS request 0x769E, tag F0,
`[petGID:u32le][F0][op][arg]` with ops 0 state, 1 learn, 2 use (queue an action), 3 wear bag
slot, 4 remove gear slot, 5 reset, 6 slot passive. Retail decoder unchanged.

## Gear degrees

Port-only; the existing
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
See [PET-LOOT-DB-CONFIG.md](PET-LOOT-DB-CONFIG.md) for the DB chance multiplier. White pet items may have zero
blues. Magic drops roll one or two, subject to the degree cap. Rare drops roll
three through six, also subject to that cap. Six-blue weight is available only
on D9. A group may appear only once on an item, and only implemented affixes roll.

Higher degrees unlock the existing stronger affix tiers and improve rolls
within each eligible tier. The integer roll floor is
`affixMin + floor((affixMax - affixMin) * percentile)`; the maximum remains the
affix tier's authored maximum. For example, a 10–30% affix rolls 10–30% at
D1 if eligible, and 20–30% at D9. A D9 item can still select a lower affix
tier; D9 does not guarantee six blues or perfect rolls.

### Source and compatibility

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

All27 slot/degree icons from `assets/README.md` are included. No extra icon
per blue count is needed: the degree selects the art, while the tooltip
shows actual options and the new per-degree cap.

## Gear spell procs

These blues have separate **pet dealing damage** and
**pet taking damage** versions:

|Spell|Effect|
|---|---|
|Chain Lightning|Up to 5 distinct monsters;8 m hops,20% less damage per hop.|
|Blizzard|5 m field,4 damage pulses across 4 seconds. Uses the penguin storm visual.|
|Fire Burst|3 m burst, up to 8 monsters.|
|Poison Cloud|5 m field,4 damage pulses across 4 seconds.|
|Mend|Heals the owner for 5% maximum HP.|

Attack versions generally roll on Collar/Charm; retaliation versions on
Harness/Charm. Existing attack-Mend stays Charm-only. Proc families start
atD3, with stronger chance tiers atD6/D7/D9, matching their existing chance
progressions. Each direction is a separate blue group and may coexist.

### Degree power

|Degree|Base proc damage|
|---|---:|
|D1|100|
|D2|213|
|D3|325|
|D4|438|
|D5|550|
|D6|663|
|D7|775|
|D8|888|
|D9|1000|

This is damage per target for Fire Burst and the first Chain Lightning hit.
Blizzard/Poison Cloud split that total across their four pulses, preserving
the remainder on the last pulse. Mend retains its existing 5% heal. This
proc damage is separate from flat fire/cold/lightning/poison damage blues.
Higher roll chance and degree power are independent. Multiple items add the
same proc's chance (capped 100%) and use its strongest contributing degree.

### Limits

- Only committed damaging pet actions or nonfatal hits taken from ordinary
  population monsters qualify. Physical and magic hits both qualify.
- Full blocks, fully shielded hits, player/PvP hits and brood clones do not.
- At most one proc fires per action, sharing the existing 2 second internal
  cooldown across both directions. Selection order: Chain, Blizzard, Fire,
  Poison, Mend. Chances still roll independently in that order.
- Fixed proc hits use normal credited settlement for loot/EXP. They do not
  generate more attacks, proc rolls, leech or learned-skill stacks.
- Only the correct population, range and line-of-sight are admitted. Area
  pulses hit at most 8 monsters. No player targets.
- At most 4 fields per pet; missed ticks do not replay a damage backlog.
  Death, owner death, town entry, separation or unsummon discard fields.
- Proc state is transient; logout/restart clears cooldowns/fields.
- Dedicated new visuals were not authored for the other gear spells.

### Configuration and stable IDs

`pet_affix_rules` controls min/max **proc chance**, selection weight and
roll power, loaded at shard startup. Setting weight 0 disables future rolls.
Already saved blue rolls retain their values.

Existing attack codes 84–86(Chain),90–91(Fire),96–97(Mend) remain stable.
Previously planned codes 87–89 now implement Blizzard when struck;
92–93 implement Poison Cloud when struck. New paired blues use 220–231
and seed automatically. The local DB metadata for 87–89 and 92–93 was updated
to match their new groups; configured chance ranges and weights were kept.
No schema migration is needed.

Power is authored perdegree in `gear[].procDamage` in `petdesign.json`;
regenerate with `scripts/tools/pet_design_export.py` and rebuild the server
and client after editing. Recovery uses `rules.procInternalCooldownMs`.

## Sealed gear and elemental blues

Collars, harnesses and charms have ordinary, Seal of Star, Seal of Moon and
Seal of Sun versions at every degree. They share the degree's icon and the
native sealed-item animated slot shine and yellow tooltip heading.

| Quality | White stats and rolled blue values |
| --- | --- |
| Ordinary | 100% |
| Seal of Star | 150% |
| Seal of Moon | 200% |
| Seal of Sun | 250% |

Blue values are multiplied once when dropped and rounded down to integers.
The stored values survive pickup, equip, trade, storage and relog without
being multiplied again. Per-item blue count limits remain unchanged. Global
cooldown reduction remains capped at 80%; proc chances remain capped at 100%.
Seals also increase the degree's proc damage, but do not change proc recovery.

The three slots reserve IDs `301xx`, `302xx` and `303xx`. The units digit is
degree 1–9; tens digit 0/1/2/3 is ordinary/Star/Moon/Sun. Existing ordinary
references and saved rolls are unchanged. Sealed codenames append `_STAR`,
`_MOON` or `_SUN` to the ordinary codename. No database schema change is needed.

### Drop configuration

Pet equipment still drops only from monsters, through the existing
`pet_gear_drop_factor` policy described in [PET-LOOT-DB-CONFIG.md](PET-LOOT-DB-CONFIG.md).
It inherits the triggering normal-equipment drop's degree and seal tier.
Thus native monster loot tables control seal rarity; there is no second,
independent seal roll and no NPC source. Changing the pet factor changes the
number of pet drops, not their relative seal odds.

### Elemental blues

Fire, cold and lightning add separate fixed-damage hits to each successfully
damaged monster, displayed through the native yellow damage-number packet.
They are no longer hidden inside the ordinary physical/magic damage formula.
They do not recursively trigger counters, leech or chance-to-cast blues.
Their tooltip lines describe these fixed hits, without obsolete 5% status
chances or a random lightning range. Chance-to-cast proc blues remain separate.

Poison's displayed value is its **total damage over four seconds**. It ticks
once per second: 12 means four ticks of 3. Remainders are distributed over the
four ticks without losing damage. Another hit refreshes this pet's poison on
that monster without postponing the next tick or stacking another copy.
Different pets retain independent poison applications. Death, recall, town
entry, logout or an invalid target discards pending ticks. Late simulation
ticks never replay a backlog.

### Building

`scripts/build/shared/portItemRows.mjs` creates all 108 reference rows. Rebuild
the server game-data bundle and refresh the client `pet-loot` asset family
together, then deploy the server and reload the client. Sealed items use the
same owned icons and models; original PK2 archives are unchanged.

## Source owners

- `apps/server/internal/game/companion/petdesign.json` + `scripts/tools/pet_design_export.py`: design and its client export.
- `apps/server/internal/game/companion/`: `design.go` (embedded design),
  `training.go` (points, rows, slots), `gear.go` (rolls, affix encoding, modifiers).
- `apps/server/internal/domain/character.go`: persisted `PetTraining`.
- `apps/server/internal/game/action/`: `pettraining.go` (commands, gear moves),
  `pettrained.go`, `petmanual.go`, `petshield.go`, `petspirit.go`, `petbond.go` (combat effects),
  `petgeardrop.go` (drops), `petcombat.go`, `monsterstrike.go`, `monsterloot.go` (existing owners hooked).
- `item/wire/pettraining.go`, `itemmove.go`: state frame and gear options on items.
- `scripts/build/shared/portItemRows.mjs`: gear item rows and text.
- Client: worker `pet-training.ts`, `foundation/ui/pet-training.ts`, `ui.ts`
  pet window, tooltips.
