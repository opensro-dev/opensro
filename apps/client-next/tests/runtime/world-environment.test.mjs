/*
===========================================================================

world-environment.test.mjs - tests for world-environment.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { sampleEnvironment, worldEnvironment, environmentTarget, advanceEnvironment } = await import(
	sourceFileUrl( path.join( root, "src/engine/foundation/rendering/world-environment.ts" ) ).href
);

test("terrain shadow floor passes through native TFACTOR byte packing before blending", () => {
	const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], fov: 1, near: 1, far: 3500 };
	const environment = { tracks: { color0x1c0: [ { t: 0, r: .1, g: .2, b: .3 } ] } };
	const data = worldEnvironment( environment, camera, 1, 0 );
	assert.deepEqual( [ ...data.slice( 40, 43 ) ], [ 25, 51, 76 ].map( v => Math.fround( v / 255 ) ) );
});
test("sky rays follow the same left-handed camera roll as world geometry", () => {
	const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], up: [ 1, 0, 0 ], fov: Math.PI / 2, near: 1, far: 100 };
	const env = worldEnvironment( undefined, camera, 2, 0 );
	const close = ( actual, expected ) => assert.ok( actual.every( ( v, i ) => Math.abs( v - expected[i] ) < 1e-6 ) );
	close( [ ...env.slice( 16, 19 ) ], [ 0, 0, 1 ] );
	close( [ ...env.slice( 20, 23 ) ], [ 0, -2, 0 ] );
	close( [ ...env.slice( 24, 27 ) ], [ 1, 0, 0 ] );
});
test("sun direction rides the sky arc and flips to the anti-solar point at night", () => {
	const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], fov: 1, near: 1, far: 3500 };
	const at = timeOfDay => worldEnvironment( { startTimeOfDay: timeOfDay, ratePerSecond: 0 }, camera, 1, 0 );
	const direction = timeOfDay => [ ...at( timeOfDay ).slice( 84, 88 ) ];
	const close = ( timeOfDay, expected ) =>
		assert.ok(
			direction( timeOfDay ).every( ( v, i ) => Math.abs( v - expected[i] ) < 1e-6 ),
			`${timeOfDay}: ${JSON.stringify( direction( timeOfDay ) )}`
		);
	// The block grew by the sun-direction vec4 the shader's stage reads.
	assert.equal( at( 0.5 ).length, 88 );
	// The same X-Y arc the sun quad rides: mid-morning rakes from the east,
	// noon is overhead, mid-afternoon rakes from the west.
	const mid = Math.SQRT1_2;
	close( 0.375, [ mid, mid, 0, 0 ] );
	close( 0.5, [ 0, 1, 0, 0 ] );
	close( 0.625, [ -mid, mid, 0, 0 ] );
	// Below the horizon the light arrives from the opposite point: midnight
	// reads zenith.
	close( 0, [ 0, 1, 0, 0 ] );
	// At the crossing the light meets the zenith from both sides, so a wall
	// facing either way keeps continuous diffuse through dawn and dusk.
	for ( const crossing of [ 0.25, 0.75 ] ) {
		const before = direction( crossing - 1e-6 ), after = direction( crossing + 1e-6 );
		for ( const wall of [ [ 1, 0 ], [ -1, 0 ] ] ) {
			const lit = d => Math.max( 0, d[0] * wall[0] + d[1] * wall[1] );
			assert.ok( Math.abs( lit( before ) - lit( after ) ) < 1e-3, `wall ${wall} pops at ${crossing}` );
		}
		close( crossing, [ 0, 1, 0, 0 ] );
	}
	// Every packed direction is unit length.
	for ( let t = 0; t < 1; t += 1 / 64 ) assert.ok( Math.abs( Math.hypot( ...direction( t ) ) - 1 ) < 1e-6 );
});

test("native environment interpolation clamps endpoints and steps tiny spans", () => {
	const keys = [ { t: 0.2, r: 0 }, { t: 0.6, r: 1 } ];
	assert.equal( sampleEnvironment( keys, 0, "r", 9 ), 0 );
	assert.equal( sampleEnvironment( keys, 1, "r", 9 ), 1 );
	assert.ok( Math.abs( sampleEnvironment( keys, 0.4, "r", 9 ) - 0.5 ) < 1e-8 );
	assert.equal( sampleEnvironment( [ { t: 0, r: 0 }, { t: 0.00001, r: 1 } ], 0.000005, "r", 9 ), 0 );
});
test("native clock wraps, fog scales by 2500, and water advances at 100ms", () => {
	const camera = { eye: [ 0, 1, 1 ], target: [ 0, 0, 0 ], fov: Math.PI / 3, near: 1, far: 3500 };
	const env = {
		startTimeOfDay: 0.5,
		ratePerSecond: 0.5,
		tracks: {
			zenith: [ { t: 0, r: 0, g: 0, b: 0 }, { t: 1, r: 1, g: 1, b: 1 } ],
			scalar0x2e8: [ { t: 0, value: 0.6 } ],
			scalar0x314: [ { t: 0, value: 1 } ]
		}
	};
	const a = worldEnvironment( env, camera, 1, 0 ), b = worldEnvironment( env, camera, 1, 1 );
	assert.equal( a[0], 0.5 );
	assert.equal( b[0], 0 );
	assert.equal( a[31], 1500 );
	assert.equal( a[32], 2500 );
	assert.equal( a[33], 1 );
	assert.equal( b[34], 10 );
	assert.ok( a.every( Number.isFinite ) );
});

test("native event palette overrides daylight and restores authored tracks when cleared", () => {
	const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], fov: 1, near: 1, far: 3500 };
	const normal = worldEnvironment( undefined, camera, 1, 0 );
	const event = worldEnvironment( undefined, camera, 1, 0, null, { mode: 3, amount: 80, eventRain: true } );
	assert.deepEqual( [ ...event.slice( 0, 3 ) ], [ 46, 75, 156 ].map( v => Math.fround( v / 255 ) ) );
	assert.deepEqual( [ ...event.slice( 4, 7 ) ], [ 105, 76, 138 ].map( v => Math.fround( v / 255 ) ) );
	assert.equal( event[31], -500 );
	assert.equal( event[32], 2500 );
	assert.equal( event[50], 1 );
	assert.equal( event[51], -1 );
	assert.deepEqual(
		worldEnvironment( undefined, camera, 1, 0, null, { mode: 1, amount: 0, eventRain: false } ),
		normal
	);
});

test("environment initializes immediately and smooths native channels before packing", () => {
	const clear = environmentTarget( undefined, 0 ),
		event = environmentTarget( undefined, 0, { mode: 1, amount: 0, eventRain: true } );
	assert.deepEqual( advanceEnvironment( null, event, 0 ), event );
	assert.deepEqual( advanceEnvironment( clear, event, 0 ), clear );
	const half = advanceEnvironment( clear, event, 1 );
	assert.equal( half[32], -250 );
	assert.equal( half[34], .5 );
	assert.equal( advanceEnvironment( clear, event, 3 )[32], -500 );
	assert.deepEqual( advanceEnvironment( clear, event, 0, true ), event );
	assert.equal( advanceEnvironment( half, clear, 1 )[32], -125 );
	assert.throws( () => advanceEnvironment( clear, event, NaN ) );
});
test("ordinary weather desaturates prescribed channels without darkening ambient", () => {
	const env = {
		tracks: {
			zenith: [ { t: 0, r: .3, g: .6, b: .9 } ],
			color0x2b4: [ { t: 0, r: .2, g: .4, b: .6 } ],
			scalar0x2e8: [ { t: 0, value: .4 } ]
		}
	};
	const clear = environmentTarget( env, 0 ), rain = environmentTarget( env, 0, { mode: 2, amount: 50 } );
	assert.deepEqual( [ ...rain.slice( 3, 6 ) ], Array( 3 ).fill( Math.fround( .6 ) ) );
	assert.deepEqual( rain.slice( 12, 15 ), rain.slice( 3, 6 ) );
	assert.deepEqual( rain.slice( 18, 21 ), rain.slice( 27, 30 ) );
	assert.equal( rain[32], 500 );
	assert.equal( rain[34], 0 );
	assert.equal( rain[35], -1 );
	assert.deepEqual( rain.slice( 9, 12 ), clear.slice( 9, 12 ) );
});

test("scenery sight range scales native fog tracks and event weather before smoothing", () => {
	const env = { tracks: { scalar0x2e8: [ { t: 0, value: .2 } ], scalar0x314: [ { t: 0, value: .9 } ] } };
	for ( const distance of [ 1500, 2500, 3500, 4500, 5500 ] ) {
		const range = Math.min( 2500, Math.fround( distance * Math.fround( .8 ) ) ),
			target = environmentTarget( env, 0, null, distance );
		assert.equal( target[32], Math.fround( .2 * range ) );
		assert.equal( target[33], Math.fround( .9 * range ) );
		const rain = environmentTarget( env, 0, { eventRain: true }, distance );
		assert.equal( rain[33], range );
		assert.equal( rain[32], Math.fround( Math.fround( -.2 ) * range ) );
	}
});
