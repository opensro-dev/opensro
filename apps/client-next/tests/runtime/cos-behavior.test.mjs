/*
===========================================================================

cos-behavior.test.mjs - tests for gameplay.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
function fixture() {
	const owner = createGameplay( () => {} );
	owner.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 102, tidWord: 0x21c6 } ] } );
	owner.worldReady();
	owner.seed( {
		gid: 1,
		refObjId: 1,
		kind: "local-player",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "Owner"
	} );
	owner.take();
	// Band 4: gid, ref, hp, mp, command mode, empty name, status, dead, summon slot.
	owner.receive( {
		opcode: 0x3158,
		payload: Uint8Array.of(
			7,
			0,
			0,
			0,
			102,
			0,
			0,
			0,
			100,
			0,
			0,
			0,
			50,
			0,
			0,
			0,
			0x47,
			0,
			0,
			0,
			0,
			0,
			0,
			0,
			0,
			0,
			0,
			14
		)
	}, 0 );
	assert.equal( owner.take().cosRecords[0].commandMode, 0x47 );
	return owner;
}
const changed = Uint8Array.of( 1, 7, 0, 0, 0, 2, 0xc7, 0, 0, 0 );
test("native behavior acknowledgement updates the owning pet bitmask", () => {
	const owner = fixture();
	// 6A9910 / 6A1B10 / 6A2350 case 6 check CCosDataManager_FindCharData (830140)
	// only, never the visible entity table.
	assert.deepEqual(
		owner.command( { kind: "cos-behavior", gid: 7, mode: 0xc7 }, 0, undefined, undefined ),
		{ opcode: 0x705b, payload: Uint8Array.of( 7, 0, 0, 0, 2, 0xc7, 0, 0, 0 ) }
	);
	assert.throws(
		() => owner.command( { kind: "cos-behavior", gid: 99, mode: 0xc7 }, 0, undefined, undefined ),
		/No owned COS behavior authority/
	);
	owner.receive( { opcode: 0xb05b, payload: changed }, 0 );
	assert.equal( owner.take().cosRecords[0].commandMode, 0xc7 );
	owner.receive( { opcode: 0xb05b, payload: Uint8Array.of( 2, 3 ) }, 0 );
	const state = owner.take();
	assert.equal( state.cosRecords[0].commandMode, 0xc7 );
	assert.equal( state.error, null );
	assert.deepEqual( state.notices, [], "native category 12/code 3 is silent" );
	owner.receive( { opcode: 0xb05b, payload: Uint8Array.of( 2, 6 ) }, 0 );
	const refused = owner.take();
	assert.equal( refused.cosRecords[0].commandMode, 0xc7 );
	assert.equal( refused.notices.at( -1 ).key, "UIIT_MSG_COSERR_YOU_DONT_HAVE_ACTIVE_COS_OBJ" );
});
test("malformed and wrong-family behavior results cannot mutate a pet", () => {
	const owner = fixture();
	for ( let n = 0; n < changed.length; n++ ) {
		assert.throws( () => owner.receive( { opcode: 0xb05b, payload: changed.slice( 0, n ) }, 0 ) );
		assert.equal( owner.take(), null );
	}
	for ( const field of [ 1, 5 ] ) {
		const p = changed.slice();
		p[field] = field === 1 ? 0 : 1;
		assert.throws( () => owner.receive( { opcode: 0xb05b, payload: p }, 0 ) );
		assert.equal( owner.take(), null );
	}
	const removed = changed.slice();
	removed[1] = 9;
	owner.receive( { opcode: 0xb05b, payload: removed }, 0 );
	assert.equal( owner.take(), null );
	owner.dispose();
});

/*
================
companion removal

Thread "Wolf cub UI is gone, but Wolf is alive": a walled-in pet that
relocates to its owner is despawned and spawned again (cosformation.go),
and the despawn took its record and status icon with it. Natively only
the owner's 0x3508 case 1 (77A570 -> CIFCOSManager_RemoveCompanion
6F2900, server 4D2140) drops a companion; a despawn takes it out of view.
================
*/
test("a despawn keeps the owned companion; only its removal notice drops it", () => {
	const owner = fixture();
	owner.receive( { opcode: 0x36ab, payload: Uint8Array.of( 7, 0, 0, 0 ) }, 1 );
	const kept = owner.take();
	assert.deepEqual( (kept?.cosRecords ?? []).map( r => r.gid ), [ 7 ], "a despawn keeps the record" );
	// Another owner's removal names no record of ours.
	owner.receive( { opcode: 0x3508, payload: Uint8Array.of( 9, 0, 0, 0, 1 ) }, 2 );
	owner.receive( { opcode: 0x3508, payload: Uint8Array.of( 7, 0, 0, 0, 1 ) }, 3 );
	assert.deepEqual( owner.take().cosRecords, [], "the removal notice drops it" );
	assert.throws( () => owner.receive( { opcode: 0x3508, payload: Uint8Array.of( 7, 0, 0, 0, 1, 0 ) }, 4 ) );
});
