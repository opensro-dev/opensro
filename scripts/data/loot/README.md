# v1.150 monster loot evidence

`generate_loot_catalog.py` compiles these committed snapshots into
`apps/server/internal/game/item/loot/.generated/`. The server embeds the result;
neither the build nor the running server reads research directories or SQL
backups. `pnpm task check:loot-catalog` checks byte-for-byte regeneration.

The client is the content boundary. vSRO supplies the baseline assignments and
mechanics; compatible ISRO random assignments fill gaps. Names, types, magic
option IDs, degrees, and required levels come from the v1.150 client. Numeric
item identities from newer backups never become runtime item identities.

## Inputs and reproduction

- `equipment-source.json` and `consumables-source.json` preserve the existing
  vSRO projection before reconstruction. The equipment snapshot contains normal
  and rare class rows; the consumable snapshot contains separate family rows,
  weights, absolute chances, quantities, and the old assigned rewards.
- `client-source.json` records enabled client items, monsters, option definitions
  and option assignments, with SHA-256 hashes of each source textdata file.
- `vsro-rewards-source.json` and `isro-rewards-source.json` record joined fixed,
  random, and custom rules, exclusions, and backup hashes. Random assignment
  records retain page and row offsets. SQL extraction excludes ghost rows and
  secondary indexes and joins allocation metadata to table names.

To refresh the client and reward snapshots from local evidence:

```text
python -B scripts/build/import_loot_evidence.py --media <v1.150-textdata> --vsro <vSRO-backup> --isro <ISRO-backup> --output scripts/data/loot
python -B scripts/build/generate_loot_catalog.py
python -B scripts/build/generate_loot_catalog.py --check
```

The importer rejects newly applicable fixed modifiers, rent codes, or random
parameters it cannot represent. The compiler rejects newly applicable custom
rules. This avoids silently dropping a feature when evidence changes. In the
current snapshots all applicable fixed rows have zero authored modifiers and
no rent code, and neither source has a custom rule for a v1.150 monster.

## Recovered rules and deliberate reconstruction

Native server instructions establish these owners:

| Address | Rule |
| --- | --- |
| `724120` | Weighted assignment, same-type lower-class fallback, inclusive absolute chance |
| `724400` | Ordinary equipment plus distribution, capped at +7 |
| `7245C0` | Unique/custom prepass, random assignments, fixed assignments, then ordinary categories |
| `724A00` | Fixed per-monster rewards |
| `724E30` | Random group candidate/admission draws, distinct duplicate rejection |
| `725600` / `725720` | Special equipment; plus is the minimum of two 1–5 draws |
| `726020` | Default unique counts, nonrepair option, B elixirs and special return scrolls |
| `726900` / `726A70` | Gold and ordinary category production |
| `727590` / `7276A0` | Equipment variance and magic option initialization |
| `729280` | Each variance field uses the minimum of three float32 draws |

Weapons have seven variance fields; armor and shields have six; accessories have
two. The unique prepass can exceed the later ordinary capacity. An ordinary
equipment item survives a failed magic admission gate; the special constructor
rejects it. Nonrepairable normal equipment receives the client degree-1
`MATTR_NOT_REPARABLE` option and its +400% durability adjustment.

Client instructions at `86E4E0`, `86DDA0`, and `77D7D0` confirm ground-item
parsing, model/label rendering and registration. Existing spawn/pickup contracts
carry these item types; no new wire format is required.

The following are reconstruction choices, not claimed recovered v1.150 rates:

- Recovery class chance 10%, cure 2%, return scroll 1%, and the selected arrow
  or bolt category 5%, per ordinary category opportunity. Item weights and
  absolute admission remain separate, so these are not final per-item chances.
- Bands start at levels 1, 20, 40, 60, 80 and 90. Recovery uses tiers 1–5 then
  special potions. Cure caps at tier 4. Ammunition stacks are 20/50/70/100/150/250.
- The authored family-7 speed-tablet opportunity follows the native ordinary
  categories. Its probabilities remain those of the supplied class table.
- The special equipment level/class map uses the compatible ordinary class
  row without its ordinary chance gate, retaining lower-class fallback.
- Ninety-six rare torso assignments have client required levels after their
  donor A-rare class window closes. Those assignments join the next enabled
  class within the same degree. Original item weights and class probabilities
  remain unchanged; the audit records each old/new class. Actual production
  selector witnesses cover every equipment assignment, including fallback.
- Client named magic assignments join to the equipment's client degree, using
  that option row's probability, allowed item categories, and value set.

Assigned random-group equipment initializes variance and authored options,
without the ordinary random-blue roll (`724E30`). Current applicable authored
option sets are empty, so these items retain +0 and no additional magic options.

The audit classifies all 8,439 enabled client item identities. A database
assignment is not enough to enable ordinary loot: the four A-elixir assignments
have disabled source classes. Degree 10–12 materials without compatible loot
equipment are unavailable in this version, including two newer fixed rewards.
The generated catalog has 21 usable fixed assignments and 42 random assignments
across 21 monsters; random groups 107 and 111 contain 83 and 15 eligible members.
Quest/event and other acquisition entries stay outside ordinary monster pools.

## Random-group probability contract

For an original pool of size `N`, candidate index `i` has integer mass
`floor(32768/N) + (i < 32768%N)`. For member threshold
`t = trunc(float32(probability) * 1000000)`, admission counts inputs from the
`2^30` combined-draw sample space whose remainder is at most `t`. The product
is the member's accepted mass. Distinct draws remove selected masses while
retaining original indices. Sampling these masses preserves accepted outcomes,
including modulo bias and inclusive zero, without millions of rejected draws.
It deliberately does not preserve random-stream consumption. There is no
attempt cap that discards an admitted reward.

Tests exhaust reduced integer sample spaces, witness each published group and
ordinary consumable assignment through its selector, check inferred band
boundaries, and exercise unique/recovery kills, persistence, pickup, equipment,
alchemy, ammunition, cures, and timed special return scrolls.
