/*
===========================================================================

cos-clean.test.mjs - the Clean command's wire and its 0xB618 answer

6A2350 case 5 retires a riding mount or transport with 0x7618 [u32 gid]
(6FF800); 7782A0 reads [1] or [2][code]. Pets cannot be cleaned.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const LOCAL = {
	gid: 1,
	refObjId: 1,
	kind: "local-player",
	regionId: 0x6b4f,
	x: 0,
	y: 0,
	z: 0,
	heading: 0,
	name: "Owner"
};

/*
================
owned

A gameplay owner holding one COS record of the given band, as 0x3158 seeds
it (830EC0): gid, ref, hp, mp, the attack pet's growth (exp, level,
satiety), a pet's command mode and empty name, status 0, past riding mounts
a dead flag, and a pet's summoner slot.
================
*/
function owned( band, tidWord ) {
	const sent = [];
	const gameplay = createGameplay( frame => sent.push( frame ) );
	gameplay.bootstrap( { refObjSnapshot: [ { kind: "cos", refObjId: 9, tidWord } ] } );
	gameplay.seed( LOCAL );
	const RECORD_BYTES = { 1: 17, 2: 21, 3: 39, 4: 28 };
	const record = new Uint8Array( RECORD_BYTES[band] );
	const view = new DataView( record.buffer );
	view.setUint32( 0, 2, true );
	view.setUint32( 4, 9, true );
	view.setUint32( 8, 100, true );
	gameplay.receive( { opcode: 0x3158, payload: record }, 0 );
	const cos = { ...LOCAL, gid: 2, refObjId: 9, kind: "cos", ownerGid: 1 };
	return { gameplay, sent, cos };
}

test("Clean retires a riding mount or a transport with 0x7618", () => {
	for ( const [band, tid] of [ [ 1, 0x09c6 ], [ 2, 0x11c6 ] ] ) {
		const { gameplay, sent, cos } = owned( band, tid );
		gameplay.command( { kind: "cos-clean", gid: 2 }, 1, cos, LOCAL );
		assert.deepEqual( sent, [ { opcode: 0x7618, payload: Uint8Array.of( 2, 0, 0, 0 ) } ], `band ${band}` );
		if ( band === 2 ) {
			assert.throws( () =>
				gameplay.command( { kind: "cos-clean", gid: 2 }, 2, { ...cos, ownerGid: 99 }, LOCAL )
			);
		}
	}
});

test("a riding mount spawns without an owner GID and is still cleaned", () => {
	// 0x3015 carries an owner GID only for bands other than 1; the 0x3158
	// record's GID is what makes the horse ours.
	const { gameplay, sent, cos } = owned( 1, 0x09c6 );
	gameplay.command( { kind: "cos-clean", gid: 2 }, 1, { ...cos, ownerGid: undefined }, LOCAL );
	assert.deepEqual( sent, [ { opcode: 0x7618, payload: Uint8Array.of( 2, 0, 0, 0 ) } ] );
	assert.throws( () =>
		gameplay.command( { kind: "cos-clean", gid: 2 }, 2, { ...cos, gid: 3, ownerGid: undefined }, LOCAL )
	);
});

test("pets cannot be cleaned", () => {
	const { gameplay, sent, cos } = owned( 4, 0x21c6 );
	assert.throws( () => gameplay.command( { kind: "cos-clean", gid: 2 }, 1, cos, LOCAL ), /clean/ );
	assert.equal( sent.length, 0 );
});

test("0xB618 accepts success and the failure grammar only", () => {
	const { gameplay } = owned( 1, 0x09c6 );
	gameplay.receive( { opcode: 0xb618, payload: Uint8Array.of( 1 ) }, 1 );
	gameplay.receive( { opcode: 0xb618, payload: Uint8Array.of( 2, 5 ) }, 1 );
	for ( const payload of [ new Uint8Array(), Uint8Array.of( 2 ), Uint8Array.of( 1, 0 ), Uint8Array.of( 3 ) ] ) {
		assert.throws( () => gameplay.receive( { opcode: 0xb618, payload }, 2 ), /cleanup/ );
	}
});

test("an attack pet attacks the selected monster with 0x769E tag 2", () => {
	const { gameplay, sent } = owned( 3, 0x19c6 );
	const monster = { ...LOCAL, gid: 40, kind: "monster" };
	gameplay.command( { kind: "cos-pet-attack", gid: 40, pet: 2 }, 1, monster, LOCAL );
	assert.deepEqual( sent, [ { opcode: 0x769e, payload: Uint8Array.of( 2, 0, 0, 0, 2, 40, 0, 0, 0 ) } ] );
	// 6A2350 case 2 also sends a pet at a player the core admitted
	// (player-attack.ts); nothing else is a target.
	gameplay.command( { kind: "cos-pet-attack", gid: 41, pet: 2 }, 2, { ...monster, gid: 41, kind: "player" }, LOCAL );
	assert.deepEqual( sent[1], { opcode: 0x769e, payload: Uint8Array.of( 2, 0, 0, 0, 2, 41, 0, 0, 0 ) } );
	assert.throws( () =>
		gameplay.command( { kind: "cos-pet-attack", gid: 40, pet: 2 }, 3, { ...monster, kind: "npc" }, LOCAL )
	);
	const pickup = owned( 4, 0x21c6 );
	assert.throws( () => pickup.gameplay.command( { kind: "cos-pet-attack", gid: 40, pet: 2 }, 1, monster, LOCAL ) );
	assert.equal( pickup.sent.length, 0, "a pickup pet does not attack" );
});
