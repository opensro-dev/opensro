/*
===========================================================================

texture-stage.ts - the native stage-0 colour and alpha ops of an effect

CEFEffect_Render (B153A0) sets D3D texture stage 0 from the effect resource
before every draw: COLORARG1/COLORARG2/COLOROP from +0x1B0/+0x1B4/+0x1B8 and
ALPHAARG1/ALPHAARG2/ALPHAOP from +0x1BC/+0x1C0/+0x1C4. The resource files
author MODULATE, MODULATE2X/4X and SELECTARG1/2 over TEXTURE and DIFFUSE, and
the alpha op decides how much an additive (SRCALPHA/ONE) effect shows.

stageResult is the reference the geometry shader mirrors (pipelines.ts):
one op of two arguments, saturated as the fixed-function pipeline does.

===========================================================================
*/

// D3DTA argument selectors used by the effect resources.
export const D3DTA_DIFFUSE = 0;
export const D3DTA_CURRENT = 1;
export const D3DTA_TEXTURE = 2;

// D3DTEXTUREOP values used by the effect resources. DISABLE appears only as
// an alpha op; inference: with stage 0's colour still enabled the stage
// passes CURRENT through, which at stage 0 is the diffuse alpha.
export const D3DTOP_DISABLE = 1;
export const D3DTOP_SELECTARG1 = 2;
export const D3DTOP_SELECTARG2 = 3;
export const D3DTOP_MODULATE = 4;
export const D3DTOP_MODULATE2X = 5;
export const D3DTOP_MODULATE4X = 6;

/*
================
colorOpPorted / alphaOpPorted / argumentPorted
================
*/
function colorOpPorted( op: number ): boolean {
	return Number.isInteger( op ) && op >= D3DTOP_SELECTARG1 && op <= D3DTOP_MODULATE4X;
}

function alphaOpPorted( op: number ): boolean {
	return op === D3DTOP_DISABLE || colorOpPorted( op );
}

function argumentPorted( arg: number ): boolean {
	return arg === D3DTA_DIFFUSE || arg === D3DTA_CURRENT || arg === D3DTA_TEXTURE;
}

/*
================
TextureStage
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
validTextureStage

Only the ops and arguments the effect resources author are ported; any
other value is a data defect, never silently approximated.
================
*/
export function validTextureStage( stage: TextureStage ): boolean {
	return colorOpPorted( stage.colorOp ) && alphaOpPorted( stage.alphaOp ) &&
		argumentPorted( stage.colorArg1 ) && argumentPorted( stage.colorArg2 ) &&
		argumentPorted( stage.alphaArg1 ) && argumentPorted( stage.alphaArg2 );
}

/*
================
packTextureStage

The material uniform's stage vector: [colour op, colour args, alpha op,
alpha args], each args value arg1 * 16 + arg2. All zero means "no native
stage": the material keeps the ordinary modulated path.
================
*/
export function packTextureStage( stage: TextureStage | undefined ): [number, number, number, number] {
	if ( !stage ) return [ 0, 0, 0, 0 ];
	return [
		stage.colorOp,
		stage.colorArg1 * 16 + stage.colorArg2,
		stage.alphaOp,
		stage.alphaArg1 * 16 + stage.alphaArg2
	];
}

/*
================
stageArgument

Stage 0 CURRENT is the diffuse colour (no earlier stage).
================
*/
function stageArgument( arg: number, texture: number, diffuse: number ): number {
	return arg === D3DTA_TEXTURE ? texture : diffuse;
}

/*
================
stageResult

One channel of one stage op, saturated to [0, 1].
================
*/
export function stageResult( op: number, arg1: number, arg2: number, texture: number, diffuse: number ): number {
	const a = stageArgument( arg1, texture, diffuse ), b = stageArgument( arg2, texture, diffuse );
	let value: number;
	switch ( op ) {
		case D3DTOP_DISABLE:
			value = diffuse;
			break;
		case D3DTOP_SELECTARG1:
			value = a;
			break;
		case D3DTOP_SELECTARG2:
			value = b;
			break;
		case D3DTOP_MODULATE2X:
			value = a * b * 2;
			break;
		case D3DTOP_MODULATE4X:
			value = a * b * 4;
			break;
		default:
			value = a * b;
	}
	return Math.min( 1, Math.max( 0, value ) );
}
