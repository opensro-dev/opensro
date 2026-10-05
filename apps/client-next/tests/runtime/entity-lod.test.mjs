/*
===========================================================================

entity-lod.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( file ) {
	return import( sourceFileUrl( "src/engine/foundation/animation/" + file + ".ts" ).href );
}
const { createEntityLod, nativeEntityDistance, nativeEntityLod } = await load( "entity-lod" );
const { createModelEmission } = await load( "model-emission" );
const { createDeferredParticles } = await load( "deferred-particles" );
const point = { regionId: 257, x: 0, y: 0, z: 0 },
	local = { ...point, gid: 1, kind: "local-player" },
	target = { ...point, gid: 2, kind: "monster", x: 800 };
test("local timer and non-appearance spawn force the same scan without resetting the timer deadline", () => {
	const owner = createEntityLod(), entities = [ local, target ];
	owner.step( entities, 1, point, 0 );
	assert.equal( owner.fraction( 2 ), 0 );
	owner.receive( [ { kind: "spawn", entity: { ...target, spawnAppearance: 1 } } ] );
	owner.step( entities, 1, point, 100 );
	assert.equal( owner.fraction( 2 ), 0 );
	owner.receive( [ { kind: "spawn", entity: { ...target, spawnAppearance: 0 } } ] );
	owner.step( entities, 1, point, 200 );
	assert.equal( owner.fraction( 2 ), 1 );
	owner.step( entities, 1, { ...point, x: 800 }, 999 );
	assert.equal( owner.fraction( 2 ), 1 );
	owner.step( entities, 1, { ...point, x: 800 }, 1000 );
	assert.equal( owner.fraction( 2 ), 0 );
	owner.receive( [ { kind: "despawn", gid: 2 } ] );
	assert.equal( owner.fraction( 2 ), 0 );
	owner.step( entities, undefined, point, 2000 );
	assert.equal( owner.fraction( 2 ), 0 );
	owner.reset();
});
test("distance stores and particle threshold distinguish the boundary and sector conversion", () => {
	assert.equal( nativeEntityDistance( point, { ...point, regionId: 258, x: -1920 } ), 0 );
	assert.equal( nativeEntityDistance( { ...point, regionId: 0x8001 }, { ...point, regionId: 0x8002 } ), 0 );
	assert.equal( nativeEntityLod( 720 ), Math.fround( .9 ) );
	assert.ok( nativeEntityLod( 720.001 ) > Math.fround( .9 ) );
	assert.equal( nativeEntityLod( 900, false ), 0 );
});
test("LOD reaches retained production emitter descriptors and freezes ticks without destroying or restarting the instance", () => {
	let id = -1;
	const emission = createModelEmission( () => id-- ), render = createDeferredParticles();
	const actor = { gid: 2, model: "body", pose: { ...point, yaw: 0 }, clip: "stand", time: 0, loop: true, scale: 1 };
	const holders = [ {
		actor,
		particles: [ { effectPath: "system/test.efp", root: true, bone: "", offset: [ 0, 0, 0 ] } ]
	} ];
	const step = ( seconds, lod ) => {
		const rows = emission.step( holders, seconds, () => true, 10, () => lod );
		render.begin( seconds, rows );
		return { actor: rows[0], sample: { ...render.sample( rows[0].gid ) } };
	};
	const first = step( 0, 0 ), near = step( .1, Math.fround( .9 ) );
	assert.ok( near.sample.time > 0 );
	const far = step( .2, 1 );
	assert.equal( far.actor.gid, first.actor.gid );
	assert.equal( far.sample.draw, false );
	assert.equal( far.sample.time, near.sample.time );
	step( 1, 1 );
	const resumed = step( 1.1, 0 );
	assert.equal( resumed.actor.gid, first.actor.gid );
	assert.equal( resumed.sample.draw, true );
	assert.ok( resumed.sample.time > far.sample.time );
	assert.ok( resumed.sample.time < .3 );
});

const { createPoseLod } = await load( "pose-lod" );
test("native crowd pose cadence samples immediately and resets on fraction changes within a bucket", () => {
	const lod = createPoseLod();
	assert.equal( lod.sample( .8, true, 1 ), true );
	assert.equal( lod.sample( .8, true, 1 ), true );
	assert.equal( lod.sample( .8, true, 2 ), false );
	assert.equal( lod.sample( .8, true, 2 ), false );
	assert.equal( lod.sample( .8, true, 3 ), true );
	assert.equal( lod.sample( .8, true, 4 ), false );
	assert.equal( lod.sample( .81, true, 5 ), true );
	assert.equal( lod.sample( .81, false, 6 ), true );
	assert.equal( lod.sample( .81, true, 7 ), false );
	assert.equal( lod.sample( .74, true, 8 ), true );
	assert.equal( lod.sample( .74, true, 9 ), true );
});

test("the scan measures where an entity is, not a lagging row", () => {
	const owner = createEntityLod();
	// The local row still holds its spawn point 800 units back; its movement
	// owner has carried it under the camera.
	const stale = { ...local, x: -800 };
	owner.step( [ stale ], 1, point, 0 );
	owner.step( [ stale ], 1, point, 1000 );
	assert.equal( owner.fraction( 1 ), 1, "a row-measured LOD drifts with the run" );
	owner.step( [ stale ], 1, point, 2000, point );
	assert.equal( owner.fraction( 1 ), 0 );
});
