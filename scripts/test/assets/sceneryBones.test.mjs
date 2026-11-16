/*
===========================================================================

sceneryBones.test.mjs - tests for scripts/build/world/objects/sceneryBones.mjs

A bone-attached scenery emitter (field00 == 0) gets its bone's BSK world
bind matrix; root emitters and bones the skeleton lacks stay as authored.
The fountain values are bagh_abal_foun.bsk's Bone02, whose parent turns
its local +X onto model +Y.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	bindSceneryParticleBones,
	hasSceneryBoneEntries,
	nativeBoneMatrix
} from "../../build/world/objects/sceneryBones.mjs";

const FOUNTAIN_BONE02 = {
	q: [ -0.5053287148475647, -0.5053285956382751, 0.4946138858795166, 0.49461403489112854 ],
	t: [ 0.000006385999768099282, 58.52566146850586, -0.000002511454567866167 ]
};

/*
================
entry
================
*/
function entry( field00, boneName ) {
	return {
		field00,
		effectPath: "map\\cj_pal_potal_blue.efp",
		boneName,
		vector3c: [ 0, 0, 0 ],
		field4c: 0,
		flags50: [ 11, 0, 0 ],
		flag53: 0,
		vector54: null
	};
}

/*
================
modifiersOf
================
*/
function modifiersOf( entries ) {
	return { particleModifiers: [ { kind: 2, stateId: -1, animationSetName: "ambient", entries } ] };
}

const skeleton = { byName: new Map( [ [ "Bone02", 0 ] ] ), bones: [ { world: FOUNTAIN_BONE02 } ] };

test("a bone emitter carries its bone's world bind matrix", () => {
	const [bound] = bindSceneryParticleBones( modifiersOf( [ entry( 0, "Bone02" ) ] ), skeleton )
		.particleModifiers[0].entries;
	assert.equal( bound.boneMatrix.length, 16 );
	assert.deepEqual( bound.boneMatrix.slice( 12 ), FOUNTAIN_BONE02.t.concat( 1 ).map( Math.fround ) );
	// The parent's turn maps the bone's +X onto model +Y.
	const xAxis = bound.boneMatrix.slice( 0, 3 );
	assert.ok(
		Math.abs( xAxis[0] ) < 1e-3 && Math.abs( xAxis[1] - 1 ) < 1e-3 && Math.abs( xAxis[2] ) < 1e-3,
		`${xAxis}`
	);
});

test("root emitters and absent bones stay as authored", () => {
	const modifiers = modifiersOf( [ entry( 1, "" ), entry( 0, "Bone99" ) ] );
	const [root, absent] = bindSceneryParticleBones( modifiers, skeleton ).particleModifiers[0].entries;
	assert.equal( root.boneMatrix, undefined );
	assert.equal( absent.boneMatrix, undefined );
	// No skeleton, no bone entries: the modifiers pass through untouched.
	assert.equal( bindSceneryParticleBones( modifiers, null ), modifiers );
	const plain = modifiersOf( [ entry( 1, "" ) ] );
	assert.equal( hasSceneryBoneEntries( plain ), false );
	assert.equal( bindSceneryParticleBones( plain, skeleton ), plain );
});

test("the identity quaternion keeps the bind translation only", () => {
	assert.deepEqual( nativeBoneMatrix( [ 0, 0, 0, 1 ], [ 1, 2, 3 ] ), [
		1,
		0,
		0,
		0,
		0,
		1,
		0,
		0,
		0,
		0,
		1,
		0,
		1,
		2,
		3,
		1
	] );
});
