/*
===========================================================================

fortress-official.test.mjs - the fortress official's protocol and window

0x71E1 requests, 0xB1E1 answers, the guild's registration notice and the
application window's slots and dates, against the native layouts
(703130, 754A40, 76C870, 663EB0, 660C70).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const fortress = await import( sourceFileUrl( "src/engine/foundation/gameplay/fortress.ts" ).href );
const apply = await import( sourceFileUrl( "src/engine/foundation/ui/fortress-war-apply.ts" ).href );

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
reply
================
*/
function reply( bytes ) {
	return { opcode: 0xb1e1, payload: Uint8Array.from( bytes ) };
}

test("the official's requests are 0x71E1 [npc][subtype][fortress][kind]", () => {
	assert.deepEqual( [ ...fortress.fortressInteraction( 0x01020304, 6 ).payload ], [ 4, 3, 2, 1, 6 ] );
	assert.deepEqual( [ ...fortress.fortressInteraction( 7, 7, 1, 0 ).payload ], [ 7, 0, 0, 0, 7, 1, 0, 0, 0, 0 ] );
	assert.equal( fortress.fortressInteraction( 7, 8, 1, 1 ).opcode, 0x71e1 );
});

test("0xB1E1 subtype 6 carries the war start SYSTEMTIME and the application", () => {
	const time = [ 0xea, 0x07, 10, 0, 3, 0, 7, 0, 20, 0, 0, 0, 0, 0, 0, 0 ];
	const fresh = fortress.fortressManagerReply( reply( [ 6, 1, ...time, 0 ] ), null );
	assert.deepEqual( fresh.application, {
		warStart: { year: 2026, month: 10, weekday: 3, day: 7, hour: 20, minute: 0 },
		applied: null
	} );
	const applied = fortress.fortressManagerReply( reply( [ 6, 1, ...time, 1, 1, 0, 0, 0, 0 ] ), null );
	assert.deepEqual( applied.application.applied, { fortress: 1, kind: 0 } );
	const withdrawn = fortress.fortressManagerReply( reply( [ 8, 1, 1, 0, 0, 0, 0 ] ), applied.application );
	assert.equal( withdrawn.application.applied, null );
	assert.equal( withdrawn.application.warStart.hour, 20 );
	assert.deepEqual( fortress.fortressManagerReply( reply( [ 7, 2, 0x0c ] ), null ), {
		ok: false,
		subtype: 7,
		code: 0x0c
	} );
	assert.equal( fortress.fortressManagerReply( reply( [ 2, 1 ] ), null ), null );
});

test("the guild hears its registration with the fortress name", () => {
	const notice = fortress.fortressRegistrationNotice( state, {
		opcode: 0x3887,
		payload: Uint8Array.of( 0x0c, 1, 0, 0, 0, 0 )
	} );
	assert.equal( notice.key, "UIIT_MSG_FORT_OFFICIAL_WARAPPLY_COMPLETE" );
	assert.deepEqual( notice.localizedArguments, [ "SN_FORTRESS_JANGAN" ] );
	const cancel = fortress.fortressRegistrationNotice( state, {
		opcode: 0x3887,
		payload: Uint8Array.of( 0x0d, 1, 0, 0, 0, 1 )
	} );
	assert.equal( cancel.key, "UIIT_MSG_FORT_OFFICIAL_UNIONAPPLY_CANCEL" );
});

test("a slot's button follows the guild's standing (663EB0)", () => {
	const owned = { ...state, wars: [ { id: 1, name: "Owners", flags: 0 } ] };
	let [slot] = apply.fortressWarSlots( owned, 1907, null, "Raiders", [] );
	assert.deepEqual( [ slot.caption, slot.enabled, slot.question ], [
		"UIIT_CTL_FORT_OFFICAL_OCCUPYAPPLY",
		true,
		0x64
	] );
	[slot] = apply.fortressWarSlots( owned, 1907, null, "Friends", [ "Owners" ] );
	assert.deepEqual( [ slot.caption, slot.question ], [ "UIIT_CTL_FORT_OFFICAL_UNIONAPPLY", 0x65 ] );
	[slot] = apply.fortressWarSlots(
		owned,
		1907,
		{ warStart: null, applied: { fortress: 1, kind: 0 } },
		"Raiders",
		[]
	);
	assert.deepEqual( [ slot.caption, slot.question ], [ "UIIT_CTL_FORT_OFFICAL_OCCUPYAPPLY_CANCEL", 0x66 ] );
	[slot] = apply.fortressWarSlots( owned, 1907, null, "Owners", [] );
	assert.equal( slot.enabled, false, "a guild holding a fortress cannot apply" );
	assert.equal(
		apply.fortressWarSlots( owned, 1, null, "Raiders", [] ).length,
		0,
		"another NPC is not this official"
	);
	assert.deepEqual( apply.fortressWarRequest( 0x67 ), { withdraw: true, request: 1 } );
});

test("the window's dates come from the war start (660C70)", () => {
	const dates = apply.fortressWarDates( { year: 2026, month: 10, weekday: 3, day: 7, hour: 20, minute: 0 } );
	assert.deepEqual( dates.war, [ 10, 7, 20, 22 ] );
	assert.deepEqual( dates.apply, [ 10, 4, 10, 6 ] );
	assert.equal(
		apply.fortressWarFormat( "%d month %d day %d hour ~ %d hour", dates.war ),
		"10 month 7 day 20 hour ~ 22 hour"
	);
});
