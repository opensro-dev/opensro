# Optional 110-cap expansion: status and behaviour

The native default remains v1.150 with cap 90. The expansion is optional,
flag-gated, and not ready to release. This document records the behaviour
contract and the known limits; the dated checkpoint history and the source
provenance are kept outside the repository.

## Flags

- `SRO_LEVEL_CAP=110` raises the stopping level. Unset, `off`, `false`, `0`
  or `90` retains cap 90. Other values fail startup. The flag is port-only,
  not native; the Nomad job and the deployment registry default it off.
- `SRO_CAP110_ITEMS=1` enables the equipment importer.
- `SRO_CAP110_SKILLS=1` enables the skill-tier importer; the database tier
  additionally needs `SRO_CAP110_SKILL_DATABASE=<absolute snapshot path>`.
- `SRO_CAP110_MONSTERS=on` enables the field-monster population.
- `SRO_PHARAOH_TOMB` needs `SRO_CAP110_MONSTERS` and refuses to start
  without it. It is port-only and off by default.
- `SRO_PHARAOH_TOMB_GEAR_RATE` (1 is the authored rate, 2 to 20; unset is 1)
  multiplies the equipment rolls of tomb elites (grade 6) and uniques
  (grade 8) only.

Turning the cap off stops further upward transitions at or above 90. It does
not lower existing characters, remove their items or undo persisted level
rewards. A release rollback needs its own content/state policy.

## How the importers behave

- Everything joins by codename, never by the later numeric ID. Native rows,
  IDs, statistics and names remain unchanged; new rows receive
  collision-free IDs, and a marker keeps every published ID stable across
  rebuilds. Reserving every retained identity happens before allocating new
  ones.
- Rebuilding with a flag off unregisters and removes the generated shards
  that flag owns; edited or unmarked import shards are refused rather than
  overwritten. The native extraction is never the output. Losing a
  previously published input (for example the database tier) on a later
  on-build fails instead of silently removing learned skills.
- The equipment import covers ordinary and Seal D10/D11 weapons, shields,
  armor and accessories. Egyptian set weapons and event/fortress tools are
  deferred; degree-12 links inside degree-11 labels are trimmed and
  recorded. Missing authored descriptions stay absent rather than receiving
  invented text; every weapon name is required.
- The skill import adds later player ranks above the native cap: the client
  tier stops at mastery 100 (in this client the "110 cap" content is a
  mastery-100 tier), and an explicit database snapshot adds mastery 101-110
  ranks. One trainable rank has SP cost 0 and needs review rather than
  shipping as-is. The learn handler's gates (mastery, prerequisites, SP) are
  unchanged; character level 110 waives none of them.
- The monster import stages field monsters, their nests and skills, and the
  database equipment-drop catalog through level 110. A pinned export hash is
  required; an export that records any other hash is refused.
- Alexandria's own shops are stocked with ordinary degree-10 goods
  (`STORE_SD_SMITH`, `STORE_SD_ARMOR`, restocked `STORE_SD_ACCESSORY`); a
  package's price is its item's price column, which is an inference because
  v1.150 has no degree-10 shop. Degree 11, grades B/C and rare or seal items
  are not stocked. Four dimensional gate pairs are restored with their
  authored fees and level-95 admission; no cave entrances were added.
- Pharaoh's Tomb runs as persistent shared areas on the default world; the
  original's per-tomb worlds and instance triggers are not modelled. Bosses
  are single-spawn nests with their authored respawn delay.
- Rewards for monsters the native client lacks come from their authored
  assigned-drop rules (alchemy materials and SD tokens), not from level
  class tables; gear for those monsters comes only from the level class
  tables, which is why tomb gear is rare by design. While
  `SRO_ALCHEMY_MATERIAL_DROPS_OFF` is on, the imported materials never drop
  (assigned drops included); only the tokens do.

## Client behaviour notes

- Skill slot icons use source column 61 under the icon root; new images are
  assigned through the existing `imagePackGroup` owner. Native absent-icon
  handling is unchanged; no replacement artwork was invented.
- Grade 8 (unique) has a port-only presentation entry: the existing Unique
  text and icon with a x1 HP factor. The server already sends current HP on
  selection; the entry is unreachable on a native build.
- Items in the gold-heap group carry a u32 amount; every other ETC group
  keeps a u16 stack count (`ITEM_ETC_SD_TOKEN_*` rides the gold group).

## Known limits

- Rogue Sable Duplicate (`SKILL_EU_ROG_STEALTHA_CHANGE_B_03`) is imported
  but refused by cast admission; the owner deferred this issue.
- No exhaustive automated combat, learning-path or numerical-balance
  certificate exists for the imported tiers; verification is representative
  casts plus the converter/loader test suites.
- About 46 imported skill animation bases have no animation set in either
  client and stay silent; some imported ranks compile to no execution shape.
- Tomb spawns, combat, boss respawn and drops are not exhaustively verified
  in game; neither is whether SD tokens pick up, stack and display correctly
  beyond the fixed session-ending pickup fault.
- Monster/drop progression beyond the imported field population, cave
  stability and deferred set items remain unfinished.

## Verification posture

Converter tests cover native precedence, ID collisions, both races,
parameter-link trimming, required names, off/on/off rebuilds and identity
retention; `check_identity_parity.py` (an overlay tool kept outside this
repository) joins client and server
character/skill/item data by codename and fails on any differing ID.
Production loaders must accept the staged tables, and published packs are
checked against the server's IDs. Source gates (scripts, formatting,
encoding, size, generated-root, typecheck) apply as usual. Where a full
gate could not run (for example the race step without a C compiler), the
checkpoint history says so instead of claiming a pass.
