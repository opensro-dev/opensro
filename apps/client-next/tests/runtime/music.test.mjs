/*
===========================================================================

music.test.mjs - tests for music.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { createMusic } = await import( sourceFileUrl( "src/engine/runtime/audio/music/music.ts" ).href );
const settle = () => new Promise( setImmediate );
function fixture( t, blocked = false ) {
	let id = 0;
	const requests = [], cancelled = [], results = new Map(), elements = [];
	class Media {
		paused = true;
		currentTime = 0;
		attempts = 0;
		allow = !blocked;
		constructor() {
			elements.push( this );
		}
		play() {
			this.attempts++;
			if ( !this.allow ) {
				return Promise.reject( Object.assign( Error( "gesture required" ), { name: "NotAllowedError" } ) );
			}
			this.paused = false;
			return Promise.resolve();
		}
		pause() {
			this.paused = true;
		}
		removeAttribute() {}
		load() {}
	}
	t.mock.method( globalThis.URL, "createObjectURL", () => "blob:music" );
	const revoked = [];
	t.mock.method( globalThis.URL, "revokeObjectURL", url => revoked.push( url ) );
	const prior = Object.getOwnPropertyDescriptor( globalThis, "Audio" );
	Object.defineProperty( globalThis, "Audio", { value: Media, configurable: true } );
	t.after( () => {
		if ( prior ) Object.defineProperty( globalThis, "Audio", prior );
		else delete globalThis.Audio;
	} );
	const assets = {
		available: () => 4,
		request( url ) {
			requests.push( url );
			return ++id;
		},
		cancel: id => cancelled.push( id ),
		take: id => {
			const result = results.get( id );
			results.delete( id );
			return result;
		}
	};
	const music = createMusic( assets, "http://localhost" );
	function load() {
		music.active( false );
		music.step();
		results.set( 1, {
			kind: "bytes",
			buffer: new TextEncoder().encode(
				JSON.stringify( { introBgmPublicPath: "/assets/audio/music/maintheme_cut.mp3" } )
			).buffer
		} );
		music.step();
		results.set( 2, { kind: "bytes", buffer: Uint8Array.of( 0xff, 0xfb, 0x90, 0 ).buffer } );
		music.step();
	}
	return { music, requests, cancelled, elements, revoked, results, load };
}
test("music primes during loading, loops once active, and survives repeated scene frames without rewinding", async t => {
	const f = fixture( t );
	f.load();
	const media = f.elements[0];
	assert.equal( media.paused, false );
	assert.equal( media.volume, 0 );
	assert.equal( media.loop, true );
	await settle();
	assert.equal( f.music.status(), "primed" );
	media.currentTime = 12;
	f.music.active( true );
	await settle();
	assert.equal( media.currentTime, 0 );
	assert.equal( media.volume, Math.pow( 10, -.7 ) );
	media.currentTime = 30;
	for ( let i = 0; i < 100; i++ ) {
		f.music.active( true );
		f.music.step();
	}
	assert.equal( media.attempts, 1 );
	assert.equal( media.currentTime, 30 );
	assert.equal( f.music.status(), "playing" );
	f.music.active( false );
	assert.equal( media.paused, true );
	f.music.dispose();
	assert.deepEqual( f.revoked, [ "blob:music" ] );
});
test("autoplay denial waits for a gesture instead of retrying every frame", async t => {
	const f = fixture( t, true );
	f.load();
	await settle();
	f.music.active( true );
	await settle();
	assert.equal( f.music.status(), "waiting-for-gesture" );
	for ( let i = 0; i < 100; i++ ) f.music.step();
	assert.equal( f.elements[0].attempts, 2 );
	f.elements[0].allow = true;
	f.music.unlock();
	await settle();
	assert.equal( f.music.status(), "playing" );
	f.music.dispose();
});
test("disposal cancels outstanding music demand and prevents further loading", t => {
	const f = fixture( t );
	f.music.active( false );
	f.music.step();
	f.music.dispose();
	f.music.dispose();
	f.music.step();
	assert.deepEqual( f.cancelled, [ 1 ] );
	assert.equal( f.requests.length, 1 );
	assert.equal( f.elements.length, 0 );
});

test("gesture during loading primes silently and world entry stops further priming", async t => {
	const f = fixture( t, true );
	f.load();
	await settle();
	const media = f.elements[0];
	media.allow = true;
	f.music.unlock();
	await settle();
	assert.equal( media.paused, false );
	assert.equal( media.volume, 0 );
	assert.equal( f.music.status(), "primed" );
	for ( let i = 0; i < 20; i++ ) f.music.active( false );
	assert.equal( media.attempts, 2 );
	assert.equal( media.volume, 0 );
	f.music.active( true );
	assert.equal( media.volume, Math.pow( 10, -.7 ) );
	assert.equal( f.elements.length, 1 );
	f.music.active( false );
	f.music.unlock();
	f.music.step();
	assert.equal( media.paused, true );
	assert.equal( media.attempts, 2 );
	assert.equal( f.music.status(), "stopped" );
	f.music.dispose();
});

test("late silent playback settlement cannot restart disposed music", async t => {
	const f = fixture( t );
	f.load();
	await settle();
	const media = f.elements[0];
	media.pause();
	let resolve;
	media.play = () =>
		new Promise( done => {
			resolve = () => {
				media.paused = false;
				done();
			};
		} );
	f.music.unlock();
	f.music.dispose();
	defined( resolve )();
	await settle();
	assert.equal( media.paused, true );
	assert.deepEqual( f.revoked, [ "blob:music" ] );
});

test("music mix changes preserve playback position and silent priming", async t => {
	const f = fixture( t );
	f.music.volume( .4 );
	f.load();
	const media = f.elements[0];
	assert.equal( media.volume, 0 );
	await settle();
	f.music.active( true );
	media.currentTime = 17;
	f.music.volume( 0 );
	assert.equal( media.volume, 0 );
	assert.equal( media.paused, false );
	f.music.volume( .8 );
	assert.equal( media.volume, .8 );
	assert.equal( media.currentTime, 17 );
	assert.equal( media.attempts, 1 );
	f.music.dispose();
});

const { createMusicSelection, musicFade } = await import(
	sourceFileUrl( "src/engine/foundation/audio/music.ts" ).href
);
const town = "/assets/audio/music/jangan_town.mp3", field = "/assets/audio/music/jangan_field.mp3";
function worldTrack( f, path = town, mode = 0 ) {
	f.music.active( false, true );
	f.music.regional( path, mode );
	f.music.step();
	const id = f.requests.length;
	f.results.set( id, { kind: "bytes", buffer: Uint8Array.of( 0xff, 0xfb, 0x90, 0 ).buffer } );
	f.music.step();
	return f.elements.at( -1 );
}

test("all native selector arms, special prefix bypasses and process carol wrap", () => {
	const pick = createMusicSelection();
	assert.deepEqual( pick( 0, town, true, "anything" ), { path: town, loop: true } );
	assert.deepEqual( pick( 0, "", false, "" ), { path: "", loop: true } );
	for ( let i = 0; i < 260; i++ ) {
		assert.equal( pick( 1, town, true, "event_carol_01.mp3" ), null );
		assert.deepEqual( pick( 1, town, false, "" ), {
			path: `/assets/audio/music/event_carol_0${i % 4 + 1}.mp3`,
			loop: false
		} );
	}
	for ( const [mode, name] of [ [ 2, "shiningstar" ], [ 3, "fortress_war" ] ] ) {
		assert.equal( pick( mode, town, true, name + "_variant.mp3" ), null );
		assert.deepEqual( pick( mode, town, false, name ), { path: `/assets/audio/music/${name}.mp3`, loop: false } );
	}
	for ( const mode of [ -1, 4, 255 ] ) assert.equal( pick( mode, town, false, "" ), null );
});

test("region handoff fades once, keeps same tracks, and reads the latest destination after stopping", async t => {
	const f = fixture( t ), media = worldTrack( f );
	await settle();
	media.currentTime = 10;
	for ( let i = 0; i < 20; i++ ) f.music.regional( town, 0 );
	assert.equal( f.elements.length, 1 );
	assert.equal( media.currentTime, 10 );
	media.currentTime = 10.1;
	f.music.regional( field, 0 );
	assert.equal( f.music.status(), "fading" );
	let factor = 1, db = -1400, steps = 0;
	while ( true ) {
		const n = musicFade( factor, db );
		steps++;
		factor = n.factor;
		db = n.db;
		if ( n.stop ) break;
		media.currentTime = 10 + steps * .25;
		f.music.step();
		assert.equal( media.volume, Math.pow( 10, db / 2000 ) );
		f.music.regional( field, 0 );
	}
	media.currentTime = 10 + steps * .25;
	f.music.step();
	assert.equal( media.paused, true );
	assert.equal( f.requests.length, 1, "no stale destination was queued" );
	f.music.regional( town, 0 );
	f.music.step();
	assert.ok( f.requests.at( -1 ).endsWith( "jangan_town.mp3" ) );
	f.music.dispose();
});

test("special EOF waits for timer, carols advance and region music loops", async t => {
	const f = fixture( t ), media = worldTrack( f, town, 1 );
	await settle();
	assert.equal( media.loop, false );
	media.ended = true;
	media.paused = true;
	for ( let i = 0; i < 30; i++ ) f.music.step();
	assert.equal( media.attempts, 1 );
	f.music.regional( town, 1 );
	f.music.step();
	assert.ok( f.requests.at( -1 ).endsWith( "event_carol_02.mp3" ) );
	f.music.dispose();
});

test("pending region load replacement, mute admission, unmute and scene teardown fence old playback", async t => {
	const f = fixture( t );
	f.music.active( false, true );
	f.music.regional( town, 0 );
	f.music.step();
	f.music.regional( field, 0 );
	assert.deepEqual( f.cancelled, [ 1 ] );
	f.music.step();
	f.results.set( 1, { kind: "bytes", buffer: Uint8Array.of( 0xff, 0xfb, 0x90, 0 ).buffer } );
	f.results.set( 2, { kind: "bytes", buffer: Uint8Array.of( 0xff, 0xfb, 0x90, 0 ).buffer } );
	f.music.step();
	await settle();
	assert.equal( f.elements.length, 1 );
	assert.equal( f.music.snapshot().path, field );
	f.music.volume( 0 );
	const count = f.requests.length;
	f.music.regional( town, 0 );
	f.elements[0].currentTime = 1;
	f.music.step();
	f.music.regional( town, 0 );
	f.music.step();
	assert.equal( f.requests.length, count );
	f.music.volume( .5 );
	f.music.regional( town, 0 );
	f.music.step();
	assert.ok( f.requests.at( -1 ).endsWith( "jangan_town.mp3" ) );
	f.music.reset();
	assert.equal( f.music.snapshot().path, null );
	assert.equal( f.music.status(), "stopped" );
	f.music.dispose();
});

test("failed regional asset retries on the next native timer request, not every frame", t => {
	const f = fixture( t );
	f.music.active( false, true );
	f.music.regional( town, 0 );
	f.music.step();
	f.results.set( 1, { kind: "error", error: "unavailable" } );
	f.music.step();
	for ( let i = 0; i < 20; i++ ) f.music.step();
	assert.equal( f.requests.length, 1 );
	assert.equal( f.music.status(), "failed" );
	f.music.regional( town, 0 );
	f.music.step();
	assert.equal( f.requests.length, 2 );
	f.music.dispose();
});

test("slow special loads keep their selected carol until readiness and cancellation permits a different mode", t => {
	const f = fixture( t );
	f.music.active( false, true );
	f.music.regional( town, 1 );
	f.music.step();
	for ( let i = 0; i < 10; i++ ) {
		f.music.regional( town, 1 );
		f.music.step();
	}
	assert.equal( f.requests.length, 1 );
	assert.equal( f.cancelled.length, 0 );
	assert.ok( f.music.snapshot().path.endsWith( "event_carol_01.mp3" ) );
	f.music.regional( town, 2 );
	f.music.step();
	assert.deepEqual( f.cancelled, [ 1 ] );
	assert.ok( f.requests.at( -1 ).endsWith( "shiningstar.mp3" ) );
	f.music.dispose();
});

test("scene reset fades the old stream and admits title only after completion", async t => {
	const f = fixture( t );
	f.load();
	f.music.active( true );
	await settle();
	const titleMedia = f.elements[0];
	f.music.active( false, true );
	assert.equal( f.music.snapshot().fading, true );
	titleMedia.currentTime = 6;
	f.music.step();
	const media = worldTrack( f );
	await settle();
	f.music.sceneReset();
	assert.equal( media.paused, false );
	assert.equal( f.music.snapshot().fading, true );
	f.music.active( true );
	assert.equal( f.elements.length, 2 );
	media.currentTime = 6;
	f.music.step();
	assert.equal( media.paused, true );
	f.music.active( true );
	f.music.step();
	assert.ok( f.requests.at( -1 ).endsWith( "maintheme_cut.mp3" ) );
	f.music.dispose();
});

/*
================
zero endpoint
================
*/
test("every audio slider has a silent zero endpoint and an audible positive range", async () => {
	const { audioAmplitude } = await import( "../../src/engine/foundation/audio/options.ts" );
	assert.equal( audioAmplitude( 0, false ), 0 );
	for ( let level = 1; level <= 100; level++ ) {
		assert.ok( audioAmplitude( level, false ) > 0 );
		assert.equal( audioAmplitude( level, true ), 0 );
	}
	assert.equal( audioAmplitude( 100, false ), 1 );
});

test("muting during a region fade never restores a nonzero music gain", async t => {
	const f = fixture( t ), media = worldTrack( f );
	await settle();
	media.currentTime = 10;
	f.music.regional( field, 0 );
	assert.equal( f.music.status(), "fading" );
	f.music.volume( 0 );
	for ( let i = 1; i <= 6; i++ ) {
		media.currentTime = 10 + i * .25;
		f.music.step();
		assert.equal( media.volume, 0 );
	}
	f.music.dispose();
});
