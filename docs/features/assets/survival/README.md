# Survival Arena buff artwork

Six original flat glyphs, exported at 64×64 for the native-sized buff board.
Large silhouettes replace the earlier detailed artwork for readability at
24px. Rebuild with `scripts/build_survival_buff_icons.py` using the pinned
asset-tool Python environment. No retail skill icon or PK2 was replaced.

| File | Meaning |
| --- | --- |
| damage-surge.png | Red lightning bolt: random 2×–10× outgoing damage |
| unstoppable.png | Gold boot: abnormal-state/knockback immunity |
| unkillable.png | Green shield: incoming damage cannot take the last HP |
| arcane-spring.png | Blue droplet: skills consume no mana |
| fire-pit.png | Orange flame: fixed burning ground |
| mirror-shell.png | Purple shield and arrows: 30-second damage reflection |

The full CIF publisher calls `buildSurvivalBuffIcons` in
`scripts/build/shared/cifResources.mjs`. It copies these into the projected
`icon/port/survival` tree. The virtual parameter-job rows use those icons
with the existing timed board, tooltips and expiration bars. Re-publish the
six paths with the ordinary owner-preserving pack publisher after changing
the art; never edit the generated pack manifest manually.
