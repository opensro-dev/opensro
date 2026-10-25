/*
===========================================================================

gm-local-commands.test.mjs - the client-local GM commands (#694-#697)

The classifier and the waypoint list are pure; the worker tests run the
real dispatcher so the cast and warp frames are proven on the wire, and
a non-GM session is proven to reach none of it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const {
	gmLocalCommand,
	gmWaypointAdd,
	gmWaypointDelete,
	gmWaypointShow,
	gmWaypointWarpFrame
} = await import( "../../src/engine/foundation/gameplay/gm-local-commands.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const GM_REQUEST = 0x75b6;
const SKILL_REQUEST = 0x72cd;
const HARMONY_ID = 72001;

test("the classifier takes each local command in its native shape", () => {
	assert.deepEqual( gmLocalCommand( "/TEXTCHAT UIIT_MSG_HELLO" ), { type: "textchat", key: "UIIT_MSG_HELLO" } );
	assert.deepEqual( gmLocalCommand( "/textchat UIIT_MSG_HELLO" ), { type: "textchat", key: "UIIT_MSG_HELLO" } );
	assert.deepEqual( gmLocalCommand( "/SMSG server restart in 5 minutes" ), {
		type: "smsg",
		text: "server restart in 5 minutes"
	} );
	assert.deepEqual( gmLocalCommand( "/SMSG" ), { type: "consumed" } );
	assert.deepEqual( gmLocalCommand( "INPUT_CHAT notice" ), { type: "inputchat", arg: "notice" } );
	assert.deepEqual( gmLocalCommand( "/useskill 1234" ), { type: "useskill", skillId: 1234 } );
	// A zero or unparseable id is matched natively and casts nothing.
	assert.deepEqual( gmLocalCommand( "/useskill 0" ), { type: "consumed" } );
	assert.deepEqual( gmLocalCommand( "/useskill abc" ), { type: "consumed" } );
	assert.deepEqual( gmLocalCommand( "/GMSKILL 0" ), { type: "gmskill" } );
	assert.deepEqual( gmLocalCommand( "/GMSKILL abc" ), { type: "gmskill" } );
	// A nonzero /GMSKILL argument jumps past the cast natively (509DFF).
	assert.deepEqual( gmLocalCommand( "/GMSKILL 55" ), { type: "consumed" } );
	assert.deepEqual( gmLocalCommand( "/GACHASTART" ), { type: "gachastart" } );
	// /GACHASTART has no token-count gate: extra tokens are ignored.
	assert.deepEqual( gmLocalCommand( "/GACHASTART now" ), { type: "gachastart" } );
	assert.deepEqual( gmLocalCommand( "IMPLE_COMMAND_ADD_WP JanganGate" ), { type: "addwp", name: "JanganGate" } );
	assert.deepEqual( gmLocalCommand( "IMPLE_COMMAND_DEL_WP JanganGate" ), { type: "delwp", name: "JanganGate" } );
	assert.deepEqual( gmLocalCommand( "IMPLE_COMMAND_SHOW_WP" ), { type: "showwp" } );
	assert.deepEqual( gmLocalCommand( "/warp JanganGate" ), { type: "warpwp", name: "JanganGate" } );
	assert.deepEqual( gmLocalCommand( "/window 1" ), { type: "window", windowed: true } );
	assert.deepEqual( gmLocalCommand( "/window 0" ), { type: "window", windowed: false } );
});

test("the classifier leaves the server family and plain lines alone", () => {
	// The coordinate /warp belongs to gmRequest (0x75B6 subcmd 0x10).
	assert.equal( gmLocalCommand( "/warp 257 1 2 3" ), null );
	assert.equal( gmLocalCommand( "/MAKEITEM ITEM_CH_SWORD_01_A 1" ), null );
	assert.equal( gmLocalCommand( "/FINDUSER Someone" ), null );
	assert.equal( gmLocalCommand( "hello world" ), null );
	assert.equal( gmLocalCommand( "/useskill" ), null );
	assert.equal( gmLocalCommand( "/window" ), null );
	assert.equal( gmLocalCommand( "/TEXTCHAT" ), null );
	// The GMSKILL and INPUT_CHAT arms gate on exactly two tokens
	// (509DAF cmp ebp, 0x2; 50BF51 cmp ebx, 0x2): a bare line is not
	// handled by them at all.
	assert.equal( gmLocalCommand( "/GMSKILL" ), null );
	assert.equal( gmLocalCommand( "/GMSKILL 1 2" ), null );
	assert.equal( gmLocalCommand( "INPUT_CHAT" ), null );
	assert.equal( gmLocalCommand( "INPUT_CHAT a b" ), null );
});

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
		x: 100.5,
		y: 20.25,
		z: 30.75,
		heading: 1234,
		name: "GM fixture"
	};
	game.bootstrap( {
		character: { gmPrivilege },
		refSkillSnapshot: [ {
			id: HARMONY_ID,
			group: 1,
			level: 1,
			status: false,
			effectRider: false,
			ui: {
				name: "SKILL_OP_HARMONY_A_01",
				trainable: false,
				targetRequired: false,
				spCost: 0,
				cooldownMs: 0,
				masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
				prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
			}
		} ]
	} );
	game.seed( local );
	game.bindReferences( {
		country: () => undefined,
		playerModels: () => [],
		item: () => undefined,
		monster: () => undefined,
		npcs: () => []
	} );
	return { game, sent, local };
}

test("waypoints save, list, warp and delete through the worker", () => {
	const f = fixture();
	try {
		assert.equal(
			f.game.command( { kind: "gm-command", line: "IMPLE_COMMAND_ADD_WP JanganGate" }, 2, undefined, f.local ),
			null
		);
		assert.deepEqual(
			f.game.take()?.gmReplies?.map( reply => reply.text ),
			[ "New WP : JanganGate", "Save WP Count : 1" ]
		);
		assert.equal(
			f.game.command( { kind: "gm-command", line: "IMPLE_COMMAND_SHOW_WP" }, 3, undefined, f.local ),
			null
		);
		assert.deepEqual(
			f.game.take()?.gmReplies?.slice( -2 ).map( reply => reply.text ),
			[ "Save WP Count : 1", "JanganGate" ]
		);
		// /warp <name> composes the 0x75B6 coordinate warp from the record.
		const frame = f.game.command( { kind: "gm-command", line: "/warp JanganGate" }, 4, undefined, f.local );
		assert.equal( frame?.opcode, GM_REQUEST );
		const view = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength );
		assert.equal( frame.payload[0], 0x10 );
		assert.equal( view.getUint16( 1, true ), 257 );
		assert.equal( view.getFloat32( 3, true ), 100.5 );
		assert.equal( view.getFloat32( 7, true ), 20.25 );
		assert.equal( view.getFloat32( 11, true ), 30.75 );
		assert.equal( view.getUint16( 15, true ), 1234 );
		// A bad name prints the native line and sends nothing.
		const before = f.sent.length;
		assert.equal(
			f.game.command( { kind: "gm-command", line: "/warp Nowhere" }, 5, undefined, f.local ),
			null
		);
		assert.equal( f.sent.length, before );
		assert.deepEqual( f.game.take()?.gmReplies?.slice( -1 ).map( reply => reply.text ), [ "Illigal WP Name" ] );
		assert.equal(
			f.game.command( { kind: "gm-command", line: "IMPLE_COMMAND_DEL_WP JanganGate" }, 6, undefined, f.local ),
			null
		);
		assert.deepEqual( f.game.take()?.gmReplies?.slice( -1 ).map( reply => reply.text ), [ "Save WP Count : 0" ] );
	} finally {
		f.game.dispose();
	}
});

test("the waypoint list ops answer in the native print shapes", () => {
	const spot = { name: "A", regionId: 1, x: 1, y: 2, z: 3, heading: 4 };
	const added = gmWaypointAdd( [], spot );
	assert.deepEqual( added.prints, [ "New WP : A", "Save WP Count : 1" ] );
	// Re-saving a name keeps the count and drops the New line.
	const resaved = gmWaypointAdd( added.waypoints, { ...spot, x: 9 } );
	assert.deepEqual( resaved.prints, [ "Save WP Count : 1" ] );
	assert.equal( resaved.waypoints[0]?.x, 9 );
	assert.deepEqual( gmWaypointShow( resaved.waypoints ), [ "Save WP Count : 1", "A" ] );
	assert.deepEqual( gmWaypointDelete( resaved.waypoints, "missing" ).prints, [ "Illigal WP Name" ] );
	assert.deepEqual( gmWaypointWarpFrame( resaved.waypoints, "missing" ).prints, [ "Illigal WP Name" ] );
	const warp = gmWaypointWarpFrame( resaved.waypoints, "a" );
	assert.equal( warp.frame?.opcode, GM_REQUEST );
	assert.equal( warp.frame?.payload.length, 17 );
});

test("/useskill and /GMSKILL cast through the worker's press door", () => {
	const f = fixture();
	try {
		const used = f.game.command( { kind: "gm-command", line: "/useskill 4242" }, 2, undefined, f.local );
		assert.equal( used?.opcode, SKILL_REQUEST );
		assert.deepEqual( [ ...used.payload ], [ 1, 4, 0x92, 0x10, 0, 0, 0 ] );
		const harmony = f.game.command( { kind: "gm-command", line: "/GMSKILL 0" }, 3, undefined, f.local );
		assert.equal( harmony?.opcode, SKILL_REQUEST );
		const view = new DataView( harmony.payload.buffer, harmony.payload.byteOffset, harmony.payload.byteLength );
		assert.equal( view.getUint32( 2, true ), HARMONY_ID );
		// A bare /GMSKILL never reaches the cast (509DAF count gate).
		const bareBefore = f.sent.length;
		assert.equal( f.game.command( { kind: "gm-command", line: "/GMSKILL" }, 5, undefined, f.local ), null );
		assert.equal( f.sent.length, bareBefore );
		// A nonzero /GMSKILL argument is the native no-op.
		const before = f.sent.length;
		assert.equal( f.game.command( { kind: "gm-command", line: "/GMSKILL 55" }, 4, undefined, f.local ), null );
		assert.equal( f.sent.length, before );
	} finally {
		f.game.dispose();
	}
});

test("a non-GM session reaches none of the local commands", () => {
	const f = fixture( false );
	try {
		const before = f.sent.length;
		for (
			const line of [
				"/useskill 4242",
				"/GMSKILL 0",
				"IMPLE_COMMAND_ADD_WP JanganGate",
				"IMPLE_COMMAND_SHOW_WP",
				"/warp JanganGate"
			]
		) {
			assert.equal( f.game.command( { kind: "gm-command", line }, 3, undefined, f.local ), null, line );
		}
		assert.equal( f.sent.length, before );
		assert.deepEqual( f.game.take()?.gmReplies ?? [], [] );
	} finally {
		f.game.dispose();
	}
});
