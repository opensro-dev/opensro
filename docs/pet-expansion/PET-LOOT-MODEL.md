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
- Original 768x512 procedural atlas: `scripts/build/char/petLootAtlas.mjs`.
  Six material tiles supply teal leather, brass, turquoise, lining, trim and
  red silk. The text recipe generates its PNG without external artwork inputs.
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
$env:SRO_GAME_ROOT = '<path-to-licensed-v1.150-client-root>'
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
- Earlier live drop/pickup tests passed with the same geometry and native glow.
  The new atlas replaces the AI texture; earlier screenshots are not evidence
  for this revised texture. Its export and geometry/material checks pass.

This targets the browser client; retail BSR/BMS/DDJ conversion is not included.
