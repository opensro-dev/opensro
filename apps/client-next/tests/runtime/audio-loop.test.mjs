/*
===========================================================================

audio-loop.test.mjs - tests for loop.ts, audio.ts, random.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";
const { audioLoopEnd } = await import( "../../src/engine/foundation/audio/loop.ts" );
const { createAudio } = await import( "../../src/engine/runtime/audio/audio.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

test("loop end never exceeds the PCM buffer and loses less than a billionth of a frame at retail length", () => {
	assert.ok( (443526 / 48000) * 48000 > 443526, "incident requires upward rounding" );
	assert.ok( audioLoopEnd( 443526, 48000 ) * 48000 <= 443526 );
	assert.ok( 443526 - audioLoopEnd( 443526, 48000 ) * 48000 < 1e-9 );
	fc.assert(
		fc.property(
			fc.integer( { min: 1, max: 0x7fffffff } ),
			fc.integer( { min: 8000, max: 192000 } ),
			( frames, rate ) => {
				const end = audioLoopEnd( frames, rate ), duration = frames / rate;
				assert.ok( end > 0 && end <= duration );
				assert.ok( end * rate <= frames );
				assert.ok( frames - end * rate <= frames * Number.EPSILON * 3 );
				if ( duration * rate <= frames ) assert.equal( end, duration );
			}
		),
		{ seed: 375, numRuns: 10000 }
	);
});

test("audio owner bounds ambient and spatial loops, leaves one-shots alone, and retains stop/reset lifecycle", async t => {
	const sources = [], decoded = { length: 443526, sampleRate: 48000, numberOfChannels: 1 }, requests = new Map();
	let id = 0;
	const node = () => ({ connect() {}, disconnect() {} });
	class Context {
		state = "running";
		destination = {};
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		resume() {
			return Promise.resolve();
		}
		close() {
			return Promise.resolve();
		}
		decodeAudioData() {
			return Promise.resolve( decoded );
		}
		createBufferSource() {
			const source = {
				...node(),
				loopEnd: 0,
				start() {
					this.started = true;
				},
				stop() {
					this.stopped = true;
					this.onended?.();
				}
			};
			sources.push( source );
			return source;
		}
		createGain() {
			return { ...node(), gain: {} };
		}
		createPanner() {
			return { ...node(), positionX: {}, positionY: {}, positionZ: {} };
		}
	}
	const originalContext = globalThis.AudioContext;
	globalThis.AudioContext = Context;
	t.after( () => {
		if ( originalContext ) globalThis.AudioContext = originalContext;
		else delete globalThis.AudioContext;
	} );
	const assets = {
		available: () => 4,
		request( path ) {
			requests.set( ++id, path );
			return id;
		},
		take( id ) {
			if ( !requests.delete( id ) ) return null;
			return { kind: "bytes", buffer: new ArrayBuffer( 1 ) };
		},
		cancel( id ) {
			requests.delete( id );
		}
	};
	const audio = createAudio( assets, "http://fixture.invalid", createPresentationRandom( 1 ) );
	t.after( () => audio.dispose() );
	audio.unlock();
	const cue = ( id, loop, spatial ) => ({
		id,
		path: "/assets/audio/wind.wav",
		gain: 1,
		x: 0,
		y: 0,
		z: 0,
		expires: 10,
		loop,
		spatial
	});
	audio.enqueue( cue( "ambient:fixture", true, false ) );
	audio.step( 0, [ 0, 0, 0 ] );
	audio.step( .01, [ 0, 0, 0 ] );
	await new Promise( setImmediate );
	audio.step( .02, [ 0, 0, 0 ] );
	audio.enqueue( cue( "effect", true, true ) );
	audio.enqueue( cue( "once", false, false ) );
	audio.step( .03, [ 0, 0, 0 ] );
	assert.equal( sources.length, 3 );
	for ( const s of sources.slice( 0, 2 ) ) {
		assert.equal( s.buffer, decoded );
		assert.ok( s.started );
		assert.ok( s.loopEnd * decoded.sampleRate <= decoded.length );
		assert.ok( s.loopEnd * decoded.sampleRate > decoded.length - 1e-9 );
	}
	assert.equal( sources[2].loopEnd, 0 );
	assert.equal( sources[2].loop, false );
	audio.enqueue( { ...cue( "effect", true, true ), stop: true } );
	assert.ok( sources[1].stopped );
	audio.reset();
	assert.ok( sources.every( s => s.stopped ) );
	audio.enqueue( cue( "restarted", true, false ) );
	audio.step( .04, [ 0, 0, 0 ] );
	assert.equal( sources.length, 4 );
	assert.ok( sources[3].started );
	assert.equal( sources[3].loopEnd, sources[0].loopEnd );
	audio.dispose();
	assert.ok( sources[3].stopped );
});

test("playByName resolves an authored label and stays silent on a miss", async t => {
	const requests = new Map(), requestedPaths = [];
	let id = 0;
	const node = () => ({ connect() {}, disconnect() {} });
	class Context {
		state = "running";
		destination = {};
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		resume() {
			return Promise.resolve();
		}
		close() {
			return Promise.resolve();
		}
		decodeAudioData() {
			return Promise.resolve( { length: 1, sampleRate: 48000, numberOfChannels: 1 } );
		}
		createBufferSource() {
			return {
				...node(),
				start() {},
				stop() {}
			};
		}
		createGain() {
			return { ...node(), gain: {} };
		}
		createPanner() {
			return { ...node(), positionX: {}, positionY: {}, positionZ: {} };
		}
	}
	const originalContext = globalThis.AudioContext;
	globalThis.AudioContext = Context;
	t.after( () => {
		if ( originalContext ) globalThis.AudioContext = originalContext;
		else delete globalThis.AudioContext;
	} );
	const assets = {
		available: () => 4,
		request( path ) {
			requests.set( ++id, path );
			requestedPaths.push( path );
			return id;
		},
		take( takeId ) {
			if ( !requests.delete( takeId ) ) return null;
			return { kind: "bytes", buffer: new ArrayBuffer( 1 ) };
		},
		cancel( takeId ) {
			requests.delete( takeId );
		}
	};
	const audio = createAudio( assets, "http://fixture.invalid", createPresentationRandom( 1 ) );
	t.after( () => audio.dispose() );
	audio.unlock();
	audio.playByName( "SND_BUTTON_CLICK" );
	audio.playByName( "SND_NO_SUCH_LABEL" );
	// An item label's files are chosen per item type; the name alone
	// cannot choose one, so it stays silent too.
	audio.playByName( "SND_EQUIP" );
	audio.step( 0, [ 0, 0, 0 ] );
	await new Promise( setImmediate );
	audio.step( .01, [ 0, 0, 0 ] );
	assert.equal( requestedPaths.length, 1, "exactly the named label was requested: " + requestedPaths.join( "," ) );
	assert.ok(
		requestedPaths[0].endsWith( "/uibutton_a.wav" ) || requestedPaths[0].endsWith( "/uibutton_b.wav" ),
		"the button label resolved to a button file: " + requestedPaths[0]
	);
});
