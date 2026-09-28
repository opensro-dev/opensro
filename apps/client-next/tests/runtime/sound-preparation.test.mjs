/*
===========================================================================

sound-preparation.test.mjs - first-use sound readiness and ownership

Cold asset completion and Web Audio decode are separate boundaries. The
first combat cue must play from a decoded buffer without starting either.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { assetRequestBudget } = await import( "../../src/engine/foundation/assets/asset-budget.ts" );
const { createSoundPreparation } = await import( "../../src/engine/runtime/audio/preparation.ts" );
const { createAudio } = await import( "../../src/engine/runtime/audio/audio.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const ROOT = "https://audio.invalid";
const SWING = "/assets/audio/sfx/swing.wav";
const HIT = "/assets/audio/sfx/hit.wav";
const VOICE = "/assets/audio/sfx/voice.wav";
const SPELL = "/assets/audio/sfx/spell.wav";

/*
================
fixture
================
*/
function fixture() {
	const documents = {
		"/assets/audio/effectsound.json": {
			rules: [
				{ object: "PLAYER", skillId: "-", publicPath: SWING },
				{ object: "PLAYER", skillId: "BASIC", publicPath: HIT },
				{ object: "PCM", skillId: "-", publicPath: VOICE },
				{ object: "PLAYER", skillId: "UNLEARNED", publicPath: "/assets/audio/unlearned.wav" }
			]
		},
		"/assets/char/roster.json": { models: [ { refObjId: 1, codename: "HERO" } ] },
		"/assets/npc/manifest.json": { models: { auxiliary: { codename: "res/auxiliary.bsr" } } },
		"/assets/anim/manifest.json": { models: { HERO: { soundProfileName: "PCM" } } },
		"/assets/data/skillData.json": { skillAudioRows: [ "2\t0\tBASIC_01\tBASIC", "3\t2\tCHILD\tCHILD" ] },
		"/assets/skill/effectRecords.json": { 3: { stages: [ { sound: SPELL } ] } }
	};
	const calls = [], pending = new Map();
	let next = 0;
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		progress: () => null,
		health: () => ({ phase: "running" }),
		install() {},
		dispose() {},
		available: () => 4 - pending.size,
		request( url, limit = 16 << 20, decode ) {
			assert.ok( limit <= assetRequestBudget( decode ), "request must fit the production decoder budget" );
			const path = new URL( url ).pathname;
			calls.push( path );
			pending.set( ++next, path );
			return next;
		},
		take( id ) {
			const path = pending.get( id );
			if ( !path ) return null;
			pending.delete( id );
			if ( path === "/assets/skill/effectRecords.json" ) return { kind: "effects", id, catalog: documents[path] };
			return {
				kind: "bytes",
				id,
				buffer: documents[path] ?
					new TextEncoder().encode( JSON.stringify( documents[path] ) ).buffer :
					new ArrayBuffer( 4 )
			};
		},
		cancel( id ) {
			pending.delete( id );
		}
	};
	return { assets, calls, pending, documents };
}
const gameplay = { localGid: 10, skills: [ 3 ], casts: [], inventory: [] };
const entities = [ { gid: 10, refObjId: 1, kind: "local-player" } ];

test("preparation resolves inherited skill groups and only resident sound profiles", () => {
	const { assets, calls } = fixture(), owner = createSoundPreparation( assets, ROOT );
	for ( let i = 0; i < 8; i++ ) owner.step( gameplay, entities );
	assert.equal( owner.error(), null );
	assert.equal( owner.ready(), true );
	assert.deepEqual( owner.paths(), [ HIT, SPELL, SWING, VOICE ].sort() );
	assert.equal( calls.length, 6 );
	owner.step( { ...gameplay, skills: [] }, entities );
	assert.deepEqual( owner.paths(), [ SWING, VOICE ].sort() );
	owner.dispose();
});

test("leaving the scene still collects an admitted catalogue and disposal releases its slot", () => {
	const { assets, pending } = fixture(), owner = createSoundPreparation( assets, ROOT );
	owner.step( gameplay, entities );
	assert.equal( pending.size, 1 );
	owner.step( null, [] );
	assert.equal( pending.size, 0 );
	assert.deepEqual( owner.paths(), [] );
	owner.step( gameplay, entities );
	assert.equal( pending.size, 1 );
	owner.dispose();
	assert.equal( pending.size, 0 );
});

test("a missing required catalogue exposes an error instead of declaring readiness", () => {
	const { assets } = fixture();
	assets.take = id => ({ kind: "error", id, error: "Asset HTTP 404" });
	const owner = createSoundPreparation( assets, ROOT );
	owner.step( gameplay, entities );
	owner.step( gameplay, entities );
	assert.equal( owner.ready(), false );
	assert.match( owner.error() ?? "", /404/ );
	owner.dispose();
});

test("first attack waits for decode readiness then plays without a new asset request", async t => {
	const { assets, calls } = fixture(), decodes = [], started = [];
	installAudioContext( t, decodes, started );
	const audio = createAudio( assets, ROOT, createPresentationRandom( 1 ) );
	t.after( () => audio.dispose() );
	audio.unlock();
	for ( let i = 0; i < 10; i++ ) {
		audio.prepareCombat( gameplay, entities );
		audio.step( i / 60, [ 0, 0, 0 ] );
	}
	assert.equal( audio.ready(), false, "downloaded bytes are not yet decoded" );
	for ( let i = 0; i < 30 && !audio.ready(); i++ ) {
		for ( const resolve of decodes.splice( 0 ) ) {
			resolve( { length: 7 << 18, numberOfChannels: 1, sampleRate: 48000 } );
		}
		await new Promise( setImmediate );
		audio.prepareCombat( gameplay, entities );
		audio.step( 1 + i / 60, [ 0, 0, 0 ] );
	}
	assert.equal( audio.ready(), true );
	assert.equal( started.length, 0, "warming must be silent" );
	const before = calls.length;
	audio.enqueue( { id: "first-hit", path: HIT, gain: 1, x: 0, y: 0, z: 0, expires: 3 } );
	audio.step( 2, [ 0, 0, 0 ] );
	assert.equal( started.length, 1 );
	assert.equal( calls.length, before, "first hit performs no network or asset lookup" );
	const peerSound = "/assets/audio/peer-spell.wav";
	audio.enqueue( { id: "peer-spell", path: peerSound, gain: 1, x: 0, y: 0, z: 0, expires: 5 } );
	audio.step( 2.1, [ 0, 0, 0 ] );
	audio.step( 2.2, [ 0, 0, 0 ] );
	for ( const resolve of decodes.splice( 0 ) ) resolve( { length: 8 << 18, numberOfChannels: 1, sampleRate: 48000 } );
	await new Promise( setImmediate );
	audio.step( 2.3, [ 0, 0, 0 ] );
	assert.equal( audio.error(), null, "Inactive warmed sounds yield room to new audible cues" );
	assert.equal( started.length, 2 );
	const afterPeer = calls.length;
	for ( let frame = 0; frame < 20; frame++ ) {
		audio.prepareCombat( gameplay, entities );
		audio.step( 3 + frame / 60, [ 0, 0, 0 ] );
	}
	assert.equal( calls.length, afterPeer, "Evicted speculative warmup does not refill in a loop" );
	assert.equal( audio.ready(), true );
});

/*
================
installAudioContext
================
*/
function installAudioContext( t, decodes, started ) {
	const previous = globalThis.AudioContext;
	const node = () => ({ connect() {}, disconnect() {} });
	/*
================
Context
================
	*/
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
			return new Promise( resolve => decodes.push( resolve ) );
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
		/*
================
createBufferSource
================
		*/
		createBufferSource() {
			return {
				...node(),
				onended: () => {},
				start() {
					started.push( this );
				},
				stop() {
					this.onended?.();
				}
			};
		}
	}
	Object.defineProperty( globalThis, "AudioContext", { value: Context, writable: true, configurable: true } );
	t.after( () => {
		if ( previous ) globalThis.AudioContext = previous;
		else Reflect.deleteProperty( globalThis, "AudioContext" );
	} );
}

/*
================
settleAudioFrame
================
*/
async function settleAudioFrame( state, seconds, bytes ) {
	for ( const resolve of state.decodes.splice( 0 ) ) {
		resolve( { length: bytes / 4, numberOfChannels: 1, sampleRate: 96000 } );
	}
	await new Promise( setImmediate );
	state.audio.prepareCombat( gameplay, entities );
	state.audio.step( seconds, [ 0, 0, 0 ] );
}

/*
================
residencyFixture
================
*/
function residencyFixture( t ) {
	const { assets, calls } = fixture(), decodes = [], started = [];
	installAudioContext( t, decodes, started );
	const audio = createAudio( assets, ROOT, createPresentationRandom( 1 ) );
	t.after( () => audio.dispose() );
	audio.unlock();
	audio.prepareCombat( gameplay, entities );
	return { audio, calls, decodes, started };
}

test("a prepared scene larger than the cache admits without endless warmup", async t => {
	const state = residencyFixture( t );
	// Four 10 MiB decoded sounds cannot all fit in the 32 MiB cache.
	for ( let frame = 0; frame < 50 && !state.audio.ready(); frame++ ) {
		await settleAudioFrame( state, frame / 60, 10 << 20 );
	}
	assert.equal( state.audio.ready(), true );
	assert.equal( state.audio.error(), null );
	assert.equal( state.started.length, 0 );
	const snapshot = state.audio.snapshot();
	assert.ok( snapshot.residentBytes <= snapshot.limitBytes );
	assert.equal( snapshot.buffers.length, 3 );
	const warmed = state.calls.length;
	for ( let frame = 0; frame < 30; frame++ ) await settleAudioFrame( state, 1 + frame / 60, 10 << 20 );
	assert.equal( state.calls.length, warmed, "Eviction cannot reopen speculative preparation" );
	const missing = snapshot.required.find( path => !snapshot.buffers.some( buffer => buffer.path === path ) );
	assert.ok( missing );
	state.audio.enqueue( { id: "demand", path: missing, gain: 1, x: 0, y: 0, z: 0, expires: 10 } );
	for ( let frame = 0; frame < 5; frame++ ) await settleAudioFrame( state, 2 + frame / 60, 10 << 20 );
	assert.equal( state.started.length, 1, "An evicted sound is available on renewed demand" );
	assert.equal( state.audio.error(), null );
});

test("active voices keep their residency while blocked demand retries after they end", async t => {
	const state = residencyFixture( t );
	for ( let frame = 0; frame < 50 && !state.audio.ready(); frame++ ) {
		await settleAudioFrame( state, frame / 60, 8 << 20 );
	}
	assert.equal( state.audio.ready(), true );
	for ( const path of [ HIT, SPELL, SWING, VOICE ] ) {
		state.audio.enqueue( { id: path, path, gain: 1, x: 0, y: 0, z: 0, expires: 10 } );
	}
	state.audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( state.started.length, 4 );
	const path = "/assets/audio/new-demand.wav";
	state.audio.enqueue( { id: "waiting", path, gain: 1, x: 0, y: 0, z: 0, expires: 10 } );
	for ( let frame = 0; frame < 5; frame++ ) await settleAudioFrame( state, 2 + frame / 60, 8 << 20 );
	assert.equal( state.audio.error(), null, "Capacity pressure is not a corrupt asset" );
	assert.equal( state.audio.snapshot().residentBytes, 32 << 20 );
	assert.equal( state.started.length, 4 );
	const blocked = state.calls.length;
	for ( let frame = 0; frame < 10; frame++ ) await settleAudioFrame( state, 3 + frame / 60, 8 << 20 );
	assert.equal( state.calls.length, blocked, "Blocked demand must not decode every frame" );
	state.started[0].onended();
	for ( let frame = 0; frame < 5; frame++ ) await settleAudioFrame( state, 5 + frame / 60, 8 << 20 );
	assert.equal( state.started.length, 5 );
	assert.equal( state.audio.snapshot().residentBytes, 32 << 20 );
	assert.equal( state.audio.error(), null );
});

test("UI and world preparation share a bounded cache without permanently pinning UI", async t => {
	const state = residencyFixture( t );
	state.audio.prepareUi( true );
	for ( let frame = 0; frame < 300 && !state.audio.ready(); frame++ ) {
		await settleAudioFrame( state, frame / 60, 2 << 20 );
	}
	assert.equal( state.audio.ready(), true );
	assert.equal( state.audio.error(), null );
	const snapshot = state.audio.snapshot();
	assert.ok( snapshot.residentBytes <= snapshot.limitBytes );
	const completed = state.calls.length;
	for ( let frame = 0; frame < 20; frame++ ) await settleAudioFrame( state, 6 + frame / 60, 2 << 20 );
	assert.equal( state.calls.length, completed );
	state.audio.prepareUi( false );
	state.audio.nativeUi( "SND_BUTTON_CLICK", 7 );
	for ( let frame = 0; frame < 5; frame++ ) await settleAudioFrame( state, 7 + frame / 60, 2 << 20 );
	assert.equal( state.started.length, 1 );
	assert.equal( state.audio.error(), null );
});
