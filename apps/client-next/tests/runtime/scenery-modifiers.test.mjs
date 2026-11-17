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
import { parseCharacterBsr } from "../../../../scripts/build/char/formats.mjs";
import { materialBsrModifiers } from "../../../../scripts/build/shared/bsrModifiers.mjs";
import { defined } from "../helpers/defined.mjs";
const { sceneryMaterial, unboundSceneryMaterial } = await import(
	"../../src/engine/foundation/rendering/scenery-modifiers.ts"
);
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

const {
	packMultiTexture,
	BASE_UV_MOVING,
	EXTRA_UV_ANIMATED,
	EXTRA_UV_ORIGIN,
	EXTRA_UV_SPHERE,
	MULTI_TEXTURE_OFF,
	MULTI_TEXTURE_REVERSED,
	MULTI_TEXTURE_STAGE1
} = await import( "../../src/engine/foundation/rendering/multi-texture.ts" );
const route = m =>
	packMultiTexture( {
		multiTexture: m.multiTexture,
		animated: !!(m.uvVelocity || m.uvAtlas),
		textureAnimationStage: m.textureAnimationStage
	} );

test("a Rev multi-texture takes stage 0 and the moved uv, the ghost's own texture stage 1", async () => {
	const { modifiers } = parseJmxResourceBsr( await loadDataAsset( "res/mob/oasis/redeyeghost.bsr" ) );
	const warnings = [];
	// Material 1 carries the ghost's [0,1] texture animation: TEXANI1R.
	const animated = sceneryMaterial( base, modifiers, 1, m => warnings.push( m ) );
	assert.deepEqual( animated.multiTexture, {
		texturePath: "prim\\mtrl\\etc\\redeyeghost_lava.ddj",
		reversed: true,
		colorOp: 13,
		factorAlpha: 124 / 255
	} );
	assert.equal( animated.textureAnimationStage, 0 );
	assert.deepEqual( route( animated ), [ MULTI_TEXTURE_REVERSED, 13, 124 / 255, EXTRA_UV_ANIMATED ] );
	// Materials 0 and 2 have the Rev stage and no animation: SPEC's sphere uv.
	const still = sceneryMaterial( base, modifiers, 0, m => warnings.push( m ) );
	assert.equal( still.uvVelocity, undefined );
	assert.deepEqual( route( still ), [ MULTI_TEXTURE_REVERSED, 13, 124 / 255, EXTRA_UV_SPHERE ] );
	assert.deepEqual( warnings, [] );
});

test("a multi-texture under a stage-0 animation leaves the moved uv on the material's texture", async () => {
	const { modifiers } = parseJmxResourceBsr( await loadDataAsset( "res/mob/oasis/redeyeghost_champ.bsr" ) );
	const warnings = [];
	const material = sceneryMaterial( base, modifiers, 1, m => warnings.push( m ) );
	assert.deepEqual( warnings, [] );
	assert.equal( material.multiTexture?.reversed, false );
	// TEXANI0 writes oT0 only: stage 1 samples the unwritten (0,0).
	assert.deepEqual( route( material ), [ MULTI_TEXTURE_STAGE1, 13, 99 / 255, EXTRA_UV_ORIGIN | BASE_UV_MOVING ] );
	// The champion's other materials have neither record.
	assert.deepEqual( route( sceneryMaterial( base, modifiers, 0, m => warnings.push( m ) ) ), [
		MULTI_TEXTURE_OFF,
		0,
		0,
		BASE_UV_MOVING
	] );
});

test("a stage-1 animation moves the multi-texture, or nothing when no stage 1 exists", () => {
	const meta = { kind: 2, stateId: -1, animationSetName: "ambient", baseWords: [ 0, 2, 272, 0xffffffff, 0, 0 ] };
	// flame_captain: words24 [1,1], matrix row 2 scrolling u at -0.32 per second.
	const animation = {
		...meta,
		words24: [ 1, 1, 1, 1, 1 ],
		matrix38: Array.from( { length: 16 }, ( _, i ) => i === 8 ? -.32 : 0 )
	};
	const stage1 = {
		...meta,
		reversed: false,
		mode: 0,
		texturePath: "prim\\mtrl\\etc\\robe_line.ddj",
		textureFactor: 0x7c000000
	};
	const warnings = [], warn = m => warnings.push( m );
	const drawn = sceneryMaterial(
		base,
		{ materialModifiers: [], textureModifiers: [ animation ], multiTextureModifiers: [ stage1 ] },
		0,
		warn
	);
	assert.deepEqual( route( drawn ), [ MULTI_TEXTURE_STAGE1, 14, 124 / 255, EXTRA_UV_ANIMATED ] );
	assert.deepEqual( drawn.uvVelocity, [ 0, 0, 0, 0, -.32, 0 ] );
	// No MultiTex: stage 1 stays DISABLE, so the material samples its plain uv.
	const alone = sceneryMaterial( base, { materialModifiers: [], textureModifiers: [ animation ] }, 0, warn );
	assert.deepEqual( route( alone ), [ MULTI_TEXTURE_OFF, 0, 0, 0 ] );
	// Rev with a stage-1 animation is SPEC: 0x200000 outranks 0x400000.
	const reversed = sceneryMaterial(
		base,
		{
			materialModifiers: [],
			textureModifiers: [ animation ],
			multiTextureModifiers: [ { ...stage1, reversed: true } ]
		},
		0,
		warn
	);
	assert.equal( route( reversed )[3], EXTRA_UV_SPHERE );
	assert.deepEqual( warnings, [] );
});

test("a stacked or unknown multi-texture stage is reported, never approximated", () => {
	const meta = { kind: 2, stateId: -1, animationSetName: "ambient", baseWords: [ 0, 2, 272, 0xffffffff, 0, 0 ] };
	const stage = {
		...meta,
		reversed: true,
		mode: 1,
		texturePath: "prim\\mtrl\\etc\\robe_line_03.ddj",
		textureFactor: 0
	};
	const warnings = [];
	const material = sceneryMaterial(
		base,
		{ materialModifiers: [], textureModifiers: [], multiTextureModifiers: [ stage, { ...stage, mode: 2 }, stage ] },
		0,
		m => warnings.push( m )
	);
	assert.deepEqual( warnings, [ "Invalid scenery multi-texture", "Unimplemented stacked scenery multi-texture" ] );
	assert.equal( material.multiTexture?.texturePath, stage.texturePath );
	assert.deepEqual( copyMaterial( material ).multiTexture, material.multiTexture );
});

test("a scene that cannot bind the MultiTex texture reports the stage and draws the rest", async () => {
	const { modifiers } = parseJmxResourceBsr( await loadDataAsset( "res/mob/oasis/redeyeghost.bsr" ) );
	const warnings = [], warn = m => warnings.push( m );
	const drawn = unboundSceneryMaterial( base, modifiers, 1, warn );
	assert.equal( drawn.multiTexture, undefined );
	assert.equal( drawn.textureAnimationStage, 0 );
	assert.deepEqual( warnings, [ "Unimplemented scenery multi-texture" ] );
	assert.deepEqual( unboundSceneryMaterial( base, undefined, 0, warn ), sceneryMaterial( base, undefined, 0, warn ) );
});

test("the character reader keeps a Rev multi-texture payload for the GLB", async () => {
	const bsr = parseCharacterBsr( await loadDataAsset( "res/mob/oasis/redeyeghost.bsr" ) );
	assert.deepEqual(
		bsr.multiTextureModifiers.map( m => [ m.reversed, m.mode, m.texturePath, m.textureFactor ] ),
		[ [ true, 1, "prim\\mtrl\\etc\\redeyeghost_lava.ddj", 0x7c000000 ] ]
	);
	assert.deepEqual( materialBsrModifiers( bsr ).multiTextureModifiers, bsr.multiTextureModifiers );
});
