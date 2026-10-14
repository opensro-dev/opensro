/*
===========================================================================

texture-variants.ts - re-encoded textures for devices without BC

Port-only, not native. The retail textures are BC (DXT1/3/5) and the
client uploads them unchanged where the GPU samples BC. A device without
BC (iPhone, iPad, most Android) otherwise decodes every level to RGBA8:
four to eight times the memory, which is what ends a phone tab. With the
Experimental "Compressed textures" row on, such a device is served the
asset build's re-encoding of each texture in a format it does sample
(ETC2 or ASTC 4x4), published beside the original under
TEXTURE_VARIANT_ROOT so every path keeps one owning pack group.

The asset build (scripts) and the asset worker read the same rules here.

===========================================================================
*/

// Where a variant set is published: TEXTURE_VARIANT_ROOT + format + the
// original path below /assets.
export const TEXTURE_VARIANT_ROOT = "/assets/texture-variants/";

/*
================
TextureVariant

A published variant set. ASTC 4x4 is 8 bits a texel for every source;
ETC2 keeps BC1's 4 bits for colour textures (RGB8A1) and 8 for alpha.
================
*/
export type TextureVariant = "etc2" | "astc";

// The adapter features the choice reads (named as WebGPU names them).
export type TextureFeature = "texture-compression-bc" | "texture-compression-etc2" | "texture-compression-astc";

/*
================
textureVariantOrder

The order a device that samples both is served in.
================
*/
export function textureVariantOrder(): readonly TextureVariant[] {
	return [ "etc2", "astc" ];
}

/*
================
textureVariantFeature

The WebGPU feature a variant set needs.
================
*/
export function textureVariantFeature( variant: TextureVariant ): TextureFeature {
	return variant === "etc2" ? "texture-compression-etc2" : "texture-compression-astc";
}

/*
================
chooseTextureVariant

The variant a device is served: none where it samples BC (the retail
textures upload unchanged) or the switch is off, else the first variant
whose feature it has.
================
*/
export function chooseTextureVariant(
	features: ReadonlySet<string>,
	enabled: boolean
): TextureVariant | null {
	if ( !enabled || features.has( "texture-compression-bc" ) ) return null;
	return textureVariantOrder().find( variant => features.has( textureVariantFeature( variant ) ) ) ?? null;
}

/*
================
textureVariantPath

The published path of path's variant, or null for a path that has none
(only native .texture containers under /assets/ are re-encoded).
================
*/
export function textureVariantPath( path: string, variant: TextureVariant ): string | null {
	if ( !path.startsWith( "/assets/" ) || !path.endsWith( ".texture" ) || path.startsWith( TEXTURE_VARIANT_ROOT ) ) {
		return null;
	}
	return TEXTURE_VARIANT_ROOT + variant + path.slice( "/assets".length );
}
