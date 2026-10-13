/*
===========================================================================

death-notice.test.mjs - the 0x37CE death notice sets the rebirth mode

CPSMission_OnDeathNoticeSetRebirthMode (74FE50) reads one byte, the death
kind, and stores it at CGInterface+0x640 (67B2E0); the death box labels its
first button from it. A new login starts at zero; a teleport keeps it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

test("the death notice carries the death kind into gameplay state", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	assert.equal( game.take()?.rebirthMode, 0 );
	assert.equal( game.receive( { opcode: 0x37ce, payload: Uint8Array.of( 2 ) }, 0 ), true );
	assert.equal( game.take()?.rebirthMode, 2, "a team death" );
	assert.throws(
		() => game.receive( { opcode: 0x37ce, payload: Uint8Array.of( 2, 0 ) }, 0 ),
		/Invalid death notice/
	);
	game.bootstrap( {}, true );
	assert.equal( game.take()?.rebirthMode, 2, "a continued entry keeps the interface" );
	game.bootstrap( {} );
	assert.equal( game.take()?.rebirthMode, 0, "a new login starts at zero" );
	game.dispose();
});
