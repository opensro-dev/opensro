/*
===========================================================================

multi-texture.ts - a BSR model's second texture stage (ModDataMultiTex)

ModDataMultiTex and ModDataMultiTexRev bind one more texture beside the
material's own, and ModDataTexAni decides which of the two its matrix
moves. This module owns that pairing: what a MultiTex record means as a
stage, and where each texture's coordinates come from at draw time. The
geometry shader (pipelines.ts) draws the packed row.

Native, SRO_Client v1.150:
- CRTModMultiTex apply 0xAEDD20 binds its texture at the next stage
  (++0xF114D8) and runs the begin entry of its mode there. Rev (0xAEE1A0)
  binds its texture at stage 0, runs the mode on the next stage, and ORs
  0x200000 | 0x80000000; CAnimInstance_RenderTrack 0xA5E419 then binds the
  material's own texture at stage 1.
- Mode 0 (0xAEDEB0 / 0xAEE340): COLOROP BLENDFACTORALPHA, TEXTUREFACTOR =
  the payload's ARGB. Mode 1 (0xAEDB00): ARG1 TEXTURE, ARG2 CURRENT,
  COLOROP BLENDTEXTUREALPHA, ALPHAOP DISABLE. Neither enables the stage's
  alpha, so a pixel's alpha is stage 0's.
- ModDataTexAni words24[0] ORs 0x400000 (1) or 0x8000000 (0) (0xAED9B0),
  and RenderTrack 0xA5E25D..0xA5E338 picks the vss2.c UV variant:
  0x200000 with 0x8000000 TEXANI1R, else 0x200000 SPEC, else 0x8000000
  TEXANI0, else 0x400000 TEXANI1, else TEXTURE0.

research/investigations/render/texani-stage1-2026-11-17.md has the trail.

===========================================================================
*/
import { D3DTOP_BLENDFACTORALPHA, D3DTOP_BLENDTEXTUREALPHA } from "./texture-stage";

// The payload's u32 mode (data+0x24): the begin/end entry pair it runs.
export const MULTI_TEXTURE_MODE_FACTOR = 0;
export const MULTI_TEXTURE_MODE_TEXTURE_ALPHA = 1;

// Packed row x: where the extra texture sits.
export const MULTI_TEXTURE_OFF = 0;
export const MULTI_TEXTURE_STAGE1 = 1; // MultiTex: base at stage 0, extra at stage 1
export const MULTI_TEXTURE_REVERSED = 2; // Rev: extra at stage 0, base at stage 1

// Packed row w (low two bits): the extra texture's coordinates.
// vs_1_1 leaves an output it does not write undefined. Inference: the
// hardware the client shipped on starts every output at (0,0,0,1), so a
// stage whose oTn the variant never writes samples the texel at (0,0).
export const EXTRA_UV_ORIGIN = 0;
export const EXTRA_UV_ANIMATED = 1; // the TexAni matrix (c23) of the plain uv
export const EXTRA_UV_SPHERE = 2; // vss2.c SPEC: the environment matrix (c17)
// Packed row w bit 2: the material's own texture samples the moved uv.
export const BASE_UV_MOVING = 4;

/*
================
MultiTexture

One active ModDataMultiTex(Rev). texturePath names the embedded image
(the GLB's extras.sroMultiTextures); factorAlpha is TEXTUREFACTOR's
alpha in [0, 1], the only channel BLENDFACTORALPHA reads.
================
*/
export interface MultiTexture {
	readonly texturePath: string;
	readonly reversed: boolean;
	readonly colorOp: typeof D3DTOP_BLENDFACTORALPHA | typeof D3DTOP_BLENDTEXTUREALPHA;
	readonly factorAlpha: number;
}

/*
================
multiTextureStage

The stage a payload's mode sets, or undefined for a mode the client's
two-entry tables (ctors 0xAEDF00, 0xAEE390) do not hold.
================
*/
export function multiTextureStage(
	texturePath: string,
	reversed: boolean,
	mode: number,
	textureFactor: number
): MultiTexture | undefined {
	if ( !texturePath || !Number.isSafeInteger( textureFactor ) || textureFactor < 0 || textureFactor > 0xffffffff ) {
		return undefined;
	}
	if ( mode !== MULTI_TEXTURE_MODE_FACTOR && mode !== MULTI_TEXTURE_MODE_TEXTURE_ALPHA ) return undefined;
	return {
		texturePath,
		reversed,
		colorOp: mode === MULTI_TEXTURE_MODE_FACTOR ? D3DTOP_BLENDFACTORALPHA : D3DTOP_BLENDTEXTUREALPHA,
		factorAlpha: (textureFactor >>> 24) / 255
	};
}

/*
================
validMultiTexture
================
*/
export function validMultiTexture( m: MultiTexture ): boolean {
	return typeof m.texturePath === "string" && !!m.texturePath && typeof m.reversed === "boolean" &&
		(m.colorOp === D3DTOP_BLENDFACTORALPHA || m.colorOp === D3DTOP_BLENDTEXTUREALPHA) &&
		Number.isFinite( m.factorAlpha ) && m.factorAlpha >= 0 && m.factorAlpha <= 1;
}

/*
================
MultiTextureRoute

What the draw needs from a material: its MultiTex stage, whether a
texture animation is active, and that animation's words24[0] (the stage
it was authored for, 0x8000000 at 0 or 0x400000 at 1).
================
*/
export interface MultiTextureRoute {
	readonly multiTexture?: MultiTexture;
	readonly animated: boolean;
	readonly textureAnimationStage?: 0 | 1;
}

/*
================
packMultiTexture

The geometry shader's multiTex row: [layout, COLOROP, factor alpha, uv
route]. The variant table above decides which texture the moved uv
reaches; every other texture samples the plain uv (or SPEC's sphere, or
the unwritten (0,0)).
================
*/
export function packMultiTexture( route: MultiTextureRoute ): [number, number, number, number] {
	const stage = route.textureAnimationStage ?? 0, m = route.multiTexture;
	if ( !m ) {
		// TEXANI1 with no MultiTex: the moved uv is oT1, and stage 1 stays
		// DISABLE (the D3D default; every MultiTex end entry restores it), so
		// nothing samples it and the material draws its plain uv.
		return [ MULTI_TEXTURE_OFF, 0, 0, route.animated && stage === 1 ? 0 : BASE_UV_MOVING ];
	}
	if ( !m.reversed ) {
		const extra = route.animated && stage === 1 ? EXTRA_UV_ANIMATED : EXTRA_UV_ORIGIN;
		const base = route.animated && stage === 0 ? BASE_UV_MOVING : 0;
		return [ MULTI_TEXTURE_STAGE1, m.colorOp, m.factorAlpha, extra | base ];
	}
	// Rev ORs 0x200000: with TexAni's 0x8000000 the variant is TEXANI1R (the
	// extra texture at stage 0 takes the moved uv); otherwise SPEC, whose
	// stage 0 takes the environment uv and which never reads c23.
	const extra = route.animated && stage === 0 ? EXTRA_UV_ANIMATED : EXTRA_UV_SPHERE;
	return [ MULTI_TEXTURE_REVERSED, m.colorOp, m.factorAlpha, extra ];
}
