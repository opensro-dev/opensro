# v1.150 monster loot evidence

`generate_loot_catalog.py` compiles the snapshots in the operator's
`<game root>/source-data/loot` into `source-data/server/loot`. Neither is in
the repository; the server loads the result from its derived data (`loot.Load`).
Neither the build nor the running server reads research directories or local
evidence stores. `pnpm task check:loot-catalog` checks byte-for-byte
regeneration.

The client is the content boundary. Ordinary drops merge both supplied shard
snapshots (#459, below); compatible random assignments from the newer snapshot
fill reward gaps. Names, types, magic option IDs, degrees, and required levels
come from the v1.150 client. Numeric item identities from newer snapshots
never become runtime item identities.

## Inputs and reproduction

- `client-source.json` records enabled client items, monsters, option definitions
  and option assignments, with SHA-256 hashes of each source textdata file.
- The two reward snapshots record joined fixed, random, and custom rules,
  exclusions, and source hashes. Random assignment records retain page and
  row offsets. Extraction excludes ghost rows and secondary indexes and joins
  allocation metadata to table names.
- The two drop snapshots record each source's drop selection as raw values:
  every drop-class table with its own width (the newer source's rare equipment
  has 60 classes, its stones 20 and its elixirs 3, against the older source's
  36, 12 and 2), the drop-item-assignment rows resolved to codenames and
  flagged when the v1.150 client lacks the item, and the gold rows. Each
  assignment keeps its source item id and its provenance: page offset, record
  offset and page LSN. A source holds older generations of a row on other
  pages, sometimes enabled together (383 item/class pairs in the older
  snapshot, 56 in the newer, none at an equal LSN), so every generation is
  kept and the generator chooses. The catalog generator owns every merge and
  filter decision (#459). Neither snapshot is proven retail: the newer source
  authors most tables only around levels 71-90, and the older source's levels
  74-89 run about ten times lower than the newer source's.

To refresh the client, reward and drop snapshots from local evidence:

```text
python -B scripts/build/import_loot_evidence.py --media <v1.150-textdata> --vsro <older-snapshot> --isro <newer-snapshot> --output scripts/data/loot
python -B scripts/build/generate_loot_catalog.py
python -B scripts/build/generate_loot_catalog.py --check
```

The importer rejects newly applicable fixed modifiers, rent codes, or random
parameters it cannot represent. The compiler rejects newly applicable custom
rules. This avoids silently dropping a feature when evidence changes. In the
current snapshots all applicable fixed rows have zero authored modifiers and
no rent code, and neither source has a custom rule for a v1.150 monster.

## Merging the two snapshots (#459)

An inference, since neither snapshot is proven retail (`audit.json` `merge`
records each decision):

- Class rows: per table and level, the newer snapshot's row when it rolls any
  class, else the older snapshot's. The newer source authors ordinary equipment
  at 71-90, stones at 1-90, elixirs at 15-90 and 111+, so the older source
  fills the rest and all tablets. Rare equipment prefers the older source
  (owner, #457): the newer source's rare table rolls only d9 A.
- Assignments: an item takes the newer snapshot's enabled rows when it has
  any, else the older snapshot's (rare equipment the other way round). Rows
  the v1.150 client lacks and zero-weight rows are dropped and logged with
  their raw id.
- Of a row's generations (same item and class), the one on the newest-LSN
  page is live, enabled or not. That rule reproduces all 336 choices the
  old single-source projection made among its duplicated stones.
- A class without a v1.150 item stays when a lower class holds items:
  the weighted selector walks down to it, so d10 equipment rolls at 91-101
  fall to d9 and the newer source's level elixirs fall to the B elixir.
  A class with nothing below it is zeroed, the same outcome.

## Recovered rules and deliberate reconstruction

Native server behaviour establishes these rules (the instruction-level
evidence is kept outside the repository):

- Weighted assignment, same-type lower-class fallback, inclusive absolute chance.
- Ordinary equipment plus distribution, capped at +7.
- Unique/custom prepass, random assignments, fixed assignments, then ordinary
  categories.
- Fixed per-monster rewards.
- Random group candidate/admission draws, distinct duplicate rejection.
- Special equipment; plus is the minimum of two 1–5 draws.
- Default unique counts, nonrepair option, B elixirs and special return scrolls.
- Gold and ordinary category production.
- Equipment variance and magic option initialization.
- Each variance field uses the minimum of three float32 draws.

Weapons have seven variance fields; armor and shields have six; accessories have
two. The unique prepass can exceed the later ordinary capacity. An ordinary
equipment item survives a failed magic admission gate; the special constructor
rejects it. Nonrepairable normal equipment receives the client degree-1
`MATTR_NOT_REPARABLE` option and its +400% durability adjustment.

Client instructions confirm ground-item parsing, model/label rendering and
registration. Existing spawn/pickup contracts carry these item types; no new
wire format is required.

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
without the ordinary random-blue roll. Current applicable authored
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
