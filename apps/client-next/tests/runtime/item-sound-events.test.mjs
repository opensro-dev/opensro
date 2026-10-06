/*
===========================================================================

item-sound-events.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { createHash } from "node:crypto";
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( "src/engine/" + file + ".ts" ).href );
}
const { createInventory } = await load( "runtime/simulation/worker/session/world/gameplay/inventory/inventory" );
const { createWorldCore } = await load( "runtime/simulation/worker/session/world/core" );
const { createPresentation } = await load( "runtime/presentation/presentation" );
const { createAudio } = await load( "runtime/audio/audio" );
const { createPresentationRandom } = await load( "runtime/random/random" );
const { itemSoundCategory, equipDurabilityWarning } = await load( "foundation/audio/item-sounds" );
const { createItemSoundCatalog } = await load( "foundation/audio/item-sound-catalog" );
/*
================
body
================
*/
const body = ( id, flags, durability = 50, quantity = 1 ) => {
	const p = Buffer.alloc( (flags & 0x60) === 0x60 ? 6 : 18 );
	p.writeUInt32LE( id );
	if ( p.length === 6 ) p.writeUInt16LE( quantity, 4 );
	else p.writeUInt32LE( durability >>> 0, 13 );
	return [ ...p ];
};
/*
================
row
================
*/
const row = ( slot, id, flags, durability = 50, quantity = 1 ) => ({
	slot,
	refObjId: id,
	body: body( id, flags, durability, quantity )
});
/*
================
move
================
*/
const move = ( a, b, n = 1, extra = [] ) => Uint8Array.of( 1, 0, a, b, n & 255, n >>> 8, extra.length / 5, ...extra );
const sword = 0x132c, robe = 0x18ac, potion = 0x8ec;
/*
================
fixture
================
*/
function fixture(
	items,
	refs = [ { refObjId: 1, typeFlags: sword }, { refObjId: 2, typeFlags: robe }, {
		refObjId: 3,
		typeFlags: potion,
		nativeFields: { maxStack: 50 }
	} ]
) {
	return {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: refs,
		equipItems: items,
		avatarItems: []
	};
}

test("ITEM category selector matches all 65536 retail machine-code results, including unused/invalid TIDs", () => {
	const oracle = JSON.parse( fs.readFileSync( "tests/fixtures/native/item-sound-selector-cases.json", "utf8" ) );
	assert.equal(
		oracle.candidateSha256,
		createHash( "sha256" ).update( fs.readFileSync( "src/engine/foundation/audio/item-sounds.ts" ) ).digest(
			"hex"
		),
		"selector changed after independent machine validation"
	);
	const expected = new Map(
		Object.entries( oracle.categories ).flatMap( ( [label, values] ) => values.map( v => [ v, label ] ) )
	);
	for ( let tid = 0; tid < 65536; tid++ ) {
		assert.equal( itemSoundCategory( tid ), expected.get( tid ) ?? "", `TID ${tid.toString( 16 )}` );
	}
	assert.equal( itemSoundCategory( 0xeac ), "", "avatar must not become generic cloth" );
	assert.equal( itemSoundCategory( 0x4b2c ), "DUELAXE", "inventory label differs from animation AXE" );
});

test("accepted bag moves, splits, merges, swaps, equip and unequip use the post-transfer destination type", () => {
	/** @type {any[]} */ const sounds = [];
	const owner = createInventory( () => {}, h => sounds.push( h ), cue => {
		assert.ok( owner.state().inventory.some( i => i.typeFlags === cue.typeFlags ) );
		sounds.push( cue );
	} );
	owner.bootstrap(
		fixture( [ row( 13, 1, sword ), row( 14, 2, robe ), row( 15, 3, potion, 0, 10 ), row( 16, 3, potion, 0, 2 ) ] )
	);
	owner.move( 13, 6, 1, 0 );
	assert.deepEqual( sounds, [] );
	owner.receive( 0xb06d, Uint8Array.of( 2, 1 ) );
	assert.deepEqual( sounds, [] );
	for ( const p of [ move( 13, 6 ), move( 6, 13 ), move( 13, 14 ), move( 15, 16, 3 ), move( 16, 17, 2 ) ] ) {
		owner.receive( 0xb06d, p );
	}
	assert.deepEqual( sounds.map( c => c.typeFlags ), [ sword, sword, sword, potion, potion ] );
	assert.equal( owner.state().inventory.find( i => i.slot === 16 ).quantity, 10 );
	owner.state();
	assert.equal( sounds.length, 5, "snapshots cannot replay occurrences" );
});

test("submoves preserve cue order and rejected/truncated/invalid tails publish neither state nor sound", () => {
	const sounds = [], owner = createInventory( () => {}, h => sounds.push( h ), c => sounds.push( c ) );
	owner.bootstrap( fixture( [ row( 13, 1, sword, 6 ), row( 14, 2, robe ) ] ) );
	const before = owner.state().inventory;
	for (
		const p of [
			move( 13, 6, 1, [ 1, 14, 0, 1, 0 ] ),
			move( 13, 6, 1, [ 0, 99, 0, 1, 0 ] ),
			move( 13, 6 ).slice( 0, 6 )
		]
	) {
		assert.throws( () => owner.receive( 0xb06d, p ) );
		assert.deepEqual( owner.state().inventory, before );
		assert.deepEqual( sounds, [] );
	}
	owner.receive( 0xb06d, move( 13, 6, 1, [ 0, 14, 0, 1, 0 ] ) );
	assert.deepEqual( sounds, [ "SND_EQDANGER", { handle: "SND_EQUIP", typeFlags: sword }, {
		handle: "SND_EQUIP",
		typeFlags: robe
	} ] );
});

test("equip warning uses signed durability and native TID exclusions, only on bag -> equipment", () => {
	for ( const tid of [ sword, robe, 0x2c ] ) {
		for ( const d of [ 0, 1, 5, 6, 7, 0xffffffff ] ) {
			assert.equal( equipDurabilityWarning( tid, d ), (d | 0) <= 6 );
		}
	}
	for ( const tid of [ 0xaac, 0xe2c, 0x1bac, potion, 0x32e ] ) {
		assert.equal( equipDurabilityWarning( tid, 0 ), false );
	}
	const heard = [], owner = createInventory( () => {}, h => heard.push( h ), c => heard.push( c ) );
	owner.bootstrap( fixture( [ row( 6, 1, sword, 0 ) ] ) );
	owner.receive( 0xb06d, move( 6, 13 ) );
	assert.deepEqual( heard, [ { handle: "SND_EQUIP", typeFlags: sword } ] );
});

test("avatar apply emits the native ITEM request after commit, retaining the silent exact-key miss", () => {
	const heard = [], owner = createInventory( () => {}, () => {}, c => heard.push( c ) );
	owner.bootstrap( fixture( [ row( 20, 1, 0xeac ) ], [ { refObjId: 1, typeFlags: 0xeac } ] ) );
	owner.avatarMove( true, 20, 0, 0 );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0x24, 20, 0, 1, 0, 0 ) );
	owner.avatarMove( false, 0, 20, 1 );
	owner.receive( 0xb06d, Uint8Array.of( 1, 0x23, 0, 20, 1, 0, 0 ) );
	assert.deepEqual( heard, [ { handle: "SND_EQUIP", typeFlags: 0xeac }, { handle: "SND_EQUIP", typeFlags: 0xeac } ] );
	assert.equal( createItemSoundCatalog()[itemSoundCategory( 0xeac )], undefined );
});

test("equipment removal keeps the removed TID for sound while bag deletion stays silent", () => {
	const cues = [], owner = createInventory( () => {}, () => {}, c => cues.push( c ) );
	owner.bootstrap( fixture( [ row( 6, 1, sword ), row( 13, 2, robe ) ] ) );
	assert.throws( () => owner.receive( 0xb06d, Uint8Array.of( 1, 7, 6, 0 ) ) );
	assert.deepEqual( cues, [] );
	owner.receive( 0xb06d, Uint8Array.of( 1, 7, 6 ) );
	owner.receive( 0xb06d, Uint8Array.of( 1, 7, 13 ) );
	assert.deepEqual( owner.state().inventory, [] );
	assert.deepEqual( cues, [ { handle: "SND_EQUIP", typeFlags: sword } ] );
});

test("B06D crosses world journal and presentation into real audio owner playback; reset cancels pending cues", async t => {
	const requests = new Map(), sources = [], gains = [];
	let serial = 0, panners = 0;
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
		decodeAudioData( data ) {
			return Promise.resolve( {
				path: new TextDecoder().decode( data ),
				length: 100,
				sampleRate: 100,
				numberOfChannels: 1
			} );
		}
		createBufferSource() {
			const s = {
				...node(),
				start() {
					sources.push( this );
				},
				stop() {
					this.onended?.();
				}
			};
			return s;
		}
		createGain() {
			const n = { ...node(), gain: {} };
			gains.push( n );
			return n;
		}
		createPanner() {
			panners++;
			return { ...node(), positionX: {}, positionY: {}, positionZ: {} };
		}
	}
	const old = globalThis.AudioContext;
	globalThis.AudioContext = Context;
	t.after( () => {
		if ( old ) globalThis.AudioContext = old;
		else delete globalThis.AudioContext;
	} );
	const assets = {
		available: () => 4,
		request( url ) {
			requests.set( ++serial, url );
			return serial;
		},
		take( id ) {
			const url = requests.get( id );
			if ( !url ) return null;
			requests.delete( id );
			return { kind: "bytes", buffer: new TextEncoder().encode( new URL( url ).pathname ).buffer };
		},
		cancel( id ) {
			requests.delete( id );
		}
	};
	const audio = createAudio( assets, "http://fixture.invalid", createPresentationRandom( 1 ) ),
		core = createWorldCore( () => {} ),
		presentation = createPresentation();
	t.after( () => audio.dispose() );
	const flush = () => {
		core.step( 100000, false );
		const b = core.take();
		if ( b ) {
			presentation.apply( b );
			core.ack( b.sequence );
		}
	};
	core.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		character: { name: "fixture" },
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 257, x: 0, y: 0, z: 0, angle: 0 } },
		...fixture( [ row( 13, 1, sword ) ] )
	} );
	flush();
	core.receive( { opcode: 0xb06d, payload: move( 13, 6 ) }, 100000 );
	flush();
	const cues = presentation.takeSounds();
	assert.deepEqual( cues, [ { kind: "item-sound", cue: { handle: "SND_EQUIP", typeFlags: sword }, at: 100000 } ] );
	assert.deepEqual( presentation.takeSounds(), [] );
	audio.unlock();
	for ( const c of cues ) audio.nativeItem( c.cue, (1000 - Math.max( 0, 100000 - c.at )) / 1000 );
	for ( let i = 0; i < 8; i++ ) {
		audio.step( 1, [ 99999, 0, 99999 ] );
		await new Promise( setImmediate );
	}
	assert.equal( sources.length, 1 );
	assert.match( sources[0].buffer.path, /itsword.wav$/ );
	assert.equal( panners, 0 );
	assert.equal( gains[0].gain.value, Math.pow( 10, -.5 ), "master effects volume, no extra table attenuation" );
	audio.nativeItem( { handle: "SND_EQUIP", typeFlags: 0xeac }, 1 );
	audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( sources.length, 1 );
	audio.nativeItem( cues[0].cue, 1 );
	audio.reset();
	audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( sources.length, 1 );
	audio.nativeItem( { handle: "SND_DROPITEM", typeFlags: sword }, 1 );
	audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( sources.length, 1, "retail has no equipment drop sound row" );
	audio.nativeItem( { handle: "SND_DROPITEM", typeFlags: 0x2ec }, 1 );
	for ( let i = 0; i < 8; i++ ) {
		audio.step( 1, [ 99999, 0, 99999 ] );
		await new Promise( setImmediate );
	}
	assert.equal( sources.length, 2 );
	assert.match( sources[1].buffer.path, /itgold.wav$/ );
	assert.equal( panners, 0 );
});
