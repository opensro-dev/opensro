/*
===========================================================================

projectile-curve.test.mjs - tests for random.ts, projectile-curve.ts,
effects.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createProjectileCurve, advanceProjectileCurve } = await import(
	"../../src/engine/foundation/animation/projectile-curve.ts"
);
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const curve = overrides => ({
	position: [ 0, 0, 0 ],
	destination: [ 100, 0, 0 ],
	velocity: [ 0, 10, 0 ],
	phase: 1,
	elapsedMs: 0,
	pauseMs: 100,
	acceleration: 1,
	...overrides
});
test("HWAN consumes five launch draws in native order and retains independent state", () => {
	const a = createProjectileCurve( [ 0, 0, 0 ], [ 100, 0, 0 ], 1 ),
		b = createProjectileCurve( [ 0, 0, 0 ], [ 100, 0, 0 ], 1 );
	// CRT seed 1 -> 41, 18467, 6334, 26500, 19169.
	assert.equal( a.state, 3403800452 );
	assert.equal( a.curve.pauseMs, 100 );
	assert.equal( a.curve.acceleration, Math.fround( 0.369 + 0.8999999761581421 ) );
	assert.deepEqual( a, b );
	advanceProjectileCurve( a.curve, 100 );
	assert.equal( a.curve.phase, 2 );
	assert.equal( b.curve.phase, 1 );
	assert.ok( a.curve.velocity[0] < 0 );
	assert.ok( a.curve.velocity[1] > 0 );
});
test("pause transition renders at source, steering starts on the following update", () => {
	const c = curve();
	assert.equal( advanceProjectileCurve( c, 0 ), true );
	assert.equal( c.elapsedMs, 0 );
	advanceProjectileCurve( c, 99 );
	assert.equal( c.phase, 1 );
	assert.deepEqual( c.position, [ 0, 0, 0 ] );
	advanceProjectileCurve( c, 1 );
	assert.equal( c.phase, 2 );
	assert.deepEqual( c.position, [ 0, 0, 0 ] );
	advanceProjectileCurve( c, 100 );
	assert.equal( c.phase, 2 );
	assert.ok( c.position[0] > 0 );
	assert.equal( c.position[1], 1 );
	const aligned = curve( { phase: 2, velocity: [ 10, 0, 0 ] } );
	advanceProjectileCurve( aligned, 10 );
	assert.equal( aligned.phase, 3 );
});
test("HWAN timeout is strict, renders its endpoint once and then retires", () => {
	const c = curve( { destination: [ 100000, 0, 0 ] } );
	advanceProjectileCurve( c, 3600 );
	assert.equal( c.phase, 3 );
	assert.equal( advanceProjectileCurve( c, 1 ), true );
	assert.equal( c.phase, 0 );
	assert.deepEqual( c.position, c.destination );
	assert.equal( advanceProjectileCurve( c, 0 ), false );
	assert.equal( advanceProjectileCurve( c, 100 ), false );
	const coincident = curve( { destination: [ 0, 0, 0 ] } );
	assert.equal( advanceProjectileCurve( coincident, 1 ), false );
	assert.throws( () => advanceProjectileCurve( curve(), -1 ), /delta/ );
	assert.throws( () => advanceProjectileCurve( curve(), 0.5 ), /delta/ );
});
test("aligned homing clamps overshoot and does not drift beyond its endpoint", () => {
	const c = curve( { phase: 3, velocity: [ 100, 0, 0 ], destination: [ 1, 0, 0 ] } );
	assert.equal( advanceProjectileCurve( c, 20 ), true );
	assert.equal( c.phase, 0 );
	assert.deepEqual( c.position, [ 1, 0, 0 ] );
});
function fixture( count = 1 ) {
	let id = 0;
	const jobs = new Map(), sounds = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	const stage = {
		resource: "absorption.efp",
		damageEvent: false,
		startEvent: 1,
		action: "AT_SOURCE",
		move: "MOV_HWAN",
		bone: null,
		offset: [ 0, 0, 0 ],
		targetOffset: [ 0, 0, 0 ],
		life: 0,
		sound: "launch.wav",
		soundEnd: "unused.wav",
		arrivalResource: "unused.efp",
		count,
		scripts: [],
		movement: { delayMs: 0, startSpeed: 200, endSpeed: 200 }
	};
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: { "1": { clips: [ "attack1" ], stages: [ stage ] } } } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, refObjId: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
		gid: 2,
		refObjId: 1,
		regionId: 258,
		x: 100,
		y: 0,
		z: 0,
		heading: 0
	} ];
	const cast = { token: 1, caster: 1, target: 2, skill: 1 }, gameplay = { casts: [], localGid: 1 };
	const step = ( time, triggers = [] ) => effects.step( entities, gameplay, time, () => true, () => 0.1, triggers );
	step( 0 );
	step( 0.01 );
	step( 0.02 );
	gameplay.casts = [ cast ];
	return { effects, stage, sounds, gameplay, entities, step, trigger: { cast, phase: "SHOT", event: 1, at: 1 } };
}
test("live source stages reverse endpoints, retain multiple movers, and do not use straight arrival effects", () => {
	const f = fixture( 2 ), first = f.step( 1, [ f.trigger ] );
	assert.equal( first.length, 2 );
	assert.notEqual( first[0].gid, first[1].gid );
	assert.equal( first[0].pose.regionId, 258 );
	assert.equal( first[0].pose.x, 100 );
	assert.equal( f.sounds.length, 2 );
	assert.equal( f.step( 1.05, [ f.trigger ] ).length, 2 );
	assert.equal( f.sounds.length, 2 );
	// Both endpoints were captured. A later caster translation cannot retarget.
	f.entities[0].x = 999;
	const terminal = f.step( 6 );
	assert.equal( terminal.length, 2 );
	for ( const actor of terminal ) {
		assert.equal( actor.pose.regionId, 257 );
		assert.equal( actor.pose.x, 0 );
		assert.ok( actor.model.includes( "absorption" ) );
	}
	assert.deepEqual( f.step( 6.01 ), [] );
	assert.equal( f.sounds.length, 2 );
	assert.equal( f.effects.error(), null );
});
test("HWAN survives cast finalization, reset releases it, and population overflow admits no partial burst", () => {
	const f = fixture();
	assert.equal( f.step( 1, [ f.trigger ] ).length, 1 );
	f.gameplay.casts = [];
	assert.equal( f.step( 1.1 ).length, 1 );
	f.effects.reset();
	assert.deepEqual( f.step( 2 ), [] );
	f.effects.dispose();
	assert.deepEqual( f.step( 3 ), [] );
	const overflow = fixture( 129 );
	assert.deepEqual( overflow.step( 1, [ overflow.trigger ] ), [] );
	// A refused effect is skipped and logged, never an error (#2440).
	assert.equal( overflow.effects.error(), null );
});
