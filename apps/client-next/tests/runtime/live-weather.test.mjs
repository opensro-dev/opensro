/*
===========================================================================

live-weather.test.mjs - tests for weather.ts, native-texture.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { GENERATED_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
const { createWeather } = await import( "../../src/engine/runtime/renderer/weather/weather.ts" );
const { decodeNativeTexture } = await import( "../../src/engine/foundation/assets/native-texture.ts" );
const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 2000, fov: 1 };
const matrix = Float32Array.from( [ .001, 0, 0, 0, 0, .001, 0, 0, 0, 0, .001, 0, 0, 0, .5, 1 ] );
function fixture() {
	let calls = 0, uploads = 0, releases = 0, updates = 0;
	const meshes = [];
	const weather = createWeather( {
		range: ( a, b ) => {
			calls++;
			return Math.trunc( (a + b) / 2 );
		}
	} );
	const gpu = {
		upload( mesh ) {
			uploads++;
			meshes.push( mesh );
			return { id: uploads };
		},
		release() {
			releases++;
		},
		updatePositions() {
			updates++;
		}
	};
	const images = new Map(
		[ "rain1", "rain2", "snow1", "snow2" ].map( n => [ "/assets/images/Map_extracted/weather/" + n + ".png", {} ] )
	);
	return {
		weather,
		meshes,
		step: (
			t,
			region = 0,
			enabled = true,
			ground = () => null
		) => (weather.update( camera, t, region, enabled, ground ), weather.prepare( gpu, images, camera, matrix )),
		stats: () => ({ calls, uploads, releases, updates }),
		gpu
	};
}
test("weather emits into retained GPU batches, consumes shared RNG, and resets without reseeding", () => {
	const f = fixture();
	f.weather.set( { mode: 2, amount: 10 } );
	f.step( 0 );
	assert.equal( f.weather.paths().length, 4 );
	assert.equal( f.step( 1 ).length, 1 );
	assert.equal( f.weather.stats().particles, 4 );
	assert.equal( f.stats().calls, 14 );
	assert.equal( f.meshes[0].instances.length, 9 * 16 );
	assert.equal( f.meshes[0].world, true );
	f.step( 1.01 );
	assert.equal( f.stats().uploads, 1 );
	assert.equal( f.stats().updates, 2 );
	const count = f.stats().calls;
	f.weather.set( null );
	assert.equal( f.weather.stats().particles, 0 );
	assert.equal( f.weather.paths().length, 0 );
	assert.deepEqual( f.step( 2 ), [] );
	assert.equal( f.stats().calls, count );
	f.weather.dispose( f.gpu );
	assert.equal( f.stats().releases, 1 );
});
test("weather clear drains existing rain and ground contacts become finite-lived ripples", () => {
	const f = fixture();
	f.weather.set( { mode: 2, amount: 10 } );
	f.step( 0 );
	f.step( 1, 0, true, () => 0 );
	assert.equal( f.weather.stats().particles, 4 );
	assert.equal( f.meshes[0].instances.length, 16 );
	f.weather.set( { mode: 1, amount: 0 } );
	f.step( 2.3 );
	assert.equal( f.weather.stats().particles, 0 );
});
test("interior weather gate prevents emission and region rebasing keeps particle coordinates finite", () => {
	const f = fixture();
	f.weather.set( { mode: 3, amount: 10 } );
	f.step( 0 );
	f.step( 1, 0, false );
	assert.equal( f.stats().calls, 0 );
	f.step( 2 );
	assert.equal( f.weather.stats().particles, 1 );
	assert.equal( f.stats().calls, 4 );
	f.step( 2.1, 1 );
	assert.ok( f.meshes.every( m => m.positions.every( Number.isFinite ) ) );
});
test("native texture decoder preserves every BGRA/BC2 mip and rejects malformed resources", async () => {
	for ( let i = 1; i <= 8; i++ ) {
		const bytes = new Uint8Array(
				await readFile( GENERATED_ROOT + "/intermediate/images/Map_extracted/sun/lens" + i + ".texture" )
			),
			texture = decodeNativeTexture( bytes );
		assert.equal( texture.format, i < 5 ? "bgra8unorm" : "bc2-rgba-unorm" );
		assert.equal( texture.levels.length, 1 + Math.log2( texture.width ) );
		assert.deepEqual( Buffer.concat( texture.levels ), Buffer.from( bytes.subarray( 20 ) ) );
		assert.throws( () => decodeNativeTexture( bytes.subarray( 0, -1 ) ) );
		const bad = bytes.slice();
		bad[16] = 1;
		assert.throws( () => decodeNativeTexture( bad ) );
	}
	for ( const size of [ 0, 4, 19 ] ) assert.throws( () => decodeNativeTexture( new Uint8Array( size ) ) );
});

// Native 8CE9C0 spawn origin and CWSnow/CWSnow2 branch; 8D16DD/8D1E9D
// wrap against the player, while 8D1910/8D20D0 gate lateral motion on visibility.
test("camera zoom cannot relocate precipitation or change snow variant probabilities", () => {
	function sample( eye, choice ) {
		let mesh;
		const weather = createWeather( { range: ( a, b ) => a === 0 && b === 4 ? choice : Math.trunc( (a + b) / 2 ) } );
		const images = new Map(
			[ "snow1", "snow2" ].map( n => [ "/assets/images/Map_extracted/weather/" + n + ".png", n ] )
		);
		const gpu = {
			upload( m, i ) {
				mesh = { m, i };
				return {};
			},
			release() {},
			updatePositions() {}
		};
		weather.set( { mode: 3, amount: 10 } );
		for ( const time of [ 0, 1 ] ) {
			(weather.update( { ...camera, eye }, time, 0, true, () => null, [ 10, 20, 30 ] ),
				weather.prepare( gpu, images, { ...camera, eye }, matrix ));
		}
		return mesh;
	}
	const rare = sample( [ 0, 0, 0 ], 0 ), common = sample( [ 0, 0, 0 ], 1 );
	assert.equal( defined( rare ).i, "snow2" );
	assert.equal( defined( common ).i, "snow1" );
	// Zoom along the same view direction changes neither origin nor geometry.
	const parallel = sample( [ 0, 0, -100 ], 0 );
	assert.deepEqual( [ ...defined( rare ).m.positions.slice( 0, 9 ) ], [
		...defined( parallel ).m.positions.slice( 0, 9 )
	] );
	assert.equal( defined( rare ).m.positions[1], 110.5 );
	assert.equal( defined( common ).m.positions[1], 108.5 );
});
test("late texture arrival cannot consume a different shared random stream or freeze snow drift", () => {
	function run( late ) {
		let calls = 0, mesh;
		const weather = createWeather( {
				range: ( a, b ) => {
					calls++;
					return Math.trunc( (a + b) / 2 );
				}
			} ),
			images = new Map(
				[ "snow1", "snow2" ].map( n => [ "/assets/images/Map_extracted/weather/" + n + ".png", {} ] )
			);
		const gpu = {
			upload( m ) {
				mesh = m;
				return {};
			},
			release() {},
			updatePositions() {}
		};
		weather.set( { mode: 3, amount: 80 } );
		for ( let i = 0; i <= 40; i++ ) {
			(weather.update( camera, i / 20, 0, true, () => null ),
				weather.prepare( gpu, late && i < 40 ? new Map() : images, camera, matrix ));
		}
		return { calls, positions: [ ...defined( mesh ).positions ], stats: weather.stats() };
	}
	assert.deepEqual( run( true ), run( false ) );
});

test("Kerberos event rain preserves snow, targets density ten, and owns one ambient loop", () => {
	const sounds = [],
		weather = createWeather( { range: ( a, b ) => Math.trunc( (a + b) / 2 ) }, event => sounds.push( event ) );
	const gpu = {
		upload() {
			return {};
		},
		release() {},
		updatePositions() {}
	};
	weather.set( { mode: 3, amount: 80, eventRain: true } );
	weather.set( { mode: 3, amount: 80, eventRain: true } );
	for ( const time of [ 0, 1 ] ) {
		(weather.update( camera, time, 0, true, () => null ), weather.prepare( gpu, new Map(), camera, matrix ));
	}
	assert.equal( weather.stats().amount, 2 );
	assert.equal( weather.stats().particles, 5 );
	assert.equal( sounds.length, 1 );
	assert.equal( sounds[0].loop, true );
	assert.equal( sounds[0].spatial, false );
	assert.equal( weather.overlay(), null ); // Event rain alone does not enter the ordinary-rain thunder lottery.
	weather.set( null );
	assert.equal( sounds.length, 2 );
	assert.equal( sounds[1].stop, true );
});

test("native app caps elapsed weather simulation at three seconds after a stall", () => {
	function sample( end ) {
		const weather = createWeather( { range: ( a, b ) => Math.trunc( (a + b) / 2 ) } ),
			gpu = {
				upload() {
					return {};
				},
				release() {},
				updatePositions() {}
			};
		weather.set( { mode: 3, amount: 80 } );
		for ( const time of [ 0, end ] ) {
			(weather.update( camera, time, 0, true, () => null ), weather.prepare( gpu, new Map(), camera, matrix ));
		}
		return weather.stats();
	}
	assert.deepEqual( sample( 30 ), sample( 3 ) );
});
