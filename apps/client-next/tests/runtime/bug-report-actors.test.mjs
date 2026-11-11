/*
===========================================================================

bug-report-actors.test.mjs - a report carries the poses it complains about

Issue #2997 parked as needs-info because a report sent while a ground
item or a pet stood sunk held no actor pose to check: state.json knew
only how many entities there were. diagnosticActors shapes the
presented entities for state.json - gid, kind, name, region and the
drawn pose at millimetre precision, nearest the local player first and
capped - so the next sunk-actor report names the pose at its own XZ.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { diagnosticActors } = await import( "../../src/engine/runtime/bug-report/bug-report.ts" );

/*
================
entity

One presented entity, only the fields the shaping reads.
================
*/
function entity( gid, x, z, y = 392.01227 ) {
	return { gid, kind: "item", name: "drop-" + gid, regionId: 0x8005, x, y, z, heading: 0, refObjId: 0 };
}

test("actor rows carry gid, kind, name, region and the pose at millimetre precision", () => {
	const rows = diagnosticActors( [ entity( 9001, -373, -5087 ) ], undefined );
	assert.deepEqual( rows, [ {
		gid: 9001,
		kind: "item",
		name: "drop-9001",
		region: "0x8005",
		x: -373,
		y: 392.012,
		z: -5087
	} ] );
});

test("actors are nearest the local pose first and the input order is untouched", () => {
	const entities = [ entity( 3, 30, 0 ), entity( 1, 10, 0 ), entity( 2, 20, 0 ) ];
	const rows = diagnosticActors( entities, { x: 0, z: 0 } );
	assert.deepEqual( rows.map( row => row.gid ), [ 1, 2, 3 ] );
	assert.deepEqual( entities.map( row => row.gid ), [ 3, 1, 2 ] );
});

test("without a local pose the presented order stands", () => {
	const entities = [ entity( 3, 30, 0 ), entity( 1, 10, 0 ) ];
	assert.deepEqual( diagnosticActors( entities, undefined ).map( row => row.gid ), [ 3, 1 ] );
});

test("the list is capped at the nearest actors", () => {
	const entities = Array.from( { length: 600 }, ( _, index ) => entity( index + 1, index + 1, 0 ) );
	const rows = diagnosticActors( entities, { x: 0, z: 0 } );
	assert.equal( rows.length, 512 );
	assert.equal( rows[0].gid, 1 );
	assert.equal( rows[511].gid, 512 );
});
