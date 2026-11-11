/*
===========================================================================

model-emission.test.mjs - tests for model-emission.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { modelAmbientParticles, createModelEmission } = await import(
	sourceFileUrl( "src/engine/foundation/animation/model-emission.ts" ).href
);
const raw = [ {
	kind: 2,
	stateId: -1,
	animationSetName: "ambient",
	entries: [ {
		field00: 1,
		effectPath: "system\\item_drop_equip.efp",
		boneName: "",
		vector3c: [ 1, 2, 3 ],
		field4c: 0,
		flags50: [ 0, 0, 0 ],
		flag53: 0
	} ]
} ];
const actor = {
	gid: 1,
	model: "body",
	pose: { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 },
	clip: "stand",
	time: 0,
	loop: true,
	scale: 1
};
test("ambient admission keeps named/state modifiers inactive, validates active overrides and converts coordinates once", () => {
	const particles = modelAmbientParticles( [ ...raw, {
		kind: 0,
		stateId: -1,
		animationSetName: "status_bad_burn",
		entries: []
	}, { kind: 1, stateId: 5, animationSetName: "default", entries: [] } ] );
	assert.equal( particles.length, 1 );
	assert.deepEqual( particles[0].offset, [ 1, 2, -3 ] );
	assert.throws(
		() => modelAmbientParticles( [ { ...raw[0], entries: [ { ...raw[0].entries[0], flag53: 1 } ] } ] ),
		/override/
	);
	assert.throws( () => modelAmbientParticles( [ { ...raw[0], kind: 9 } ] ), /selector/ );
});
test("ambient admission accepts the shipped plant_lingt row's nonzero event key without surfacing it", () => {
	// The shipped row (world/dungeon/regions/0x8004.json): the original binds
	// this entry - field4c is its animation-event registration key (AE36C0),
	// never an admission test in CRTMod_BindModelParticles_AE3840 (#3134).
	const particles = modelAmbientParticles( [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ {
			field00: 1,
			effectPath: "dun\\plant_lingt.efp",
			boneName: "",
			vector3c: [ -887.889404296875, -143.37686157226562, 781.6449584960938 ],
			field4c: 80,
			flags50: [ 31, 0, 0 ],
			flag53: 0
		} ]
	} ] );
	assert.equal( particles.length, 1 );
	assert.equal( particles[0].effectPath, "dun/plant_lingt.efp" );
	assert.equal( particles[0].root, true );
	assert.equal( particles[0].deferred, 31 );
	assert.equal( particles[0].triggerMs, undefined );
	assert.deepEqual( particles[0].offset, [ -887.889404296875, -143.37686157226562, -781.6449584960938 ] );
	assert.throws(
		() => modelAmbientParticles( [ { ...raw[0], entries: [ { ...raw[0].entries[0], field4c: -1 } ] } ] ),
		/trigger or override/
	);
	assert.throws(
		() => modelAmbientParticles( [ { ...raw[0], entries: [ { ...raw[0].entries[0], field4c: 1.5 } ] } ] ),
		/trigger or override/
	);
});
test("cold resources, capacity pressure and clip changes preserve bounded emitter ownership; replacement and reset retire it", () => {
	let id = -1, ready = false;
	const owner = createModelEmission( () => id-- ),
		particles = modelAmbientParticles( raw ),
		holders = [ { actor, particles } ];
	assert.deepEqual( owner.step( holders, 0, () => ready, 10 ), [] );
	assert.deepEqual( owner.step( holders, 1, () => ready, 10 ), [] );
	assert.equal( id, -2 );
	ready = true;
	const first = owner.step( holders, 2, () => ready, 10 )[0];
	assert.equal( first.time, 0 );
	assert.deepEqual( owner.step( holders, 3, () => ready, 0 ), [] );
	const resumed = owner.step( [ { actor: { ...actor, clip: "run" }, particles } ], 4, () => ready, 10 )[0];
	assert.equal( resumed.gid, first.gid );
	assert.equal( resumed.time, 2 );
	assert.equal( id, -2 );
	const replaced = owner.step( [ { actor: { ...actor, model: "other" }, particles } ], 5, () => ready, 10 )[0];
	assert.notEqual( replaced.gid, first.gid );
	assert.equal( replaced.time, 0 );
	owner.step( [], 6, () => ready, 10 );
	const respawn = owner.step( holders, 7, () => ready, 10 )[0];
	assert.notEqual( respawn.gid, replaced.gid );
	owner.reset();
	assert.notEqual( owner.step( holders, 8, () => ready, 10 )[0].gid, respawn.gid );
});

test("disappearance transfers the same emitter clock and identity to the private visual holder", () => {
	let id = -1;
	const owner = createModelEmission( () => id-- ), particles = modelAmbientParticles( raw );
	const child = owner.step( [ { actor, particles } ], 1, () => true, 10 )[0];
	assert.equal( owner.transfer( 1, 9 ), particles );
	const next = owner.step( [ { actor: { ...actor, gid: 9, opacity: .5 }, particles } ], 2, () => true, 10 )[0];
	assert.equal( next.gid, child.gid );
	assert.equal( next.time, 1 );
	assert.equal( next.opacity, undefined );
	assert.equal( next.attachment.gid, 9 );
	assert.deepEqual( owner.step( [], 3, () => true, 10 ), [] );
	assert.deepEqual( owner.transfer( 9, 10 ), [] );
});

test("installed override animation hides ambient wrappers without restarting them on restoration", () => {
	let next = -1;
	const owner = createModelEmission( () => next-- ),
		particles = [ { effectPath: "system/a.efp", root: true, bone: "", offset: [ 0, 0, 0 ] } ],
		actor = { gid: 1, model: "body", pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }, scale: 1 };
	const first = owner.step( [ { actor, particles } ], 0, () => true, 10 )[0];
	const hidden = owner.step(
		[ {
			actor: { ...actor, modelAnimation: { selected: { set: "default", state: 2, override: true } } },
			particles
		} ],
		1,
		() => true,
		10
	)[0];
	assert.equal( hidden.gid, first.gid );
	assert.equal( hidden.deferredParticle.lodHidden, true );
	const restored = owner.step( [ { actor, particles } ], 2, () => true, 10 )[0];
	assert.equal( restored.gid, first.gid );
	assert.equal( restored.deferredParticle.lodHidden, false );
});
