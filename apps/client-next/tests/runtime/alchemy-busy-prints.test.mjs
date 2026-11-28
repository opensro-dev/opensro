/*
===========================================================================

alchemy-busy-prints.test.mjs - the alchemy box refuses a stall visit and a trade

#2628: while the alchemy box shows, the native client refuses two
requests locally. A click on a stall keeper selects them, then prints
{0x12, 0x17} and neither walks nor visits (CGInterface_OnWorldClick
698C2E). The trade action prints {0x12, 0x19} and builds no 0x7237
(NetClient_SendTradeRequest7237 6FE914). Closed, both go on as before.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
const { createGameplay } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ) ).href
);

const REGION = 257;
const LOCAL = { regionId: REGION, x: 60, y: 10, z: 100, gid: 7, heading: 0, appearanceState: [ 1, 0, 0 ] };
// The stall-visit (0x761F), trade (0x7237) and predicted-walk frames.
const STALL_VISIT = 0x761f;
const TRADE_REQUEST = 0x7237;
const PREDICTED_MOVE = 9;
// CGInterface_ExecuteActionCommand 1000 + 6: trade with the selected player.
const ACTION_EXCHANGE = 1006;

/*
================
player

A player row at x in REGION; stall keepers carry title mode 4 in their
appearance state.
================
*/
function player( x, { stall = false } = {} ) {
	return {
		gid: 100 + x,
		refObjId: 1907,
		kind: "player",
		name: "p",
		regionId: REGION,
		x,
		y: 10,
		z: 100,
		heading: 0,
		...(stall ? { appearanceState: [ 0, 0, 0, 0, 0, 0, 4 ] } : {})
	};
}

/*
================
started

A seeded local player, with the alchemy box open when alchemy is set.
The world's entities are the rows of entities (the trade action reads
its target from the world).
================
*/
function started( alchemy, entities = new Map() ) {
	const sent = [], game = createGameplay( f => sent.push( f ), undefined, undefined, gid => entities.get( gid ) );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( LOCAL );
	game.step( 50, LOCAL );
	if ( alchemy ) game.command( { kind: "alchemy-open" }, 60 );
	game.take();
	sent.length = 0;
	return { game, sent };
}

const noticeKeys = game => (game.take()?.notices ?? []).map( notice => notice.key );

test("a stall keeper clicked behind the alchemy box is selected, refused with 12:17, never walked to", () => {
	for ( const x of [ 100, 400 ] ) {
		const { game, sent } = started( true );
		game.command( { kind: "select", gid: 100 + x }, 100, player( x, { stall: true } ), LOCAL );
		game.step( 120, LOCAL );
		assert.deepEqual( noticeKeys( game ), [ "UIIT_MSG_REINFORCERR_CANNOT_JOIN_FLEAMARKET" ], `x ${x}` );
		assert.equal( sent.some( f => f.opcode === STALL_VISIT ), false, `x ${x}: no visit` );
		assert.equal( sent.some( f => f.opcode === PREDICTED_MOVE ), false, `x ${x}: no walk` );
	}
});

test("with the alchemy box closed, a near stall keeper is visited", () => {
	const { game, sent } = started( false );
	game.command( { kind: "select", gid: 200 }, 100, player( 100, { stall: true } ), LOCAL );
	assert.equal( sent.some( f => f.opcode === STALL_VISIT ), true );
	assert.deepEqual( noticeKeys( game ), [] );
});

test("the trade action behind the alchemy box prints 12:19 and sends no request", () => {
	for ( const alchemy of [ true, false ] ) {
		const target = player( 100 );
		/** @type {Map<number, object>} */
		const world = new Map();
		world.set( LOCAL.gid, LOCAL ).set( target.gid, target );
		const { game, sent } = started( alchemy, world );
		game.command( { kind: "select", gid: target.gid }, 100, target, LOCAL );
		game.take();
		sent.length = 0;
		game.command( { kind: "action-command", id: ACTION_EXCHANGE }, 120, undefined, LOCAL );
		assert.equal( sent.some( f => f.opcode === TRADE_REQUEST ), !alchemy, `alchemy ${alchemy}` );
		assert.deepEqual(
			noticeKeys( game ),
			alchemy ? [ "UIIT_MSG_REINFORCERR_CANNOT_TRADE" ] : [],
			`alchemy ${alchemy}`
		);
	}
});
