/*
===========================================================================

cosmetic-pose-admission.test.mjs - overload may reuse a pose, never a bind pose

The rig's authored poses differ from rest, so a T-pose initialization or a
missed action transition is observable in the actual renderer socket matrix.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

/*
================
rig
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function rig() {
	const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	return {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ "stand", "run", "hit" ].map( ( name, index ) => ({
			name,
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( index + 1, 0, 0, index + 1, 0, 0 )
			} ]
		}) ),
		primitives: [ {
			name: "body",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity,
			image: -1,
			geometry: { positions: new Float32Array( 9 ), indices: Uint32Array.of( 0, 1, 2 ), transform: identity }
		} ]
	};
}

test("exhausted cosmetic budget still samples cold rigs, new clips, event layers and protected targets", () => {
	const c = createCharacters();
	c.model( "body", rig(), [] );
	c.frameWork( { level: () => 2, remaining: () => 0, spend() {} } );
	/** @type {import("../../src/engine/contracts/character.ts").CharacterActor} */
	let actor = {
		gid: 1,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true,
		animationLod: { fraction: .8, crowded: true, optional: true }
	};
	/** @type {any} */
	const gpu = { upload: () => ({}), updateInstances: draw => draw, updateBones() {}, release() {} };
	const sample = time => {
		c.actors( [ actor ] );
		c.prepare( gpu, gpu, 257, undefined, false, time );
		const matrix = c.localMatrix( [ actor ], 1, "root" );
		assert.ok( matrix );
		return matrix[12];
	};
	assert.equal( sample( 0 ), 1, "first draw uses authored stand, never rest" );
	actor = { ...actor, clip: "run", time: .016 };
	assert.equal( sample( .016 ), 2, "clip entry bypasses both cosmetic and distance LOD" );
	actor = { ...actor, layers: [ { clip: "hit", time: 0, loop: false, weight: 1, lane: "event" } ] };
	assert.equal( sample( .032 ), 3, "a new action cannot reuse the old locomotion sample" );
	actor = {
		...actor,
		clip: "stand",
		layers: undefined,
		animationLod: { fraction: .8, crowded: true, optional: false }
	};
	assert.equal( sample( .048 ), 1, "selected actors remain fully sampled" );
	c.model( "replacement", rig(), [] );
	actor = { ...actor, model: "replacement" };
	assert.equal( sample( .064 ), 1, "replacement resource initializes before drawing" );
	c.dispose( gpu, null );
});

test("optional sampling shares one budget and resumes the oldest resident pose first", () => {
	const c = createCharacters(), model = rig();
	model.clips[0].channels[0].values[3] = 2;
	c.model( "body", model, [] );
	let remaining = 1;
	c.frameWork( {
		level: () => 0,
		remaining: () => remaining,
		spend() {
			remaining = 0;
		}
	} );
	/** @type {any} */
	const gpu = { upload: () => ({}), updateInstances: draw => draw, updateBones() {}, release() {} };
	const sample = time => {
		remaining = 1;
		const actors = [ 1, 2 ].map( gid => ({
			gid,
			model: "body",
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
			scale: 1,
			clip: "stand",
			time: time + (gid - 1) * .01,
			loop: true,
			animationLod: { fraction: 0, crowded: false, optional: true }
		}) );
		c.actors( actors );
		c.prepare( gpu, gpu, 257, undefined, false, time );
		return actors.map( actor => {
			const matrix = c.localMatrix( actors, actor.gid, "root" );
			assert.ok( matrix );
			return matrix[12];
		} );
	};
	const initial = sample( 0 ), first = sample( .2 ), second = sample( .4 );
	assert.ok( initial.every( value => value !== undefined && value >= 1 ), "both cold rigs initialize" );
	assert.ok( first[0] > initial[0] );
	assert.equal( first[1], initial[1], "the second optional unit waits when the shared budget is spent" );
	assert.equal( second[0], first[0] );
	assert.ok( second[1] > first[1], "the deferred actor gets the next budget instead of starving" );
	c.dispose( gpu, null );
});
