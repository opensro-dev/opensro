# Optional Jupiter field and Mirror Dimension temples

Port-only, not native. The default stays v1.150, and every switch below is off by default. The import adds the
later client's Jupiter field and the two Mirror Dimension temple sets on top of the native tables, joined by
codename. Native rows, IDs and names never change.

## Switches

The temple switch needs the field switch; the importer and the server loader both refuse the pairing otherwise.

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_JUPITER` | Build and server | The Jupiter field (world 90), its terrain, navmesh and monster population, `jupiter-field.txt` |
| `SRO_JUPITER_TEMPLE` | Build and server | The two Mirror Dimension temple sets (worlds 86-89, 93, 94) beside the field, `jupiter-temple.txt`. Needs `SRO_JUPITER` |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`) and the
monster population from the export folder `SRO_JUPITER_DATABASE` names. The export records the shard backup it was
extracted from; an export made from another backup is refused before anything is staged.

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/jupiter_import.py` after the cap-110 populations. Behind the
field switch: the field's 127 regions' terrain remapped onto the merged tile catalog, their object catalog, navmesh
and minimap tiles, the region code and zone name rows, every Data file the native tree lacks (the monsters' models
among them), and the monster population. Behind the temple switch, the same for the temple worlds' outdoor regions.

| Part | Result (real inputs) |
| --- | --- |
| Regions | 140 distinct region ids across the seven worlds (the temple copies share region ids), 136 with sector terrain |
| Interiors | 4 dungeon regions (bit 15 set) shared by the temple copies: their `ainavdata` and the `dungeon/jupiter` DOF tree |
| Monsters | 101 kinds (100 `MOB_JUPITER_` kinds the population places, plus the field's one guest) in `characterdata_170000.txt` / `skilldata_170000.txt` |
| Placements | 2,296 nests: 770 in the field, 1,526 in the temple worlds |
| Worlds | `GROUP_JUPITER_FIELD` (90) and `GROUP_JUPITER_A/B/A2/B2/A3/B3` (86-89, 93, 94), registered as permanent outdoor worlds while their switch is on |

## Decisions recorded

These are inferences and port choices, not native evidence:

- **Admission window.** The kinds keep their authored levels; the family admits `MOB_JUPITER_` kinds at levels
  111-120 and grades 0, 1, 3, 6, 8 on both sides (importer and server loader). Nothing raises the player level cap.
  Kinds the source never places outdoors carry no nests and stay out.
- **The guest.** The source places one Arabia kind in the field, `MOB_ARABIA_KIDEMONAS` (level 120, grade 3). Like
  the cap-110 field's Apis, it is admitted by name on the same window, importer and server alike.
- **The guest's hive.** Its eleven spots carry a hive total cap of one alive. The nest schema has no cross-nest cap,
  so the spots project onto the first nest (lowest NestID) with the cap as its count.
- **NPC rows stay out.** The population export also places the area's NPCs (41 believer, commerce and garrison rows,
  all level 0). They are roster content for the NPC slice, not monsters, and never reach either population file.
- **No authored loot.** The import writes no reward table; the kinds drop on the native posture.
- **Identity band.** The import's character and skill shards own the 170000 band (120000 is the cap-110 family's,
  130000-150000 the pet import's, 160000 the Sky Temple's). Identities are marker-persistent across rebuilds.
- **Name rows.** The later client's name table is a shard index with 17-column rows; the import follows the index
  and maps rows onto the 15-column layout the name merge reads, absent translations taking the English cell - the
  same mapping the mall avatar import applies.
- **Interiors are published, not yet enterable.** The dungeon catalogue now carries the four regions (#3295):
  `dungeonInfo.mjs` appends their rows when the import's DOF tree sits beside `dungeoninfo.txt` in the variant
  build, each region bound by the id its DOF header embeds (32787 `piety_boss_di`, 32788 `piety_boss_uno`,
  32789 `piety_boss_upi`, 32790 `hide`), and the builders publish `dungeon-resources.json` plus the four region
  bundles `0x8013`-`0x8016`. A build without the temple switch has no such files and its catalogue is
  byte-identical. The native dungeoninfo entries stay the admission authority and no gate exists yet, so the
  interiors stand published with no way in - the same posture the temple bands shipped in. One source quirk
  rides along: `piety_boss_width.bms` names a lightmap file the later client's archive only ships under a spaced
  name, so that mesh publishes without a lightmap, which is how retail draws it too.
- **Gates and entry are a follow-up slice**, as are the area's NPCs and services. Which temple copy a gate admits
  is undecided there, not here.

The server side is `monster/jupiter.go` (admission and the world join), `world/jupiter` (the identity table) and
`instance.JupiterFieldDefinitions` / `JupiterTempleDefinitions` (registration). The switches are registered in
`shardsettings` like every other shard-wide setting, so a deployment can name them and the catalogue can describe
the shard they produce.
