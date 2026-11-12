# Optional gears magic stones

Port-only, not native. The default stays v1.150, and the switch below is off by default. The import adds the
later client's leveled magic stones on top of the native tables, joined by codename. Native rows, IDs and
names never change.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_MALL_CATEGORIES` | Build and server | The mall's category layout and, with it, the 112 gears stone rows in Premium > Gears |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/gears_stone_import.py` after the second-hunting-grounds
pass. Behind the switch: exactly the 112 rows `ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_<kind>_<degree>` (16 kinds,
degrees 1-7) staged in `itemdata_190000.txt`, under fresh identities above every published row of the merged
item tables, remembered in `.opensro-gears-stones.json` so a rebuild never moves an item. The server authors
the listing itself (`commerce/mallcategories.go`): each stone stands in Premium > Gears at an authored 30
silk, badged new, and only while its row stands — a build without the import lists none and boots the rest.

## Decisions recorded

These are inferences and port choices, not native evidence:

- **No new mechanic.** Each stone is a native magic stone (type 3/3/11/1, the solid kind 3/3/11/7) whose
  Descriptions[0] names a shipped magicoption at its degree, so the native alchemy stone path serves them
  unchanged: no catalog addition, no handler. The shipped table carries every kind's option at degrees 1-12.
- **Admission.** Exactly the 112 codename rows; a missing, miscast or mislabelled row refuses the build
  rather than staging a partial set the mall would half list.
- **Price.** Authored at 30 silk (the summon scrolls' tier) in one constant the shard owner can retune.
- **Left out.** Sabakun's jewel and the extension gear are separate lines of the same epic: no row for either
  stands in any pinned client this build reads, and no behaviour is invented here.
