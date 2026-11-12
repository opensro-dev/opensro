/*
===========================================================================

normal-tile.ts - CIFNormalTile's repeated background

CIFNormalTile::RenderTiled (6F86F0) repeats its texture across the control
at natural texel scale, right and bottom remainders included; no stretched
interior, no tint.

===========================================================================
*/

import type { UiQuad, UiRect } from "@/engine/contracts/ui";

/*
================
normalTile

One tiled quad whose uv runs one unit per texture. It used to be one quad
per texture-sized tile, edge to edge; at a fractional UI scale each shared
edge inside an output pixel was covered partly by both quads, and their
two partial coverages let the world show through as a thin line (the job
alias window). A single quad has no inner edges to lose.
================
*/
export function normalTile(
	rect: UiRect,
	texture: string,
	size: readonly [number, number] | undefined,
	clip: UiRect
): UiQuad[] {
	if ( !size ) return [];
	if ( !size.every( n => Number.isFinite( n ) && n > 0 ) ) throw Error( "Invalid tile sprite extent" );
	const [, , w, h] = rect, [tw, th] = size;
	if ( !(w > 0 && h > 0) ) return [];
	return [ { rect, texture, uv: [ 0, 0, w / tw, h / th ], color: [ 1, 1, 1, 1 ], clip, tiled: true } ];
}
