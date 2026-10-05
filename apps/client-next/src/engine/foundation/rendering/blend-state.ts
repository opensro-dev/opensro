/*
===========================================================================

blend-state.ts - D3D9 SRCBLEND/DESTBLEND as WebGPU blend state

Every resource that blends names its D3DBLEND pair: an effect resource
(CEFEffect_Render B153A0, +0x1A8/+0x1AC) and a BSR material modifier
(CRTModMtrl_BeginStates AED240, its +0x60/+0x61 bytes). The device turns
a pair into the WebGPU blend the fixed-function pipeline would apply
(pipelines.ts blendState), with D3D9's rules:

  - without D3DRS_SEPARATEALPHABLENDENABLE the alpha channel blends with
    the same factors;
  - the back buffer is X8R8G8B8 (the windowed device takes the desktop
    format, sub_a1e190, at the 32-bit mode sub_a1b8f0 forces), so it has no
    alpha: DESTALPHA reads 1 and INVDESTALPHA 0;
  - BOTHSRCALPHA and BOTHINVSRCALPHA, as the source factor, set both;
  - BLENDFACTOR reads D3DRS_BLENDFACTOR, the pass's blend constant.

===========================================================================
*/

export const D3DBLEND_ZERO = 1;
export const D3DBLEND_ONE = 2;
export const D3DBLEND_SRCCOLOR = 3;
export const D3DBLEND_INVSRCCOLOR = 4;
export const D3DBLEND_SRCALPHA = 5;
export const D3DBLEND_INVSRCALPHA = 6;
export const D3DBLEND_DESTALPHA = 7;
export const D3DBLEND_INVDESTALPHA = 8;
export const D3DBLEND_DESTCOLOR = 9;
export const D3DBLEND_INVDESTCOLOR = 10;
export const D3DBLEND_SRCALPHASAT = 11;
export const D3DBLEND_BOTHSRCALPHA = 12;
export const D3DBLEND_BOTHINVSRCALPHA = 13;
export const D3DBLEND_BLENDFACTOR = 14;
export const D3DBLEND_INVBLENDFACTOR = 15;

/*
================
BlendPair

A blending material's factors; a material that blends without naming
them uses SRCALPHA/INVSRCALPHA (the device's DEFAULT_BLEND).
================
*/
export interface BlendPair {
	readonly source: number;
	readonly destination: number;
}

// D3DRS_BLENDFACTOR after a bloom composite: the bloom owner's blend byte
// (192, installed by its constructor 8BB530) in RGB, alpha 255
// (SWorld_CompositeBloom 8AA560). Nothing restores it.
export const BLOOM_BLEND_BYTE = 192;

/*
================
validBlend

D3D9 defines every factor for the source; the destination takes neither
SRCALPHASAT nor the BOTH* forms.
================
*/
export function validBlend( pair: BlendPair ): boolean {
	const defined = ( factor: number ) =>
		Number.isInteger( factor ) && factor >= D3DBLEND_ZERO && factor <= D3DBLEND_INVBLENDFACTOR;
	return defined( pair.source ) && defined( pair.destination ) &&
		(pair.destination < D3DBLEND_SRCALPHASAT || pair.destination > D3DBLEND_BOTHINVSRCALPHA);
}

/*
================
blendAdds

The material adds onto what is behind it (destination factor ONE): the
picking and alpha-readback paths skip it, as they skipped additive
effects.
================
*/
export function blendAdds( material: { readonly blend: boolean; readonly blendPair?: BlendPair; } ): boolean {
	return material.blend && material.blendPair?.destination === D3DBLEND_ONE;
}
