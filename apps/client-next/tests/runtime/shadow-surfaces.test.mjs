/*
===========================================================================

shadow-surfaces.test.mjs - tests for shadow-surfaces.ts

The owner keeps the submitted terrain ranges per cell in submission order
and stamps each changed cell, so a shadow receiver is rebuilt only when a
cell under it changes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createShadowSurfaces } = await import(
	pathToFileURL( "src/engine/runtime/renderer/world/shadow-surfaces.ts" ).href
);
const { terrainCellKey } = await import(
	pathToFileURL( "src/engine/foundation/rendering/terrain-interaction.ts" ).href
);

/*
================
group

A terrain group with one range per listed cell, in index order.
================
*/
function group( cells ) {
	const geometry = { positions: new Float32Array( 3 ), indices: new Uint32Array( 3 ) };
	return {
		geometry,
		ranges: cells.map( ( cell, i ) => ({ cell, lod: 0, indexStart: i * 6, indexCount: 6 }) )
	};
}

test("rows keep group order then index order, whatever order groups change in", () => {
	const owner = createShadowSurfaces(), a = group( [ [ 1, 1 ], [ 1, 1 ] ] ), b = group( [ [ 1, 1 ] ] );
	owner.replace( b, 1, undefined, b.ranges );
	owner.replace( a, 0, undefined, [ a.ranges[1] ] );
	owner.replace( a, 0, [ a.ranges[1] ], a.ranges );
	owner.commit();
	const rows = owner.surfaces().get( terrainCellKey( 1, 1 ) );
	assert.deepEqual( rows.map( r => [ r.positions === a.geometry.positions ? "a" : "b", r.start ] ), [
		[ "a", 0 ],
		[ "a", 6 ],
		[ "b", 0 ]
	] );
});

test("a change stamps only its own cells; reset stamps every cell", () => {
	const owner = createShadowSurfaces(), g = group( [ [ 0, 0 ], [ 5, 5 ] ] );
	owner.replace( g, 0, undefined, g.ranges );
	owner.commit();
	const stamp = owner.revision();
	assert.equal( owner.changedSince( stamp, 0, 0, 1, 1 ), false );
	// Dropping the far range changes cell (5, 5), not (0, 0).
	owner.replace( g, 0, g.ranges, [ g.ranges[0] ] );
	owner.commit();
	assert.equal( owner.changedSince( stamp, 0, 0, 1, 1 ), false );
	assert.equal( owner.changedSince( stamp, 4, 4, 5, 5 ), true );
	assert.equal( owner.surfaces().has( terrainCellKey( 5, 5 ) ), false );
	// An unchanged choice commits nothing.
	const settled = owner.revision();
	owner.replace( g, 0, [ g.ranges[0] ], [ g.ranges[0] ] );
	owner.commit();
	assert.equal( owner.revision(), settled );
	owner.reset();
	assert.equal( owner.changedSince( settled, 0, 0, 0, 0 ), true );
	assert.equal( owner.surfaces().size, 0 );
});
