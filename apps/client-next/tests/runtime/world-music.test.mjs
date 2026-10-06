/*
===========================================================================

world-music.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { defined } from "../helpers/defined.mjs";
async function load( path ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { createGameplay } = await load( "runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const { createAudio } = await load( "runtime/audio/audio.ts" );
const { musicSampleRate } = await load( "foundation/audio/music.ts" );
const { decodeAmbientProfiles } = await load( "foundation/audio/environment.ts" );
const { fortressMusicActive, fortressMusicMode } = await load( "foundation/gameplay/fortress.ts" );
const u32 = n => [ n & 255, (n >>> 8) & 255, (n >>> 16) & 255, n >>> 24 ];
function list( flags ) {
	return Uint8Array.from( [ 0, 1, ...u32( 1 ), 1, 0, 70, ...Array( 16 ).fill( 0 ), 0, 0, flags, ...u32( 1 ) ] );
}
test("production fortress packets latch music before end flags and after start flags; entry and reset close the lifecycle", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {
		localPlayerEntry: { fortressWorld: 2 },
		gameWorldData: [ { gameWorldId: 2, warName: "F" } ],
		siegeFortressData: [ { fortressId: 1, codeName: "F" } ]
	} );
	g.receive( { opcode: 0x3887, payload: list( 1 ) }, 0 );
	g.enterMusic();
	assert.equal( g.take().musicMode, 3 );
	g.receive( { opcode: 0x3887, payload: Uint8Array.of( 6 ) }, 1 );
	let state = g.take();
	assert.equal( state.musicMode, 0 );
	assert.equal( state.fortress.wars[0].flags, 0 );
	// Repeated off XORs the flag on, but must not manufacture a mode-3 write.
	g.receive( { opcode: 0x3887, payload: Uint8Array.of( 6 ) }, 2 );
	assert.equal( g.take().musicMode, 0 );
	g.receive( { opcode: 0x3887, payload: Uint8Array.of( 2 ) }, 3 );
	assert.equal( g.take().musicMode, 3 );
	g.receive( { opcode: 0x3887, payload: Uint8Array.of( 0x31 ) }, 4 );
	assert.equal( g.take().musicMode, 3 );
	g.reset();
	g.bootstrap( {} );
	g.enterMusic();
	assert.equal( g.take().musicMode, 0 );
	g.dispose();
});
test("real profiles retain all city/field tracks and MP3 clocks are readable without resampling", async () => {
	const profiles = decodeAmbientProfiles(
		JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/audio/effectenvsnd.json", "utf8" ) )
	);
	assert.equal( profiles.length, 27 );
	assert.ok( profiles.every( p => p.music ) );
	const paths = new Set( [
		...profiles.map( p => p.music ),
		...[ "event_carol_01", "event_carol_02", "event_carol_03", "event_carol_04", "shiningstar", "fortress_war" ]
			.map( n => "/assets/audio/music/" + n + ".mp3" )
	] );
	for ( const path of paths ) {
		const bytes = await readFile( CLIENT_PUBLIC_ROOT + path );
		assert.equal( musicSampleRate( bytes ), 44100, path );
	}
	assert.equal( musicSampleRate( Uint8Array.of( 255, 251, 148, 0 ) ), 48000 );
	assert.throws( () => musicSampleRate( new Uint8Array( 4 ) ) );
});

test("fortress music rejects zero and unresolved identifiers, and only consumes bit zero", () => {
	const state = {
		worldId: 0x10002,
		worlds: [ { id: 2, code: "F" } ],
		fortresses: [ { id: 1, code: "F" } ],
		wars: [ { id: 1, flags: 1 } ]
	};
	assert.equal( fortressMusicActive( state ), true );
	for (
		const other of [ { ...state, worldId: 3 }, { ...state, fortresses: [] }, { ...state, wars: [] }, {
			...state,
			wars: [ { id: 1, flags: 6 } ]
		}, { ...state, fortresses: [ { id: 0, code: "F" } ], wars: [ { id: 0, flags: 1 } ] } ]
	) {
		assert.equal( fortressMusicActive( other ), false );
		assert.equal( fortressMusicMode( 2, other, other, 2 ), 2 );
		assert.equal( fortressMusicMode( 3, other, other, 6 ), 3 );
	}
});
test("production audio timer selects region music, preserves null/missing lookup and stops at scene exit", async t => {
	const prior = Object.getOwnPropertyDescriptor( globalThis, "Audio" );
	let media;
	class Media {
		paused = true;
		ended = false;
		currentTime = 0;
		constructor() {
			media = this;
		}
		play() {
			this.paused = false;
			return Promise.resolve();
		}
		pause() {
			this.paused = true;
		}
		removeAttribute() {}
		load() {}
	}
	Object.defineProperty( globalThis, "Audio", { value: Media, configurable: true } );
	t.after( () => {
		if ( prior ) Object.defineProperty( globalThis, "Audio", prior );
		else delete globalThis.Audio;
	} );
	t.mock.method( URL, "createObjectURL", () => "blob:world-music" );
	t.mock.method( URL, "revokeObjectURL", () => {} );
	const requests = [], pending = new Map();
	let seq = 0;
	const catalog = {
			profiles: [ {
				name: "city",
				bgmTrack: "Jangan_Town.ogg",
				bgmPublicPath: "/assets/audio/music/jangan_town.mp3",
				ambience: { day: [], night: [] }
			} ]
		},
		regions = {
			regions: [ { name: "city", entries: [ { sectorX: 1, sectorY: 1, coverage: "all" } ] }, {
				name: "absent",
				entries: [ { sectorX: 2, sectorY: 1, coverage: "all" } ]
			} ]
		};
	const assets = {
		available: () => 4,
		request( path ) {
			requests.push( path );
			pending.set( ++seq, path );
			return seq;
		},
		cancel( id ) {
			pending.delete( id );
		},
		take( id ) {
			const path = pending.get( id );
			if ( !path ) return null;
			pending.delete( id );
			const buffer = path.endsWith( ".mp3" ) ?
				Uint8Array.of( 255, 251, 144, 0 ).buffer :
				new TextEncoder().encode( JSON.stringify( path.endsWith( "effectenvsnd.json" ) ? catalog : regions ) )
					.buffer;
			return { kind: "bytes", buffer };
		}
	};
	const audio = createAudio( assets, "http://fixture.invalid", {
		range() {
			throw Error( "music must not consume presentation RNG" );
		}
	} );
	const pose = { regionId: 257, x: 1, y: 0, z: 1, angle: 0 };
	audio.music( false, true );
	const frame = ( seconds, p = pose, mode = 0 ) => {
		audio.world( p, undefined, 0, mode );
		audio.step( seconds, [ 0, 0, 0 ] );
	};
	for ( const at of [ 0, .01, .02, .03, 1.99 ] ) frame( at );
	assert.ok( !requests.some( p => p.endsWith( ".mp3" ) ) );
	frame( 2 );
	frame( 2.01 );
	await Promise.resolve();
	assert.equal( audio.musicStatus(), "playing" );
	assert.ok( audio.musicSnapshot().path.endsWith( "jangan_town.mp3" ) );
	const count = requests.length;
	frame( 4, { ...pose, regionId: 258 } );
	frame( 6, { ...pose, regionId: 259 } );
	assert.equal( requests.length, count );
	assert.equal( defined( media ).paused, false );
	frame( 8, pose, 3 );
	assert.equal( audio.musicStatus(), "fading" );
	defined( media ).currentTime = 5;
	frame( 9, pose, 3 );
	frame( 10, pose, 3 );
	frame( 10.01, pose, 3 );
	assert.ok( audio.musicSnapshot().path.endsWith( "fortress_war.mp3" ) );
	assert.equal( defined( media ).loop, false );
	audio.world( null, undefined, 0 );
	audio.music( true, false );
	assert.equal( audio.musicSnapshot().fading, true );
	defined( media ).currentTime = 6;
	audio.step( 16, [ 0, 0, 0 ] );
	assert.equal( defined( media ).paused, true );
	audio.dispose();
});
