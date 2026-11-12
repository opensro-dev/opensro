# Optional second hunting grounds

Port-only, not native. The default stays v1.150, and the switch below is off by default. The import adds the
later client's second hunting ground set (the `RN_NEW_HUNTING_SECOND_FIELD` regions) on top of the native
tables, joined by codename. Native rows, IDs and names never change.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_HUNTING2` | Build and server | The set's 104 regions, their terrain, navmesh and monster population, `hunting2-field.txt` |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`).
There is no database export for this set: it postdates the later shard backup, so its population evidence is
the client's own `npcpos` placements standing in its regions, joined to `characterdata` by codename
(research/investigations/later-sources-3247).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/hunting2_import.py` after the Jupiter merge. Behind the
switch: the 104 regions' terrain remapped onto the merged tile catalog, their object catalog, navmesh and
minimap tiles, the region code and zone name rows, every Data file the native tree lacks (the monsters'
models among them), and the monster population.

| Part | Result (real inputs) |
| --- | --- |
| Regions | 104 region ids, all with sector terrain, navmesh and minimap tiles in the v1.696 client |
| Monsters | 71 kinds in `characterdata_180000.txt` / `skilldata_180000.txt` (identities above the published rows; marker-persistent) |
| Placements | 4,060 client placements turned into 4,060 nests standing in 63 of the regions |
| World | The default world (world 1): no new world is registered |

## Decisions recorded

These are inferences and port choices, not native evidence:

- **Admission.** Exactly the kinds `npcpos` places in the set's regions, at the levels `characterdata`
  authors (49-140, grades 0 and 1), on both sides (importer and server loader). The set re-stages known
  species across a wide span - the same species stands at several levels - so the authored level is the
  spawn level: no re-banding, and nothing raises the player level cap. The remaining 41 regions carry no
  monster placement in the client and stand empty, as authored.
- **Placements become nests** by the cap-110 client posture: one nest of one monster per placement point,
  radius 0, the synthesized ordinary controls (sight range 100) and respawn posture (1-3 s). There are no
  server nests to copy caps or tactics from; counts per placement are import-time authoring.
- **No authored loot.** The import writes no reward table; the kinds drop on the native posture.
- **Identities.** The import's character and skill shards are `characterdata_180000.txt` /
  `skilldata_180000.txt` (one importer per shard name: 120000 is the cap-110 family's, 170000 Jupiter's).
  Identities allocate above every published row and are marker-persistent across rebuilds.
- **Name rows.** The later client's name table is a shard index with 17-column rows; the import follows
  the index and maps rows onto the 15-column layout, absent translations taking the English cell (the
  Jupiter import's helpers).

The server side is `monster/hunting2.go` (admission and the default-world join). The switch is registered
in `shardsettings` like every other shard-wide setting, so a deployment can name it and the catalogue can
describe the shard it produces.
