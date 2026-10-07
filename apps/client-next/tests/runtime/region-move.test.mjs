/*
===========================================================================

region-move.test.mjs - tests for region-move.ts, navigation.ts

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
const { regionMoveAllowed, regionMoveDestination, regionMoveContinuation } = await import(
	"../../src/engine/foundation/navigation/region-move.ts"
);
const { createNavigation } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
);
const pose = ( x, z = 10, regionId = 257 ) => ({ x, y: 0, z, regionId, angle: 0 });
/*
================
continueRegionMove
================
*/
function continueRegionMove( from, to, step ) {
	if ( from.regionId === to.regionId && from.x === to.x && from.y === to.y && from.z === to.z ) {
		return { point: { ...from }, status: 0 };
	}
	if ( !regionMoveAllowed( from, to ) ) return { point: null, status: 0x10000000 };
	for ( let calls = 1;; calls++ ) {
		const target = regionMoveDestination( to, from.regionId ), result = step( from, target );
		const decision = regionMoveContinuation( from, target, result, calls );
		if ( decision === "stop" ) return result;
		if ( decision === "reject" ) return { point: null, status: 0x10000000 };
		from = result.point;
	}
}
test("region-manager protocol agrees with the unmodified original caller interval", () => {
	const reference = JSON.parse(
		fs.readFileSync( "tests/fixtures/native/native-region-move-reference.json", "utf8" )
	);
	assert.equal( reference.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	assert.equal(
		reference.generatorSha256,
		createHash( "sha256" ).update( fs.readFileSync( "tools/native-region-move-reference.py" ) ).digest( "hex" )
	);
	const stored = p => ({ ...p, x: Math.fround( p.x ), y: Math.fround( p.y ), z: Math.fround( p.z ) });
	for ( const row of reference.rows ) {
		let calls = 0;
		const actual = continueRegionMove( stored( row.start ), stored( row.target ), () => {
			const [status, p] = row.steps[calls++];
			return { status, point: stored( p ) };
		} );
		assert.deepEqual( actual, { status: row.status, point: row.point }, row.name );
		assert.equal( calls, row.calls, row.name );
	}
});
test("reflection resumes the original destination and preserves terminal contact", () => {
	const calls = [];
	const result = continueRegionMove( pose( 10 ), pose( 100 ), ( from, to ) => {
		calls.push( [ from.x, to.x ] );
		return calls.length === 1 ? { point: pose( 50.2 ), status: 16 } : { point: pose( 75 ), status: 1 };
	} );
	assert.deepEqual( calls, [ [ 10, 100 ], [ 50.2, 100 ] ] );
	assert.equal( result.point.x, 75 );
	assert.equal( result.status, 1 );
});
test("continuation distance tests its start and the sixth continuation rejects", () => {
	let calls = 0;
	const run = ( x, status ) =>
		continueRegionMove( pose( 10 ), pose( x ), () => {
			calls++;
			return { point: pose( x - .1 ), status };
		} );
	run( 14.99, 16 );
	assert.equal( calls, 1 );
	calls = 0;
	assert.equal( run( 15, 16 ).status, 16 );
	assert.equal( calls, 2 );
	calls = 0;
	const r = continueRegionMove( pose( 10 ), pose( 100 ), () => {
		calls++;
		return { point: pose( 10 ), status: 4 };
	} );
	assert.equal( calls, 6 );
	assert.equal( r.status, 0x10000000 );
	assert.equal( r.point, null );
	calls = 0;
	const last = continueRegionMove(
		pose( 10 ),
		pose( 100 ),
		() => ({ point: pose( 10 ), status: ++calls === 6 ? 1 : 16 })
	);
	assert.equal( last.status, 1 );
	assert.equal( calls, 6 );
});
test("region continuation re-expresses the destination after a sector exit", () => {
	const calls = [];
	const r = continueRegionMove( pose( 1900 ), pose( 50, 10, 258 ), ( from, to ) => {
		calls.push( [ from.regionId, to.regionId, to.x ] );
		return calls.length === 1 ? { point: pose( .1, 10, 258 ), status: 16 } : { point: to, status: 0 };
	} );
	assert.deepEqual( calls, [ [ 257, 257, 1970 ], [ 258, 258, 50 ] ] );
	assert.equal( r.point.regionId, 258 );
	assert.equal(
		continueRegionMove( pose( 0 ), pose( 1921 ), () => {
			throw Error( "must not dispatch" );
		} ).status,
		0x10000000
	);
});
/*
================
navigation
================
*/
function navigation( flags = 0 ) {
	const nav = createNavigation();
	const b = values => Buffer.from( values.buffer ).toString( "base64" );
	nav.install( 257, {
		regionId: 257,
		complete: true,
		objects: [ {
			x: 0,
			y: 0,
			z: 0,
			yaw: 0,
			mesh: {
				vertices: Float32Array.of( 0, 0, 0, 0, 0, 100, 100, 0, 0 ),
				cells: Uint16Array.of( 0, 1, 2 ),
				edges: Uint32Array.of( 1, 2, 0, 65535, flags, 0 ),
				bounds: [ 0, 0, 0, 100, 0, 100 ],
				passThrough: false
			}
		} ],
		navmesh: {
			regionSize: 1920,
			tileSize: 20,
			tilesPerAxis: 96,
			regions: [ {
				dx: 0,
				dz: 0,
				blockedTiles: b( new Uint8Array( 9216 ) ),
				tileCellIds: b( new Uint32Array( 9216 ) ),
				heightMap: b( new Float32Array( 97 * 97 ) ),
				cells: { count: 1 }
			} ]
		}
	} );
	return nav;
}

test("live navigation distinguishes a hard region rejection from missing coverage", () => {
	const nav = navigation();
	const rejected = { slide: false, status: 0 };
	assert.equal( nav.clip( pose( 0 ), pose( 1921 ), rejected ), null );
	assert.equal( rejected.status, 0x10000000 );
	const unavailable = { slide: false, status: 0 };
	assert.equal( nav.clip( pose( 10, 10, 258 ), pose( 20, 10, 258 ), unavailable ), null );
	assert.equal( unavailable.status, 0 );
	nav.clear();
});
test("live outdoor open and marker outlines exit to terrain, rails still stop", () => {
	for ( const flag of [ 0, 0x80 ] ) {
		const nav = navigation( flag );
		const result = nav.clip( pose( 10, 10 ), pose( 80, 80 ) );
		assert.equal( defined( result ).x, 80 );
		assert.equal( defined( result ).z, 80 );
		nav.clear();
	}
	const rail = navigation( 2 ).clip( pose( 10, 10 ), pose( 80, 80 ) );
	assert.ok( defined( rail ).x < 50 );
	assert.ok( defined( rail ).z < 50 );
	const entry = navigation().clip( pose( 80, 80 ), pose( 10, 10 ) );
	assert.equal( defined( entry ).x, 10 );
});
test("open exit close to the original start returns the native reflected point", () => {
	const result = navigation().clip( pose( 49, 49 ), pose( 51, 51 ) );
	assert.equal( defined( result ).x, Math.fround( 100 - Math.fround( 49.858577728271484 ) ) );
	assert.equal( defined( result ).z, defined( result ).x );
});

test("retained object owner is retired when outline reflection continues on terrain", () => {
	for ( const flag of [ 0, 0x80 ] ) {
		const nav = navigation( flag ), output = { slide: false, sourceOwner: { placement: 0, cell: 0 } };
		const result = nav.clip( pose( 10, 10 ), pose( 80, 80 ), output );
		assert.equal( defined( result ).x, 80 );
		assert.equal( defined( result ).z, 80 );
		assert.equal( output.owner, undefined );
	}
});
