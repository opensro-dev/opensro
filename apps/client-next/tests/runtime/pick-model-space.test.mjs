/*
===========================================================================

pick-model-space.test.mjs - tests for picking.ts model-space queries

An unskinned mesh is tested in its own space (localRay) and a skinned one
can share its posed vertices across queries (SkinnedPickVertices). Both
must answer exactly as the world-space walk does: the same depth, the same
winding rule under a mirrored instance, the same occlusion.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
const { pickGeometry, occludesGeometry, geometryPickBlocks, createSkinnedPickVertices } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/picking.ts" ).href
);
const I = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );

function random( seed ) {
	let state = seed;
	return () => (state = (state * 1103515245 + 12345) % 2147483648) / 2147483648;
}

// A rotation about y, a non-uniform scale (negative x when mirrored) and a translation.
function instance( angle, scale, mirrored ) {
	const c = Math.cos( angle ), s = Math.sin( angle ), sx = mirrored ? -scale[0] : scale[0];
	return Float32Array.of( c * sx, 0, -s * sx, 0, 0, scale[1], 0, 0, s * scale[2], 0, c * scale[2], 0, 3, -2, 5, 1 );
}

// The same mesh with the instance applied to its positions, so the world walk sees it.
function baked( geometry, m ) {
	const positions = new Float32Array( geometry.positions.length );
	for ( let i = 0; i < positions.length; i += 3 ) {
		const [x, y, z] = [ geometry.positions[i], geometry.positions[i + 1], geometry.positions[i + 2] ];
		for ( let axis = 0; axis < 3; axis++ ) {
			positions[i + axis] = m[axis] * x + m[axis + 4] * y + m[axis + 8] * z + m[axis + 12];
		}
	}
	return { ...geometry, positions };
}

function mesh( next, triangles, doubleSided ) {
	const positions = new Float32Array( triangles * 9 ), indices = new Uint32Array( triangles * 3 );
	for ( let i = 0; i < positions.length; i++ ) positions[i] = next() * 4 - 2;
	for ( let i = 0; i < indices.length; i++ ) indices[i] = i;
	return { positions, indices, transform: I, material: { color: [ 1, 1, 1, 1 ], doubleSided } };
}

test("a transformed instance answers the depth its baked world copy answers", () => {
	const next = random( 7 );
	let hits = 0;
	for ( let round = 0; round < 60; round++ ) {
		const doubleSided = round % 2 === 0, geometry = mesh( next, 40, doubleSided );
		const m = instance( next() * 6.28, [ .5 + next(), .5 + next(), .5 + next() ], round % 3 === 0 );
		const world = baked( geometry, m );
		for ( let r = 0; r < 20; r++ ) {
			const ray = {
				start: [ 3 + next() * 2 - 1, -2 + next() * 2 - 1, -10 ],
				delta: [ next() * .4 - .2, next() * .4 - .2, 30 ]
			};
			const surface = { blocks: geometryPickBlocks( geometry ) };
			const local = pickGeometry( ray, geometry, m, undefined, surface ),
				expected = pickGeometry( ray, world, I, undefined, { blocks: geometryPickBlocks( world ) } );
			if ( expected === null ) {
				assert.equal( local, null );
				continue;
			}
			hits++;
			assert.ok( local !== null && Math.abs( local - expected ) < 1e-5, `${local} vs ${expected}` );
		}
	}
	// The rounds must exercise hits, not only misses.
	assert.ok( hits > 100, `hits ${hits}` );
});

test("a mirrored instance keeps a single-sided face's culling of the baked copy", () => {
	const geometry = {
		positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: I,
		material: { color: [ 1, 1, 1, 1 ], doubleSided: false }
	};
	for ( const mirrored of [ false, true ] ) {
		const m = instance( 0, [ 1, 1, 1 ], mirrored ), world = baked( geometry, m );
		for ( const direction of [ 1, -1 ] ) {
			const ray = { start: [ 3, -2, 5 - direction * 2 ], delta: [ 0, 0, direction * 4 ] };
			assert.equal(
				pickGeometry( ray, geometry, m, undefined, {} ),
				pickGeometry( ray, world, I, undefined, {} ),
				`mirrored ${mirrored} direction ${direction}`
			);
		}
	}
});

test("a skinned mesh's shared posed vertices answer every query as a fresh walk does", () => {
	const next = random( 11 ), geometry = mesh( next, 30, true ), vertices = geometry.positions.length / 3;
	geometry.joints = new Uint16Array( vertices * 4 );
	geometry.weights = new Float32Array( vertices * 4 );
	for ( let v = 0; v < vertices; v++ ) {
		geometry.joints[v * 4] = 0;
		geometry.joints[v * 4 + 1] = 1;
		geometry.weights[v * 4] = .3;
		geometry.weights[v * 4 + 1] = .7;
	}
	const palette = new Float32Array( 32 );
	palette.set( instance( .4, [ 1, 1, 1 ], false ), 0 );
	palette.set( instance( -.3, [ 1.2, .8, 1 ], false ), 16 );
	// Joint transforms rotate and scale in place; the instance carries the translation.
	for ( const at of [ 12, 13, 14, 28, 29, 30 ] ) palette[at] = 0;
	const m = instance( 1.1, [ 1, 1, 1 ], false ), skinned = createSkinnedPickVertices( geometry );
	let blocked = 0;
	for ( let r = 0; r < 80; r++ ) {
		const ray = { start: [ 3 + next() * 3 - 1.5, -2 + next() * 3 - 1.5, -10 ], delta: [ 0, 0, 30 ] },
			limit = next();
		const fresh = occludesGeometry( ray, geometry, m, limit, { palette } ),
			shared = occludesGeometry( ray, geometry, m, limit, { palette, skinned } );
		assert.equal( shared, fresh );
		if ( fresh ) blocked++;
	}
	assert.ok( blocked > 5, `blocked ${blocked}` );
});
