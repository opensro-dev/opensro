/*
===========================================================================

terrain-flat-normals.test.mjs - terrain decodes with the retail flat normals

Retail terrain is NOLIGHT: every vertex carries (0,1,0). The terrain relief
row that computed heightfield normals was removed at the owner's request
(2026-10-11), so sloped ground decodes flat at every level of detail, and
an option a stale caller still passes changes nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createWorldDecoder } = await import(
	new URL( "../../src/engine/runtime/assets/worker/world/world.ts", import.meta.url ).href
);

/*
================
slopedBundle

One terrain block whose heights rise 10 units per cell along x.
================
*/
function slopedBundle() {
	return {
		source: { sectorX: 1, sectorY: 1 },
		terrain: {
			blocks: [ {
				blockX: 0,
				blockZ: 0,
				heights: Array.from( { length: 289 }, ( _, i ) => i % 17 * 10 ),
				textureData: Array( 289 ).fill( 0 )
			} ]
		},
		terrainTextures: {
			tileCatalog: { referencedTiles: [ { textureId: 0, imagePublicPath: "/assets/ground.png" } ] }
		},
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
}

test("sloped terrain decodes with flat normals at every LOD, whatever a stale option says", () => {
	const decoder = createWorldDecoder();
	const plain = decoder.decode( slopedBundle(), false );
	const stale = decoder.decode( slopedBundle(), false, /** @type {any} */ ({ terrainNormals: true }) );
	const lods = new Set();
	for ( const group of plain.groups ) {
		for ( const range of group.ranges ?? [] ) lods.add( range.lod );
		const normals = group.geometry.normals;
		for ( let i = 0; i < normals.length; i += 3 ) {
			assert.deepEqual( [ ...normals.slice( i, i + 3 ) ], [ 0, 1, 0 ] );
		}
	}
	assert.equal( lods.size, 4 );
	assert.deepEqual( stale, plain );
});
