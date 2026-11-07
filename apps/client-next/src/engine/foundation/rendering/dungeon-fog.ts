/*
===========================================================================

dungeon-fog.ts - the terms of a dungeon block's shader fog

A .dof block's FogParameter reaches room geometry through the retail vertex
shaders (Data shader/vertexshader0*.c), not through D3DFOG_LINEAR:

	distance = 2 ^ -( viewZ * density ) ^ 2
	height   = 1 - ( |worldY - center| - band ) * slope
	oFog     = clamp( min( distance, height ), 0, 1 )

A9CFE0 feeds both constants per block: c16.w is the density (A5A66E) and c22
is ( center, band, 1 / divisor, rise ) (A5A680). The block's near and far
planes only serve fixed-function draws: 8AEEAC turns on table fog for
dungeon water, which keeps the linear ramp.

===========================================================================
*/
import type { WorldMaterial } from "@/engine/contracts/scene";

/*
================
dungeonHeightFog

The ( center, band, slope ) of a block's height term, slope 0 when it has
none. The .dof fields keep the names the build publishes: unk3 center Y,
unk4 clear half-height, unk5 divisor, unk6 rise.

A5A680 treats a divisor of 0 as 1. For rise <= 0 it returns without writing
c22, which leaves the previous block's constants live; its flag at +0xE8 has
no reader near the batch code. Inferred: the authored intent of a block
without height fog is none, so the port does not carry that state across.
================
*/
export function dungeonHeightFog( fog: NonNullable<WorldMaterial["fog"]> ): readonly [number, number, number] {
	const height = fog.heightFog;
	if ( !height || height.unk6 <= 0 ) return [ 0, 0, 0 ];
	return [ height.unk3, height.unk4, height.unk6 / (height.unk5 === 0 ? 1 : height.unk5) ];
}
