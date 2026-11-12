# Arabia (SRO_ARABIA)

Port-only, not native. The Arabia continent postdates the v1.150
client, so the whole feature sits behind one switch, `SRO_ARABIA`,
off by default in code and deployment. With the flag off the build
and the server are unchanged.

| Switch | Default | Settles |
| --- | --- | --- |
| `SRO_ARABIA` (build + server) | off | the continent's terrain and names, its monster kinds and nests, and the Baghdad dungeon's world registration |

Both sides read the same switch: the variant build stages the
continent only while it is on (and a switch off reaps what a previous
on build left), and the server reads the two population files, admits
the kinds and registers the dungeon world only while it is on.

## The import

`scripts/arabia_import.py`, chained in the Alexandria variant build
(`build_alexandria_variant.py`) after Muhan. Both sources are the
operator's own and neither is committed:

| Source | Carries | Guard |
| --- | --- | --- |
| the v1.696 client (`SRO_V1696_ROOT`) | terrain, navmesh, minimap, region and zone names, the character and skill rows | archive sha256 pinned in `scripts/v1696_archive_pins.json`, verified before anything is read |
| the population export (`SRO_ARABIA_DATABASE`, `arabia-population.json`) | 1,656 authored nests of the town, three fields and dungeon | the export records its shard backup's sha256; only the pinned backup's export is admitted |

| Output | What it is |
| --- | --- |
| `characterdata_210000.txt` / `skilldata_210000.txt` | the 51 nest kinds converted to the v1.150 layout with fresh identities, their skills merged the cap-110 way |
| `arabia-field.txt` | Version-2 nests of the town and the three fields (persistent world) |
| `arabia-dungeon.txt` | Version-2 nests of the Baghdad dungeon (world 112) |
| variant files | the merged regions' terrain remapped onto the tile catalog, navmesh, availability bits, minimap tiles, region codes and zone names |

Measured on the real sources: 51 kinds at levels 121-130 (grades
0/1/3/6/8), 1,327 field nests and 237 dungeon nests, 72 NPC nests
counted and left for the NPC pass. The run reproduces byte-identically
(field sha256 `a1c88902...`, dungeon sha256 `e6aa849a...`, shard
sha256 `7164da39...`), and off reaps exactly the marker-owned outputs.

## Region reconciliation (recorded)

The client's regioncode names 264 `RN_ARABIA_*` regions (FIELD 181,
TOWN 26, PETRA 28, DUNGEON 20, GARDEN 9) and the build merges exactly
those - terrain and navmesh complete for all of them (1,052 map
files, 264 navmesh files, no gaps). The backup's nests cover 173 of
the 264. The counts differ from the first inventory's 263/207 because
each source draws the continent's border its own way; the import
takes the client's region set (it owns the terrain) and the backup's
nest set (it owns the spawns), and records both numbers here and in
the build summary rather than assuming either.

## Run mechanics

Baghdad town and the three fields stand on the persistent world, so
their nests join the default world as the cap-110 field population
does. One projection rule is worth naming: 22 field nests carry a
hive total cap of 1 (one kind, eleven spots in each of two fields);
the row schema has no cross-nest cap, so the capped kind projects
onto its first nest with the cap as its count - the same projection
the Jupiter population uses. The Baghdad dungeon is registered as a
permanent world (112, `GROUP_ARABIA_DUNGEON`, Type 0) while the
switch is on, and its 237 nests - every one at the source's second
spawn kind - join it there.

The boss-lair, Petra and Garden worlds (113-115) author no nests in
the backup. Their regions' terrain merges with the continent, so the
ground is walkable, but no world is registered for them and nothing
spawns there: their content is the admission slice's to source or
leave empty deliberately.

## Left for the admission slice

Recorded so the follow-up needs no second pass over the sources
(#3320 tracks it):

- Travel to Baghdad (the town is an island continent: a gate, ferry
  or teleport row must land there) and the Baghdad dungeon's entry.
- The town's NPC population (72 authored nests, counted by the
  import) placed by the NPC pass with their shops and talk menus,
  the Alexandria NPC pattern.
- Whatever content worlds 113-115 should carry.
- Any drops beyond the native posture: the import writes no reward
  table, so the kinds drop as their grade's native table says.
