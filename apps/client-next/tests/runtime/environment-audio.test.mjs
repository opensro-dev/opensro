/*
===========================================================================

environment-audio.test.mjs - tests for audio.ts, random.ts, environment.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
const { createAudio } = await import( "../../src/engine/runtime/audio/audio.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { decodeAmbientProfiles, decodeAudioRegions, ambientPeriod, ambientProfileName } = await import(
	"../../src/engine/foundation/audio/environment.ts"
);
const pose = { regionId: 257, x: 1, y: 0, z: 1, angle: 0 }, seed = { day: 0, hour: 12, minute: 0, receivedAtMs: 0 };
const layer = ( min, max, file = "bird" ) => ({ min, max, publicPath: `/assets/audio/sfx/prim/snd/env/${file}.wav` });
function fixture( day = [ layer( 3, 6 ), layer( 2, 7 ) ], night = [ layer( 0, 0 ) ] ) {
	const catalog = { profiles: [ { name: "city", ambience: { day, night } } ] },
		regions = { regions: [ { name: "city", entries: [ { sectorX: 1, sectorY: 1, coverage: "all" } ] } ] };
	const requests = new Map(), cancelled = [];
	let id = 0;
	const random = createPresentationRandom( 7, 1, 128 ),
		audio = createAudio(
			{
				available: () => 4,
				request( path ) {
					requests.set( ++id, path );
					return id;
				},
				take( id ) {
					const path = requests.get( id );
					if ( !path ) return null;
					requests.delete( id );
					const value = path.endsWith( "effectenvsnd.json" ) ? catalog : regions;
					return { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer };
				},
				cancel( id ) {
					cancelled.push( id );
					requests.delete( id );
				}
			},
			"http://localhost",
			random
		);
	const frame = ( seconds, s = seed ) => {
		audio.step( seconds, [ 0, 0, 0 ] );
		audio.world( pose, s, 0 );
	};
	frame( 0 );
	frame( .01 );
	frame( .02 );
	frame( .03 );
	assert.equal( audio.error(), null );
	return { audio, random, frame, requests, cancelled };
}
test("native 2000ms timer initializes without RNG, decrements by two and processes authored order", () => {
	const f = fixture();
	f.frame( 1.999 );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.frame( 2 );
	const initial = f.random.takeTrace();
	assert.equal( initial.length, 1, "min 2 fires on admission tick, min 3 decrements to 1" );
	const reference = createPresentationRandom( 7, 1, 128 );
	reference.range( 2, 7 );
	assert.deepEqual( initial, reference.takeTrace() );
	f.frame( 3.999 );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.frame( 4 );
	reference.range( 3, 6 );
	assert.deepEqual( f.random.takeTrace(), reference.takeTrace() );
	// One callback after a long stall: no replay of all missed two-second ticks.
	f.frame( 100 );
	assert.ok( f.random.takeTrace().length <= 2 );
	f.frame( 100.001 );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.audio.dispose();
});
test("same profile preserves countdowns; day/night switches on the next timer and loops consume no RNG", () => {
	const f = fixture( [ layer( 4, 8 ) ], [ layer( 0, 0 ) ] );
	f.frame( 2 );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.frame( 3 );
	f.frame( 4 );
	assert.equal( f.random.takeTrace().length, 1 );
	f.frame( 4.1, { ...seed, hour: 23 } );
	f.frame( 6, { ...seed, hour: 23 } );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.frame( 8, { ...seed, hour: 23 } );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.audio.reset();
	f.audio.world( null, undefined, 0 );
	f.audio.step( 10, [ 0, 0, 0 ] );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.audio.dispose();
});
test("unpublished or undecoded sounds retain native RNG consumption", () => {
	const missing = { min: 2, max: 5 }, f = fixture( [ missing ] );
	f.frame( 2 );
	assert.equal( f.random.takeTrace().length, 1 );
	assert.equal( f.audio.error(), null );
	f.audio.dispose();
});
test("unmapped region preserves active native layers; world exit clears them without reseeding", () => {
	const f = fixture( [ layer( 2, 3 ) ] );
	f.frame( 2 );
	assert.equal( f.random.takeTrace().length, 1 );
	f.audio.world( { ...pose, regionId: 258 }, { ...seed, hour: 23 }, 0 );
	f.audio.step( 4, [ 0, 0, 0 ] );
	assert.equal( f.random.takeTrace().length, 1 );
	f.audio.world( null, undefined, 0 );
	f.audio.step( 6, [ 0, 0, 0 ] );
	assert.deepEqual( f.random.takeTrace(), [] );
	f.audio.dispose();
});
test("published catalogs admit all profiles without losing missing-resource rows", async () => {
	const raw = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/audio/effectenvsnd.json", "utf8" ) ),
		profiles = decodeAmbientProfiles( raw ),
		regions = decodeAudioRegions(
			JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/audio/regioninfo.json", "utf8" ) )
		);
	assert.equal( profiles.length, raw.profiles.length );
	assert.ok( profiles.flatMap( p => [ ...p.day, ...p.night ] ).some( l => l.path === null ) );
	assert.ok( ambientProfileName( regions, { ...pose, regionId: 0x6b4f, x: 1234, z: 346 } ) );
	assert.throws( () =>
		decodeAmbientProfiles( { profiles: [ { name: "bad", ambience: { day: [ layer( 4, 4 ) ], night: [] } } ] } )
	);
	assert.throws( () =>
		decodeAmbientProfiles( {
			profiles: [ {
				name: "bad",
				ambience: { day: [ { ...layer( 1, 3 ), publicPath: "/assets/audio/../bad" } ], night: [] }
			} ]
		} )
	);
});
test("native audio phase uses closed 04:00..20:00 calendar interval", () => {
	for ( const [hour, period] of [ [ 3, "night" ], [ 4, "day" ], [ 12, "day" ], [ 20, "day" ], [ 21, "night" ] ] ) {
		assert.equal( ambientPeriod( { ...seed, hour }, 0 ), period );
	}
	assert.equal( ambientPeriod( { ...seed, hour: 20 }, 20 ), "night" );
});

test("native region rectangles override ALL, use inclusive endpoints and preserve authored precedence", () => {
	const all = name => ({ name, entries: [ { sectorX: 1, sectorY: 1, coverage: "all" } ] });
	const rect = ( name, x, y, width, height ) => ({
		name,
		entries: [ { sectorX: 1, sectorY: 1, coverage: "rect", rect: { x, y, width, height } } ]
	});
	const regions = decodeAudioRegions( {
		regions: [
			all( "old" ),
			rect( "shore", 100, 200, 300, 400 ),
			all( "field" ),
			rect( "overlap", 250, 350, 500, 600 )
		]
	} );
	for (
		const [x, z, expected] of [
			[ 99, 200, "field" ],
			[ 100, 200, "shore" ],
			[ 300, 400, "shore" ],
			[ 301, 300, "field" ],
			[ 350, 500, "overlap" ],
			[ 501, 500, "field" ]
		]
	) assert.equal( ambientProfileName( regions, { ...pose, x, z } ), expected );
	const thin = decodeAudioRegions( { regions: [ rect( "line", 0, 1600, 1920, 1600 ) ] } );
	assert.equal( ambientProfileName( thin, { ...pose, x: 500, z: 1600 } ), "line" );
	assert.equal( ambientProfileName( thin, { ...pose, x: 500, z: 1601 } ), null );
	assert.equal( ambientProfileName( regions, { ...pose, regionId: 258 } ), null );
});

test("published Jangan boundary follows native RECT before town ALL", async () => {
	const regions = decodeAudioRegions(
		JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/audio/regioninfo.json", "utf8" ) )
	);
	assert.equal( ambientProfileName( regions, { ...pose, regionId: 169 | (97 << 8), x: 1720, z: 1000 } ), "장안필드" );
	assert.equal( ambientProfileName( regions, { ...pose, regionId: 169 | (97 << 8), x: 1721, z: 1000 } ), "장안" );
});
test("frozen native callback recording binds RTTI owner and pre-update RNG countdowns", async () => {
	const trace = JSON.parse( await readFile( "tests/fixtures/native/native-environment-audio-order.json", "utf8" ) );
	const { createHash } = await import( "node:crypto" );
	assert.equal( trace.complete, true );
	assert.equal( trace.error, null );
	assert.equal(
		createHash( "sha256" ).update(
			await readFile( "tests/fixtures/native/native-environment-audio-order.source.py" )
		).digest( "hex" ),
		trace.policy.harnessSha256
	);
	const owner = JSON.parse( await readFile( "tests/fixtures/native/environment-audio-owner.json", "utf8" ) );
	assert.equal( owner.binarySha256, trace.policy.binarySha256 );
	assert.equal( owner.owner.rtti, ".?AVCGEffSoundBody@@" );
	assert.equal( owner.vtableEntry.targetVa, "0x8f7590" );
	const rows = trace.events.filter( e => e.consumer === "CGEffSoundBody.environment-countdown" );
	assert.equal( rows.length, 3 );
	for ( const row of rows ) {
		const same = trace.events.filter( e => e.frame === row.frame ),
			callback = same.find( e => e.kind === "environment-audio-timer" ),
			update = same.find( e => e.kind === "update-dispatch" );
		assert.equal( callback.timerId, 1 );
		assert.equal( callback.owner, row.owner );
		assert.ok( callback.index < row.index && row.index < update.index );
		const [min, max, remaining] = row.countdown;
		assert.ok( min > 0 && remaining <= 2 );
		const rng = createPresentationRandom( row.stateBefore, 1, 1 );
		assert.equal( rng.range( min, max ), min + row.value % (max - min) );
		assert.equal( rng.takeTrace()[0].stateAfter, row.stateAfter );
	}
});
