/*
===========================================================================

gm-monster-commands.test.mjs - GM commands through the real worker dispatcher

The composer alone cannot prove reference and selection ownership: commands
must reach the transport with the newest selection and obey session lifetime.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const GM_REQUEST = 0x75b6;
const SELECT_RESULT = 0xb45a;
const RELEASE_RESULT = 0xb4b3;
const DESPAWN = 0x36ab;
const MONSTER_A = 0x12345678;
const MONSTER_B = MONSTER_A + 1;

/*
================
fixture
================
*/
function fixture( gmPrivilege = true ) {
	/** @type {import("../../src/engine/contracts/network.ts").WireFrame[]} */
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ) );
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const local = {
		gid: 1,
		kind: "local-player",
		refObjId: 1907,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "GM fixture"
	};
	game.bootstrap( { character: { gmPrivilege } } );
	game.seed( local );
	game.bindReferences( {
		country: () => undefined,
		playerModels: () => [],
		item: () => undefined,
		monster: name =>
			name === "MOB_CH_TIGERWOMAN" ?
				{ refObjId: 1954, monsterType: 3 } :
				name === "MOB_GM_TRAINING_DUMMY_021" ?
				{ refObjId: 5000121, monsterType: 0 } :
				undefined,
		npcs: () => []
	} );
	return { game, sent, local };
}

/*
================
selectMonster
================
*/
function selectMonster( f, gid, grant = true ) {
	const monster = { ...f.local, gid, kind: "monster", refObjId: 1954, name: "Tiger Girl" };
	f.game.command( { kind: "select", gid }, 0, monster, f.local );
	if ( !grant ) return;
	const payload = Buffer.alloc( 14 );
	payload[0] = 1;
	payload.writeUInt32LE( gid, 1 );
	payload[5] = 1;
	f.game.receive( { opcode: SELECT_RESULT, payload }, 1 );
}

/*
================
commandPayload
================
*/
function commandPayload( f, line ) {
	const frame = f.game.command( { kind: "gm-command", line }, 2, undefined, f.local );
	assert.ok( frame );
	assert.equal( frame?.opcode, GM_REQUEST );
	assert.equal( f.sent.at( -1 ), frame, "The real dispatcher must send the composed frame" );
	return [ ...frame.payload ];
}

test("ZOE resolves Tiger Girl through the worker's monster catalog", () => {
	const f = fixture();
	try {
		assert.deepEqual( commandPayload( f, "/ZOE MOB_CH_TIGERWOMAN 2" ), [ 12, 0xa2, 7, 0, 0, 2 ] );
		assert.deepEqual( commandPayload( f, "/zoe MOB_CH_TIGERWOMAN 2" ), [ 12, 0xa2, 7, 0, 0, 2 ] );
	} finally {
		f.game.dispose();
	}
});

test("MOBKILL uses the latest click while its grant is still pending", () => {
	const f = fixture();
	try {
		selectMonster( f, MONSTER_A );
		assert.deepEqual( commandPayload( f, "/mobkill 0" ), [ 20, 0x78, 0x56, 0x34, 0x12, 0, 0 ] );
		selectMonster( f, MONSTER_B, false );
		assert.deepEqual( commandPayload( f, "/MOBKILL 1" ), [ 20, 0x79, 0x56, 0x34, 0x12, 1, 0 ] );
	} finally {
		f.game.dispose();
	}
});

test("MOBKILL never reuses a released, removed or reset selection", () => {
	for ( const end of [ "release", "despawn", "reset" ] ) {
		const f = fixture();
		try {
			selectMonster( f, MONSTER_A );
			if ( end === "release" ) {
				f.game.command( { kind: "release-target" }, 2, undefined, f.local );
			} else if ( end === "despawn" ) {
				const payload = Buffer.alloc( 4 );
				payload.writeUInt32LE( MONSTER_A );
				f.game.receive( { opcode: DESPAWN, payload }, 2 );
			} else {
				f.game.reset();
				f.game.bootstrap( { character: { gmPrivilege: true } } );
				f.game.seed( f.local );
			}
			const before = f.sent.length;
			assert.equal( f.game.command( { kind: "gm-command", line: "/mobkill 0" }, 3, undefined, f.local ), null );
			assert.equal( f.sent.length, before, end );
			if ( end === "release" ) {
				f.game.receive( { opcode: RELEASE_RESULT, payload: Uint8Array.of( 1 ) }, 4 );
				assert.equal(
					f.game.command( { kind: "gm-command", line: "/mobkill 1" }, 5, undefined, f.local ),
					null
				);
			}
		} finally {
			f.game.dispose();
		}
	}
});

test("non-GMs cannot send monster commands through gameplay", () => {
	const f = fixture( false );
	try {
		selectMonster( f, MONSTER_A );
		const before = f.sent.length;
		for ( const line of [ "/MOBKILL 0", "/MOBKILL 1", "/ZOE MOB_CH_TIGERWOMAN 2", "/TRAININGDUMMY 21" ] ) {
			assert.equal( f.game.command( { kind: "gm-command", line }, 3, undefined, f.local ), null );
		}
		assert.equal( f.sent.length, before );
	} finally {
		f.game.dispose();
	}
});
