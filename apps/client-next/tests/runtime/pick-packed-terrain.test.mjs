/*
===========================================================================

pick-packed-terrain.test.mjs - terrain picks the same from its packed stream

Prepared terrain keeps only its packed vertex stream and positions
(world-scene.ts prepareWorldScene); picking then reads a vertex's uv and
colour alpha from the packed stream (picking.ts). Every ray must decide
exactly as it did against the split streams.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { pickGeometry } = await load( "src/engine/foundation/rendering/picking.ts" );
const { packGeometryVertices } = await load( "src/engine/foundation/rendering/geometry-vertices.ts" );

const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );

/*
================
quad

Two triangles over [0,10]^2 at y 0: the first opaque by colour, the second
faded out by vertex alpha; uvs cover a mask whose left half is clear.
================
*/
function quad() {
	return {
		positions: new Float32Array( [ 0, 0, 0, 10, 0, 0, 0, 0, 10, 10, 0, 10 ] ),
		normals: new Float32Array( [ 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ] ),
		uvs: new Float32Array( [ 0, 0, 1, 0, 0, 1, 1, 1 ] ),
		colors: new Float32Array( [ 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0 ] ),
		indices: new Uint32Array( [ 0, 2, 1, 1, 2, 3 ] ),
		transform: identity(),
		material: { color: [ 1, 1, 1, 1 ], alphaCutoff: .5, blend: false, doubleSided: true }
	};
}

test("terrain picks the same from its packed stream as from its split streams", () => {
	const split = quad();
	/** @type {any} */
	const packed = { ...quad(), dynamicVertices: true };
	packed.vertices = packGeometryVertices( packed );
	packed.normals = packed.uvs = packed.colors = undefined;
	// A 2x1 mask: the left texel clear, the right one opaque.
	const surface = { alpha: { width: 2, height: 1, pixels: Uint8Array.of( 0, 255 ) } };
	const rays = [];
	for ( let x = .5; x < 10; x += 1 ) {
		for ( let z = .5; z < 10; z += 1 ) rays.push( { start: [ x, 5, z ], delta: [ 0, -10, 0 ] } );
	}
	let hits = 0, misses = 0;
	for ( const ray of rays ) {
		const want = pickGeometry( ray, split, identity(), undefined, surface );
		assert.equal( pickGeometry( ray, packed, identity(), undefined, surface ), want );
		if ( want === null ) misses++;
		else hits++;
	}
	assert.ok( hits > 0 && misses > 0, "the rays cover both picked and rejected texels" );
});
