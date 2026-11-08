/*
===========================================================================

audio-load-failure.test.mjs - what a failed sound load may cost

A sound the asset loader could not fetch after its bounded transport
retries is a cue that did not play. On a world sound (a monster's
footsteps) it must not become the session's standing error; on the
critical sets and on permanent failures it still names itself, with
the retry facts the loader reported (#2863).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createAudio } = await import( "../../src/engine/runtime/audio/audio.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const WALK = "/assets/audio/sfx/prim/snd/monster/cara_bunwang_walk.wav";
const BUFF_END = "/assets/audio/sfx/prim/snd/ui/buf_disappear.wav";

/*
================
Source

One played voice, as the audio owner drives it.
================
*/
class Source {
	loopEnd = 0;
	started = false;
	stopped = false;
	/** @type {null | (() => void)} */
	onended = null;
	buffer = null;
	loop = false;
	/*
	================
	connect
	================
	*/
	connect() {}
	/*
	================
	disconnect
	================
	*/
	disconnect() {}
	/*
	================
	start
	================
	*/
	start() {
		this.started = true;
	}
	/*
	================
	stop
	================
	*/
	stop() {
		this.stopped = true;
		this.onended?.();
	}
}

/*
================
fixture

One audio owner over a scripted asset owner: outcomes maps a path to
the error results its requests answer with, in order; bytes when the
script runs out. Requests are logged by path.
================
*/
/**
 * @param {Map<string, { kind: "error"; error: string; transient?: true; }[]>} outcomes
 */
function fixture( outcomes ) {
	const sources = [],
		decoded = { length: 4399, sampleRate: 22050, numberOfChannels: 1 },
		requests = [],
		pending = new Map();
	let next = 0;
	const node = () => ({ connect() {}, disconnect() {} });
	class Context {
		state = "running";
		destination = {};
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		/*
		================
		resume
		================
		*/
		resume() {
			return Promise.resolve();
		}
		/*
		================
		close
		================
		*/
		close() {
			return Promise.resolve();
		}
		/*
		================
		decodeAudioData
		================
		*/
		decodeAudioData() {
			return Promise.resolve( decoded );
		}
		/*
		================
		createBufferSource
		================
		*/
		createBufferSource() {
			const source = new Source();
			sources.push( source );
			return source;
		}
		/*
		================
		createGain
		================
		*/
		createGain() {
			return { ...node(), gain: {} };
		}
		/*
		================
		createPanner
		================
		*/
		createPanner() {
			return { ...node(), positionX: {}, positionY: {}, positionZ: {} };
		}
	}
	const previous = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		progress: () => null,
		health: () => ({ phase: "running" }),
		available: () => 4,
		/*
		================
		request
		================
		*/
		request( url ) {
			const path = new URL( url ).pathname;
			requests.push( path );
			pending.set( ++next, path );
			return next;
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const path = pending.get( id );
			if ( !path ) return null;
			pending.delete( id );
			const scripted = outcomes.get( path );
			if ( scripted && scripted.length ) {
				const result = scripted.shift();
				if ( result ) {
					return {
						kind: "error",
						id,
						error: result.error,
						...(result.transient ? { transient: true } : {})
					};
				}
			}
			return { kind: "bytes", id, buffer: new ArrayBuffer( 4 ) };
		},
		/*
		================
		textureFeatures
		================
		*/
		textureFeatures() {},
		/*
		================
		hdTextures
		================
		*/
		hdTextures() {},
		/*
		================
		cancel
		================
		*/
		cancel( id ) {
			pending.delete( id );
		},
		/*
		================
		install
		================
		*/
		install() {},
		/*
		================
		dispose
		================
		*/
		dispose() {}
	};
	const audio = createAudio( assets, "http://fixture.invalid", createPresentationRandom( 1 ) );
	audio.unlock();
	return {
		audio,
		sources,
		requests,
		/*
		================
		restore
		================
		*/
		restore() {
			audio.dispose();
			if ( previous ) Object.defineProperty( globalThis, "AudioContext", previous );
			else Reflect.deleteProperty( globalThis, "AudioContext" );
		}
	};
}

/*
================
cue

One world-sound cue at the listener, the way a walking monster emits
its footsteps.
================
*/
function cue( id, path ) {
	return { id, path, gain: 1, x: 0, y: 0, z: 0, expires: 100, loop: false, spatial: true };
}

test("a transient load fault on a world sound drops the cue, not the session (#2863)", async t => {
	const f = fixture(
		new Map( [
			[ WALK, [ { kind: "error", error: "Error: TypeError: Load failed", transient: true } ] ]
		] )
	);
	t.after( f.restore );
	f.audio.enqueue( cue( "walk-1", WALK ) );
	f.audio.step( 0, [ 0, 0, 0 ] );
	f.audio.step( .01, [ 0, 0, 0 ] );
	assert.equal( f.audio.error(), null, "a spent transport retry on a footstep is not a standing error" );
	assert.equal( f.audio.failure(), null );
	// The path backs off: a fresh cue inside the window loads nothing.
	f.audio.enqueue( cue( "walk-2", WALK ) );
	f.audio.step( 1, [ 0, 0, 0 ] );
	assert.deepEqual( f.requests, [ WALK ] );
	// Past the backoff the next cue loads, decodes, and plays.
	f.audio.step( 2.5, [ 0, 0, 0 ] );
	f.audio.step( 2.51, [ 0, 0, 0 ] );
	await new Promise( setImmediate );
	f.audio.step( 2.52, [ 0, 0, 0 ] );
	assert.deepEqual( f.requests, [ WALK, WALK ] );
	assert.equal( f.sources.length, 1 );
	assert.ok( f.sources[0].started );
	assert.equal( f.audio.error(), null );
});

test("a permanent load fault on a world sound still names itself, as permanent (#2863)", t => {
	const f = fixture(
		new Map( [
			[ WALK, [ { kind: "error", error: "Error: Asset HTTP 404" } ] ]
		] )
	);
	t.after( f.restore );
	f.audio.enqueue( cue( "walk-1", WALK ) );
	f.audio.step( 0, [ 0, 0, 0 ] );
	f.audio.step( .01, [ 0, 0, 0 ] );
	assert.equal( f.audio.error(), "Error: Asset HTTP 404" );
	assert.deepEqual( f.audio.failure(), { path: WALK, message: "Error: Asset HTTP 404", transient: false } );
});

test("a transient fault on a critical sound keeps the standing error and names its cause (#2863)", t => {
	const f = fixture(
		new Map( [
			[ BUFF_END, [ { kind: "error", error: "Error: TypeError: Load failed", transient: true } ] ]
		] )
	);
	t.after( f.restore );
	f.audio.enqueue( cue( "buff-end", BUFF_END ) );
	f.audio.step( 0, [ 0, 0, 0 ] );
	f.audio.step( .01, [ 0, 0, 0 ] );
	assert.equal( f.audio.error(), "Error: TypeError: Load failed" );
	assert.deepEqual( f.audio.failure(), {
		path: BUFF_END,
		message: "Error: TypeError: Load failed",
		transient: true
	} );
});
