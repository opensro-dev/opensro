/*
===========================================================================

fortress-battle.test.mjs - native local and named battle-rank notices

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { fortressBattleRankNotice, fortressBattleRanks } = await import(
	"../../src/engine/foundation/gameplay/fortress.ts"
);

/*
================
frame
================
*/
function frame( payload ) {
	return { opcode: 0x3887, payload: Uint8Array.from( payload ) };
}

test("all six ranks name the local character in banner and chat", () => {
	for ( let rank = 1; rank <= 6; rank++ ) {
		const notice = fortressBattleRankNotice( frame( [ 14, 1, rank ] ), "Player" );
		assert.deepEqual( notice, {
			key: "UIIT_MSG_FORT_BATTLERANK_GRANT",
			value: 0,
			arguments: [ "Player", "" ],
			localizedArguments: [ null, fortressBattleRanks()[rank - 1].name ],
			banner: true
		} );
	}
	assert.equal( fortressBattleRankNotice( frame( [ 14, 1, 1 ] ), "" ), null );
});

test("commander broadcast uses the named character and checks complete framing", () => {
	const notice = fortressBattleRankNotice( frame( [ 14, 0, 4, 0, ...Buffer.from( "Hero" ), 6 ] ), "Me" );
	assert.deepEqual( notice?.arguments, [ "Hero", "" ] );
	assert.deepEqual( notice?.localizedArguments, [ null, "SN_SKILL_COMBAT_COMMANDER" ] );
	for ( const bytes of [ [ 14 ], [ 14, 1 ], [ 14, 0, 4, 0, 65, 6 ], [ 14, 1, 1, 0 ] ] ) {
		assert.throws( () => fortressBattleRankNotice( frame( bytes ), "Me" ) );
	}
	assert.equal( fortressBattleRankNotice( frame( [ 17 ] ), "Me" ), null );
});
