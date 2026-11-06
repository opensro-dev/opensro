/*
===========================================================================

academy-buff.test.mjs - the Academy cumulated buff on the board

The saved-buff state frame (0x3CC1, 770E70), the received-buff list
(0xB00C, 770970), their system-chat prints and the magic-state
board's slots 7 and 8, including the slot-7 pass the Guardian
confirm sends as the empty 0x700C.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { academyPacket, academyPassBuffRequest } = await import(
	"../../src/engine/foundation/gameplay/academy.ts"
);
const { academyBuffNotices } = await import( "../../src/engine/foundation/gameplay/academy-notices.ts" );
const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const packetOf = ( state, frame, nowMs ) => defined( academyPacket( state, frame, nowMs ), "academy packet" );

/*
================
state

A camp member's academy state with a master and two students.
================
*/
function state() {
	return {
		member: true,
		page: 0,
		pages: 0,
		rows: [],
		request: null,
		result: null,
		localMemberId: 11,
		members: [
			{ id: 10, name: "Master", kind: 0 },
			{ id: 11, name: "Me", kind: 2 },
			{ id: 12, name: "Donor", kind: 2 }
		]
	};
}

/*
================
buffStateFrame

[u8 state] plus, for the show states, [u32 minutes][u32 value].
================
*/
function buffStateFrame( buffState, minutes, value ) {
	const payload = new Uint8Array( minutes === undefined ? 1 : 9 ), view = new DataView( payload.buffer );
	payload[0] = buffState;
	if ( minutes !== undefined ) {
		view.setUint32( 1, minutes, true );
		view.setUint32( 5, value, true );
	}
	return { opcode: 0x3cc1, payload };
}

/*
================
buffListFrame

[u8 mode][i8 count] plus [u32 minutes][u32 member id][u32 value] rows.
================
*/
function buffListFrame( mode, rows = [] ) {
	const payload = new Uint8Array( mode === 2 ? 1 : 2 + rows.length * 12 ),
		view = new DataView( payload.buffer );
	payload[0] = mode;
	if ( mode !== 2 ) {
		payload[1] = rows.length;
		rows.forEach( ( row, i ) => {
			view.setUint32( 2 + i * 12, row[0], true );
			view.setUint32( 2 + i * 12 + 4, row[1], true );
			view.setUint32( 2 + i * 12 + 8, row[2], true );
		} );
	}
	return { opcode: 0xb00c, payload };
}

test("a 0x3CC1 show state holds the saved buff; the clears empty it", () => {
	const shown = packetOf( state(), buffStateFrame( 0, 1400, 25 ), 5000 );
	assert.deepEqual( shown.savedBuff, { minutes: 1400, value: 25, receivedAtMs: 5000 } );
	const entered = packetOf( state(), buffStateFrame( 1, 900, 40 ), 6000 );
	assert.deepEqual( entered.savedBuff, { minutes: 900, value: 40, receivedAtMs: 6000 } );
	for ( const clear of [ 5, 8, 9 ] ) {
		const cleared = packetOf( shown, buffStateFrame( clear ), 7000 );
		assert.equal( cleared.savedBuff, undefined, "state " + clear );
	}
	const ignored = packetOf( shown, buffStateFrame( 2 ), 7000 );
	assert.deepEqual( ignored.savedBuff, shown.savedBuff, "state 2 is read and ignored" );
	assert.throws(
		() => academyPacket( state(), { opcode: 0x3cc1, payload: Uint8Array.of( 0, 1, 2 ) }, 0 ),
		/Truncated/
	);
	assert.throws(
		() => academyPacket( state(), { opcode: 0x3cc1, payload: Uint8Array.of( 5, 1 ) }, 0 ),
		/Trailing/
	);
});

test("the expired state shows like the others and prints its line", () => {
	const expired = packetOf( state(), buffStateFrame( 7, 0, 25 ), 5000 );
	assert.deepEqual( expired.savedBuff, { minutes: 0, value: 25, receivedAtMs: 5000 } );
	assert.deepEqual( academyBuffNotices( state(), buffStateFrame( 7, 0, 25 ) ), [
		{ key: "UIIT_CTL_TC_SAVE_BUFF_TIME_OVER", value: 0, nativeType: 0 }
	] );
	assert.deepEqual( academyBuffNotices( state(), buffStateFrame( 0, 100, 1 ) ), [] );
});

test("0xB00C rows upsert by donor and leave by the removal modes", () => {
	const added = packetOf( state(), buffListFrame( 0, [ [ 700, 12, 30 ], [ 300, 10, 5 ] ] ), 1000 );
	assert.deepEqual( added.receivedBuffs, [
		{ memberId: 12, minutes: 700, value: 30, receivedAtMs: 1000 },
		{ memberId: 10, minutes: 300, value: 5, receivedAtMs: 1000 }
	] );
	const refreshed = packetOf( added, buffListFrame( 1, [ [ 690, 12, 20 ] ] ), 2000 );
	assert.deepEqual( refreshed.receivedBuffs, [
		{ memberId: 12, minutes: 690, value: 20, receivedAtMs: 2000 },
		{ memberId: 10, minutes: 300, value: 5, receivedAtMs: 1000 }
	] );
	for ( const mode of [ 5, 8, 9 ] ) {
		const removed = packetOf( refreshed, buffListFrame( mode, [ [ 0, 12, 0 ] ] ), 3000 );
		assert.deepEqual( removed.receivedBuffs, [
			{ memberId: 10, minutes: 300, value: 5, receivedAtMs: 1000 }
		], "mode " + mode );
	}
	const refused = packetOf( refreshed, buffListFrame( 2 ), 3000 );
	assert.equal( refused, refreshed, "mode 2 changes no state" );
	assert.throws( () => academyPacket( state(), buffListFrame( 3, [] ), 0 ), /Invalid academy buff list mode/ );
});

test("the pass refusal and the received lines print to system chat", () => {
	assert.deepEqual( academyBuffNotices( state(), buffListFrame( 2 ) ), [
		{ key: "UIIT_MSG_TC_PASS_ERROR", value: 0, nativeType: 0 }
	] );
	assert.deepEqual( academyBuffNotices( state(), buffListFrame( 0, [ [ 700, 12, 30 ] ] ) ), [
		{ key: "UIIT_MSG_TC_RECIEVE_BUFF", value: 0, nativeType: 0, arguments: [ "Donor" ] }
	] );
	// The master donor and an unknown donor print nothing (81AFB0).
	assert.deepEqual( academyBuffNotices( state(), buffListFrame( 0, [ [ 700, 10, 30 ] ] ) ), [] );
	assert.deepEqual( academyBuffNotices( state(), buffListFrame( 0, [ [ 700, 99, 30 ] ] ) ), [] );
	// A refresh (mode 1) is silent.
	assert.deepEqual( academyBuffNotices( state(), buffListFrame( 1, [ [ 700, 12, 30 ] ] ) ), [] );
});

test("the pass request is the empty 0x700C", () => {
	const frame = academyPassBuffRequest();
	assert.equal( frame.opcode, 0x700c );
	assert.equal( frame.payload.length, 0 );
});

/*
================
helpCopy

The board's copy with each key spelled out for exact assertions.
================
*/
function helpCopy( key ) {
	return "<" + key + ">";
}

test("the board shows the saved and received rows with native help", () => {
	const game = /** @type {any} */ ({
		localGid: 1,
		academy: {
			...state(),
			savedBuff: { minutes: 1300, value: 25, receivedAtMs: 0 },
			receivedBuffs: [ { memberId: 12, minutes: 700, value: 30, receivedAtMs: 0 } ]
		}
	});
	const board = buffBoard( game, 0, new Set(), undefined, true, helpCopy );
	const saved = defined( board.find( row => row.id === "academy-buff-saved" ), "saved row" );
	assert.equal( saved.path, "/assets/images/Media_extracted/icon/skill/etc/tc_reserve_buff.png" );
	assert.equal( saved.passBuff, true, "an hour has accumulated at 140 minutes in" );
	assert.equal( saved.fraction, (1300 * 60000 + 1) / 86400000 );
	assert.equal(
		saved.helpText,
		"<UIIT_CTL_TC_SAVE_BUFF>\n \n<UIIT_STT_REMAIN_TIME>: 21<PARAM_HOUR> 40<PARAM_MINUTE>\n" +
			"<UIIT_STT_SILKMALL_P_POINT>: 25 <UIIT_CTL_TC_SAVE_BUFF_POINT>"
	);
	const received = defined( board.find( row => row.id === "academy-buff-received:12" ), "received row" );
	assert.equal( received.path, "/assets/images/Media_extracted/icon/skill/etc/tc_amends_buff.png" );
	assert.equal( received.passBuff, undefined, "a received row is never clicked" );
	assert.equal(
		received.helpText,
		"<UIIT_CTL_TC_RESERVE_BUFF>\n \n<UIIT_STT_REMAIN_TIME>: 11<PARAM_HOUR> 40<PARAM_MINUTE>\n" +
			"<UIIT_STT_LETTER_SENDER>: Donor\n<UIIT_STT_SILKMALL_P_POINT>: 30 <UIIT_CTL_TC_RESERVE_BUFF_POINT>"
	);
});

test("the pass is offered only after the first accumulated hour", () => {
	const boardAt = minutes =>
		buffBoard(
			/** @type {any} */ ({
				localGid: 1,
				academy: { ...state(), savedBuff: { minutes, value: 1, receivedAtMs: 0 } }
			}),
			0,
			new Set(),
			undefined,
			true,
			helpCopy
		).find( row => row.id === "academy-buff-saved" );
	assert.equal( boardAt( 1440 )?.passBuff, undefined, "a fresh window has nothing accumulated" );
	assert.equal( boardAt( 1381 )?.passBuff, undefined, "59 minutes in" );
	assert.equal( boardAt( 1380 )?.passBuff, true, "60 minutes in" );
});

test("a run-out row leaves the board", () => {
	const game = /** @type {any} */ ({
		localGid: 1,
		academy: {
			...state(),
			savedBuff: { minutes: 10, value: 1, receivedAtMs: 0 },
			receivedBuffs: [ { memberId: 12, minutes: 10, value: 1, receivedAtMs: 0 } ]
		}
	});
	const board = buffBoard( game, 10 * 60000 + 1, new Set(), undefined, true, helpCopy );
	assert.equal( board.some( row => row.id.startsWith( "academy-buff" ) ), false );
});
