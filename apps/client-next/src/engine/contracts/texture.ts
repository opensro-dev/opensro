/*
===========================================================================

texture.ts - retained image resources shared by asset and renderer owners

Native resources retain their authored mip blocks for upload and device
restoration. Bitmaps remain available for sources that are not block compressed.
Only the renderer owns GPU textures; these contracts contain CPU resources.

===========================================================================
*/

/*
================
NativeTexture

A complete two-dimensional mip chain. Each level owns tightly packed rows;
block-compressed levels include the whole final block at small mip sizes.
================
*/
export interface NativeTexture {
	readonly kind: "native-texture";
	readonly width: number;
	readonly height: number;
	// BC formats are the retail textures; ETC2 and ASTC are port-only
	// variants for devices without BC (texture-variants.ts).
	readonly format:
		| "bgra8unorm"
		| "bc1-rgba-unorm"
		| "bc2-rgba-unorm"
		| "bc3-rgba-unorm"
		| "etc2-rgb8a1unorm"
		| "etc2-rgba8unorm"
		| "astc-4x4-unorm";
	readonly levels: readonly Uint8Array[];
	// A variant's level-0 alpha, one byte a texel, taken by the build from the
	// retail BC texels (decodeNativeTexture). Picking reads it because a
	// variant has no CPU decoder; absent on a variant, the source is opaque.
	readonly pickAlpha?: Uint8Array;
}

/*
================
WorldTexture

The image owner closes bitmaps on retirement. Native arrays are ordinary
owned memory and become collectible when their final consumer releases them.
================
*/
export type WorldTexture = ImageBitmap | NativeTexture;

/*
================
UiTexture

A UI image: a decoded bitmap or pixel array, or a native block texture
whose first level the UI renderer draws (minimap tiles).
================
*/
export type UiTexture = ImageBitmap | ImageData | NativeTexture;
