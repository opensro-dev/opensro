/*
===========================================================================

texture-stage.test.mjs - D3D9 texture stage 0 as the reference evaluates it

texture-stage.ts is the reference the geometry shader mirrors, so each op,
argument selector and modifier is checked against the fixed-function
definition by hand-computed values.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const stage = await import( "../../src/engine/foundation/rendering/texture-stage.ts" );
const { stageColor, stageArgument, packTextureStage, validTextureStage } = stage;

const inputs = {
	texture: [ 0.5, 0.25, 1, 0.75 ],
	diffuse: [ 0.5, 1, 0.25, 0.5 ],
	specular: [ 0.1, 0.2, 0.3, 0.4 ],
	factor: [ 0, 0, 0, 0.6 ]
};
const T = stage.D3DTA_TEXTURE, D = stage.D3DTA_DIFFUSE;

/*
================
colorOf

Stage 0 with colour op op over TEXTURE, DIFFUSE and alpha SELECTARG1 TEXTURE.
================
*/
function colorOf( op, arg1 = T, arg2 = D ) {
	return stageColor(
		{ colorOp: op, colorArg1: arg1, colorArg2: arg2, alphaOp: 2, alphaArg1: T, alphaArg2: D },
		inputs
	);
}

/*
================
near

Element-wise equality within float rounding.
================
*/
function near( actual, expected ) {
	assert.equal( actual.length, expected.length );
	for ( let i = 0; i < expected.length; i++ ) {
		assert.ok( Math.abs( actual[i] - expected[i] ) < 1e-9, `channel ${i}: ${actual[i]} != ${expected[i]}` );
	}
}

test("argument selectors and their modifiers", () => {
	near( stageArgument( T, inputs ), inputs.texture );
	near( stageArgument( D, inputs ), inputs.diffuse );
	// CURRENT at stage 0 is the diffuse colour; TEMP starts at zero.
	near( stageArgument( stage.D3DTA_CURRENT, inputs ), inputs.diffuse );
	near( stageArgument( stage.D3DTA_TEMP, inputs ), [ 0, 0, 0, 0 ] );
	near( stageArgument( stage.D3DTA_TFACTOR, inputs ), inputs.factor );
	near( stageArgument( stage.D3DTA_SPECULAR, inputs ), inputs.specular );
	near( stageArgument( T | stage.D3DTA_COMPLEMENT, inputs ), [ 0.5, 0.75, 0, 0.25 ] );
	near( stageArgument( T | stage.D3DTA_ALPHAREPLICATE, inputs ), [ 0.75, 0.75, 0.75, 0.75 ] );
	// ALPHAREPLICATE applies before COMPLEMENT.
	near( stageArgument( T | stage.D3DTA_ALPHAREPLICATE | stage.D3DTA_COMPLEMENT, inputs ), [
		0.25,
		0.25,
		0.25,
		0.25
	] );
});

test("every colour op on texture and diffuse, saturated", () => {
	near( colorOf( stage.D3DTOP_SELECTARG1 ), [ 0.5, 0.25, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_SELECTARG2 ), [ 0.5, 1, 0.25, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATE ), [ 0.25, 0.25, 0.25, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATE2X ), [ 0.5, 0.5, 0.5, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATE4X ), [ 1, 1, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_ADD ), [ 1, 1, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_ADDSIGNED ), [ 0.5, 0.75, 0.75, 0.75 ] );
	near( colorOf( stage.D3DTOP_ADDSIGNED2X ), [ 1, 1, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_SUBTRACT ), [ 0, 0, 0.75, 0.75 ] );
	near( colorOf( stage.D3DTOP_ADDSMOOTH ), [ 0.75, 1, 1, 0.75 ] );
	// Blend by diffuse alpha (0.5), texture alpha (0.75), factor alpha (0.6).
	near( colorOf( stage.D3DTOP_BLENDDIFFUSEALPHA ), [ 0.5, 0.625, 0.625, 0.75 ] );
	near( colorOf( stage.D3DTOP_BLENDCURRENTALPHA ), [ 0.5, 0.625, 0.625, 0.75 ] );
	near( colorOf( stage.D3DTOP_BLENDTEXTUREALPHA ), [ 0.5, 0.4375, 0.8125, 0.75 ] );
	near( colorOf( stage.D3DTOP_BLENDFACTORALPHA ), [ 0.5, 0.55, 0.7, 0.75 ] );
	near( colorOf( stage.D3DTOP_BLENDTEXTUREALPHAPM ), [ 0.625, 0.5, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATEALPHA_ADDCOLOR ), [ 0.875, 1, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATECOLOR_ADDALPHA ), [ 1, 1, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATEINVALPHA_ADDCOLOR ), [ 0.625, 0.5, 1, 0.75 ] );
	near( colorOf( stage.D3DTOP_MODULATEINVCOLOR_ADDALPHA ), [ 1, 1, 0.75, 0.75 ] );
	// ARG0 is CURRENT (the diffuse colour) for MULTIPLYADD and LERP.
	near( colorOf( stage.D3DTOP_MULTIPLYADD ), [ 0.75, 1, 0.5, 0.75 ] );
	near( colorOf( stage.D3DTOP_LERP ), [ 0.5, 0.25, 0.4375, 0.75 ] );
});

test("DOTPRODUCT3 is signed, scaled by 4 and replicated into alpha", () => {
	// 4 * ((0 * 0) + (-0.25 * 0.5) + (0.5 * -0.25)) = -1, saturated to 0.
	near( colorOf( stage.D3DTOP_DOTPRODUCT3 ), [ 0, 0, 0, 0 ] );
	const bright = { ...inputs, texture: [ 1, 1, 1, 1 ], diffuse: [ 1, 0.75, 0.5, 1 ] };
	// 4 * (0.25 + 0.125 + 0) = 1.5, saturated to 1, alpha included.
	near(
		stageColor( { colorOp: 24, colorArg1: T, colorArg2: D, alphaOp: 2, alphaArg1: D, alphaArg2: D }, bright ),
		[ 1, 1, 1, 1 ]
	);
});

test("alpha ops evaluate on the alpha channel; disabling passes current alpha", () => {
	const base = { colorOp: 2, colorArg1: T, colorArg2: D, alphaArg1: T, alphaArg2: D };
	near( stageColor( { ...base, alphaOp: 4 }, inputs ), [ 0.5, 0.25, 1, 0.375 ] );
	near( stageColor( { ...base, alphaOp: 5 }, inputs ), [ 0.5, 0.25, 1, 0.75 ] );
	near( stageColor( { ...base, alphaOp: 10 }, inputs ), [ 0.5, 0.25, 1, 0.25 ] );
	near( stageColor( { ...base, alphaOp: 1 }, inputs ), [ 0.5, 0.25, 1, 0.5 ] );
	near( stageColor( { ...base, alphaOp: 14 }, inputs ), [ 0.5, 0.25, 1, 0.65 ] );
});

test("a disabled colour op disables the stage: the diffuse colour passes", () => {
	near(
		stageColor( { colorOp: 1, colorArg1: T, colorArg2: D, alphaOp: 2, alphaArg1: T, alphaArg2: D }, inputs ),
		inputs.diffuse
	);
});

test("validity refuses what stage 0 cannot evaluate", () => {
	const fire = { colorOp: 5, colorArg1: 2, colorArg2: 0, alphaOp: 4, alphaArg1: 2, alphaArg2: 0 };
	assert.equal( validTextureStage( fire ), true );
	for ( let op = 1; op <= 26; op++ ) {
		const needsNext = op === 17 || op === 22 || op === 23;
		assert.equal( validTextureStage( { ...fire, colorOp: op } ), !needsNext, `colour op ${op}` );
		const colourOnly = op >= 18 && op <= 21;
		assert.equal( validTextureStage( { ...fire, alphaOp: op } ), !needsNext && !colourOnly, `alpha op ${op}` );
	}
	for ( const op of [ 0, 27, 1.5, NaN ] ) assert.equal( validTextureStage( { ...fire, colorOp: op } ), false );
	assert.equal( validTextureStage( { ...fire, colorArg1: 0x32 } ), true );
	assert.equal( validTextureStage( { ...fire, colorArg1: 6 } ), false );
	assert.equal( validTextureStage( { ...fire, alphaArg2: 0x40 } ), false );
	assert.equal( validTextureStage( { ...fire, alphaArg2: -1 } ), false );
});

test("packing: op and arg1 * 64 + arg2 per channel, zero for no stage", () => {
	const fire = { colorOp: 5, colorArg1: 2, colorArg2: 0, alphaOp: 4, alphaArg1: 0x22, alphaArg2: 0x13 };
	assert.deepEqual( packTextureStage( fire ), [ 5, 128, 4, 0x22 * 64 + 0x13 ] );
	assert.deepEqual( packTextureStage( undefined ), [ 0, 0, 0, 0 ] );
});
