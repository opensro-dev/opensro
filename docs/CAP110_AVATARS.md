# Cap-110 mall avatars

Port-only, not native. A newer (cap-110) client ships mall avatar outfits that
v1.150 never had. `scripts/import_cap110_avatars.py` copies a whole set from
that client into the v1.150 extraction, and `SRO_MALL_EXTRA_GOODS=on,cap110`
lists it in the Item Mall. Without the import, or with the setting left at its
default `on`, the game is the v1.150 game exactly.

The first set is **Cursed Soul** (`piratecrew`): dress, hat and back
accessory, male and female, each with a recolour.

## What the import writes

The v1.150 extraction (`extracted/`) is never edited. The importer writes an
overlay beside it, and a merge step builds a separate tree from the two.

```powershell
$env:SRO_GAME_ROOT = 'C:\sroserver\client'
py -3 scripts/import_cap110_avatars.py 'C:\path\to\VSRO_Client' piratecrew
py -3 scripts/merge_extraction_overlays.py cap110
```

`--dry-run` lists what the import would write. The archives are only read.
Running the import again replaces the set's overlay folder and changes nothing.

| Overlay path (`extracted/overlays/cap110/<set>/`) | Content |
| --- | --- |
| `Data_extracted/...` | the set's models (`res/item/avatar`), meshes, materials and textures: new files |
| `Media_extracted/icon/item/avatar/...` | the inventory and mall icons: new files |
| `Media_extracted/server_dep/silkroad/textdata/ItemData_30000.txt` | the item rows: a new file |
| the same folder: `itemdata.txt`, `textdataname.txt`, `refpackageitem.txt`, `refpricepolicyofitem.txt`, `refscrapofpackageitem.txt` | only the rows to append to retail's table of that name (the index lists `ItemData_30000.txt`) |
| `overlay.json` | the set and its item IDs |

`merge_extraction_overlays.py` builds `extracted-merged/`: every retail file
hard-linked (a copy where the volume cannot link, so it costs no space), the
overlay's new files added, and each overlay table's rows appended after
retail's in a new file. It refuses a row retail or another overlay already has,
and a new file that would replace a different one. `overlay-manifest.json`
records the overlays, the added files with their hashes, the appended rows and
the hash of retail's preparation record.

The donor's item IDs are kept. The newer client reuses some IDs for other items
than v1.150 does, so the import refuses an ID (item, package or scrap) the
extraction already uses. Only mall rows of the named set are taken; the donor's
event copies are left out. The donor's price rows carry one extra cell, which
the import drops; every mall row takes the extraction's service value and the
silk price of its tier.

Never replace the v1.150 textdata wholesale with the newer client's tables:
the shared rows differ.

## Build and list

`SRO_EXTRACTION_OVERLAYS=cap110` makes every builder and Python tool read
`extracted-merged/` in place of `extracted/` (`scripts/build/world/paths.mjs`
and `scripts/sro_paths.py` own the rule). A missing merged tree, or one merged
for other overlays, stops the build. Unset, the build is retail exactly.

```powershell
$env:SRO_EXTRACTION_OVERLAYS = 'cap110'
pnpm assets build
pnpm task build server-game-data
```

Re-running `prepare_client_resources.py` repairs retail only; run the merge
again afterwards. Then start GameWorld with `SRO_MALL_EXTRA_GOODS=on,cap110`
(Nomad variable `mall_extra_goods`). The setting is a comma list; `on` is the
v1.150 outfits the mall never sold, `cap110` is this set, and `tab:package`
entries still work. If the packages were not built in, GameWorld stops at boot
and the message names the setting and the import script.

## Items and prices

The mall prices follow the v1.150 outfit tiers, not the donor's later ones:
130 silk for a dress, 30 for a hat and 15 for the back accessory. The attachment is listed on the hat tab,
as Halloween's is, because v1.150 has no attachment tab.

| | Male | Female |
| --- | --- | --- |
| Dress | `ITEM_MALL_AVATAR_M_PIRATECREW` (24357) | `ITEM_MALL_AVATAR_W_PIRATECREW` (24358) |
| Hat | `ITEM_MALL_AVATAR_M_PIRATECREW_HAT` (24353) | `ITEM_MALL_AVATAR_W_PIRATECREW_HAT` (24354) |
| Accessory | `ITEM_MALL_AVATAR_M_PIRATECREW_ATTACH` (24363) | `ITEM_MALL_AVATAR_W_PIRATECREW_ATTACH` (24364) |
| Recolour dress | `ITEM_MALL_AVATAR_M_PIRATECREW_2` (35580) | `ITEM_MALL_AVATAR_W_PIRATECREW_2` (35581) |
| Recolour hat | `ITEM_MALL_AVATAR_M_PIRATECREW_HAT_2` (35578) | `ITEM_MALL_AVATAR_W_PIRATECREW_HAT_2` (35579) |

Outside the mall, a GM can create one with `/MAKEITEM <codename> 1`.

## Verification scope

The importer's row handling and the overlay merge have unit tests
(`scripts/test/python/test_cap110_avatars.py`, `test_extraction_overlays.py`),
including that retail files keep their bytes. The setting's parsing and its
refusal without the import are tested without game data; the test that lists
the imported set in a real catalogue runs only where the overlay was built in.
The author bought and equipped the outfits in a local client. Other players
seeing them and sets beyond Cursed Soul were not checked.
