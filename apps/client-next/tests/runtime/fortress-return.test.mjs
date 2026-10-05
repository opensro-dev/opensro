/*
===========================================================================

fortress-return.test.mjs - the action window's "Return to fortress" (1015)

CGInterface_ExecuteActionCommand (695420) case 1015 against the 0x7025
request it sends, the 0x1F refusals it raises itself, and the 0x3792 /
0xB025 replies the server answers with.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const portal = await import( "../../src/engine/foundation/gameplay/fortress-return.ts" );

const bootstrap = fortress.fortressBootstrap( {
	siegeFortressData: [ {
		fortressId: 1,
		codeName: "FORTRESS_JANGAN",
		nameStrId: "SN_FORTRESS_JANGAN",
		officialNpcCode: "NPC_CH_FORTRESS_OFFICIAL",
		officialRefObjId: 1907,
		requestFee: 5000000
	} ]
} );

/*
================
occupied

The bootstrap state with Jangan held by guild "Wolves".
================
*/
function occupied() {
	return { ...bootstrap, wars: [ { id: 1, name: "Wolves", flags: 0 } ] };
}

test("the occupier asks for its own fortress", () => {
	const request = portal.fortressReturnRequest( occupied(), "Wolves", 0 );
	assert.ok( "frame" in request );
	assert.equal( request.frame.opcode, 0x7025 );
	assert.deepEqual( [ ...request.frame.payload ], [ 1, 0, 0, 0 ] );
});

test("the client refuses itself without a fortress or during the cooldown", () => {
	assert.deepEqual( portal.fortressReturnRequest( occupied(), undefined, 0 ), { code: 7 } );
	assert.deepEqual( portal.fortressReturnRequest( occupied(), "Ravens", 0 ), { code: 7 } );
	assert.deepEqual( portal.fortressReturnRequest( occupied(), "Wolves", 1 ), { code: 8 } );
});

test("0x3792 kind 5 starts the cooldown and 0xB025 carries the refusal", () => {
	const cooldown = { opcode: 0x3792, payload: Uint8Array.of( 2, 5, 0x58, 0x02, 0, 0 ) };
	assert.equal( portal.fortressPortalCooldown( cooldown ), 600 );
	assert.equal(
		portal.fortressPortalCooldown( { opcode: 0x3792, payload: Uint8Array.of( 2, 4, 0, 0, 0, 0 ) } ),
		null
	);
	assert.equal( portal.fortressReturnRefusal( { opcode: 0xb025, payload: Uint8Array.of( 2, 8 ) } ), 8 );
	assert.equal( portal.fortressReturnRefusal( { opcode: 0xb025, payload: Uint8Array.of( 1 ) } ), null );
	assert.throws( () => portal.fortressReturnRefusal( { opcode: 0xb025, payload: Uint8Array.of( 2 ) } ) );
});
