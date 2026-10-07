/*
===========================================================================

vertex-direction.test.mjs - tests for vertex-direction.ts,
object-navigation.ts, navmeshWireDecode.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { createHash } from "node:crypto";

const { vertexDirection, outsideEdgeStart } = await import(
	sourceFileUrl( "src/engine/foundation/navigation/vertex-direction.ts" ).href
);
const oracle = JSON.parse( fs.readFileSync( "tests/fixtures/native/native-vertex-direction-reference.json", "utf8" ) );
test("all 256 decoded directions match native table initialization", () => {
	assert.equal(
		oracle.generatorSha256,
		createHash( "sha256" ).update( fs.readFileSync( "tools/native-vertex-direction-reference.py" ) ).digest( "hex" )
	);
	assert.equal( oracle.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	for ( let i = 0; i < 256; i++ ) {
		assert.deepEqual( vertexDirection( i ), oracle.table.slice( i * 2, i * 2 + 2 ), `direction ${i}` );
	}
});
test("outside blocking biases original start and breaks endpoint ties toward vertex 1", () => {
	for ( const r of oracle.rows ) {
		assert.deepEqual(
			outsideEdgeStart( r.source, r.hit, [ 0, 0 ], [ 0, 100 ], ...r.directions ),
			r.result,
			JSON.stringify( r )
		);
	}
	assert.notDeepEqual( oracle.rows[2].result, oracle.rows[0].result );
	assert.ok( oracle.rows[3].result[0] < -99 ); // Reject hit-based pullback mutation.
});

test("outside contact uses decoded endpoint directions without acquiring a cell", async () => {
	const { navContactDetail } = await import(
		sourceFileUrl( "src/engine/foundation/navigation/object-navigation.ts" ).href
	);
	const mesh = {
		vertices: Float32Array.of( 0, 7, 0, 0, 7, 100, 100, 7, 0 ),
		vertexDirections: Uint8Array.of( 0, 64, 0 ),
		cells: Uint16Array.of( 0, 1, 2 ),
		edges: Uint32Array.of( 0, 1, 0, 65535, 1, 0 ),
		bounds: [ 0, 7, 0, 100, 7, 100 ],
		passThrough: false
	};
	const response = navContactDetail( { x: 0, y: 0, z: 0, yaw: 0, mesh }, [ -10, 7, 50 ], [ 10, 7, 50 ] );
	assert.equal( response.cell, 65535 );
	assert.equal( response.status, 1 );
	assert.deepEqual( response.point, [ oracle.rows[2].result[0], 7, oracle.rows[2].result[1] ] );
});

test("a finite walker meets an outline from its actual final-step source", async () => {
	const { navContactDetail } = await import(
		sourceFileUrl( "src/engine/foundation/navigation/object-navigation.ts" ).href
	);
	const mesh = {
		vertices: Float32Array.of( 0, 7, 0, 0, 7, 100, 100, 7, 0 ),
		vertexDirections: Uint8Array.of( 0, 64, 0 ),
		cells: Uint16Array.of( 0, 1, 2 ),
		edges: Uint32Array.of( 0, 1, 0, 65535, 1, 0 ),
		bounds: [ 0, 7, 0, 100, 7, 100 ],
		passThrough: false
	};
	const p = { x: 0, y: 0, z: 0, yaw: 0, mesh };
	let current = -90;
	// These are accepted 20ms movement steps at 50 units per second.
	for ( let frame = 0; frame < 89; frame++ ) {
		assert.equal( navContactDetail( p, [ current, 7, 50 ], [ current + 1, 7, 50 ], [], [], false, true ), null );
		current += 1;
	}
	const contact = navContactDetail( p, [ current, 7, 50 ], [ current + 2, 7, 50 ], [], [], false, true );
	assert.ok( contact );
	const expected = outsideEdgeStart( [ -1, 50 ], [ 0, 50 ], [ 0, 0 ], [ 0, 100 ], 0, 64 );
	assert.deepEqual( [ contact.point[0], contact.point[2] ], expected );
	assert.notDeepEqual( expected, outsideEdgeStart( [ -.01, 50 ], [ 0, 50 ], [ 0, 0 ], [ 0, 100 ], 0, 64 ) );
});

test("legacy WIP bridge uses the same native cosine/negative-sine table", async () => {
	const { normalizeNavVertRegionLinkTable20c0 } = await import(
		sourceFileUrl( "tests/oracles/legacy/packages/wip-bridge/src/navmesh/navmeshWireDecode.ts" ).href
	);
	const table = normalizeNavVertRegionLinkTable20c0();
	for ( let i = 0; i < 256; i++ ) {
		assert.deepEqual( [ table[i].x0c, table[i].z10 ], oracle.table.slice( i * 2, i * 2 + 2 ) );
	}
});
