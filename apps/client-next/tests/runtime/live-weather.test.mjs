/*
===========================================================================

live-weather.test.mjs - tests for weather.ts, native-texture.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle. The drops live on the
GPU (device/weather-particles.ts); these tests run them through the CPU
model of that pool (tests/helpers/weather-reference.mjs), which the
browser test pins to the real one.

===========================================================================
*/
import { GENERATED_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createWeatherReference } from "../helpers/weather-reference.mjs";
const { createWeather } = await import( "../../src/engine/runtime/renderer/weather/weather.ts" );
const { decodeNativeTexture } = await import( "../../src/engine/foundation/assets/native-texture.ts" );
const { regionOffsetXZ } = await import( "../../src/engine/foundation/world/world-space.ts" );
const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 2000, fov: 1 };
const matrix = Float32Array.from( [ .001, 0, 0, 0, 0, .001, 0, 0, 0, 0, .001, 0, 0, 0, .5, 1 ] );
const PATHS = [ "rain1", "snow1", "snow2" ].map( n => "/assets/images/Map_extracted/weather/" + n + ".png" );
const midpoint = ( a, b ) => Math.trunc( (a + b) / 2 );

/*
================
fixture

A weather owner over the pool model, with every texture resident and a
counting midpoint RNG (choice answers the snow variant draw).
================
*/
function fixture( { choice, sounds } = {} ) {
	let calls = 0;
	const pool = createWeatherReference();
	const weather = createWeather(
		{
			range: ( a, b ) => {
				calls++;
				return a === 0 && b === 4 && choice !== undefined ? choice : midpoint( a, b );
			}
		},
		sounds && (event => sounds.push( event ))
	);
	const images = new Map( PATHS.map( path => [ path, path ] ) );
	return {
		weather,
		pool,
		images,
		calls: () => calls,
		step: ( t, { region = 0, enabled = true, view = camera, at = undefined, textures = images } = {} ) => {
			weather.update( view, t, region, enabled, at );
			return weather.prepare( pool.commands, textures, view, matrix );
		}
	};
}

test("weather emits into retained GPU regions, consumes shared RNG, and resets without reseeding", () => {
	const f = fixture();
	f.weather.set( { mode: 2, amount: 10 } );
	f.step( 0 );
	assert.equal( f.weather.paths().length, 3 );
	assert.equal( f.step( 1 ).length, 1 );
	assert.equal( f.pool.alive(), 4 );
	assert.equal( f.calls(), 14 );
	const mesh = f.pool.state.uploads[0].mesh;
	assert.equal( mesh.instances.length, 9 * 16 );
	assert.equal( mesh.world, true );
	assert.equal( mesh.weather.region, 0 );
	f.step( 1.01 );
	assert.equal( f.pool.state.uploads.length, 1, "the region draw is retained" );
	const count = f.calls();
	f.weather.set( null );
	assert.deepEqual( f.weather.stats().windows, [ 0, 0, 0 ] );
	assert.equal( f.weather.paths().length, 0 );
	assert.deepEqual( f.step( 2 ), [] );
	assert.equal( f.calls(), count );
	f.weather.dispose( f.pool.commands );
	assert.equal( f.pool.state.releases, 1 );
});

test("weather clear drains existing rain as drops fall past the cull", () => {
	const f = fixture();
	f.weather.set( { mode: 2, amount: 10 } );
	f.step( 0 );
	f.step( 1 );
	assert.equal( f.pool.alive(), 4 );
	f.weather.set( { mode: 1, amount: 0 } );
	// No ground stops a drop (#4108): two capped 3 s steps (the native
	// elapsed cap) carry every drop past the below-eye cull, and mode 1
	// emits no replacements; the next step frees their slots.
	f.step( 4 );
	f.step( 7 );
	assert.equal( f.pool.alive(), 0 );
	f.step( 8 );
	assert.deepEqual( f.weather.stats().windows, [ 0, 0, 0 ] );
	assert.deepEqual( f.step( 9 ), [], "an empty region is not drawn" );
});

test("max rain settles at the steady state the emission, fall law and cull set", () => {
	// #4108 cost pin. Emission at the 255 cap is amount*dt*2 = 8.5 drops a
	// step. A drop spawns 100 above the reference with start y+490 and dies
	// when 4.9*((age+10000)/1000)^2 passes 490+100+500: age 4916 ms, 294
	// steps, so 8.5 * 294 = 2499 live drops. The CPU keeps no drop: its
	// live window bounds them and is never shorter than the pool's count.
	const f = fixture(), view = { ...camera, eye: [ 0, 100, 0 ], target: [ 0, 100, 1 ] };
	f.weather.set( { mode: 2, amount: 255 } );
	for ( let i = 0; i <= 600; i++ ) f.step( i / 60, { view } );
	assert.equal( f.pool.alive(), 2499 );
	const window = f.weather.stats().windows[0];
	assert.ok( window >= 2499 && window < 2499 + 64, `window ${window}` );
	assert.equal( f.pool.state.overwrites, 0 );
	assert.equal( f.weather.stats().overflow, 0 );
});

test("interior weather gate pauses the drops; region rebases move old drops, not newborn ones", () => {
	const f = fixture();
	f.weather.set( { mode: 3, amount: 10 } );
	f.step( 0 );
	f.step( 1, { enabled: false } );
	assert.equal( f.calls(), 0 );
	assert.equal( f.pool.state.steps, 0, "a paused frame steps nothing" );
	f.step( 2 );
	assert.equal( f.pool.alive(), 1 );
	assert.equal( f.calls(), 4 );
	const before = f.pool.drop( 1, 0 );
	// Cross into region 1 while paused: the rebase waits for the next step.
	f.step( 2.05, { region: 1, enabled: false } );
	f.step( 2.1, { region: 1 } );
	const [dx, dz] = regionOffsetXZ( 1, 0 ), moved = f.pool.drop( 1, 0 );
	assert.ok( Number.isFinite( dx ) && Number.isFinite( dz ) );
	// The rebase moved it by the region offset; a wrap may then fold it
	// back toward the reference in steps of 200.
	const folded = ( value, offset ) => Math.abs( (((value - offset) % 200) + 300) % 200 - 100 ) < 1;
	assert.ok( folded( moved.x - before.x, dx ) && folded( moved.z - before.z, dz ), "the old drop missed the rebase" );
	for ( let i = 0; i < 4; i++ ) f.step( 3 + i, { region: 1 } );
	const newborn = f.pool.drop( 1, 1 );
	assert.ok( newborn.flags & 1 );
	assert.ok( Math.abs( newborn.x ) <= 101 && Math.abs( newborn.z - 1 ) <= 101, "a newborn drop was rebased" );
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
		const f = fixture( { choice } ), view = { ...camera, eye };
		f.weather.set( { mode: 3, amount: 10 } );
		for ( const time of [ 0, 1 ] ) f.step( time, { view, at: [ 10, 20, 30 ] } );
		const region = choice === 0 ? 2 : 1;
		return { image: f.pool.state.uploads[0]?.image, drop: f.pool.drop( region, 0 ) };
	}
	const rare = sample( [ 0, 0, 0 ], 0 ), common = sample( [ 0, 0, 0 ], 1 );
	assert.equal( rare.image, PATHS[2] );
	assert.equal( common.image, PATHS[1] );
	// Zoom along the same view direction changes neither origin nor drop.
	assert.deepEqual( sample( [ 0, 0, -100 ], 0 ).drop, rare.drop );
	// Spawned at 20 + 100, fallen one second at 10 (CWSnow2) or 12 (CWSnow).
	assert.equal( rare.drop.y, 110 );
	assert.equal( common.drop.y, 108 );
});

test("rain never lands: only the streak region draws, and drops fall through the old ground line", () => {
	// #4108: native rain computes no ground height (the 8CE9C0 census): no
	// splash batch, every draw the nine-instance shape.
	const f = fixture(), view = { ...camera, eye: [ 0, 100, 0 ], target: [ 0, 100, 1 ] };
	f.weather.set( { mode: 2, amount: 255 } );
	for ( let i = 0; i <= 360; i++ ) f.step( i / 60, { view } );
	const uploads = f.pool.state.uploads;
	assert.ok( uploads.length > 0 );
	assert.ok( uploads.every( upload => upload.image === PATHS[0] ), "a non-rain draw was uploaded" );
	assert.ok( uploads.every( upload => upload.mesh.instances.length === 9 * 16 ) );
	assert.ok( f.pool.alive() > 0 );
});

test("late texture arrival cannot consume a different shared random stream or freeze snow drift", () => {
	function run( late ) {
		const f = fixture();
		f.weather.set( { mode: 3, amount: 80 } );
		for ( let i = 0; i <= 40; i++ ) f.step( i / 20, { textures: late && i < 40 ? new Map() : f.images } );
		return {
			calls: f.calls(),
			drops: [ 1, 2 ].map( region => [ ...f.pool.regions[region].words ] ),
			stats: f.weather.stats()
		};
	}
	assert.deepEqual( run( true ), run( false ) );
});

test("Kerberos event rain preserves snow, targets density ten, and owns one ambient loop", () => {
	const sounds = [], f = fixture( { sounds } );
	f.weather.set( { mode: 3, amount: 80, eventRain: true } );
	f.weather.set( { mode: 3, amount: 80, eventRain: true } );
	for ( const time of [ 0, 1 ] ) f.step( time, { textures: new Map() } );
	assert.equal( f.weather.stats().amount, 2 );
	assert.equal( f.pool.alive(), 5 );
	assert.equal( sounds.length, 1 );
	assert.equal( sounds[0].loop, true );
	assert.equal( sounds[0].spatial, false );
	assert.equal( f.weather.overlay(), null ); // Event rain alone does not enter the ordinary-rain thunder lottery.
	f.weather.set( null );
	assert.equal( sounds.length, 2 );
	assert.equal( sounds[1].stop, true );
});

test("native app caps elapsed weather simulation at three seconds after a stall", () => {
	function sample( end ) {
		const f = fixture();
		f.weather.set( { mode: 3, amount: 80 } );
		for ( const time of [ 0, end ] ) f.step( time, { textures: new Map() } );
		const { windows, spawned, overflow, amount, remainder } = f.weather.stats();
		return { windows, spawned, overflow, amount, remainder, drops: [ ...f.pool.regions[1].words ] };
	}
	assert.deepEqual( sample( 30 ), sample( 3 ) );
});

test("a frame uploads new drops' records only, never per-drop geometry", () => {
	// #4692: the CPU writes 32 bytes a new drop and nothing for a live one.
	const f = fixture(), sizes = [];
	const spawn = f.pool.commands.weatherSpawn;
	f.pool.commands.weatherSpawn = ( region, slot, records ) => {
		sizes.push( records.byteLength );
		spawn( region, slot, records );
	};
	f.weather.set( { mode: 2, amount: 255 } );
	for ( let i = 0; i <= 600; i++ ) f.step( i / 60 );
	assert.equal( sizes.reduce( ( a, b ) => a + b, 0 ), f.pool.state.spawns * 32 );
	assert.ok( f.pool.state.spawns < 601 * 9, "more records than the emission law spawns" );
	assert.equal( f.pool.state.overwrites, 0 );
});

test("heavy snow over a long descent never writes a spawn over a live flake", () => {
	// The eye sinks at 9 units/s, just slower than CWSnow2 falls: flakes
	// live far longer than on level ground. The windows keep every live
	// flake; a full ring drops the spawn (counted), never overwrites.
	const f = fixture();
	f.weather.set( { mode: 3, amount: 255 } );
	for ( let i = 0; i <= 60 * 240; i++ ) {
		const y = -9 * i / 60, view = { ...camera, eye: [ 0, y, 0 ], target: [ 0, y, 1 ] };
		f.step( i / 60, { view, at: [ 0, y, 1 ] } );
	}
	assert.equal( f.pool.state.overwrites, 0 );
	const stats = f.weather.stats();
	for ( const region of [ 1, 2 ] ) assert.ok( stats.windows[region] >= f.pool.alive( region ) );
});

test("snow falls, wraps and wanders alike at 30 and 500 frames a second", () => {
	// The first flake's frame counter ticks at 60 Hz whatever the render
	// rate (its wrap and flip bits with it), and its fall follows dt.
	function run( fps ) {
		const f = fixture();
		f.weather.set( { mode: 3, amount: 10 } );
		for ( let i = 0; i <= fps * 10; i++ ) f.step( i / fps );
		return f.pool.drop( 1, 0 );
	}
	const slow = run( 30 ), fast = run( 500 );
	assert.ok( slow.frame > 300, `frame ${slow.frame}` );
	assert.ok( Math.abs( slow.frame - fast.frame ) <= 2, `${slow.frame} vs ${fast.frame} ticks` );
	assert.ok( Math.abs( slow.y - fast.y ) < .5, `${slow.y} vs ${fast.y}` );
});

test("snow's horizontal wander and wrap do not depend on the frame rate", () => {
	// The midpoint RNG spawns every flake at the reference's x, so a
	// flake's x is its drift alone. The reference then walks away at
	// 30 units/s: every flake wraps back within 100 of it on the 60 Hz
	// ticks. Both rates must give the same spread and the same wrap.
	function run( fps ) {
		const f = fixture();
		f.weather.set( { mode: 3, amount: 255 } );
		const seconds = 20;
		for ( let i = 0; i <= fps * seconds; i++ ) {
			const t = i / fps, walk = t > 10 ? (t - 10) * 30 : 0;
			f.step( t, { at: [ walk, 0, 1 ] } );
		}
		const reference = 300, offsets = [];
		for ( let slot = 0; slot < f.pool.regions[1].words.length / 8; slot++ ) {
			const drop = f.pool.drop( 1, slot );
			if ( drop.flags & 1 ) offsets.push( drop.x - reference );
		}
		const inside = offsets.filter( x => Math.abs( x ) <= 102 ).length / offsets.length;
		const spread = Math.sqrt( offsets.reduce( ( sum, x ) => sum + x * x, 0 ) / offsets.length );
		return { inside, spread, count: offsets.length };
	}
	const slow = run( 30 ), fast = run( 500 );
	assert.ok( slow.count > 1000 && fast.count > 1000 );
	assert.ok( slow.inside > .97 && fast.inside > .97, JSON.stringify( { slow, fast } ) );
	assert.ok( Math.abs( slow.spread - fast.spread ) / slow.spread < .1, JSON.stringify( { slow, fast } ) );
});

test("a refused step forgets the drops instead of letting the CPU clock run ahead", () => {
	const f = fixture();
	f.weather.set( { mode: 2, amount: 255 } );
	for ( let i = 0; i <= 60; i++ ) f.step( i / 60 );
	let refusedSerial = 0;
	const stepCommand = f.pool.commands.weatherStep;
	f.pool.commands.weatherStep = step => {
		refusedSerial = step.serial;
		return stepCommand( step );
	};
	f.pool.state.refuse = true;
	f.step( 61 / 60 );
	f.pool.state.refuse = false;
	f.pool.commands.weatherStep = stepCommand;
	assert.deepEqual( f.weather.stats().windows, [ 0, 0, 0 ] );
	assert.equal( f.weather.stats().refused, 1 );
	// Forgotten drops sit outside every window: no later step moves or
	// draws one (every visible slot holds a drop born after the refusal),
	// and new spawns take their slots.
	const words = f.pool.regions[0].words;
	for ( let i = 62; i <= 600; i++ ) {
		f.step( i / 60 );
		for ( const slot of f.pool.visible( 0 ) ) {
			assert.ok( words[slot * 8 + 7] > refusedSerial, `abandoned drop in slot ${slot} reappeared` );
		}
	}
	assert.ok( f.weather.stats().windows[0] > 0 );
	assert.equal( f.weather.stats().overflow, 0 );
});
