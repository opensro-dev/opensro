/*
===========================================================================

compound-sockets.test.mjs - skill markers across body and equipment branches

Exercise actual pose matrices so missing launch points cannot hide behind
catalog presence or a successful damage packet.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { publishEquipmentParticleMetadata } from "../../../../scripts/build/char/equipmentParticles.mjs";

const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { appendEquipmentSockets, equipmentSocket } = await import(
	"../../src/engine/foundation/animation/equipment-sockets.ts"
);

/*
================
node
Build a rigid marker with a visible translation for matrix assertions.
================
*/
function node( name, parent, x ) {
	return { name, parent, translation: [ x, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] };
}

/*
================
socket
Require a resolved marker before examining its matrix.
================
*/
function socket( pose, name ) {
	const matrix = pose.socket( name );
	assert.ok( matrix, `Missing socket ${name}` );
	return matrix;
}

test("unqualified skill markers search attached handles while qualified item markers stay distinct", () => {
	const body = [ node( "hand", -1, 10 ) ];
	const branches = [
		{ part: "WA", attachBone: "hand", nodes: [ node( "ai_end", -1, 2 ) ] },
		{ part: "WL", attachBone: "hand", nodes: [ node( "ai_end", -1, 7 ) ] }
	];
	const pose = createCharacterPose( {
		nodes: appendEquipmentSockets( body, branches, 6 ),
		clips: [],
		primitives: [],
		images: []
	} );
	pose.evaluate( "", 0 );
	assert.equal( socket( pose, "ai_end" )[12], 12 );
	assert.equal( socket( pose, equipmentSocket( 6, "WL", "ai_end" ) )[12], 17 );
	assert.equal( pose.socket( "missing" ), null );
	assert.equal( pose.socket( "$root" ), null );
	const copy = socket( pose, "ai_end" );
	copy[12] = 99;
	assert.equal( socket( pose, "ai_end" )[12], 12 );
});

test("a body marker retains priority over namesakes in attached equipment", () => {
	const pose = createCharacterPose( {
		nodes: appendEquipmentSockets( [ node( "hand", -1, 10 ), node( "ai_end", 0, 3 ) ], [
			{ part: "WA", attachBone: "hand", nodes: [ node( "ai_end", -1, 2 ) ] }
		], 6 ),
		clips: [],
		primitives: [],
		images: []
	} );
	pose.evaluate( "", 0 );
	assert.equal( socket( pose, "ai_end" )[12], 13 );
	assert.equal( socket( pose, equipmentSocket( 6, "WA", "ai_end" ) )[12], 12 );
});

test("ordinary crossbow publication includes unweighted markers without a rare glow or shared cache entry", async () => {
	const dress = { equipment: { 10883: { bodies: { EU_M: { parts: [ "WA" ] }, EU_W: { parts: [ "WA" ] } } } } };
	await publishEquipmentParticleMetadata( dress );
	assert.equal( dress.specialGlows[10883], undefined );
	for ( const entry of Object.values( dress.equipment[10883].bodies ) ) {
		assert.equal( entry.branches.length, 1 );
		const branch = entry.branches[0];
		assert.equal( branch.attachBone, "Bip01 R HandMid" );
		assert.ok( branch.nodes.some( n => n.name === "ai_end" ) );
		const pose = createCharacterPose( {
			nodes: appendEquipmentSockets( [ node( branch.attachBone, -1, 0 ) ], entry.branches, 6 ),
			clips: [],
			primitives: [],
			images: []
		} );
		pose.evaluate( "", 0 );
		assert.ok( socket( pose, "ai_end" ).every( Number.isFinite ) );
	}
});
