/*
===========================================================================

compound-attachments.test.mjs - independent skeletons follow native attach roots

Checks sampled matrices rather than actor presence. A rotated bind pose exposes
hair and avatar resources attached sideways; animation, mounts and missing
markers exercise the same renderer path.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );

/*
================
model

The import reflection is shared by every decoded GLB. The head's bind rotation
must cancel even when its current animation turns through another quarter turn.
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function model() {
	const q = Math.SQRT1_2;
	return {
		nodes: [
			{
				name: "__gltf_left_handed__",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ -1, 1, 1 ]
			},
			{ name: "head", parent: 0, translation: [ 2, 10, 0 ], rotation: [ 0, 0, q, q ], scale: [ 1, 1, 1 ] }
		],
		images: [],
		primitives: [],
		clips: [ {
			name: "turn",
			duration: 1,
			channels: [ {
				node: 1,
				path: "rotation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0 ),
				values: Float32Array.of( 0, 0, 1, 0 )
			} ]
		} ]
	};
}

/*
================
near
================
*/
function near( actual, expected ) {
	assert.equal( actual.length, expected.length );
	for ( let i = 0; i < actual.length; i++ ) {
		assert.ok( Math.abs( actual[i] - expected[i] ) < 1e-5, `${i}: ${actual[i]} != ${expected[i]}` );
	}
}

/*
================
actor
================
*/
/**
 * @param {number} gid
 * @param {Partial<import("../../src/engine/contracts/character.ts").CharacterActor>} extra
 * @returns {import("../../src/engine/contracts/character.ts").CharacterActor}
 */
function actor( gid, extra = {} ) {
	return {
		gid,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		clip: "",
		time: 0,
		loop: true,
		scale: 1,
		...extra
	};
}

test("compound roots cancel the full imported bind rotation and retain current animation", () => {
	const pose = createCharacterPose( model() );
	pose.evaluate( "", 0 );
	near( pose.socket( "head", true ), [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, -2, 10, 0, 1 ] );
	const ordinary = defined( pose.socket( "head" ) );
	assert.ok( Math.abs( ordinary[1] - 1 ) < 1e-5 );
	pose.evaluate( "turn", 0 );
	near( pose.socket( "head", true ), [ 0, -1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, -2, 10, 0, 1 ] );
	assert.equal( pose.socket( "absent", true ), null );
	const copy = defined( pose.socket( "head", true ) );
	copy[12] = 900;
	assert.equal( defined( pose.socket( "head", true ) )[12], -2 );
});

test("renderer compound attachments inherit wearer placement and scale once", () => {
	const renderer = createCharacters();
	renderer.model( "body", model(), [] );
	const body = actor( 1, { scale: 2 } );
	const child = actor( 2, { attachment: { gid: 1, bone: "head", offset: [ 0, 0, 0 ], basis: "compound" } } );
	near( renderer.matrix( [ body, child ], 2 ), [ 2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, -4, 20, 0, 1 ] );
	const turn = { ...body, clip: "turn" };
	near( renderer.matrix( [ turn, child ], 2 ), [ 0, -2, 0, 0, 2, 0, 0, 0, 0, 0, 2, 0, -4, 20, 0, 1 ] );
	const missing = { ...child, attachment: { ...defined( child.attachment ), bone: "absent" } };
	near( renderer.matrix( [ body, missing ], 2 ), renderer.matrix( [ body ], 1 ) );
	assert.equal( renderer.matrix( [ child ], 2 ), null );
});

test("compound attachment fallback resolves the mount marker with the same bind cancellation", () => {
	const renderer = createCharacters();
	renderer.model( "body", model(), [] );
	const source = model();
	const riderModel = { ...source, nodes: source.nodes.slice( 0, 1 ), clips: [] };
	renderer.model( "rider", riderModel, [] );
	const mount = actor( 1, { scale: 3 } );
	const rider = actor( 2, { model: "rider", mountedOn: 1 } );
	const child = actor( 3, { attachment: { gid: 2, bone: "head", offset: [ 0, 0, 0 ], basis: "compound" } } );
	near( renderer.matrix( [ mount, rider, child ], 3 ), [ 3, 0, 0, 0, 0, 3, 0, 0, 0, 0, 3, 0, -6, 30, 0, 1 ] );
});
