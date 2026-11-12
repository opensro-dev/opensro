# Optional Constantinople fortress area

Port-only, not native. The default stays v1.150, and the switch below is off by default. The import adds
the later client's Constantinople fortress area on top of the native tables, joined by codename and region
id. Native rows, IDs and names never change, and the flag-off build is byte-identical to the build without
this import.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_FORT_CONSTANTINOPLE` | Build and server | The fortress's 36 regions, its 49 event zones, its structures and its siegefortress row |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/constantinople_import.py` after the Sky Temple merge,
sharing that import's catalogue-merge machinery. Behind the switch: the 36 `RN_FORT_CONSTANTINOPLE`
regions' terrain remapped onto the merged tile catalogue, their object catalogue and navmesh, the region
code and zone name rows, the model closure of the placed objects and the new structures, the 49
`INS_FORT_CT` event-zone rows in `eventzonedata.txt`, the `FORTRESS_CONSTANTINOPLE` row in
`siegefortress.txt`, the seven structure characters the shipped tables lack in
`characterdata_210000.txt`, and the zones' placements staged as
`Data_extracted/navmesh/objectstring.portonly.ifo`, which the server bundle's structure-zones projection
merges beside the native `objectstring.ifo` when the file exists.

| Part | Result (real inputs) |
| --- | --- |
| Regions | 36 region ids (sectors x 90-95, z 59-64), all with sector terrain and navmesh in the v1.696 client |
| Characters | 7 new structures in `characterdata_210000.txt` (the fort stone, the big and small gates, the four gate pulleys), identities above the published rows, marker-persistent |
| Zones | 49 event zones of kinds 1-3 standing their default structures in world 5 (`INS_FORT_CT`, already in the shipped gameworld catalogue) |
| Registration | `siegefortress.txt` row id 4 joins `loadFortressCatalog`; `enterworld.siegeFortressRows` projects the same row to the client while the switch is on |

The fortress then runs on the port's existing fortress line: the authority catalog binds world 5 to
fortress id 4 by the world's first string column, `appendFortressStructures` stands each zone's default
structure on its placement, and the war, tax, staff and structure services apply unchanged.

## Decisions recorded

These are inferences and port choices, not native evidence:

- **No new world registration.** The shipped gameworld catalogue already carries world 5 (`INS_FORT_CT`)
  and its siege class; the later client's `gameworlddata.txt` does not even carry the row. The import
  therefore adds no world definition - the fortress joins through its tables alone.
- **No skill shard.** The later gates carry two skill cells the shipped tables dropped; the shipped
  `STRUCTURE_*` twins the conversion produces carry exactly the converted posture (the fort stone keeps
  the one converted skill its Jangan twin has). Nothing re-authors skills for structures.
- **Staff spawns are a follow-up.** `npcpos` places the fortress manager, battle aide and smith in the
  yard; all three are shipped characters, but standing them up is a population change with its own
  admission rules, and no part of the fortress registration waits on it. Not imported here.
- **Identities.** The import's character shard is `characterdata_210000.txt` (one importer per shard
  name: 120000 is the cap-110 family's, 160000 the Sky Temple's, 170000 Jupiter's, 180000 the second
  hunting grounds', 190000 the GOD temples', 200000 Arabia's and Muhan's). Identities allocate above
  every published row and are marker-persistent across rebuilds.
- **Placements ride a side file.** The native `navmesh/objectstring.ifo` is a shipped client file and is
  never rewritten; the fortress's 49 placement lines stage beside it and the bundle merges them,
  refusing a name both files place differently.

The switch is registered in `shardsettings` (not a catalogue setting: the catalogue publishes no fortress
state) and described on the public rules page.
