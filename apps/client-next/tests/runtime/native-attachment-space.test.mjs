/*
===========================================================================

native-attachment-space.test.mjs - stage objects drawn in 8D6880 native space

An imported model's space is Ry(PI) of native space (exportGlb mirrors Z,
the loader's __gltf_left_handed__ root adds Sx), so a named socket is
Ry(PI) x bone x Sz and a root is placement x Ry(PI). The holder matrix E
undoes that; an .efp program draws native coordinates through E and an
imported mesh takes Ry(PI) once more after 8D9EC0's rotation. The expected
matrices here are built from that rule and the renderer's own socket, not
copied from the renderer's output.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defined } from "../helpers/defined.mjs";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { placement } = await import( "../../src/engine/foundation/rendering/world-math.ts" );
const { multiply } = await import( "../../src/engine/foundation/math/pose-math.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

const REGION = 257;
const RY_PI = [ -1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1 ];
const SZ = [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1 ];

/*
================
holderModel

A body with one rotated, translated marker, so a missing conversion cannot
pass as the identity.
================
*/
function holderModel() {
	// 0.8 rad about the unit axis (1, 2, 2) / 3.
	const half = .4, sin = Math.sin( half ) / 3;
	return {
		nodes: [ {
			name: "hand",
			parent: -1,
			translation: [ 2, 3, 4 ],
			rotation: [ sin, 2 * sin, 2 * sin, Math.cos( half ) ],
			scale: [ 1, 1, 1 ]
		} ],
		primitives: [],
		images: [],
		clips: []
	};
}

/*
================
product

The column-major product of the given 4x4 matrices, left to right.
================
*/
function product( ...matrices ) {
	let result = Float32Array.from( matrices[0] );
	for ( const next of matrices.slice( 1 ) ) {
		const out = new Float32Array( 16 );
		multiply( result, next, out );
		result = out;
	}
	return result;
}

/*
================
assertMatrix
================
*/
function assertMatrix( actual, expected, label ) {
	assert.ok( actual, label );
	for ( let i = 0; i < 16; i++ ) {
		assert.ok( Math.abs( actual[i] - expected[i] ) < 1e-5, `${label}[${i}]: ${actual[i]} != ${expected[i]}` );
	}
}

/*
================
scene

The holder, its socket and placement, and a builder for an attached child.
================
*/
function scene() {
	const renderer = createCharacters();
	renderer.model( "body", holderModel(), [] );
	const holder = {
		gid: 1,
		model: "body",
		pose: { regionId: REGION, x: 10, y: 0, z: 20, yaw: radians( .7 ) },
		scale: 1,
		clip: "",
		time: 0,
		loop: true
	};
	const socket = renderer.localMatrix( [ holder ], 1, "hand" );
	const P = placement( REGION, REGION, 10, 0, 20, radians( .7 ) );
	const child = ( attachment, extra = {} ) => ({
		gid: 2,
		model: "fx",
		pose: holder.pose,
		scale: 1,
		clip: "",
		time: 0,
		loop: true,
		attachment: { gid: 1, offset: [ 0, 0, 0 ], ...attachment },
		...extra
	});
	return { renderer, holder, socket, P, child };
}

test("an .efp on a named bone draws native space: placement x socket x Sz", () => {
	const { renderer, holder, socket, P, child } = scene();
	const efp = child( { bone: "hand", basis: "native" } );
	assertMatrix( renderer.matrix( [ holder, efp ], 2 ), product( P, socket, SZ ), "efp" );
});

test("an imported mesh on a named bone leaves native space by Ry(PI)", () => {
	const { renderer, holder, socket, P, child } = scene();
	const mesh = child( { bone: "hand", basis: "native-bsr" } );
	assertMatrix( renderer.matrix( [ holder, mesh ], 2 ), product( P, socket, SZ, RY_PI ), "mesh" );
});

test("8D9EC0 rotates in native space, before the mesh conversion", () => {
	const { renderer, holder, socket, P, child } = scene();
	const angle = 1.1, c = Math.fround( Math.cos( angle ) ), s = Math.fround( Math.sin( angle ) );
	// 0x451A10 stores +sin at _13 and -sin at _31.
	const rotation = [ c, 0, s, 0, 0, 1, 0, 0, -s, 0, c, 0, 0, 0, 0, 1 ];
	const mesh = child( { bone: "hand", basis: "native-bsr" }, { effectRotation: { axis: "y", angle } } );
	assertMatrix(
		renderer.matrix( [ holder, mesh ], 2 ),
		product( P, socket, SZ, rotation, RY_PI ),
		"rotated mesh"
	);
});

test("a root binding takes the holder's native world matrix", () => {
	const { renderer, holder, P, child } = scene();
	assertMatrix(
		renderer.matrix( [ holder, child( { bone: "", root: true, basis: "native" } ) ], 2 ),
		product( P, RY_PI ),
		"root efp"
	);
	assertMatrix(
		renderer.matrix( [ holder, child( { bone: "", root: true, basis: "native-bsr" } ) ], 2 ),
		P,
		"root mesh"
	);
});

test("an '@' binding keeps the bone position and drops its rotation (8D6AFF)", () => {
	const { renderer, holder, socket, P, child } = scene();
	const kept = defined( renderer.matrix( [ holder, child( { bone: "hand", basis: "native" } ) ], 2 ) );
	const efp = renderer.matrix( [ holder, child( { bone: "hand", basis: "native", keepRotation: false } ) ], 2 );
	const identity = [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, kept[12], kept[13], kept[14], 1 ];
	assertMatrix( efp, identity, "efp" );
	const mesh = renderer.matrix(
		[ holder, child( { bone: "hand", basis: "native-bsr", keepRotation: false } ) ],
		2
	);
	assertMatrix( mesh, product( identity, RY_PI ), "mesh" );
	assert.ok( Math.abs( kept[12] - product( P, socket )[12] ) < 1e-5 );
});

test("an effect basis is the exact world basis its producer states", () => {
	const { renderer } = scene();
	/** @type {[number, number, number, number, number, number, number, number, number]} */
	const basis = [ 0, 0, 1, 0, 1, 0, -1, 0, 0 ];
	const actor = {
		gid: 3,
		model: "fx",
		pose: { regionId: REGION, x: 1, y: 2, z: 3, yaw: radians( 0 ) },
		scale: 2,
		clip: "",
		time: 0,
		loop: true,
		effectBasis: basis
	};
	const matrix = defined( renderer.matrix( [ actor ], 3 ) );
	for ( let c = 0; c < 3; c++ ) {
		for ( let r = 0; r < 3; r++ ) assert.equal( matrix[c * 4 + r], basis[c * 3 + r] * 2 );
	}
});
