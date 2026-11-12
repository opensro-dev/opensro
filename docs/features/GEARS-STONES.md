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
- **Left out, on evidence (#3331).** The two other Premium > Gears kinds of the screenshot epic were
  searched for in every source this build reads, and both stay unlisted — not for missing rows. An
  earlier line here recorded that no pinned client holds a row for either; that was wrong. Sabakun's
  jewel (`ITEM_ETC_NASRUN_ARCHEMY_UPGRADE_STONE`) and the extension gear (`ITEM_MALL_NASRUN_EXTENSION`)
  stand in the pinned v1.696 client's `ItemData_24400.txt` (ids 24397 and 24395, the stones' own shard
  neighbourhood), in the VSRO client's `ItemData_25000.txt` (ids 24417 and 24415), and in the ISRO-R
  shard backup's item census. What neither v1.150 nor this port holds is anything for them to act on:
  both serve only the Devil's Spirit (the later client's Nasrun). The extension gear lengthens a worn
  Nasrun's awakening by four weeks; the jewel upgrades one. The Nasrun wearable is item type 3/1/14 —
  v1.150's avatars are 3/1/13, the later-avatar import selects 3/1/13 only and skips the Nasrun rows
  for absent resources — and no awakening clock, Nasrun alchemy (the jewel's recipe and stones) or
  upgrade path exists here yet. Listing either now would sell an item that cannot be used, and a substitute
  effect would be invented behaviour. The owner's call (2026-11-12): port every prerequisite first. #3348
  ports the Devil's Spirit system (the wearable, its awakening clock, Nasrun alchemy) and then lists both.
- **The Halloween summon scrolls (#3331).** The screenshot's Joon (500 silk) and Firespirits (200)
  summon scrolls for HOT shop > Halloween Exclusive have no row in any source searched: the v1.150,
  VSRO and pinned v1.696 item tables, the ISRO-R shard census (40,691 codenames) and its game-reference
  item tables (whose only summon scrolls are the party scrolls and v1.150's own E050729 family). The
  content archive (#3329) packs world areas, not mall stock, and no pinned archive exists yet. With no
  source row there is nothing to price or summon, so the two stay unlisted; a future pinned source
  carrying their rows would admit them through the summon-scroll pattern `action/summonscroll.go`
  already serves.
