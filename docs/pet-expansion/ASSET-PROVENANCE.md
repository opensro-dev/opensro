# Attack-pet asset provenance

No retail archives or generated GLBs/WAVs are included. Existing licensed
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
| Bear standing roar | `apps/client-next/src/engine/foundation/animation/bear-roar.ts` | Original animation adjustment on licensed native bear rig; see recipe for bone operations |
| Shield, roar aura, retaliation ripple and Guardian recovery | Client `characters/resources/resources.ts` | Original procedural geometry/textures; standard renderer |
| Pet bodies/rigs, native drop glow and audio | Operator-supplied v1.150 client | Native dependencies remain outside Git; hashes below |

The former AI-authored PNG sheets/atlas and XLSX have been removed from the
tracked tree. The canonical JSON retains their final balance values. Gear art
is exported from the three owned source atlases; the new skill atlas is a separate owned
source asset. No external non-native binary download is required. Generated
output belongs in the existing ignored asset tree.

## Pinned native inputs

These hashes identify the supplied v1.150 inputs used for this validation,
not a grant to redistribute them. Material/animation dependencies continue
through the existing BSR build pipeline and licensed preparation manifest.

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
