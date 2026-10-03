/*
===========================================================================

blend-state.test.mjs - D3D9 blend pairs and the TEXTUREFACTOR pulse

The blend pair a resource authors becomes the WebGPU blend the
fixed-function pipeline applies (pipelines.ts blendState), and a BSR
modifier's pulse steps TEXTUREFACTOR's alpha as CRTModMtrl_AdvancePulseChannel
(AECAB0) does.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { validBlend, blendAdds } = await import( "../../src/engine/foundation/rendering/blend-state.ts" );
const { blendState, DEFAULT_BLEND } = await import( "../../src/engine/runtime/renderer/device/pipelines.ts" );
const { createTextureFactorPulse } = await import( "../../src/engine/foundation/rendering/texture-factor-pulse.ts" );

/*
================
factors

A blend state's colour factors, after checking alpha blends with the same.
================
*/
function factors( source, destination ) {
	const state = blendState( { source, destination } );
	assert.deepEqual( state.alpha, state.color, "alpha blends with the colour factors" );
	assert.equal( state.color.operation, "add" );
	return [ state.color.srcFactor, state.color.dstFactor ];
}

test("every D3DBLEND factor maps as the X8R8G8B8 back buffer reads it", () => {
	assert.deepEqual( factors( 1, 2 ), [ "zero", "one" ] );
	assert.deepEqual( factors( 3, 4 ), [ "src", "one-minus-src" ] );
	assert.deepEqual( factors( 5, 6 ), [ "src-alpha", "one-minus-src-alpha" ] );
	// No destination alpha: DESTALPHA reads 1, INVDESTALPHA 0.
	assert.deepEqual( factors( 7, 8 ), [ "one", "zero" ] );
	assert.deepEqual( factors( 9, 10 ), [ "dst", "one-minus-dst" ] );
	assert.deepEqual( factors( 11, 2 ), [ "src-alpha-saturated", "one" ] );
	assert.deepEqual( factors( 14, 15 ), [ "constant", "one-minus-constant" ] );
});

test("BOTHSRCALPHA and BOTHINVSRCALPHA as the source set both factors", () => {
	assert.deepEqual( factors( 12, 2 ), [ "src-alpha", "one-minus-src-alpha" ] );
	assert.deepEqual( factors( 13, 2 ), [ "one-minus-src-alpha", "src-alpha" ] );
});

test("validity: every source factor, no SRCALPHASAT or BOTH* destination", () => {
	for ( let factor = 1; factor <= 15; factor++ ) {
		assert.equal( validBlend( { source: factor, destination: 2 } ), true, `source ${factor}` );
		const sourceOnly = factor >= 11 && factor <= 13;
		assert.equal( validBlend( { source: 5, destination: factor } ), !sourceOnly, `destination ${factor}` );
	}
	for ( const factor of [ 0, 16, 2.5, NaN ] ) {
		assert.equal( validBlend( { source: factor, destination: 2 } ), false );
		assert.throws( () => blendState( { source: factor, destination: 2 } ), /Undefined D3D blend pair/ );
	}
	assert.deepEqual( DEFAULT_BLEND, { source: 5, destination: 6 } );
});

test("only a destination of ONE adds onto what is behind", () => {
	assert.equal( blendAdds( { blend: true, blendPair: { source: 5, destination: 2 } } ), true );
	assert.equal( blendAdds( { blend: true, blendPair: { source: 2, destination: 2 } } ), true );
	assert.equal( blendAdds( { blend: true, blendPair: { source: 5, destination: 6 } } ), false );
	assert.equal( blendAdds( { blend: true } ), false );
	assert.equal( blendAdds( { blend: false, blendPair: { source: 5, destination: 2 } } ), false );
});

test("the pulse packs the value before stepping and turns at its bounds", () => {
	// rate 2.5 over 1000 ms steps 25. The value starts at 0 rising; past the
	// high bound (250) it clamps to 240 and falls, below the low bound (15) it
	// clamps to 16 and rises. Each tick packs the value from before its step.
	const pulse = createTextureFactorPulse( { low: 16, high: 240, rate: 2.5 } );
	const packed = [];
	for ( let tick = 0; tick < 21; tick++ ) {
		pulse.stepDelta( 1000 );
		packed.push( Math.round( pulse.factor[3] * 255 ) );
	}
	assert.deepEqual(
		packed,
		[ 0, 25, 50, 75, 100, 125, 150, 175, 200, 225, 240, 215, 190, 165, 140, 115, 90, 65, 40, 16, 41 ]
	);
	// The factor's RGB stays zero: ARGB with only the alpha byte written.
	assert.deepEqual( [ ...pulse.factor.subarray( 0, 3 ) ], [ 0, 0, 0 ] );
});

test("the pulse reports a change only when the packed byte moves", () => {
	const pulse = createTextureFactorPulse( { low: 0, high: 255, rate: 1 } );
	// The first frame publishes no delta; the packed 0 is already the factor.
	assert.equal( pulse.step( 5 ), false );
	// 0.5 s at rate 1 steps 5; the byte packed this tick is still 0.
	assert.equal( pulse.step( 5.5 ), false );
	assert.equal( pulse.step( 6 ), true );
	assert.equal( Math.round( pulse.factor[3] * 255 ), 5 );
	// Steps below one byte truncate to the same packed value.
	const slow = createTextureFactorPulse( { low: 0, high: 255, rate: 0.01 } );
	slow.stepDelta( 1000 );
	assert.equal( slow.stepDelta( 1000 ), false );
	assert.throws( () => pulse.step( NaN ), /Invalid modifier clock/ );
});
