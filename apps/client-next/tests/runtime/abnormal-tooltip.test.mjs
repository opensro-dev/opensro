/*
===========================================================================

abnormal-tooltip.test.mjs - keep abnormal power, grade and actor ownership apart

Exercises native snapshot decoding through the shared tooltip used by the
local board and target, pet and party viewers. The server's power byte must
never become a grade or leak into another character's status description.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { parseAbnormalSnapshot } = await import( "../../src/engine/foundation/gameplay/abnormal-snapshot.ts" );
const { buffTooltip } = await import( "../../src/engine/foundation/ui/buff-tooltip.ts" );
const { buffViewerIcons, rebuildBuffViewer, targetBuffViewer } = await import(
	"../../src/engine/foundation/ui/buff-viewer.ts"
);

const LOCAL_GID = 7;
const TARGET_GID = 8;
const PET_GID = 9;
const POWER_MASK = 0x203f;
const GRADE_MASK = 0x017fcfc0;
const TEXT = {
	PARAM_BU: "Burn",
	PARAM_POWER: "Effect",
	UIIT_STT_GRADE: "level",
	UIIT_STT_REMAIN_TIME: "Remaining time",
	PARAM_SECOND: "Second",
	PARAM_MINUTE: "Minute",
	PARAM_HOUR: "Hour",
	PARAM_DAY: "Day"
};

/*
================
text
================
*/
function text( key ) {
	return TEXT[key] ?? key;
}

/*
================
snapshot

One active native status: 30 seconds total, 2 elapsed, power or grade 38.
================
*/
function snapshot( bit ) {
	const body = new Uint8Array( 9 );
	const view = new DataView( body.buffer );
	view.setUint32( 0, 2 ** bit, true );
	view.setUint16( 4, 300, true );
	view.setUint16( 6, 20, true );
	body[8] = 38;
	return parseAbnormalSnapshot( body, 1000 );
}

/*
================
state
================
*/
function state( bit ) {
	return {
		localGid: LOCAL_GID,
		vitals: [ LOCAL_GID, TARGET_GID, PET_GID ].map( gid => ({
			gid,
			abnormal: 2 ** bit,
			abnormalLevels: (2 ** bit & GRADE_MASK) ? [ { bit: 2 ** bit, level: gid === LOCAL_GID ? 38 : 2 } ] : []
		}) ),
		abnormalRecords: snapshot( bit )
	};
}

/*
================
tooltip
================
*/
function tooltip( game, source, now = 2000 ) {
	const catalog = Object.assign( new Map(), { groups: new Map() } );
	return buffTooltip( { kind: "abnormal", ...source }, game, now, catalog, text ).map( row => row.value );
}

test("BUG-040: Burn power 38 is a detail, never the heading grade", () => {
	const rows = tooltip( state( 3 ), { gid: LOCAL_GID, bit: 3 } );
	assert.equal( rows[0], "Burn" );
	assert.ok( rows.includes( "\nEffect 38" ) );
	assert.ok( rows.includes( "\nRemaining time 27Second" ) );
});

test("all named abnormal statuses preserve the native power/grade split", () => {
	for ( let bit = 0; bit < 25; bit++ ) {
		const mask = 2 ** bit;
		if ( !(mask & (POWER_MASK | GRADE_MASK)) ) continue;
		const rows = tooltip( state( bit ), { gid: LOCAL_GID, bit } );
		assert.equal( rows[0].includes( "38" ), Boolean( mask & GRADE_MASK ), `heading bit ${bit}` );
		assert.equal( rows.includes( "\nEffect 38" ), Boolean( mask & POWER_MASK ), `power bit ${bit}` );
		assert.equal( rows.some( row => row.includes( "Remaining time" ) ), bit !== 24, `time bit ${bit}` );
	}
});

test("target and pet viewers use their own grades and never the local snapshot", () => {
	for ( const gid of [ TARGET_GID, PET_GID ] ) {
		for ( const bit of [ 3, 11, 14 ] ) {
			const rows = tooltip( state( bit ), { gid, bit } );
			assert.ok( rows.every( row => !row.includes( "38" ) ) );
			assert.equal( rows[0].endsWith( "2level" ), bit !== 3 );
			assert.ok( rows.every( row => !row.includes( "Remaining time" ) && !row.includes( "Effect" ) ) );
		}
	}
});

test("party cells suppress grade, power and private timers even for the local member", () => {
	for ( const gid of [ LOCAL_GID, TARGET_GID ] ) {
		for ( const bit of [ 3, 11, 14 ] ) {
			const rows = tooltip( state( bit ), { gid, bit, unlevelled: true } );
			assert.ok( rows.every( row => !/38|2level|Remaining time|Effect/.test( row ) ) );
		}
	}
});

test("a viewer targeting the local player still uses viewer-only details", () => {
	const layout = targetBuffViewer();
	for ( const bit of [ 3, 14 ] ) {
		const slots = rebuildBuffViewer( layout, [], 2 ** bit, () => undefined );
		const icon = buffViewerIcons( layout, slots, LOCAL_GID, () => undefined )[0];
		assert.equal( icon.helpSource.viewer, true );
		const rows = tooltip( state( bit ), icon.helpSource );
		assert.equal( rows[0].includes( "38" ), bit === 14 );
		assert.ok( rows.every( row => !row.includes( "Effect" ) && !row.includes( "Remaining time" ) ) );
	}
});

test("local grade headings use native spacing and missing snapshots use public grades", () => {
	const game = state( 11 );
	assert.equal( tooltip( game, { gid: LOCAL_GID, bit: 11 } )[0], "PARAM_BLOOD 38 level" );
	game.abnormalRecords = [];
	assert.ok( tooltip( game, { gid: LOCAL_GID, bit: 11 } )[0].includes( "38" ) );
	assert.ok( tooltip( game, { gid: LOCAL_GID, bit: 11 } ).every( row => !row.includes( "Remaining time" ) ) );
});

test("expired, removed and zero-power states do not invent details", () => {
	const game = state( 3 );
	assert.ok( tooltip( game, { gid: LOCAL_GID, bit: 3 }, 60000 ).every( row => !row.includes( "Remaining time" ) ) );
	game.abnormalRecords = [ { ...game.abnormalRecords[0], level: 0 } ];
	assert.ok( tooltip( game, { gid: LOCAL_GID, bit: 3 } ).every( row => !row.includes( "Effect" ) ) );
	game.vitals[0].abnormal = 0;
	assert.deepEqual( tooltip( game, { gid: LOCAL_GID, bit: 3 } ), [] );
});
