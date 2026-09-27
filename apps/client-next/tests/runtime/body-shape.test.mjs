/*
===========================================================================

body-shape.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { bodyBoneScale, advanceBodyShape, createCharacterPose } = {
	...(await load( "src/engine/foundation/animation/body-shape.ts" )),
	...(await load( "src/engine/foundation/animation/animation-pose.ts" ))
};
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
test("native volume tables preserve neutral, gender branches and unaffected bones", () => {
	for ( const female of [ false, true ] ) {
		for (
			const name of [
				"Bip01 Spine",
				"Bip01 Spine1",
				"Bip01 L UpperArm",
				"Bip01 R UpperArm",
				"Bip01 L Thigh",
				"Bip01 R Thigh",
				"Bip01 Pelvis",
				"Bone01",
				"Bip01 Head"
			]
		) assert.equal( bodyBoneScale( name, 2, female ), 1 );
	}
	assert.equal( bodyBoneScale( "Bip01 Spine", 0, false ), Math.fround( .88 ) );
	assert.equal( bodyBoneScale( "Bip01 Spine", 0, true ), Math.fround( .95 ) );
	assert.equal( bodyBoneScale( "Bone01", 0, true ), Math.fround( .8 ) );
	assert.equal( bodyBoneScale( "Bone01", 4, false ), 1 );
	assert.equal( bodyBoneScale( "Bip01 Head", 0, true ), 1 );
});
test("retargeting keeps the displayed size and new figures initialize independently", () => {
	let s = advanceBodyShape( null, "a", 1, 2, 0 );
	s = advanceBodyShape( s, "a", 1.06, 4, 1 );
	assert.equal( s.height, 1 );
	s = advanceBodyShape( s, "a", 1.06, 4, 1.5 );
	assert.equal( s.volume, 3 );
	s = advanceBodyShape( s, "a", .94, 0, 1.5 );
	assert.equal( s.volume, 3 );
	s = advanceBodyShape( s, "a", .94, 0, 2 );
	assert.equal( s.volume, 1.5 );
	s = advanceBodyShape( s, "b", 1, 2, 2 );
	assert.equal( s.volume, 2 );
	assert.equal( s.height, 1 );
});
test("radial deformation affects skin palettes, not child translations or sockets; neutral restores GPU eligibility", () => {
	const nodes = [ "Bip01 Spine", "child" ].map( ( name, i ) => ({
		name,
		parent: i - 1,
		translation: [ i * 10, 0, 0 ],
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	}) );
	const primitive = { joints: [ 0, 1 ], inverseBind: Float32Array.from( [ ...identity(), ...identity() ] ) };
	const model = {
		nodes,
		primitives: [ primitive ],
		clips: [ {
			name: "idle",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: new Float32Array( [ 0, 1 ] ),
				values: new Float32Array( 6 )
			} ]
		} ]
	};
	const pose = createCharacterPose( model ), out = new Float32Array( 32 );
	pose.evaluate( "idle", .5, true, undefined, true );
	assert.ok( pose.gpuSample() );
	const socket = pose.socket( "child" );
	pose.bodyVolume( 4, false );
	assert.equal( pose.gpuSample(), null );
	pose.palette( primitive, out );
	assert.equal( out[0], 1 );
	assert.equal( out[5], Math.fround( 1.1 ) );
	assert.equal( out[10], Math.fround( 1.1 ) );
	assert.equal( out[21], 1 );
	assert.equal( out[28], 10 );
	assert.deepEqual( pose.socket( "child" ), socket );
	const revision = pose.revision();
	pose.bodyVolume( 2, false );
	assert.ok( pose.revision() > revision );
	assert.ok( pose.gpuSample() );
	pose.palette( primitive, out );
	assert.equal( out[5], 1 );
});
