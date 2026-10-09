/*
===========================================================================

audio-volume-live.test.mjs - quiet settings on the real live audio owner

Fake browser nodes expose gain and source identity; the production owner
still performs loading, decoding, channel classification and preference updates.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createAudio } = await import( "../../src/engine/runtime/audio/audio.ts" );
const { initialAudioOptions, audioAmplitude } = await import( "../../src/engine/foundation/audio/options.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createCharacterSounds } = await import( "../../src/engine/runtime/characters/sounds/sounds.ts" );

test("live effects preferences gate rule countdowns without muting other channels", t => {
	const { audio } = fixture( t );
	const heard = [];
	const sounds = createCharacterSounds( event => heard.push( event ), () => 0, () => null, audio.effectsEnabled );
	sounds.catalog( [ {
		object: "CAT",
		handle: "SND_STAND",
		event1: "-",
		skip: 1,
		publicPath: "/assets/audio/sfx/cat.wav"
	} ] );
	for (
		const patch of [
			{ muteEffects: true },
			{ effects: 0 },
			{ muteBgm: true, muteEnvironment: true },
			{ muteEffects: true },
			{ effects: .02 },
			{ effects: 50 }
		]
	) {
		audio.options( { ...initialAudioOptions(), ...patch } );
		sounds.emit(
			String( heard.length ),
			"CAT",
			[ "SND_STAND" ],
			{ player: false, berserk: false },
			[ 0, 0, 0 ],
			0
		);
	}
	assert.equal( heard.length, 2 );
});

/*
================
fixture
================
*/
function fixture( t ) {
	const sources = [], gains = [], panners = [], media = [];
	/*
	================
	Node
	================
	*/
	class Node {
		gain = { value: 1 };
		buffer = null;
		loop = false;
		/** @type {(() => void) | null} */
		onended = null;
		positionX = { value: 0 };
		positionY = { value: 0 };
		positionZ = { value: 0 };
		distanceModel = "linear";
		refDistance = 0;
		maxDistance = 0;
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
		start() {}
		/*
		================
		stop
		================
		*/
		stop() {
			this.onended?.();
		}
	}
	/*
	================
	Context
	================
	*/
	class Context {
		state = "running";
		listener = { positionX: { value: 0 }, positionY: { value: 0 }, positionZ: { value: 0 } };
		destination = new Node();
		/*
		================
		resume
		================
		*/
		async resume() {}
		/*
		================
		close
		================
		*/
		async close() {}
		/*
		================
		decodeAudioData
		================
		*/
		async decodeAudioData() {
			return { length: 48000, numberOfChannels: 1, sampleRate: 48000 };
		}
		/*
		================
		createBufferSource
		================
		*/
		createBufferSource() {
			const node = new Node();
			sources.push( node );
			return node;
		}
		/*
		================
		createGain
		================
		*/
		createGain() {
			const node = new Node();
			gains.push( node );
			return node;
		}
		/*
		================
		createPanner
		================
		*/
		createPanner() {
			const node = new Node();
			panners.push( node );
			return node;
		}
	}
	/*
	================
	Media
	================
	*/
	class Media {
		paused = true;
		currentTime = 0;
		ended = false;
		volume = 1;
		/*
		================
		constructor
		================
		*/
		constructor() {
			media.push( this );
		}
		/*
		================
		play
		================
		*/
		async play() {
			this.paused = false;
		}
		/*
		================
		pause
		================
		*/
		pause() {
			this.paused = true;
		}
		/*
		================
		removeAttribute
		================
		*/
		removeAttribute() {}
		/*
		================
		load
		================
		*/
		load() {}
	}
	/** @type {[string, typeof Context | typeof Media][]} */
	const browserGlobals = [ [ "AudioContext", Context ], [ "Audio", Media ] ];
	for ( const [name, value] of browserGlobals ) {
		const prior = Object.getOwnPropertyDescriptor( globalThis, name );
		Object.defineProperty( globalThis, name, { value, configurable: true } );
		t.after( () => {
			if ( prior ) Object.defineProperty( globalThis, name, prior );
			else delete globalThis[name];
		} );
	}
	let serial = 0;
	const requests = new Map();
	const audio = createAudio(
		{
			progress: () => null,
			health: () => ({ phase: "running" }),
			install: () => {},
			dispose: () => {},
			available: () => 8,
			request( path ) {
				requests.set( ++serial, path );
				return serial;
			},
			take( id ) {
				const path = requests.get( id );
				if ( !path ) return null;
				requests.delete( id );
				const buffer = path.endsWith( "option.json" ) ?
					new TextEncoder().encode(
						JSON.stringify( { introBgmPublicPath: "/assets/audio/music/title.mp3" } )
					).buffer :
					Uint8Array.of( 0xff, 0xfb, 0x90, 0 ).buffer;
				return { kind: "bytes", id, buffer };
			},
			cancel( id ) {
				requests.delete( id );
			}
		},
		"https://fixture.invalid",
		createPresentationRandom( 1 )
	);
	t.after( () => audio.dispose() );
	audio.unlock();
	/*
	================
	settle
	================
	*/
	async function settle() {
		for ( let i = 0; i < 12; i++ ) {
			audio.step( 0, [ 0, 0, 0 ] );
			await new Promise( setImmediate );
		}
	}
	return { audio, sources, gains, panners, media, settle };
}

/*
================
live channel changes
================
*/
test("quiet preferences update existing voices independently without rewriting authored gain", async t => {
	const f = fixture( t );
	const settings = { ...initialAudioOptions(), bgm: .02, effects: .1, environment: .5 };
	f.audio.options( settings );
	f.audio.music( true );
	/** @type {[string, number, boolean][]} */
	const events = [ [ "effect", .4, false ], [ "ambient:rain", 1, true ], [ "effect-loop", .6, true ] ];
	for ( const [id, gain, loop] of events ) {
		f.audio.enqueue( {
			id,
			path: `/assets/audio/${id}.wav`,
			gain,
			loop,
			spatial: id === "effect",
			x: 0,
			y: 0,
			z: 0,
			expires: 10
		} );
	}
	await f.settle();
	assert.equal( f.sources.length, 3 );
	assert.equal( f.gains[0].gain.value, .4 * audioAmplitude( .1, false ) );
	assert.equal( f.gains[1].gain.value, audioAmplitude( .5, false ) );
	assert.equal( f.gains[2].gain.value, .6 * audioAmplitude( .1, false ) );
	assert.equal( f.media[0].volume, audioAmplitude( .02, false ) );
	const identities = [ ...f.sources ], media = f.media[0];
	f.audio.options( { ...settings, effects: .2, muteEnvironment: true } );
	assert.deepEqual( f.sources, identities );
	assert.equal( f.gains[0].gain.value, .4 * audioAmplitude( .2, false ) );
	assert.equal( f.gains[1].gain.value, 0 );
	assert.equal( f.gains[2].gain.value, .6 * audioAmplitude( .2, false ) );
	assert.equal( f.media[0], media );
	assert.equal( media.volume, audioAmplitude( .02, false ) );
	f.audio.options( { ...settings, effects: 20, environment: .98 } );
	assert.equal( f.gains[0].gain.value, .4 * audioAmplitude( 20, false ) );
	assert.equal( f.gains[1].gain.value, audioAmplitude( .98, false ) );
	assert.equal( f.panners[0].distanceModel, "linear" );
	assert.equal( f.panners[0].refDistance, 100 );
	assert.equal( f.panners[0].maxDistance, 300 );
	f.audio.options( { ...settings, muteBgm: true, muteEffects: true, muteEnvironment: true } );
	assert.equal( media.volume, 0 );
	assert.ok( f.gains.every( node => node.gain.value === 0 ) );
});

/*
================
restore order
================
*/
test("quiet restore order is independent and newly started voices inherit the current gain", async t => {
	const f = fixture( t );
	f.audio.options( { ...initialAudioOptions(), effects: .02 } );
	f.audio.enqueue( {
		id: "new",
		path: "/assets/audio/new.wav",
		gain: .75,
		spatial: false,
		x: 0,
		y: 0,
		z: 0,
		expires: 10
	} );
	await f.settle();
	assert.equal( f.gains[0].gain.value, .75 * audioAmplitude( .02, false ) );
	f.audio.options( { ...initialAudioOptions(), effects: 1 } );
	assert.equal( f.gains[0].gain.value, .75 * audioAmplitude( 1, false ) );
});
