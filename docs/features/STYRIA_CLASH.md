# Optional Styria Clash arena

Port-only, not native. The default stays v1.150, and the switch below is off by default. The import adds the
later client's job arena ground (the `RN_ARENA_JOB_WAR` regions) on top of the native tables. Native rows, IDs and
names never change.

This is the first slice of #3272: the map alone. The match rules (registration, the Hunter and Thief sides, the
match instance, treasure, scoring and rewards) are the Battle Arena engine, epic #3648, which will land behind the
same switch. Until then nothing registers the arena's world or links a gate to it.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_STYRIA_CLASH` | Build (server from #3648) | The arena's four regions: terrain, navmesh, minimap tiles and region codes |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/styria_import.py` as one of the later layers
(`scripts/variant_later_layers.py`), after the second hunting grounds. Behind the switch, the later-world engine
(`scripts/later_world_import.py`) merges the four regions' terrain remapped onto the merged tile catalog, their
object catalog, navmesh and minimap tiles, the region code rows, and every Data file the native tree lacks.

| Part | Result (real inputs) |
| --- | --- |
| Regions | 31985, 31986, 32241, 32242: outdoor sectors x 241-242, y 124-125; 16 map files, 4 navmesh, 4 minimap tiles, 4 region codes |
| Data files | Every v1.696 Data file the native tree lacks (13,275 with no other later layer on; none new when an earlier v1.696 layer such as Arabia already added them) |
| Monsters | None: the client places nothing in the arena |
| World | None yet: world 107 (`INS_ARENA_JOB_WAR`) is the engine's |

## Decisions recorded

These are inferences and port choices, not native evidence:

- **Regions.** The arena is exactly the regions `regioncode` lists under `RN_ARENA_JOB_WAR`. The import refuses
  a client that lists any other set, or a dungeon region under the code.
- **No population.** The client's `npcpos` places nothing in the four regions. Its two arena kinds
  (`MOB_EV_ARENA_JOB_THIEF`, `MOB_EV_ARENA_JOB_HUNTER`) belong to the match rules and arrive with #3648. The
  import's shard names (`characterdata_230000.txt` / `skilldata_230000.txt`) are reserved so the band stays this
  import's, and no rows are written to them.
- **No zone name.** The client authors no `textzonename` row for the four regions (by id or by code); its
  "Styria Clash" label lives in `refregion` and the arena UI text (`UIIT_STT_ARENA_JOB_WAR`), which the arena
  screens of #3648 carry. No player can reach the ground before then: no gate links to it.
- **Off.** A build with the switch off reaps exactly what the import's marker (`.opensro-styria.json`) records and
  reads no archive.
