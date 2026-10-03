/*
===========================================================================

pick-alpha.ts - a texture's alpha channel as a picking mask

Picking tests a ray's texel against the surface's alpha. The mask used to
be read back on the main thread for every world texture as it arrived (a
canvas draw plus getImageData), which stalled frames on region changes.
The asset worker now takes it from the pixels it decodes for DDS world
textures; the main thread reads a bitmap back (readback.ts) only for a
texture that arrives without one.

===========================================================================
*/

import type { PickAlpha } from "@/engine/foundation/rendering/picking";

/*
================
rgbaPickAlpha

The alpha bytes of tightly packed RGBA pixels.
================
*/
export function rgbaPickAlpha( width: number, height: number, rgba: ArrayLike<number> ): PickAlpha {
	const pixels = new Uint8Array( width * height );
	for ( let i = 0; i < pixels.length; i++ ) pixels[i] = rgba[i * 4 + 3]!;
	return { width, height, pixels };
}

/*
================
validPickAlpha

A mask that fits its image: anything else is ignored and read back instead.
================
*/
export function validPickAlpha( alpha: PickAlpha | undefined, width: number, height: number ): alpha is PickAlpha {
	return !!alpha && alpha.width === width && alpha.height === height &&
		alpha.pixels instanceof Uint8Array && alpha.pixels.length === width * height;
}
