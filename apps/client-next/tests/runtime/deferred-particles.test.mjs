/*
===========================================================================

deferred-particles.test.mjs - tests for deferred-particles.ts, characters.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createDeferredParticles, particleQueryPoint } = await import(
	sourceFileUrl( "src/engine/foundation/animation/deferred-particles.ts" ).href
);
const actor = { gid: 1, model: "effect", time: 0, deferredParticle: { offset: 11 } };
/*
================
ask

One frame's query slot: plan the queue, then send the query when one is due.
================
*/
function ask( owner, ids, enabled ) {
	owner.plan( ids, enabled );
	return owner.issue();
}
test("deferred visibility controls native 50-ms ticks and strict query cadence", () => {
	const owner = createDeferredParticles();
	function frame( time, result ) {
		owner.begin( time, [ { ...actor, time } ] );
		const query = ask( owner, [ 1 ], new Set( [ 1 ] ) );
		if ( query ) owner.deliver( [ result ] );
		owner.complete();
		return { query, ...owner.sample( 1 ) };
	}
	assert.equal( frame( 0 ).draw, false );
	assert.equal( frame( .5 ).query, false );
	let row = frame( .501, true );
	assert.equal( row.query, true );
	assert.equal( row.alpha, 1 );
	assert.equal( row.time, 0 );
	row = frame( .601 );
	assert.equal( row.alpha, 151 );
	assert.equal( row.time, .1 );
	row = frame( .801 );
	assert.equal( row.alpha, 255 );
	assert.ok( Math.abs( row.time - .3 ) < 1e-8 );
	row = frame( 1.002, false );
	assert.equal( row.alpha, 0 );
	assert.equal( row.draw, false );
	assert.ok( Math.abs( row.time - .3 ) < 1e-8 );
	row = frame( 1.302 );
	assert.ok( Math.abs( row.time - .3 ) < 1e-8 );
	row = frame( 1.503, true );
	assert.equal( row.alpha, 255 );
	assert.ok( Math.abs( row.time - .5 ) < 1e-8 );
	owner.begin( 2, [] );
	assert.equal( owner.sample( 1 ), undefined );
	owner.reset();
});
test("missing visible frames reset fading, retired IDs and model replacements cannot inherit query results", () => {
	const owner = createDeferredParticles();
	owner.begin( 0, [ actor ] );
	ask( owner, [ 1 ], new Set( [ 1 ] ) );
	owner.complete();
	owner.begin( .6, [ actor ] );
	ask( owner, [ 1 ], new Set( [ 1 ] ) );
	owner.deliver( [ true ] );
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 255 );
	owner.begin( .7, [ actor ] );
	ask( owner, [], new Set() );
	owner.begin( 1.2, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), true );
	owner.deliver( [ false ] );
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 0 );
	owner.begin( 1.3, [ { ...actor, model: "replacement" } ] );
	assert.equal( owner.sample( 1 ).time, 0 );
	assert.equal( owner.sample( 1 ).visible, false );
	assert.throws( () => ask( owner, [ 1, 1 ], new Set( [ 1 ] ) ), /queue/ );
});
test("query points preserve camera offsets and zero-distance normalization", () => {
	assert.deepEqual( particleQueryPoint( [ 1, 2, 3 ], [ 1, 2, 13 ], 11 ), [ 1, 2, 13 ] );
	assert.deepEqual( particleQueryPoint( [ 1, 2, 3 ], [ 1, 2, 3 ], 11 ), [ 1, 2, 3 ] );
	assert.deepEqual( particleQueryPoint( [ 1, 2, 3 ], [ 99, 98, 97 ], 1 ), [ 1, 2, 3 ] );
});

test("production renderer freezes the actual EFP graph while hidden and resumes without catch-up, in one pass", async () => {
	const { createCharacters } = await import(
		sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
	);
	const { GRAPHIC_OPTION_CHARACTERS } = await import(
		sourceFileUrl( "src/engine/foundation/rendering/graphic-options.ts" ).href
	);
	const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const graph = [ {
		parent: -1,
		parents: [ 0 ],
		births: [ 0 ],
		frames: 400,
		keepMatrix: true,
		keepOrigin: true,
		positionDepth: 0,
		matrixDepth: 0,
		velocityDepth: 0,
		followDepth: 0,
		scales: [],
		positions: [],
		rotations: [],
		program: { vectors: [ { name: "SetVelocity", value: [ 1, 0, 0 ], flags: 0, frames: [ 0 ] } ] }
	} ];
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ { name: "effect", duration: 20, channels: [] } ],
		particleGraph: graph,
		primitives: [ {
			name: "particle",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			particleEmitter: 0,
			emission: { births: [ 0 ], lifetime: 20, capacity: 1 },
			geometry: {
				positions: new Float32Array( 9 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity(),
				material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: true, doubleSided: true }
			}
		} ]
	};
	const { createParticleReference } = await import( "../helpers/particle-reference.mjs" );
	const owner = createCharacters(), reference = createParticleReference();
	owner.model( "effect", model, [] );
	owner.model( "ordinary", {
		...model,
		particleGraph: undefined,
		primitives: model.primitives.map( p => ({ ...p, particleEmitter: undefined, emission: undefined }) )
	}, [] );
	const gpu = {
		upload( g ) {
			return { deferred: !!g.material.deferredParticle, instances: [ ...g.instances ] };
		},
		release( d ) {
			reference.release( d );
		},
		updateInstances( d, v ) {
			d.instances = [ ...v ];
			return d;
		},
		presentParticles( d, particles ) {
			d.instances = [ ...reference.present( d, particles ).matrices ];
		},
		updateBones( d ) {
		}
	};
	// The answer of a query lands between frames (renderer.ts: the frame
	// never waits for it), so it lights the frame after the one that asked.
	let answer = null;
	function frame( time, visible = true ) {
		owner.actors( [ {
			...actor,
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
			clip: "effect",
			time,
			loop: true,
			scale: 1
		}, {
			...actor,
			gid: 2,
			model: "ordinary",
			deferredParticle: undefined,
			pose: { regionId: 257, x: 5, y: 0, z: 0, yaw: 0 },
			clip: "effect",
			time,
			loop: true,
			scale: 1
		} ] );
		if ( answer !== null ) owner.deliverDeferred( [ answer ] );
		answer = null;
		const draws = owner.prepare( gpu, {}, 257, undefined, false, time );
		const plan = owner.deferredPlan( [ 0, 0, 30 ] );
		assert.ok( plan, "the light effect is queued" );
		if ( plan.query ) answer = visible;
		assert.ok( draws.some( d => !d.deferred ), "the ordinary actor draws in the same pass" );
		return { query: plan.query, drawn: draws.filter( d => d.deferred ).map( d => d.instances[12] ) };
	}
	// Each drawn particle sits at its tick position plus less than one tick of
	// motion: presentation carries the pending fraction (particle-presentation.ts).
	const atTick = ( { drawn }, ticks ) => {
		assert.equal( drawn.length, ticks.length );
		drawn.forEach( ( value, i ) =>
			assert.ok( value >= ticks[i] - 1e-4 && value < ticks[i] + 1, `${value} vs ${ticks[i]}` )
		);
	};
	try {
		atTick( frame( 0 ), [] );
		atTick( frame( .5 ), [] );
		// The 500 ms slot asks; the answer lights the next frame.
		const asked = frame( .501 );
		assert.equal( asked.query, true );
		atTick( asked, [] );
		atTick( frame( .601 ), [ 2 ] );
		atTick( frame( .801 ), [ 6 ] );
		// Hidden from 1.002's answer on: no draw and no tick.
		atTick( frame( 1.002, false ), [ 10 ] );
		atTick( frame( 1.302 ), [] );
		atTick( frame( 1.503 ), [] );
		// Seen again: it resumes at the tick it stopped on, without catch-up.
		atTick( frame( 1.603 ), [ 12 ] );
		// /char hides every character: the last pass's light effects leave
		// the query with them.
		owner.setRenderMode( GRAPHIC_OPTION_CHARACTERS, false );
		assert.deepEqual( owner.prepare( gpu, {}, 257, undefined, false, 1.703 ), [] );
		assert.equal( owner.deferredPlan( [ 0, 0, 30 ] ), null );
		owner.setRenderMode( GRAPHIC_OPTION_CHARACTERS, true );
		// A preview pass runs no particle query.
		owner.prepare( gpu, {}, 257, identity(), true, 1.803 );
		assert.equal( owner.deferredPlan( [ 0, 0, 30 ] ), null );
	} finally {
		owner.dispose( gpu, null );
	}
});

test("ordinary fallback keeps instance alpha, bypasses query cadence and ticks even at zero alpha", () => {
	const owner = createDeferredParticles();
	owner.begin( 0, [ actor ], false );
	assert.equal( owner.sample( 1 ).instanceAlpha, 255 );
	assert.equal( owner.sample( 1 ).draw, true );
	owner.begin( .5, [ actor ], false );
	assert.equal( owner.sample( 1 ).time, .5 );
	owner.begin( .6, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), false );
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 0 );
	owner.begin( 1.2, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), true );
	owner.deliver( [ true ] );
	owner.complete();
	assert.equal( owner.sample( 1 ).instanceAlpha, 255 );
	owner.begin( 1.8, [ actor ] );
	ask( owner, [ 1 ], new Set( [ 1 ] ) );
	owner.deliver( [ false ] );
	owner.complete();
	assert.equal( owner.sample( 1 ).instanceAlpha, 0 );
	const before = owner.sample( 1 ).time;
	owner.begin( 2, [ actor ], false );
	assert.equal( owner.sample( 1 ).instanceAlpha, 0 );
	assert.ok( owner.sample( 1 ).time > before );
	owner.begin( 2.5, [ { ...actor, deferredParticle: { offset: 11, nightOnly: true } } ], false, false );
	assert.equal( owner.sample( 1 ).draw, false );
	const paused = owner.sample( 1 ).time;
	owner.begin( 3, [ { ...actor, deferredParticle: { offset: 11, nightOnly: true } } ], false, false );
	assert.equal( owner.sample( 1 ).time, paused );
});

test("a query in flight completes on last visibility and its answer lands on a later frame", () => {
	const owner = createDeferredParticles();
	owner.begin( 0, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), false );
	owner.complete();
	owner.begin( .6, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), true );
	// The issuing frame completes without the answer: no throw, no fade yet.
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 0 );
	// The next cadence slot does not issue a second query behind the first.
	owner.begin( 1.2, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), false );
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 0 );
	// A malformed answer is refused and the query stays in flight.
	assert.throws( () => owner.deliver( [ true, false ] ), /results/ );
	owner.deliver( [ true ] );
	owner.begin( 1.3, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), false );
	owner.complete();
	assert.equal( owner.sample( 1 ).visible, true );
	assert.ok( owner.sample( 1 ).alpha > 0 );
	// Nothing is in flight any more.
	assert.throws( () => owner.deliver( [ true ] ), /results/ );
});

test("an answer issued for a replaced model does not light its replacement", () => {
	const owner = createDeferredParticles();
	owner.begin( 0, [ actor ] );
	ask( owner, [ 1 ], new Set( [ 1 ] ) );
	owner.complete();
	owner.begin( .6, [ actor ] );
	assert.equal( ask( owner, [ 1 ], new Set( [ 1 ] ) ), true );
	owner.begin( .7, [ { ...actor, model: "replacement" } ] );
	owner.deliver( [ true ] );
	assert.equal( owner.sample( 1 ).visible, false );
	ask( owner, [ 1 ], new Set( [ 1 ] ) );
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 0 );
});
