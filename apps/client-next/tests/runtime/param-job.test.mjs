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
