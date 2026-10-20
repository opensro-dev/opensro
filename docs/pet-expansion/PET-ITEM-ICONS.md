# Pet equipment icons

There are27 distinct inventory icons: Collar, Harness and Charm, each with
degrees1–9. These follow the original32x32 gear style: a compact painted
object on near-black navy with a restrained steel-blue edge. Degrees differ
in shape, material and ornamentation, not only color. Existing names and
stats are unchanged.

| Degree | Collar | Harness | Charm |
| --- | --- | --- | --- |
|1|Rawhide|Rope|Bone|
|2|Studded|Leather|Fang|
|3|Bronze|Studded|Jade|
|4|Iron|Scale|Amber|
|5|Steel|Chain|Moonstone|
|6|Silver|Plate|Sunstone|
|7|Gilded caravan|Gilded|Starstone|
|8|Runed|Runed|Spirit|
|9|Dragonbone|Dragonscale|Primal|

## Source and export

- Original painted source: `docs/pet-expansion/assets/pet-gear-{collar,harness,charm}.png`.
  Each square3x3 atlas contains degrees1..9, left to right and top to bottom.
  Native character necklaces/rings supplied the dark background, painted-metal
  and restrained-highlight style references. The subjects are original artwork.
- `scripts/build/shared/pet_item_icons.py` deterministically exports all27 cells
  as32x32 RGBA PNGs. No source-art generation or network call is needed to build.
- `scripts/build/shared/petItemIcons.mjs` publishes the27 PNGs using the existing
  public-tree writer. Full CIF builds and `pnpm assets refresh pet-loot` both call it.
- `scripts/build/shared/portItemRows.mjs` binds every slot/degree to
  `item/pet/{slot}_{degree:02}.ddj`. These are logical icon identities, published as
  `/assets/images/Media_extracted/icon/item/pet/{slot}_{degree:02}.png`; no native
  DDJ binaries are overwritten. The pack owner remains `ui-icons`.

Inventory icon references arrive in the server's item snapshot. After changing
the icon-column binding, rebuild the server game-data bundle with the normal
`buildServerGameDataBundle.mjs` builder and restart the local backend gracefully.
Publishing client images alone does not update a running server's reference table.
Changing artwork without changing its path only needs client asset publication
and client reload.

## Checks

Nine focused asset/pipeline tests pass: all27 icon names unique, all27 emitted
images distinct32x32 RGBA PNGs with visible object detail, source row preservation,
pet drop geometry/material/glow and family registration. No inventory/character
data is edited to install these icons.

The current source atlases replace the earlier procedural placeholders.
Names, paths, equipment stats and bindings remain unchanged.
