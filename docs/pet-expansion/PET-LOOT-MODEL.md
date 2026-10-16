# Caravan collar ground-drop model

Owner selected concept 2 on 2026-10-15, requesting the existing chest style.
The final prop uses muted teal leather, antique brass panels and buckle,
a turquoise medallion and two small red tassels. On the owner's follow-up,
all dimensions were increased **80% (scale1.8)**: width about7.66, height2.09.
It now reuses the native armor chest's ambient particle glow, including
`system/item_drop_equip.efp` and `system/item_drop_acc.efp`. No bespoke glow
shader or emissive material was added; the original chest remains unchanged.

## Editable source

- Geometry: `scripts/build/char/petLootCollar.mjs` (24-sided oval band, low-poly fittings).
- Detailed owned1536x1024 atlas: `scripts/build/char/pet-loot-textures/caravan-atlas.png`.
  Six material tiles: stitched teal leather, engraved floral brass, veined turquoise,
  dark leather lining, braided/beaded gold trim and crimson silk threads. Generated
  specifically for this prop with image generation; no retail texture art is copied.
  Original64x64 swatches remain preserved as the earlier version, now unused.
- Item binding: `PORT_PET_DROP_MODEL` in `scripts/build/shared/portItemRows.mjs`.
  All 27 existing pet equipment items use this one loot silhouette. Their inventory icons,
  stats and drops are unchanged; other items retain their native models.
- `scripts/build/char/buildDropModelAssets.mjs` includes the prop in normal full builds.
  `item/etc/drop_pet_caravan_collar.bsr` is a logical resource identity for our GLB,
  not a recovered/native BSR file.

The normal avatar exporter embeds the textures and native material-lighting metadata.
All six materials share one embedded atlas. UVs stay inset from tile boundaries;
the circular medallion and gem use radial UVs instead of repeating the entire
texture on each triangle. Geometry size and native glow are unchanged by this pass.
The drop builder borrows the chest's particle modifiers, requiring a valid native
donor instead of silently publishing an unlit fallback. The metadata-only BSR
refresher preserves this owned entry, which has no retail BSR on disk.
An initial textureless version looked pale under native ambient lighting; colored
textures fixed that. Do not replace them with diffuse-only color factors.

## Rebuild / publish

Set `SRO_GAME_ROOT` to the licensed client root containing `extracted/`, then run:

```powershell
$env:SRO_GAME_ROOT = 'E:/CodeProjects/SRO/AssetImports/OpenSRO-v150'
pnpm assets refresh pet-loot
```

The family holds the asset lock, rebuilds the drop manifest and client presentation
bindings, and republishes their existing pack owners. It does not restart the server.
Reload the client to pick up the new pack index. GLB output is generated, not checked in:
`/assets/itemdrop/item/etc/drop_pet_caravan_collar.glb`.

## Verification

- Seven focused asset/pipeline tests pass: finite grounded geometry, outward band
  normals, exact1.8 scaling, triangle budget, shared high-detail embedded atlas,
  inset/radial UV mapping,
  native glow donor validation, all pet bindings and preservation of the native
  template, and focused publication task registration.
- `pnpm task check:scripts` and `git diff --check` pass.
- All 27 published pet item references resolve to the owned prop; 39/39 drop models built.
- Live test: LabPetTest dropped/recovered its Rawhide Collar twice; the final textured
  version rendered beside the unchanged Cloth Shell gold chest in Hotan. Both items
  were recovered and Penguin was re-summoned. No character equipment was removed.
- Local comparison evidence: `E:/CodeProjects/SRO/outputs/pet-design-codex/pet-loot-collar-native-comparison.png`.
- Follow-up: enlarged1.8 collar and borrowed native glow rebuilt/published and
  verified in-game with no renderer errors. Dropped collar recovered, Penguin
  restored. New preview: `E:/CodeProjects/SRO/outputs/pet-design-codex/pet-loot-collar-large-glow.png`.
- Rich-texture pass: atlas published and verified in the live renderer, including
  drop/pickup with no warning/error logs. Collar recovered and Penguin restored.
  Proof: `E:/CodeProjects/SRO/outputs/pet-design-codex/pet-loot-collar-rich-textures.png`.

Skill testing remains postponed. This check used manual ground drops; it did not
rerun monster drop-rate or pet skill acceptance tests. This prop targets OpenSRO's
browser client; a retail client BSR/BMS/DDJ conversion is not included.
