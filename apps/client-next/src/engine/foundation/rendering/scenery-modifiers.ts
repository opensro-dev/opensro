/*
===========================================================================

scenery-modifiers.ts - a BSR model's material and texture modifiers

The native ModDataMtrl and ModDataTexAni records (loaded by sub_acb7e0 and
sub_ac58b0) override a material while their animation set plays: colour
timelines, blend and stage-0 state, alpha test, UV animation and atlases.
sceneryMaterial applies the ported states; any other state is reported to
the caller instead of being approximated.

===========================================================================
*/
import type { WorldMaterial } from "@/engine/contracts/scene";
import { validTextureStage, type TextureStage } from "@/engine/foundation/rendering/texture-stage";
import { validBlend } from "@/engine/foundation/rendering/blend-state";
export interface Modifier {
	readonly kind: number;
	readonly stateId: number;
	readonly animationSetName: string;
	readonly baseWords: readonly number[];
	readonly baseBytes?: readonly number[];
}
export interface MaterialModifier extends Modifier {
	readonly field24?: number;
	readonly mode?: number;
	readonly flags: number;
	readonly colors: readonly { readonly time: number; readonly value: readonly number[]; }[];
	readonly words50: readonly number[];
	readonly bytes60: readonly number[];
	readonly field70: number;
}
export interface TextureModifier extends Modifier {
	readonly words24: readonly number[];
	readonly matrix38: readonly number[];
}
export interface SceneryModifiers {
	readonly particleModifiers?: unknown;
	readonly materialModifiers: readonly MaterialModifier[];
	readonly textureModifiers: readonly TextureModifier[];
}
// AED240 applies target -1 before the material traversal; indexed overrides are
// applied only to their material. Never merge BSRs solely by their mesh paths.
/*
================
sceneryMaterial

source with the active modifiers of material index applied, in order:
target -1 first, then the indexed ones. warn receives every state that is
not ported.
================
*/
export function sceneryMaterial(
	source: WorldMaterial,
	modifiers: SceneryModifiers | undefined,
	index: number,
	warn: ( message: string ) => void,
	select: ( m: Modifier ) => boolean = m => m.kind === 2 && m.animationSetName.toLowerCase() === "ambient"
): WorldMaterial {
	if ( !modifiers ) return source;
	let material = source;
	const active = ( m: Modifier ) => select( m ) && (m.baseWords[3] === 0xffffffff || m.baseWords[3] === index);
	for ( const m of modifiers.materialModifiers ) {
		if ( !active( m ) ) continue;
		const b = m.bytes60, w = m.words50, color = m.colors[0]?.value;
		const constant = !m.colors.length ||
			m.colors.every( k =>
				k.value.length === 4 && k.value.every( ( v, i ) => Number.isFinite( v ) && v === color?.[i] )
			);
		if ( b.length !== 16 || w.length !== 4 || (!constant && (m.mode === undefined || m.field24 === undefined)) ) {
			warn( "Malformed scenery material state" );
			continue;
		}
		// sub_aed240 (CRTModMtrl_BeginStates) applies the +0x60 block under its
		// four words: +0x50 alpha test (ALPHAREF b8, ALPHAFUNC b9); +0x54 alpha
		// blending (SRCBLEND b0, DESTBLEND b1) and stage 0's COLOROP/ARG1/ARG2
		// and ALPHAOP/ARG1/ARG2 (b2..b7), with +0x5c adding TEXTUREFACTOR from
		// the pulse channel (b10..b15, sub_aecab0); +0x58 depth writes off.
		const blendPair = { source: b[0]!, destination: b[1]! };
		const stage: TextureStage = {
			colorOp: b[2]!,
			colorArg1: b[3]!,
			colorArg2: b[4]!,
			alphaOp: b[5]!,
			alphaArg1: b[6]!,
			alphaArg2: b[7]!
		};
		if (
			w[0] !== 0 && (!Number.isInteger( b[9] ) || b[9]! < 1 || b[9]! > 8) ||
			w[1] !== 0 && (!validBlend( blendPair ) || !validTextureStage( stage ))
		) {
			warn( "Undefined scenery material state" );
			continue;
		}
		const rate = new DataView( Uint8Array.from( b.slice( 12, 16 ) ).buffer ).getFloat32( 0, true );
		material = {
			...material,
			colorTimeline: !constant ?
				{ duration: m.field24!, mode: m.mode as 0 | 1 | 2, flags: m.flags, colors: m.colors } :
				material.colorTimeline,
			...(w[1] ?
				{
					blend: true,
					blendPair,
					textureStage: stage,
					shaderDiffuse: true,
					surfaceAlpha: true,
					...(w[3] ? { textureFactorPulse: { low: b[10]!, high: b[11]!, rate } } : {})
				} :
				{}),
			depthWrite: w[2] ? false : material.depthWrite,
			alphaCompare: w[0] ? b[9] as WorldMaterial["alphaCompare"] : material.alphaCompare,
			alphaCutoff: w[0] ? b[8]! / 255 : material.alphaCutoff,
			// +0x28 flag 4 also sets D3DRS_SPECULARENABLE, which adds the vertex
			// specular (oD1) after stage 0. BSR draws use shader\vss0.c and
			// vss2.c, the only vertex shaders the client loads; neither writes
			// oD1, so the sum is zero and there is nothing to draw.
			// +0x70 == 1: D3DRS_CULLMODE NONE.
			doubleSided: m.field70 === 1 || material.doubleSided,
			ambient: color && (m.flags & 1) ? [ color[0]!, color[1]!, color[2]! ] : material.ambient,
			color: color && (m.flags & 2) ? [ color[0]!, color[1]!, color[2]!, material.color[3] ] : material.color
		};
	}
	for ( const m of modifiers.textureModifiers ) {
		if ( !active( m ) ) continue;
		const v = m.matrix38;
		if ( m.words24[0] === 0 && m.words24[1] === 0 ) {
			const [start, end, flags] = m.baseBytes ?? [],
				fps = m.words24[2],
				rows = m.words24[3],
				columns = m.words24[4];
			if (
				[ start, end, flags, fps, rows, columns ].some( v => v === undefined || !Number.isSafeInteger( v ) ) ||
				!fps || fps > 1000 || !rows || !columns
			) {
				warn( "Invalid scenery texture atlas" );
				continue;
			}
			material = {
				...material,
				uvVelocity: undefined,
				uvAtlas: { start: start!, end: end!, flags: flags!, fps, rows, columns }
			};
			continue;
		}
		if (
			m.words24.length !== 5 || m.words24[0] !== 0 || m.words24[1] !== 1 || v.length !== 16 ||
			!v.every( Number.isFinite )
		) {
			warn( "Unimplemented scenery texture animation" );
			continue;
		}
		material = { ...material, uvAtlas: undefined, uvVelocity: [ v[0]!, v[1]!, v[4]!, v[5]!, v[8]!, v[9]! ] };
	}
	return material;
}
