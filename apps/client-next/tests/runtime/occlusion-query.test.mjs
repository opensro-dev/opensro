/*
===========================================================================

occlusion-query.test.mjs - tests for picking.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const { pickGeometry, occludesGeometry, nearestOccluder, createOcclusionMemo, geometryPickBlocks } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/picking.ts" ).href
);
const I = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
test("any-hit agrees with nearest-hit visibility including transparent front faces and strict depth ties", () => {
	const geometry = {
		positions: Float32Array.of( -1, -1, .2, 1, -1, .2, 0, 1, .2, -1, -1, .7, 1, -1, .7, 0, 1, .7 ),
		indices: Uint32Array.of( 0, 1, 2, 3, 4, 5 ),
		colors: Float32Array.of( 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1 ),
		transform: I,
		material: { color: [ 1, 1, 1, 1 ], doubleSided: true, alphaCutoff: .1 }
	};
	const ray = { start: [ 0, 0, 0 ], delta: [ 0, 0, 1 ] };
	for ( const opacity of [ 0, .05, .5, 1 ] ) {
		for ( const doubleSided of [ false, true ] ) {
			for ( const direction of [ 1, -1 ] ) {
				geometry.material.doubleSided = doubleSided;
				ray.delta[2] = direction;
				ray.start[2] = direction === 1 ? 0 : 1;
				const surface = { opacity, blocks: geometryPickBlocks( geometry ) },
					nearest = pickGeometry( ray, geometry, I, undefined, surface );
				for ( const limit of [ 0, .1, .2, .5, .7, 1, nearest ?? 0, (nearest ?? 0) + 1e-8 ] ) {
					assert.equal(
						occludesGeometry( ray, geometry, I, limit, { surface } ),
						nearest !== null && nearest < limit
					);
					// The nearest occluder before limit: the first-hit answer, and the depth.
					assert.equal(
						nearestOccluder( ray, geometry, I, limit, { surface } ),
						nearest !== null && nearest < limit ? nearest : null
					);
				}
			}
		}
	}
});
test("blocking query stops before touching later triangles", () => {
	const positions = Float32Array.of( -1, -1, .25, 1, -1, .25, 0, 1, .25 ),
		geometry = { positions, indices: Uint32Array.of( 0, 1, 2 ), transform: I };
	// A poison tail detects traversal after a complete accepted blocker.
	geometry.indices = {
		length: 6,
		0: 0,
		1: 1,
		2: 2,
		get 3() {
			throw Error( "unneeded triangle" );
		}
	};
	assert.equal( occludesGeometry( { start: [ 0, 0, 0 ], delta: [ 0, 0, 1 ] }, geometry, I, .5 ), true );
	assert.throws( () => pickGeometry( { start: [ 0, 0, 0 ], delta: [ 0, 0, 1 ] }, geometry, I ), /unneeded triangle/ );
});

test("an occlusion memo answers every depth on a ray as a fresh first-hit query would, scanning each ray at most as needed", () => {
	// Three rays: an occluder at .4, one at .9, none. Queries arrive in a
	// shuffled order of depths, as candidates do.
	const occluders = new Map( [ [ "a", .4 ], [ "b", .9 ], [ "c", Infinity ] ] ), rays = [ ...occluders.keys() ];
	let scans = 0;
	const memo = createOcclusionMemo( ( ray, limit ) => {
		scans++;
		const at = occluders.get( ray );
		return at < limit ? at : null;
	} );
	let seed = 7;
	const random = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
	for ( let i = 0; i < 400; i++ ) {
		const ray = rays[Math.floor( random() * rays.length )], limit = random();
		assert.equal( memo( ray, limit ), occluders.get( ray ) < limit, `${ray} at ${limit}` );
	}
	// A found occluder answers every later depth; a clear ray rescans only for
	// a depth past every one asked.
	assert.ok( scans < 40, `${scans} scans for 400 queries` );
});
