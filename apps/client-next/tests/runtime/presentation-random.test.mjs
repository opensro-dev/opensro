/*
===========================================================================

presentation-random.test.mjs - tests for random.ts, star-construction.ts,
crt-random.ts, projectile-curve.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { buildNativeSkyStarPrimitive } = await import( "../../src/engine/foundation/rendering/star-construction.ts" );
const { crtRandomRange } = await import( "../../src/engine/foundation/math/crt-random.ts" );
const { createProjectileCurve } = await import( "../../src/engine/foundation/animation/projectile-curve.ts" );
const { launchOrb } = await import( "../../src/engine/foundation/animation/orb-mover.ts" );
const { initialStarFlicker, advanceStarFlicker } = await import(
	"../../src/engine/foundation/rendering/star-flicker.ts"
);

test("WinMain reseeding follows static star construction; subsequent consumers share one stream", () => {
	for ( const seed of [ 0, 1, 0x80000000, 0xffffffff ] ) {
		const owner = createPresentationRandom( seed ), sky = buildNativeSkyStarPrimitive( 1 );
		assert.deepEqual( owner.sky().vertices, sky.vertices );
		let state = seed;
		const a = [ 10, 20, 30 ], b = [ 90, 40, 20 ];
		const sound = crtRandomRange( state, 0, 2 );
		assert.equal( owner.range( 0, 2 ), sound.value );
		state = sound.state;
		const curve = createProjectileCurve( a, b, state );
		assert.deepEqual( owner.curve( a, b ), curve.curve );
		state = curve.state;
		const unchanged = owner.range( 100, 100 );
		assert.equal( unchanged, 100 );
		const orb = launchOrb( a, b, state );
		assert.deepEqual( owner.orb( a, b ), orb.mover );
		state = orb.random;
		const previous = initialStarFlicker( 999, 0 );
		const flicker = advanceStarFlicker( { ...previous, random: state }, 100, true );
		assert.deepEqual( owner.flicker( previous, 100, true ), flicker );
		const next = crtRandomRange( flicker.random, 0, 32768 );
		assert.equal( owner.range( 0, 32768 ), next.value );
	}
});
test("replay construction is immutable and reading the field cannot reseed later consumers", () => {
	const a = createPresentationRandom( 1 ), b = createPresentationRandom( 1 ), field = a.sky();
	assert.equal( field, a.sky() );
	assert.throws( () => {
		field.vertices[0].x = 0;
	} );
	for ( let i = 0; i < 20; i++ ) {
		assert.equal( a.range( 0, 32768 ), b.range( 0, 32768 ) );
		a.sky();
	}
	assert.deepEqual( field.vertices, createPresentationRandom( 2 ).sky().vertices );
	assert.notDeepEqual( field.vertices, createPresentationRandom( 1, 2 ).sky().vertices );
	assert.equal(
		createPresentationRandom( 1, 2 ).range( 0, 32768 ),
		createPresentationRandom( 1, 3 ).range( 0, 32768 )
	);
	for ( const invalid of [ -1, 2 ** 32, NaN, 1.5 ] ) assert.throws( () => createPresentationRandom( invalid ) );
});
test("a span wider than int32 wraps like the native helper, returning lower and no draw", () => {
	// 878a10 forms upper - lower in a wrapping 32-bit register and returns
	// lower, without a draw, when that span reads non-positive. Authored
	// spin endpoints are full int32 fields, so a pair can span more than
	// int32 (issue 2576): the port threw "Invalid CRT random range" and
	// ended the session at world entry instead.
	const wide = crtRandomRange( 1, -2000000000, 2000000000 );
	assert.equal( wide.value, -2000000000 );
	assert.equal( wide.state, 1 );
	const full = crtRandomRange( 0xffffffff, -0x80000000, 0x7fffffff );
	assert.equal( full.value, -0x80000000 );
	assert.equal( full.state, 0xffffffff );
	const edge = crtRandomRange( 7, -1, 0x7fffffff );
	assert.equal( edge.value, -1 );
	assert.equal( edge.state, 7 );
	// The wrapped call leaves the shared stream exactly where it was.
	const a = createPresentationRandom( 9 ), first = a.range( 0, 32768 );
	const b = createPresentationRandom( 9 );
	b.range( -2000000000, 2000000000 );
	assert.equal( b.range( 0, 32768 ), first );
});

test("inputs no native caller can produce still throw", () => {
	for (
		const [state, lower, upper] of [
			[ 1, 0.5, 2 ],
			[ 1, 0, NaN ],
			[ 1, -(2 ** 31) - 1, 0 ],
			[ 1, 0, 2 ** 31 ],
			[ -1, 0, 2 ],
			[ 2 ** 32, 0, 2 ],
			[ NaN, 0, 2 ]
		]
	) assert.throws( () => crtRandomRange( state, lower, upper ), /Invalid CRT random range/ );
});

import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
test("all 3000 startup stars and final RNG state match the original suspended-process capture", async () => {
	const receipt = JSON.parse( await readFile( "tests/fixtures/native/native-startup-rng-verified.json", "utf8" ) );
	const original = await readFile( "tests/fixtures/native/native-startup-rng-verified-stars.bin" );
	assert.equal( receipt.pass, true );
	assert.equal( createHash( "sha256" ).update( original ).digest( "hex" ), receipt.stars.sha256 );
	const field = buildNativeSkyStarPrimitive( 1 ), bytes = Buffer.alloc( field.vertices.length * 16 );
	field.vertices.forEach( ( v, i ) => {
		bytes.writeFloatLE( v.x, i * 16 );
		bytes.writeFloatLE( v.y, i * 16 + 4 );
		bytes.writeFloatLE( v.z, i * 16 + 8 );
		bytes.writeUInt32LE( v.colorArgb, i * 16 + 12 );
	} );
	assert.deepEqual( bytes, original );
	assert.equal( field.nativeRand.stateAfterConstruction, receipt.output );
	assert.equal( receipt.input, 1 );
	assert.deepEqual( receipt.intensities, Array( 10 ).fill( 0 ) );
});

test("optional RNG observation preserves behavior and captures zero-consumption operations", () => {
	const traced = createPresentationRandom( 7, 1, 32 ), ordinary = createPresentationRandom( 7 );
	assert.equal( traced.range( 0, 32768 ), ordinary.range( 0, 32768 ) );
	assert.equal( traced.range( 5, 5 ), ordinary.range( 5, 5 ) );
	assert.deepEqual( traced.curve( [ 0, 0, 0 ], [ 10, 20, 30 ] ), ordinary.curve( [ 0, 0, 0 ], [ 10, 20, 30 ] ) );
	assert.deepEqual(
		traced.flicker( initialStarFlicker(), 100, true ),
		ordinary.flicker( initialStarFlicker(), 100, true )
	);
	const events = traced.takeTrace();
	assert.equal( traced.takeTrace().length, 0 );
	assert.deepEqual( events.map( e => e.operation ), [ "range", "range", "curve", "flicker" ] );
	assert.equal( events[1].stateBefore, events[1].stateAfter );
	for ( let i = 1; i < events.length; i++ ) assert.equal( events[i].stateBefore, events[i - 1].stateAfter );
	events[3].stateAfter = 0;
	assert.equal( traced.range( 0, 32768 ), ordinary.range( 0, 32768 ) );
});
