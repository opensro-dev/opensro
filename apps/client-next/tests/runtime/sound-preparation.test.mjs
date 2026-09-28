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
	const previous = globalThis.AudioContext;
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
			return new Promise( resolve => decodes.push( resolve ) );
		}
		createGain() {
			return { ...node(), gain: {} };
		}
		createPanner() {
			return { ...node(), positionX: {}, positionY: {}, positionZ: {} };
		}
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
