/*
===========================================================================

pick-alpha.ts - a texture's alpha channel as a picking mask

Picking tests a ray's texel against the surface's alpha. The mask used to
be read back on the main thread for every world texture as it arrived (a
canvas draw plus getImageData), which stalled frames on region changes.
The asset worker now takes it from the pixels it decodes for DDS world
textures, and reads its own PNG bitmaps back (bitmapPickAlpha); the main
thread reads a bitmap back (readback.ts) only for a texture that arrives
without one. A native block texture (.texture) has no bitmap: its mask is
its first level decoded (nativePickAlpha).

===========================================================================
*/

import type { NativeTexture } from "@/engine/contracts/texture";
import { decodeNativeTextureLevel } from "@/engine/foundation/assets/native-texture";
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
nativePickAlpha

The alpha of a native block texture's first level: DXT1 punch-through and
DXT3/DXT5 alpha cut the same outlines the converted PNGs did (the decoder
is bit-exact against them, minimapTextureParity.test.mjs). Without it a
cutout object (leaves, fences) picked as a solid quad.
================
*/
export function nativePickAlpha( texture: NativeTexture ): PickAlpha {
	return rgbaPickAlpha( texture.width, texture.height, decodeNativeTextureLevel( texture, 0 ) );
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

/*
================
bitmapPickAlpha

The alpha channel of a decoded bitmap, read back through a 2D canvas.
Works on the main thread and in workers (OffscreenCanvas). A region
transition trace showed this readback at 0.46 ms of every main-thread
frame while PNG world textures arrived; the asset worker now does it.
================
*/
export function bitmapPickAlpha( image: ImageBitmap ): PickAlpha {
	const canvas = new OffscreenCanvas( image.width, image.height ),
		context = canvas.getContext( "2d", { willReadFrequently: true } );
	if ( !context ) throw new Error( "Picking alpha readback unavailable" );
	context.drawImage( image, 0, 0 );
	return rgbaPickAlpha( image.width, image.height, context.getImageData( 0, 0, image.width, image.height ).data );
}
