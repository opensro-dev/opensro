/*
===========================================================================

param-job.test.mjs - EXP/SP scroll rows on the buff board (kind 4)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { createParamJobs, paramJobFraction, paramJobHelpText, paramJobRemainingMs } = await import(
	"../../src/engine/foundation/gameplay/param-job.ts"
);
const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );

// TID 3/3/3/10 with the low class bits set the way itemdata words carry them.
const PARAM_ITEM = 0x51ec | 1;
// TID 3/3/13/4 (Gold/Silver Time tickets) and TID 3/3/13/5 (Skill Time tickets).
const GOLD_TIME_TICKET = 0x26ec | 1;
const SKILL_TIME_TICKET = 0x2eec | 1;

/*
================
row

[u32 owner][u32 remaining][u32 ref], or the 8-byte end body.
================
*/
function frame( opcode, ...words ) {
	const payload = new Uint8Array( words.length * 4 ), view = new DataView( payload.buffer );
	words.forEach( ( word, i ) => view.setUint32( i * 4, word, true ) );
	return { opcode, payload };
}

test("a start row joins the board once its internal item is known", () => {
	const jobs = createParamJobs();
	assert.equal( jobs.receive( frame( 0x3602, 9, 3600, 501 ), 1000 ), true );
	assert.deepEqual( jobs.state(), [], "no reference yet" );
	jobs.reference( { refObjId: 501, typeFlags: PARAM_ITEM, nativeFields: { itemParam1_29c: 3600 }, icon: "a.ddj" } );
	const [row] = jobs.state();
	assert.equal( row.reference.durationSec, 3600 );
	assert.equal( paramJobFraction( row, row.reference, 1000 + 1800_000 ), 0.5 );
});

test("Gold Time (TID 3/3/13/4) and Skill Time (TID 3/3/13/5) tickets join the board", () => {
	const jobs = createParamJobs();
	jobs.reference( {
		refObjId: 3884,
		typeFlags: GOLD_TIME_TICKET,
		nativeFields: { itemParam1_29c: 2419200 },
		icon: "item/etc/mall_optlevel_upgrade_scroll_1.ddj",
		name: "Gold Time Service Ticket (4 weeks)"
	} );
	jobs.reference( {
		refObjId: 23268,
		typeFlags: SKILL_TIME_TICKET,
		nativeFields: { itemParam1_29c: 86400 },
		icon: "item/etc/mall_skill_time.ddj",
		name: "Skill Time Ticket (1 day)"
	} );
	assert.equal( jobs.receive( frame( 0x3602, 9, 2419200, 3884 ), 1000 ), true );
	assert.equal( jobs.receive( frame( 0x32af, 9, 43200, 23268 ), 1000 ), true );
	const rows = jobs.state();
	assert.equal( rows.length, 2 );
	assert.equal( rows[0].itemRefObjId, 3884 );
	assert.equal( rows[0].reference.durationSec, 2419200 );
	assert.equal( rows[1].itemRefObjId, 23268 );
	assert.equal( paramJobFraction( rows[1], rows[1].reference, 1000 ), 0.5 );
});

/*
================
helpCopy

The board help's words, resolved as the client's text lookup resolves
them.
================
*/
const helpCopy = symbol =>
	({
		PARAM_DURA: "Continuous hours",
		PARAM_REMAINED_PERIOD: "Left service period",
		PARAM_DAY: "Day",
		PARAM_HOUR: "Hour",
		PARAM_MINUTE: "Minute",
		PARAM_SECOND: "Second"
	})[symbol] ?? symbol;

test("the kind-4 help reads name, total duration and the remained period", () => {
	// A four-week premium service row, one minute in.
	assert.equal(
		paramJobHelpText(
			"Premium PLUS",
			2419200,
			paramJobRemainingMs( { itemRefObjId: 24306, remainingSec: 2419200, receivedAtMs: 0 }, 60000 ),
			helpCopy
		),
		"Premium PLUS\n \nContinuous hours: 672Hour\n \nLeft service period: 27Day 23Hour 59Minute"
	);
	// An hour-long scroll with half an hour left counts its duration in minutes.
	assert.equal(
		paramJobHelpText( "Exp scroll", 3600, 1800000, helpCopy ),
		"Exp scroll\n \nContinuous hours: 60Minute\n \nLeft service period: 0Day 0Hour 30Minute"
	);
	// Under a minute the period counts seconds; the duration stays whole minutes.
	assert.equal(
		paramJobHelpText( "Exp scroll", 3600, 45000, helpCopy ).split( "\n" )[4],
		"Left service period: 45Second"
	);
});

test("the board slot's hover help is the composed kind-4 text", async () => {
	const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
	const jobs = createParamJobs();
	jobs.reference( {
		refObjId: 24306,
		typeFlags: GOLD_TIME_TICKET,
		nativeFields: { itemParam1_29c: 2419200 },
		icon: "item/etc/mall_gold_time_service_ticket.ddj",
		name: "Premium PLUS"
	} );
	jobs.receive( frame( 0x3602, 9, 2419200, 24306 ), 0 );
	const board = buffBoard(
		/** @type {any} */ ({ localGid: 1, paramJobs: jobs.state() }),
		60000,
		new Set(),
		undefined,
		true,
		helpCopy
	);
	const slot = board.find( row => row.id === "param-job:24306" );
	assert.ok( slot );
	assert.equal(
		slot.helpText,
		"Premium PLUS\n \nContinuous hours: 672Hour\n \nLeft service period: 27Day 23Hour 59Minute"
	);
});

test("resume replaces, end removes, and other items are not param jobs", () => {
	const jobs = createParamJobs();
	jobs.reference( { refObjId: 501, typeFlags: PARAM_ITEM, nativeFields: { itemParam1_29c: 3600 } } );
	jobs.reference( { refObjId: 777, typeFlags: 0x1234, nativeFields: { itemParam1_29c: 10 } } );
	jobs.receive( frame( 0x3602, 9, 3600, 501 ), 0 );
	jobs.receive( frame( 0x32af, 9, 1200, 501 ), 5 );
	assert.equal( jobs.state().length, 1 );
	assert.equal( jobs.state()[0].remainingSec, 1200 );
	jobs.receive( frame( 0x3602, 9, 50, 777 ), 0 );
	assert.equal( jobs.state().length, 1, "a non-param item has no board reference" );
	jobs.receive( frame( 0x36d4, 9, 501 ), 10 );
	assert.deepEqual( jobs.state(), [] );
	assert.throws( () => jobs.receive( { opcode: 0x36d4, payload: new Uint8Array( 4 ) }, 0 ), /end/ );
	assert.equal( jobs.receive( frame( 0x3691, 1 ), 0 ), false );
});

test("arena powers use native timed HUD cells, tooltips, refresh and removal", () => {
	const jobs = createParamJobs();
	for ( const kind of [ 1, 3, 5, 6, 7, 8 ] ) {
		jobs.receive( frame( 0x3602, 100006, kind === 1 || kind === 8 ? 30 : 60, 0x7f530000 | kind ), 1000 );
	}
	const cells = buffBoard( { localGid: 100006, vitals: [], paramJobs: jobs.state() }, 11000 );
	assert.equal( cells.length, 6 );
	assert.deepEqual( cells.map( c => c.label ), [
		"Damage Surge",
		"Unstoppable",
		"Unkillable",
		"Arcane Spring",
		"Fire Pit",
		"Mirror Shell"
	] );
	assert.ok(
		cells.every( c => c.path.endsWith( ".png" ) && c.fraction !== null && c.fraction > 0 && c.fraction < 1 )
	);
	assert.ok( cells[0].helpText );
	assert.ok( cells.every( c => c.path.includes( "/icon/port/survival/" ) ) );
	assert.equal( new Set( cells.map( c => c.path ) ).size, 6 );
	assert.match( cells[0].helpText, /20s remaining/ );
	jobs.receive( frame( 0x3602, 100006, 60, 0x7f530003 ), 11000 );
	assert.equal( jobs.state().length, 6, "refresh must replace, not duplicate" );
	jobs.receive( frame( 0x36d4, 100006, 0x7f530003 ), 71000 );
	assert.equal( jobs.state().length, 5 );
	jobs.clear();
	assert.equal( jobs.state().length, 0 );
});

test("arena HUD timers preserve all eight native parameter-job slots", () => {
	const jobs = createParamJobs();
	for ( let i = 1; i <= 8; i++ ) {
		jobs.reference( { refObjId: i, typeFlags: PARAM_ITEM, nativeFields: { itemParam1_29c: 60 }, icon: "a.ddj" } );
		jobs.receive( frame( 0x3602, 1, 60, i ), 0 );
	}
	for ( const k of [ 1, 3, 5, 6 ] ) jobs.receive( frame( 0x3602, 1, 60, 0x7f530000 | k ), 0 );
	assert.equal( jobs.state().length, 12 );
	assert.throws( () => jobs.receive( frame( 0x3602, 1, 60, 9 ), 0 ), /capacity/ );
});
