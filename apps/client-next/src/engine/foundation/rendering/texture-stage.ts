/*
===========================================================================

texture-stage.ts - D3D9 texture stage 0: every colour and alpha op

The native renderer draws with one texture stage, configured from the
resource: CEFEffect_Render (B153A0) sets COLOROP/ARG1/ARG2 and
ALPHAOP/ARG1/ARG2 from an effect resource (+0x1B0..+0x1C4), and a BSR
material modifier sets the same six states from its +0x62..+0x67 bytes
(sub_aed240). This module is the reference the geometry shader mirrors
(pipelines.ts): stageColor evaluates stage 0 exactly as the fixed-function
pipeline defines each op, on rgba values in [0, 1], saturating the result.

Stage 0 has no earlier stage, so CURRENT is the diffuse colour and TEMP is
zero (D3D9 initialises it to 0). Three ops need a following stage that
these single-stage draws never have (PREMODULATE, BUMPENVMAP,
BUMPENVMAPLUMINANCE): a stage naming one is a data defect and is refused,
as are the colour-only ops when given as an alpha op.

===========================================================================
*/

// D3DTA argument selectors and modifier bits.
export const D3DTA_DIFFUSE = 0;
export const D3DTA_CURRENT = 1;
export const D3DTA_TEXTURE = 2;
export const D3DTA_TFACTOR = 3;
export const D3DTA_SPECULAR = 4;
export const D3DTA_TEMP = 5;
export const D3DTA_SELECTMASK = 0x0f;
export const D3DTA_COMPLEMENT = 0x10;
export const D3DTA_ALPHAREPLICATE = 0x20;

// D3DTEXTUREOP.
export const D3DTOP_DISABLE = 1;
export const D3DTOP_SELECTARG1 = 2;
export const D3DTOP_SELECTARG2 = 3;
export const D3DTOP_MODULATE = 4;
export const D3DTOP_MODULATE2X = 5;
export const D3DTOP_MODULATE4X = 6;
export const D3DTOP_ADD = 7;
export const D3DTOP_ADDSIGNED = 8;
export const D3DTOP_ADDSIGNED2X = 9;
export const D3DTOP_SUBTRACT = 10;
export const D3DTOP_ADDSMOOTH = 11;
export const D3DTOP_BLENDDIFFUSEALPHA = 12;
export const D3DTOP_BLENDTEXTUREALPHA = 13;
export const D3DTOP_BLENDFACTORALPHA = 14;
export const D3DTOP_BLENDTEXTUREALPHAPM = 15;
export const D3DTOP_BLENDCURRENTALPHA = 16;
export const D3DTOP_PREMODULATE = 17;
export const D3DTOP_MODULATEALPHA_ADDCOLOR = 18;
export const D3DTOP_MODULATECOLOR_ADDALPHA = 19;
export const D3DTOP_MODULATEINVALPHA_ADDCOLOR = 20;
export const D3DTOP_MODULATEINVCOLOR_ADDALPHA = 21;
export const D3DTOP_BUMPENVMAP = 22;
export const D3DTOP_BUMPENVMAPLUMINANCE = 23;
export const D3DTOP_DOTPRODUCT3 = 24;
export const D3DTOP_MULTIPLYADD = 25;
export const D3DTOP_LERP = 26;

/*
================
TextureStage

Stage 0's six states as the resource authors them. COLORARG0/ALPHAARG0
(MULTIPLYADD, LERP) are never set by the native callers and keep their
D3D9 default, CURRENT.
================
*/
export interface TextureStage {
	readonly colorOp: number;
	readonly colorArg1: number;
	readonly colorArg2: number;
	readonly alphaOp: number;
	readonly alphaArg1: number;
	readonly alphaArg2: number;
}

/*
================
StageInputs

The values stage 0's arguments select: the texel, the diffuse and
specular vertex colours, and TEXTUREFACTOR, each rgba in [0, 1].
================
*/
export interface StageInputs {
	readonly texture: readonly number[];
	readonly diffuse: readonly number[];
	readonly specular: readonly number[];
	readonly factor: readonly number[];
}

/*
================
opDefined

An op stage 0 can evaluate on its own; the colour-only ops are refused
as an alpha op.
================
*/
function opDefined( op: number, alpha: boolean ): boolean {
	if ( !Number.isInteger( op ) || op < D3DTOP_DISABLE || op > D3DTOP_LERP ) return false;
	if ( op === D3DTOP_PREMODULATE || op === D3DTOP_BUMPENVMAP || op === D3DTOP_BUMPENVMAPLUMINANCE ) return false;
	return !alpha || op < D3DTOP_MODULATEALPHA_ADDCOLOR || op > D3DTOP_MODULATEINVCOLOR_ADDALPHA;
}

/*
================
argumentDefined
================
*/
function argumentDefined( arg: number ): boolean {
	return Number.isInteger( arg ) && arg >= 0 &&
		(arg & ~(D3DTA_SELECTMASK | D3DTA_COMPLEMENT | D3DTA_ALPHAREPLICATE)) === 0 &&
		(arg & D3DTA_SELECTMASK) <= D3DTA_TEMP;
}

/*
================
validTextureStage

Every op stage 0 defines and every argument, with its modifiers. Anything
else is a data defect, never approximated.
================
*/
export function validTextureStage( stage: TextureStage ): boolean {
	return opDefined( stage.colorOp, false ) && opDefined( stage.alphaOp, true ) &&
		argumentDefined( stage.colorArg1 ) && argumentDefined( stage.colorArg2 ) &&
		argumentDefined( stage.alphaArg1 ) && argumentDefined( stage.alphaArg2 );
}

/*
================
packTextureStage

The material uniform's stage vector: [colour op, colour args, alpha op,
alpha args], each args value arg1 * 64 + arg2. All zero means "no native
stage": the material keeps the ordinary modulated path.
================
*/
export function packTextureStage( stage: TextureStage | undefined ): [number, number, number, number] {
	if ( !stage ) return [ 0, 0, 0, 0 ];
	return [
		stage.colorOp,
		stage.colorArg1 * 64 + stage.colorArg2,
		stage.alphaOp,
		stage.alphaArg1 * 64 + stage.alphaArg2
	];
}

/*
================
stageArgument

The rgba an argument selects, with ALPHAREPLICATE (x.aaaa) and then
COMPLEMENT (1 - x) applied. CURRENT is the diffuse colour at stage 0, and
TEMP holds its initial zero.
================
*/
export function stageArgument( arg: number, inputs: StageInputs ): number[] {
	const select = arg & D3DTA_SELECTMASK;
	let value = select === D3DTA_TEXTURE ?
		[ ...inputs.texture ] :
		select === D3DTA_TFACTOR ?
		[ ...inputs.factor ] :
		select === D3DTA_SPECULAR ?
		[ ...inputs.specular ] :
		select === D3DTA_TEMP ?
		[ 0, 0, 0, 0 ] :
		[ ...inputs.diffuse ];
	if ( arg & D3DTA_ALPHAREPLICATE ) value = [ value[3]!, value[3]!, value[3]!, value[3]! ];
	if ( arg & D3DTA_COMPLEMENT ) value = value.map( v => 1 - v );
	return value;
}

/*
================
evaluate

One op on channel c of arguments a and b (a0 is ARG0, CURRENT), before
saturation. DOTPRODUCT3 and the colour-only ops read across channels.
================
*/
function evaluate(
	op: number,
	a: readonly number[],
	b: readonly number[],
	a0: readonly number[],
	c: number,
	inputs: StageInputs
): number {
	switch ( op ) {
		case D3DTOP_SELECTARG1:
			return a[c]!;
		case D3DTOP_SELECTARG2:
			return b[c]!;
		case D3DTOP_MODULATE:
			return a[c]! * b[c]!;
		case D3DTOP_MODULATE2X:
			return a[c]! * b[c]! * 2;
		case D3DTOP_MODULATE4X:
			return a[c]! * b[c]! * 4;
		case D3DTOP_ADD:
			return a[c]! + b[c]!;
		case D3DTOP_ADDSIGNED:
			return a[c]! + b[c]! - .5;
		case D3DTOP_ADDSIGNED2X:
			return (a[c]! + b[c]! - .5) * 2;
		case D3DTOP_SUBTRACT:
			return a[c]! - b[c]!;
		case D3DTOP_ADDSMOOTH:
			return a[c]! + b[c]! - a[c]! * b[c]!;
		case D3DTOP_BLENDDIFFUSEALPHA:
		case D3DTOP_BLENDCURRENTALPHA:
			return a[c]! * inputs.diffuse[3]! + b[c]! * (1 - inputs.diffuse[3]!);
		case D3DTOP_BLENDTEXTUREALPHA:
			return a[c]! * inputs.texture[3]! + b[c]! * (1 - inputs.texture[3]!);
		case D3DTOP_BLENDFACTORALPHA:
			return a[c]! * inputs.factor[3]! + b[c]! * (1 - inputs.factor[3]!);
		case D3DTOP_BLENDTEXTUREALPHAPM:
			return a[c]! + b[c]! * (1 - inputs.texture[3]!);
		case D3DTOP_MODULATEALPHA_ADDCOLOR:
			return a[c]! + a[3]! * b[c]!;
		case D3DTOP_MODULATECOLOR_ADDALPHA:
			return a[c]! * b[c]! + a[3]!;
		case D3DTOP_MODULATEINVALPHA_ADDCOLOR:
			return (1 - a[3]!) * b[c]! + a[c]!;
		case D3DTOP_MODULATEINVCOLOR_ADDALPHA:
			return (1 - a[c]!) * b[c]! + a[3]!;
		case D3DTOP_DOTPRODUCT3:
			return 4 * ((a[0]! - .5) * (b[0]! - .5) + (a[1]! - .5) * (b[1]! - .5) + (a[2]! - .5) * (b[2]! - .5));
		case D3DTOP_MULTIPLYADD:
			return a0[c]! + a[c]! * b[c]!;
		case D3DTOP_LERP:
			return a0[c]! * a[c]! + (1 - a0[c]!) * b[c]!;
		default:
			throw Error( "Undefined stage-0 texture op" );
	}
}

/*
================
stageColor

Stage 0's rgba output. A disabled colour op disables the stage: the
output is the diffuse colour, alpha included. A disabled alpha op passes
CURRENT's alpha. DOTPRODUCT3 as the colour op replicates into alpha.
================
*/
export function stageColor( stage: TextureStage, inputs: StageInputs ): number[] {
	if ( stage.colorOp === D3DTOP_DISABLE ) return [ ...inputs.diffuse ];
	const saturate = ( v: number ) => Math.min( 1, Math.max( 0, v ) );
	const current = stageArgument( D3DTA_CURRENT, inputs );
	const a = stageArgument( stage.colorArg1, inputs ), b = stageArgument( stage.colorArg2, inputs );
	const rgb = [ 0, 1, 2 ].map( c => saturate( evaluate( stage.colorOp, a, b, current, c, inputs ) ) );
	if ( stage.colorOp === D3DTOP_DOTPRODUCT3 ) return [ rgb[0]!, rgb[1]!, rgb[2]!, rgb[0]! ];
	if ( stage.alphaOp === D3DTOP_DISABLE ) return [ rgb[0]!, rgb[1]!, rgb[2]!, current[3]! ];
	const aa = stageArgument( stage.alphaArg1, inputs ), ab = stageArgument( stage.alphaArg2, inputs );
	return [ rgb[0]!, rgb[1]!, rgb[2]!, saturate( evaluate( stage.alphaOp, aa, ab, current, 3, inputs ) ) ];
}
