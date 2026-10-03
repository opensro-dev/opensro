/*
===========================================================================

scenery-modifiers.test.mjs - tests for scenery-modifiers.ts,
texture-motion.ts, geometry.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { loadDataAsset } from "../../../../scripts/build/shared/jmxAssetIO.mjs";
import { parseJmxResourceBsr } from "../../../../scripts/build/world/objects/formats.mjs";
import { defined } from "../helpers/defined.mjs";
const { sceneryMaterial } = await import( "../../src/engine/foundation/rendering/scenery-modifiers.ts" );
const { createTextureMotion } = await import( "../../src/engine/foundation/rendering/texture-motion.ts" );
const { copyMaterial } = await import( "../../src/engine/foundation/rendering/geometry.ts" );
const base = {
	color: [ .58, .58, .58, 1 ],
	alphaCutoff: 128 / 255,
	blend: false,
	doubleSided: false,
	objectFade: true
};
test("native waterfall retains alpha, depth policy, authored colors and signed UV velocity", async () => {
	const bytes = await loadDataAsset( "res/nature/particle/cj_waterfall02_01.bsr" );
	const { modifiers } = parseJmxResourceBsr( bytes );
	const warnings = [];
	const material = sceneryMaterial( base, modifiers, 0, m => warnings.push( m ) );
	assert.deepEqual( warnings, [] );
	assert.equal( material.blend, true );
	assert.equal( material.depthWrite, false );
	assert.equal( material.surfaceAlpha, true );
	assert.equal(
		material.alphaCutoff,
		base.alphaCutoff,
		"disabled modifier alpha-test override preserves the source policy"
	);
	assert.equal( material.doubleSided, true );
	assert.deepEqual( material.uvVelocity, [ 0, 0, 0, 0, 0, Math.fround( -1.91 ) ] );
	assert.equal( material.color[0], Math.fround( .58 ) );
	assert.equal( defined( material.ambient )[0], Math.fround( .58 ) );
	assert.equal( base.alphaCutoff, 128 / 255 );
	assert.equal( base.blend, false );
	const owned = copyMaterial( material );
	assert.notEqual( owned.uvVelocity, material.uvVelocity );
	const other = sceneryMaterial(
		base,
		{ materialModifiers: [], textureModifiers: defined( modifiers ).textureModifiers },
		1,
		() => {}
	);
	assert.equal( other.uvVelocity, undefined, "indexed UV modifier must not affect a sibling material" );
});
test("texture motion preserves shear, scrolling, float32 accumulation and stopped clocks", () => {
	const motion = createTextureMotion( [ .25, -.5, .75, 1, -2, 3 ] );
	const matrix = motion.matrix;
	assert.equal( motion.step( 10 ), false );
	assert.equal( motion.step( 10.5 ), true );
	assert.equal( motion.matrix, matrix );
	assert.deepEqual( [ ...matrix ], [ 1.125, .375, -1, 0, -.25, 1.5, 1.5, 0 ] );
	assert.equal( motion.step( 10.5 ), false );
	assert.throws( () => motion.step( NaN ) );
	motion.step( 11 );
	assert.deepEqual( [ ...matrix ], [ 1.25, .75, -2, 0, -.5, 2, 3, 0 ] );
});
test("truncated material and UV records cannot become partial scenery products", async () => {
	const bytes = await loadDataAsset( "res/nature/particle/waterfall-turtle01-1.bsr" );
	const begin = bytes.readUInt32LE( 0x24 ), end = defined( parseJmxResourceBsr( bytes ).modifiers ).next;
	for ( let n = begin; n < end; n++ ) {
		assert.throws( () => parseJmxResourceBsr( bytes.subarray( 0, n ) ), `truncation ${n}` );
	}
});

/*
================
projected

The first material modifier of a BSR projected onto base, with its warnings.
================
*/
async function projected( source, from = base ) {
	const { modifiers } = parseJmxResourceBsr( await loadDataAsset( source ) );
	const modifier = defined( modifiers ).materialModifiers[0], warnings = [];
	const material = sceneryMaterial( from, modifiers, modifier.baseWords[3], m => warnings.push( m ) );
	return { material, warnings };
}

test("native NPC modifiers keep their alpha test, D3D blend pair and stage-0 ops", async () => {
	// CRTModMtrl_BeginStates (AED240): +0x54 installs SRCBLEND/DESTBLEND and the
	// six stage states; +0x50 the alpha function. Untouched depth state stays.
	const blend = { source: 5, destination: 6 };
	const cases = [
		[ "res/npc/npc/khotanshop_designer.bsr", 6, {
			colorOp: 5,
			colorArg1: 0,
			colorArg2: 2,
			alphaOp: 2,
			alphaArg1: 2,
			alphaArg2: 2
		} ],
		[ "res/npc/npc/centralasiashop_warehouse.bsr", 8, {
			colorOp: 5,
			colorArg1: 0,
			colorArg2: 2,
			alphaOp: 2,
			alphaArg1: 2,
			alphaArg2: 2
		} ],
		// Alpha MODULATE of TEXTURE by itself: the texel alpha squared.
		[ "res/npc/npc/centralasiasystem_flyship.bsr", undefined, {
			colorOp: 5,
			colorArg1: 0,
			colorArg2: 2,
			alphaOp: 4,
			alphaArg1: 2,
			alphaArg2: 2
		} ]
	];
	for ( const [source, compare, stage] of cases ) {
		const { material, warnings } = await projected( source, { ...base, depthWrite: false } );
		assert.deepEqual( warnings, [], source );
		assert.equal( material.alphaCompare, compare, source );
		assert.deepEqual( material.blendPair, blend, source );
		assert.deepEqual( material.textureStage, stage, source );
		assert.equal( material.shaderDiffuse, true, source );
		assert.equal( material.depthWrite, false, "zero override must not enable depth writes" );
		assert.doesNotThrow( () => copyMaterial( material ) );
	}
});

test("sword skill trails blend and evaluate stage 0 as their modifiers author", async () => {
	// a and b: SRCALPHA/ONE (additive) with MODULATE4X; c: SRCALPHA/INVSRCALPHA
	// with ADD. Alpha selects TEXTURE in all three.
	const cases = [
		[ "res/etc/sword_skill_a.bsr", { source: 5, destination: 2 }, 6 ],
		[ "res/etc/sword_skill_b.bsr", { source: 5, destination: 2 }, 6 ],
		[ "res/etc/sword_skill_c.bsr", { source: 5, destination: 6 }, 7 ]
	];
	for ( const [source, blendPair, colorOp] of cases ) {
		const { material, warnings } = await projected( source );
		assert.deepEqual( warnings, [], source );
		assert.equal( material.blend, true, source );
		assert.deepEqual( material.blendPair, blendPair, source );
		assert.deepEqual(
			material.textureStage,
			{ colorOp, colorArg1: 0, colorArg2: 2, alphaOp: 3, alphaArg1: 2, alphaArg2: 2 },
			source
		);
		assert.equal( material.textureFactorPulse, undefined, source );
		const owned = copyMaterial( material );
		assert.notEqual( owned.blendPair, material.blendPair );
		assert.deepEqual( owned.textureStage, material.textureStage );
	}
});

/*
================
syntheticModifiers

One ambient material modifier for every material, with the +0x50 words and
+0x60 bytes given.
================
*/
function syntheticModifiers( words, bytes ) {
	return {
		materialModifiers: [ {
			kind: 2,
			animationSetName: "ambient",
			baseWords: [ 0, 0, 0, 0xffffffff ],
			words50: words,
			bytes60: bytes,
			colors: [],
			field70: 0,
			flags: 0
		} ],
		textureModifiers: []
	};
}

// SRCALPHA/INVSRCALPHA, MODULATE TEXTURE DIFFUSE, SELECTARG1 TEXTURE, alpha
// ref 0x80 GREATER, pulse 16..240 at rate 2.5.
const PULSE_RATE = [ ...new Uint8Array( Float32Array.of( 2.5 ).buffer ) ];
const STATE_BYTES = [ 5, 6, 4, 2, 0, 2, 2, 0, 0x80, 5, 16, 240, ...PULSE_RATE ];

test("the +0x5c word adds the TEXTUREFACTOR pulse with its float32 rate", () => {
	const warnings = [];
	const material = sceneryMaterial(
		base,
		syntheticModifiers( [ 0, 1, 0, 1 ], STATE_BYTES ),
		0,
		m => warnings.push( m )
	);
	assert.deepEqual( warnings, [] );
	assert.deepEqual( material.textureFactorPulse, { low: 16, high: 240, rate: 2.5 } );
	assert.equal( material.alphaCompare, base.alphaCompare, "+0x50 off keeps the source alpha test" );
	assert.equal( material.depthWrite, base.depthWrite, "+0x58 off keeps the source depth writes" );
	const owned = copyMaterial( material );
	assert.notEqual( owned.textureFactorPulse, material.textureFactorPulse );
	assert.deepEqual( owned.textureFactorPulse, material.textureFactorPulse );
	// Without +0x54 the pulse word installs nothing.
	const unblended = sceneryMaterial( base, syntheticModifiers( [ 0, 0, 0, 1 ], STATE_BYTES ), 0, () => {} );
	assert.equal( unblended.textureFactorPulse, undefined );
	assert.equal( unblended.blendPair, undefined );
});

test("undefined native states are refused, never approximated", () => {
	const refused = [
		// DESTBLEND BOTHSRCALPHA is source-only.
		[ [ 0, 1, 0, 0 ], [ 5, 12, ...STATE_BYTES.slice( 2 ) ] ],
		// BUMPENVMAP needs a following stage.
		[ [ 0, 1, 0, 0 ], [ 5, 6, 22, ...STATE_BYTES.slice( 3 ) ] ],
		// MODULATEALPHA_ADDCOLOR is colour-only.
		[ [ 0, 1, 0, 0 ], [ 5, 6, 4, 2, 0, 18, ...STATE_BYTES.slice( 6 ) ] ],
		// ALPHAFUNC 9 is not a D3DCMPFUNC.
		[ [ 1, 0, 0, 0 ], [ ...STATE_BYTES.slice( 0, 9 ), 9, ...STATE_BYTES.slice( 10 ) ] ]
	];
	for ( const [words, bytes] of refused ) {
		const warnings = [];
		const material = sceneryMaterial( base, syntheticModifiers( words, bytes ), 0, m => warnings.push( m ) );
		assert.deepEqual( warnings, [ "Undefined scenery material state" ], `${words} ${bytes}` );
		assert.equal( material, base );
	}
	const warnings = [];
	sceneryMaterial( base, syntheticModifiers( [ 0, 1, 0 ], STATE_BYTES ), 0, m => warnings.push( m ) );
	assert.deepEqual( warnings, [ "Malformed scenery material state" ] );
});
