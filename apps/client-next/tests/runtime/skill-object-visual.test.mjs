/*
===========================================================================

skill-object-visual.test.mjs - skill-object presentation admission and lifetime

Checks cold assets, object removal, shared models and malformed publication
without replacing the shipped presentation owner with a test renderer.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createSkillObjects, SKILL_OBJECT_MANIFESTS } = await import(
	"../../src/engine/runtime/characters/skill-objects.ts"
);

const RESOURCE = "res/etc/capture_trap.bsr";
const MODEL = "/assets/skillfx/etc/capture_trap.glb";

/*
================
fixture

Give each test its own installation allocator and catalog ownership.
================
*/
function fixture() {
	let id = 100;
	const owner = createSkillObjects( () => id++ );
	owner.catalog( SKILL_OBJECT_MANIFESTS[0], {
		format: "sro-skill-stage-models",
		objects: { 7108: { kind: "model", path: RESOURCE } },
		models: { [RESOURCE]: { glb: MODEL, clips: [ "stand" ], clipLoop: false } }
	} );
	const entity = {
		gid: 0x01000001,
		refObjId: 0xffffffff,
		kind: "skill-object",
		name: "",
		regionId: 0x6454,
		x: 10,
		y: 20,
		z: 30,
		heading: 0,
		skillObject: { skillId: 7108 }
	};
	return { owner, entity };
}

test("a cold trap starts at zero after admission and retires with world scope", () => {
	const { owner, entity } = fixture();
	const ready = { ready: () => true, plan: () => true };
	assert.equal( owner.frame( entity, 10, { ready: () => false, plan: () => true } ), null );
	assert.equal( owner.frame( entity, 20, { ready: () => true, plan: () => false } ), null );
	const first = owner.frame( entity, 30, ready );
	assert.equal( first?.actor.time, 0 );
	assert.equal( first?.actor.model, MODEL );
	assert.equal( first?.actor.pickable, false );
	assert.equal( first?.actor.scale, 1 );
	assert.equal( first?.actor.pose.y, 20 );
	assert.equal( owner.frame( entity, 32, ready )?.actor.time, 2 );
	owner.retain( [ entity ] );
	assert.equal( owner.frame( entity, 33, ready )?.actor.modifierId, first?.actor.modifierId );
	owner.retain( [] );
	const reentry = owner.frame( entity, 40, ready );
	assert.equal( reentry?.actor.time, 0 );
	assert.notEqual( reentry?.actor.modifierId, first?.actor.modifierId );
	owner.reset();
	assert.equal( owner.frame( entity, 50, ready )?.actor.time, 0 );
	owner.dispose();
	assert.equal( owner.frame( entity, 60, ready ), null );
});

test("malformed resource replacements cannot erase a usable object catalog", () => {
	const { owner, entity } = fixture();
	const ready = { ready: () => true, plan: () => true };
	assert.throws( () =>
		owner.catalog( SKILL_OBJECT_MANIFESTS[0], {
			format: "sro-skill-stage-models",
			objects: { 7108: { kind: "model", path: "../unpublished.bsr" } },
			models: {}
		} )
	);
	assert.throws( () =>
		owner.catalog( SKILL_OBJECT_MANIFESTS[0], {
			format: "sro-skill-stage-models",
			objects: { 7108: { kind: "model", path: RESOURCE } },
			models: { [RESOURCE]: { glb: MODEL, clips: [ 42 ], clipLoop: false } }
		} )
	);
	assert.equal( owner.frame( entity, 0, ready )?.actor.model, MODEL );
	assert.equal( owner.frame( { ...entity, skillObject: { skillId: 999 } }, 0, ready ), null );
});
