/*
===========================================================================

contact-response.test.mjs - tests for contact-response.ts, navigation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { createHash } from "node:crypto";
import { defined } from "../helpers/defined.mjs";
const oracle = JSON.parse( fs.readFileSync( "tests/fixtures/native/native-contact-reference.json", "utf8" ) );
const sha = p => createHash( "sha256" ).update( fs.readFileSync( p ) ).digest( "hex" );
assert.equal( oracle.generatorSha256, sha( "tools/native-contact-reference.py" ) );
const { cellEntry, edgeResponse } = await import( "../../src/engine/foundation/navigation/contact-response.ts" );
const { createNavigation } = await import(
	"../../src/engine/foundation/navigation/clipper.ts"
);
test("cell-entry stores agree exactly with complete native 45c1b0 execution", () => {
	for ( const row of oracle.snap ) {
		assert.deepEqual( cellEntry( row.center, row.point ), row.result, JSON.stringify( row ) );
	}
});
test("outline response preserves native reflection, cell ownership and budget results", () => {
	for ( const row of oracle.walk ) {
		const actual = edgeResponse(
			[ Math.fround( 100 / 3 ), Math.fround( 100 / 3 ) ],
			[ 50, 50 ],
			row.requested,
			row.flags,
			row.blockThrough,
			row.budget
		);
		assert.deepEqual( actual, {
			point: row.point,
			status: row.status,
			cell: row.cell,
			remainingBudget: row.remainingBudget
		}, JSON.stringify( row ) );
	}
});
test("movement navigation delivers exact native blocked endpoints and optional slide normals", () => {
	const nav = createNavigation(), regionId = 0x8001;
	const product = {
		regionId,
		complete: true,
		objects: [ {
			x: 0,
			y: 0,
			z: 0,
			yaw: 0,
			mesh: {
				vertices: Float32Array.of( 0, 0, 0, 0, 0, 100, 100, 0, 0 ),
				cells: Uint16Array.of( 0, 1, 2 ),
				edges: Uint32Array.of( 0, 1, 0, 65535, 2, 0, 1, 2, 0, 65535, 2, 0, 2, 0, 0, 65535, 2, 0 ),
				bounds: [ 0, 0, 0, 100, 0, 100 ],
				passThrough: false
			}
		} ]
	};
	const pose = p => ({ regionId, x: p[0], y: p[1], z: p[2], angle: 0 });
	const output = { slide: false };
	for ( const row of oracle.slide ) {
		const [x, y, z] = row.placement ?? [ 0, 0, 0 ];
		nav.install( regionId, { ...product, objects: [ { ...product.objects[0], x, y, z } ] } );
		output.slide = row.enabled;
		const result = nav.clip( pose( row.start ), pose( row.requested ), output );
		assert.deepEqual( [ defined( result ).x, defined( result ).y, defined( result ).z ], row.point );
		assert.deepEqual( output.normal ?? [ 0, 0, 0 ], row.normal );
		assert.equal( output.cell, 0 );
		assert.equal( output.edge, 1 );
	}
	nav.install( regionId, product );
	output.slide = false;
	nav.clip( pose( [ 10, 0, 10 ] ), pose( [ 20, 0, 20 ] ), output );
	assert.equal( output.normal, undefined );
	assert.equal( output.cell, undefined );
});
