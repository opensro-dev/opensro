# Pet skill icons

The three `pet-skills*.png` atlases are original AI-generated artwork created
for the pet expansion.
Native character skill icons supplied the visual reference, not the subjects.
No retail PK2 is edited.

Each atlas has five equal columns (the two active and three passive
rows) and four equal rows: Wolf, Bear, Penguin, Crow. Book I uses the simpler
`pet-skills-1.png`, book II uses
`pet-skills-2.png`, and book III retains the unchanged `pet-skills.png`.
Row-help icons use the book III symbol at20px. `pet-skill-icons.ts` selects
cells using normalized UVs.

`buildPetItemIcons()` publishes this source through full CIF and focused
`pnpm assets refresh pet-loot` builds, with the normal publication ledger and
UI icon packs. Set `SRO_GAME_ROOT` to your extracted client root.

Top-right badges show server-observed Resolve, bear retaliation hits or crow
burst progress. Mark Prey shows attacks remaining to recharge. The left row
help also shows the owner's independent retaliation count. These are transient
combat values, not saved skill ranks or client-predicted animation counts.

## Pet equipment icons

`pet-gear-collar.png`, `pet-gear-harness.png` and `pet-gear-charm.png` are
original painted3x3 sheets, degrees1..9 in reading order. The normal
`scripts/build/shared/pet_item_icons.py` export converts each cell to32px RGBA.
Native character necklaces/rings provided style references, not copied subjects.
All existing logical item icon paths stay unchanged, so both player and pet
inventories use the new artwork after normal asset publication/client reload.

## Pet equipment icons

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

### Source and export

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

## Provenance

No retail archives or generated GLBs/WAVs are included. Existing
client assets are resolved by the standard local build. The new original pet
skill atlas is tracked as source artwork; its published copy belongs to the
normal generated tree. Nothing is injected into a PK2 archive.

| Addition | Editable source | Origin / build input |
| --- | --- | --- |
| Caravan collar ground geometry | `scripts/build/char/petLootCollar.mjs` | Original procedural geometry, owner-selected design |
| Collar atlas | `scripts/build/char/petLootAtlas.mjs` | Original procedural teal leather/brass/turquoise recipe; no external art |
| 27 degree icons | `docs/pet-expansion/assets/pet-gear-*.png` and `scripts/build/shared/pet_item_icons.py` | Original AI-painted collar/harness/charm artwork; native character gear style references, deterministic32px export |
| 60 pet skill icons | `docs/pet-expansion/assets/pet-skills*.png` | Original AI-generated painted atlases; simple/intermediate/full variants, native character skill style |
| Skill/equipment balance | `apps/server/internal/game/companion/petdesign.json` | Original port-only design; one source, the exporter generates the client module |
| Bear standing roar | `apps/client-next/src/engine/foundation/animation/bear-roar.ts` | Original animation adjustment on the native bear rig; see recipe for bone operations |
| Shield, roar aura, retaliation ripple and Guardian recovery | Client `characters/resources/resources.ts` | Original procedural geometry/textures; standard renderer |
| Pet bodies/rigs, native drop glow and audio | Operator-supplied v1.150 client | Native dependencies remain outside Git; hashes below |

The former AI-authored PNG sheets/atlas and XLSX have been removed from the
tracked tree. The canonical JSON retains their final balance values. Gear art
is exported from the three owned source atlases; the new skill atlas is a separate owned
source asset. No external non-native binary download is required. Generated
output belongs in the existing ignored asset tree.

### Pinned native inputs

These hashes identify the supplied v1.150 inputs used for this validation,
not a grant to redistribute them. Material/animation dependencies continue
through the existing BSR build pipeline and preparation manifest.

| Native path | SHA-256 |
| --- | --- |
| `Data.pk2/res/cos/p_bear01.bsr` | `bcebfd57f2d876a68023bae1a20071d5ad42f2ec09c4f8a68de51b8145f7b42a` |
| `Data.pk2/res/cos/p_bear02.bsr` | `a29888117982e578bd223b48903a9678040b95e614b9f3f0d65e6199959873bf` |
| `Data.pk2/res/cos/p_penguin01.bsr` | `90502d07ee6c868759a40d284e3a41d7825eb5b05a8c125285dce7a22e087f2d` |
| `Data.pk2/res/cos/p_penguin02.bsr` | `c07dbb8709861baf149bd63fd3d4f28f71f1d6d4152b04b0ce86b1f1cbfc23b6` |
| `Data.pk2/res/cos/p_raven01.bsr` | `54601256687029c5ca858a563cf0097b4069b1be045ab782e9f9e8dacbdf0472` |
| `Data.pk2/res/cos/p_raven02.bsr` | `68b9418358dab594078fe3986bf2f4f25352e65f7b97547b29aced31de314ad4` |
| `Data.pk2/res/cos/p_wolf_01.bsr` | `d15dec08a55fb1d46ab89c1c03df89850b7e42a08da9a031818f793df86f81e6` |
| `Data.pk2/res/cos/p_wolf_02.bsr` | `3412c07111170367b5faca650342f0f19cd471963b3cc7cb3c3defad20ee6351` |
| `Data.pk2/prim/snd/cos/cos_bear2_shout1.wav` | `6be49e29a16b263eaf4aa73aeb66eb07436ef77dd68d8923e2acee15a1a29f66` |
| `Data.pk2/prim/snd/skill2/usk_warrior_guard_phyfence_a.wav` | `ccbf9f30b1c0a39b38b13fb6362ae3be9da046631ddd860440f73000dbff3d60` |
| `Data.pk2/prim/snd/skill2/usk_warlock_blood_explosion_bomb.wav` | `b163f0a19b2d45d94abff5edaa0910d4f059546f698761e20e0f03bcc706fae0` |
| `Data.pk2/prim/snd/skill2/usk_cleric_heal_target_a.wav` | `dde1832e90757ce3a0693b41790e2d9ee1201f523378f84715981fda07182996` |
| `Data.pk2/prim/snd/skill2/usk_cleric_rebirth_target_a.wav` | `881b63c798a992f82ed567ad1b64e6de03931154002e3b8b6bffb13c936c239f` |
| `Particles.pk2/system/item_drop_equip.efp` | `d1ebc82a2bd4d05f2d1dbaf733ed02c1d8aab46f83d4f68f0a6cf8a570f91d87` |
| `Particles.pk2/system/item_drop_acc.efp` | `94a506e125dbc817e32177dac8f4276cffa232e7efbd273c4cb75b5bce52b15e` |
