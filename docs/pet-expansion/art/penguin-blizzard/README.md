# Penguin Blizzard artwork

Owned transparent artwork generated with the built-in image-generation tool on 2026-11-04. No retail textures were copied. The existing penguin mesh is unchanged.

## Prompt

Create one premium game VFX particle texture atlas on genuine transparent alpha, square, exactly four equal quadrants with spacious transparent gutters. Production texture artwork for a magnificent penguin blizzard in a fantasy MMORPG, not a scene or concept sheet. No penguin, bear, ground, environment, text or borders. Luminous pale icy blue and cool white, intricate crystalline detail. Top left: soft translucent snowy mist in a loose spiral curl, turbulent wisps and airy edges. Top right: sweeping S-shaped frost-wind ribbon, thin icy-white strands, blue transparent fringes and glitter. Bottom left: crisp six-arm branching faceted snowflake, tiny luminous edge. Bottom right: organic radially branching lacy frost veins, transparent holes, feathered irregular edge; no solid disc or square. At least 10% transparent border in each cell. Readable over grass, translucent and softly lit.

## Build and delivery

Keep frost-wind.png as the editable source. Run from the repository root:

    python scripts/build/char/build_penguin_blizzard_atlas.py

The shared compiler creates a 512px BC3 atlas with transparent filtering gutters, clears insignificant alpha-matte RGB and records the source SHA-256. The generated TypeScript asset ships inside the client bundle. Existing BC3 upload/software fallback handles it; no PK2 changes or separate asset hosting are required.

The runtime combines this atlas with bounded wind, snow and frost meshes, plus faceted procedural 3D ice. See docs/pet-expansion/PENGUIN-BLIZZARD-REFERENCE-2026-11-05.md for timing and verification.
