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

- Editable full-size AI-authored sheets: `scripts/build/shared/pet-item-icons/{collar,harness,charm}-degrees.png`.
  Each has nine cells in reading order (1–9); native gear images were style references.
- `scripts/build/shared/pet_item_icons.py` extracts each cell, downsamples to32x32
  and supplies the native-style one-pixel border. It does not recolor one degree
  into another. The original source sheets remain unchanged.
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
data is edited to install these icons. Skill testing remains postponed.

## Deployment evidence — 2026-10-15

Published all27 icons in the `ui-icons` pack and verified all27 client bindings.
Rebuilt the server game-data bundle with source revision `workspace-pet-icons`;
manifest SHA256 `030dfaa6caac1ed4e7772443f00140a35121daa26992c5a7cf317a0180fe6592`.
Gracefully restarted Agent0 and GameWorld0; both deployments are1/1 healthy.
Reloaded the browser and confirmed the new inventory artwork for the test items
(including degree1 Collar, Harness and Charm). No renderer warnings/errors.
All27 exports are checked; not every degree was individually inspected in-game.

Logs and live proof: `E:/CodeProjects/SRO/outputs/pet-design-codex/`:
`pet-icons-publish.log`, `pet-icons-server-data.log`, `pet-icons-deploy.log`,
`pet-icons-live-inventory.png`. Browser left with inventory open in safe Hotan;
LabPetTest90 and Penguin remain intact. These visual changes are uncommitted.
