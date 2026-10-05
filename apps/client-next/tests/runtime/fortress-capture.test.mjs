/*
===========================================================================

fortress-capture.test.mjs - the fortress war's capture frames

0x3887 subtypes 8 (conquest), 0x0A (the stone's guard falls) and 0x0B (a
structure's state) against CNetProcessSecond_OnFortressWarState3887
(76C870) cases 8, 0xA and 0xB.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );

const state = fortress.fortressBootstrap( {
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
frame
================
*/
function frame( bytes ) {
	return { opcode: 0x3887, payload: Uint8Array.from( bytes ) };
}

const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ];
const str = s => [ s.length & 255, s.length >>> 8, ...Buffer.from( s ) ];

test("subtype 8 names the guild and the fortress it took", () => {
	const notice = defined(
		fortress.fortressCaptureNotice(
			state,
			frame( [ 8, ...u32( 1 ), ...str( "Raiders" ), ...u32( 77 ), ...u32( 0 ), ...u32( 0 ), ...u32( 0 ) ] ),
			undefined
		)
	);
	assert.equal( notice.key, "UIIT_MSG_FORT_WAR_CONQUER" );
	assert.deepEqual( notice.arguments, [ "Raiders", "" ] );
	assert.deepEqual( notice.localizedArguments, [ null, "SN_FORTRESS_JANGAN" ] );
});

test("subtype 0xA announces the stone's falling guard", () => {
	const notice = defined( fortress.fortressCaptureNotice( state, frame( [ 0x0a, ...u32( 1 ) ] ), undefined ) );
	assert.equal( notice.key, "UIIT_MSG_FORT_STRUCTURE_STATUS_CANCEL" );
});

test("subtype 0xB reads a structure's state and names a destroyed one", () => {
	const row = frame( [ 0x0b, ...u32( 1 ), ...u32( 400123 ), ...u32( 85 ), 1, 0 ] );
	assert.deepEqual( fortress.fortressStructureState( row ), {
		fortressId: 1,
		gid: 400123,
		eventStructId: 85,
		state: 1
	} );
	const notice = defined(
		fortress.fortressCaptureNotice( state, row, "Guard Tower" )
	);
	assert.deepEqual( [ notice.key, notice.arguments ], [ "UIIT_MSG_FORT_STRUCTURE_STATUS_DESTROY", [
		"Guard Tower"
	] ] );
	assert.equal(
		fortress.fortressCaptureNotice( state, row, undefined ),
		null,
		"an unseen structure has no name"
	);
	const camp = frame( [ 0x0b, ...u32( 1 ), ...u32( 400124 ), ...u32( 91 ), 0, 0, ...str( "Raiders" ) ] );
	assert.equal( fortress.fortressStructureState( camp )?.guildName, "Raiders" );
	assert.deepEqual( defined( fortress.fortressCaptureNotice( state, camp, "Headquarters" ) ).arguments, [
		"Raiders",
		"Headquarters"
	] );
});
