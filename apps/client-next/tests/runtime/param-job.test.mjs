/*
===========================================================================

param-job.test.mjs - EXP/SP scroll rows on the buff board (kind 4)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { createParamJobs, paramJobFraction } = await import( "../../src/engine/foundation/gameplay/param-job.ts" );

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
