/*
===========================================================================

abnormal-snapshot.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const snap = await import( "../../src/engine/foundation/gameplay/abnormal-snapshot.ts" );
const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const { buffTooltip } = await import( "../../src/engine/foundation/ui/buff-tooltip.ts" );

/*
================
payload

Build the native duration, elapsed and power-or-grade records in mask order.
================
*/
function payload( mask, rows ) {
	const body = new Uint8Array( 4 + rows.length * 5 );
	const view = new DataView( body.buffer );
	view.setUint32( 0, mask, true );
	let offset = 4;
	for ( const row of rows ) {
		view.setUint16( offset, row[0], true );
		view.setUint16( offset + 2, row[1], true );
		body[offset + 4] = row[2];
		offset += 5;
	}
	return body;
}
test("77C110 records levels, grades and the zero byte, and 6841C0 announces both edges", () => {
	const stun = 1 << 14, burn = 8, plain = 1 << 12;
	const body = payload( stun | burn | plain, [ [ 30, 5, 6 ], [ 80, 1, 4 ], [ 40, 2, 3 ] ] );
	const records = snap.parseAbnormalSnapshot( body, 1000 );
	assert.deepEqual( records.map( r => [ r.bit, r.level, r.grade, r.durationMs, r.elapsedMs ] ), [
		[ 3, 6, 0, 3000, 500 ],
		[ 12, 0, 0, 8000, 100 ],
		[ 14, 0, 3, 4000, 200 ]
	] );
	assert.equal( snap.abnormalBarFraction( records[0], 1500 ), 2000 / 3000 );
	assert.equal( snap.abnormalBarFraction( { ...records[0], bit: 24, durationMs: 0 }, 9000 ), 1 );
	const gained = snap.abnormalSnapshotNotices( 0, records );
	assert.deepEqual( gained.map( n => n.key ), [
		"UIIT_MSG_STATE_SKILL_CURSING_BU",
		"UIIT_MSG_STATE_SKILL_CURSING_STUN"
	] );
	assert.ok( gained.every( n => n.nativeType === 3 && n.banner ) );
	const cleared = snap.abnormalSnapshotNotices( stun | burn, [] );
	assert.deepEqual( cleared.map( n => n.key ), [
		"UIIT_MSG_STATE_SKILL_CURSING_RELEASE_BU",
		"UIIT_MSG_STATE_SKILL_CURSING_RELEASE_STUN"
	] );
});
test("6E6AA0 bar uses the snapshot clock and the tooltip reads the record", () => {
	const game = {
		revision: 1,
		pose: null,
		authoritativePose: null,
		pendingMoves: 0,
		acknowledgedMove: 0,
		target: 0,
		targetPending: 0,
		inventory: [],
		inventoryPending: false,
		casts: [],
		error: null,
		localGid: 7,
		vitals: [ { gid: 7, abnormal: 8 } ],
		abnormalRecords: [ { bit: 3, level: 6, grade: 0, durationMs: 3000, elapsedMs: 0, receivedAtMs: 1000 } ],
		skillCatalog: [],
		notices: []
	};
	const row = buffBoard( game, 2500 ).find( item => item.id === "abnormal:3" );
	assert.ok( row );
	assert.equal( row.fraction, 1500 / 3000 );
	const text = key => key;
	const catalog = Object.assign( new Map(), { groups: new Map() } );
	const tip = buffTooltip( { kind: "abnormal", gid: 7, bit: 3 }, game, 2500, catalog, text );
	assert.equal( tip[0].value, "PARAM_BU" );
	assert.ok( tip.some( row => row.value === "\nPARAM_POWER 6" ) );
	assert.ok( tip.some( row => row.value === "\nUIIT_STT_REMAIN_TIME 1PARAM_SECOND" ) );
});
