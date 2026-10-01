/*
===========================================================================

character-pick.test.mjs - box selection and the mesh refinement it yields to

Uses the shipped Brontes (kyklopess) model, whose rest-pose box spans about
73 units while its body is far narrower: a ray through the empty box must not
confirm the mesh, a ray through the torso must.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
import { avatarToGlb } from "../../../../scripts/build/char/exportGlb.mjs";

const load = async path => import( sourceFileUrl( path ).href );
const { selectPickCandidate, meshUnderRays } = await load( "src/engine/foundation/animation/character-pick.ts" );
const { characterPickVolume } = await load( "src/engine/foundation/animation/character-pick-volume.ts" );
const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
const { createCharacterDecoder } = await load( "src/engine/runtime/assets/worker/model/character/character.ts" );

const IDENTITY = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );

/*
================
model
================
*/
function model( path ) {
	return decodeGlb( readPublishedAssetBytesSync( path, publicRoot ) );
}

/*
================
decodeGlb
================
*/
function decodeGlb( bytes ) {
	const end = 20 + bytes.readUInt32LE( 12 ),
		binary = bytes.subarray( end + 8 );
	const source = createCharacterDecoder().decode( {
		json: JSON.parse( bytes.subarray( 20, end ) ),
		binary: binary.buffer.slice( binary.byteOffset, binary.byteOffset + binary.length )
	} );
	return source.model ?? source;
}

/*
================
downRay

A vertical ray from above through (x, z), spanning the box's height.
================
*/
function downRay( x, z ) {
	return { start: [ x, 200, z ], delta: [ 0, -400, 0 ] };
}

test("the nearest box hit wins and an exact-center hit replaces an off-center winner", () => {
	const actor = gid => ({ gid, clip: "", time: 0 });
	const near = { actor: actor( 1 ), hits: [ { ray: 0, depth: .1, distance: 10 } ] };
	const center = { actor: actor( 2 ), hits: [ { ray: 4, depth: .5, distance: 50 } ] };
	assert.equal( selectPickCandidate( [ near ] ).gid, 1 );
	assert.equal( selectPickCandidate( [ near, center ] ).gid, 2 );
	const closerOffCenter = { actor: actor( 3 ), hits: [ { ray: 1, depth: .05, distance: 5 } ] };
	assert.equal( selectPickCandidate( [ near, center, closerOffCenter ] ).gid, 3 );
	assert.equal( selectPickCandidate( [] ), null );
});

test("a giant's empty rest-pose box does not confirm its mesh; its body does", () => {
	const brontes = model( "/assets/npc/mob/europe/kyklopess.material-1.glb" );
	const box = characterPickVolume( brontes ), pose = createCharacterPose( brontes );
	const candidate = () => ({
		actor: { gid: 7, clip: "stand", time: 0, loop: true },
		matrix: IDENTITY,
		model: brontes,
		hits: [ { ray: 0, depth: .5, distance: 200 } ]
	});
	// Near a box corner: inside the box's footprint, far from the body.
	const corner = downRay( box[0] + 1, box[2] + 1 );
	assert.equal( meshUnderRays( candidate(), [ corner ], pose ), false );
	// Straight down through the model origin meets the torso.
	const torso = downRay( 0, 0 );
	assert.equal( meshUnderRays( candidate(), [ torso ], pose ), true );
});

test("the pick box is the base resource's authored box, carried into model space", () => {
	// Native (x, y, z) exports as glTF (x, y, -z); the decoder's Sx(-1) root
	// makes it (-x, y, -z). The box must land where the vertices do.
	const glb = avatarToGlb( {
		name: "box-fixture",
		aggregateBox: [ -9, 0, -2, 8, 18, 1 ],
		parts: [],
		materials: new Map(),
		clips: [],
		skeleton: {
			boneCount: 1,
			byName: new Map( [ [ "root", 0 ] ] ),
			bones: [ { name: "root", parentIndex: -1, local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] } } ]
		}
	} );
	const decoded = decodeGlb( Buffer.from( glb ) );
	assert.deepEqual( decoded.aggregateBox, [ -8, 0, -1, 9, 18, 2 ] );
	assert.deepEqual( characterPickVolume( decoded ), [ -8, 0, -1, 9, 18, 2 ] );
});
