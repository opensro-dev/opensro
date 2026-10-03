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
test("deferred visibility controls native 50-ms ticks and strict query cadence", () => {
	const owner = createDeferredParticles();
	function frame( time, result ) {
		owner.begin( time, [ { ...actor, time } ] );
		const query = owner.plan( [ 1 ], new Set( [ 1 ] ) );
		owner.complete( query ? [ result ] : undefined );
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
	owner.plan( [ 1 ], new Set( [ 1 ] ) );
	owner.complete();
	owner.begin( .6, [ actor ] );
	owner.plan( [ 1 ], new Set( [ 1 ] ) );
	owner.complete( [ true ] );
	assert.equal( owner.sample( 1 ).alpha, 255 );
	owner.begin( .7, [ actor ] );
	owner.plan( [], new Set() );
	owner.begin( 1.2, [ actor ] );
	assert.equal( owner.plan( [ 1 ], new Set( [ 1 ] ) ), true );
	owner.complete( [ false ] );
	assert.equal( owner.sample( 1 ).alpha, 0 );
	owner.begin( 1.3, [ { ...actor, model: "replacement" } ] );
	assert.equal( owner.sample( 1 ).time, 0 );
	assert.equal( owner.sample( 1 ).visible, false );
	assert.throws( () => owner.plan( [ 1, 1 ], new Set( [ 1 ] ) ), /queue/ );
});
test("query points preserve camera offsets and zero-distance normalization", () => {
	assert.deepEqual( particleQueryPoint( [ 1, 2, 3 ], [ 1, 2, 13 ], 11 ), [ 1, 2, 13 ] );
	assert.deepEqual( particleQueryPoint( [ 1, 2, 3 ], [ 1, 2, 3 ], 11 ), [ 1, 2, 3 ] );
	assert.deepEqual( particleQueryPoint( [ 1, 2, 3 ], [ 99, 98, 97 ], 1 ), [ 1, 2, 3 ] );
});

test("production renderer freezes the actual EFP graph while hidden and resumes without catch-up", async () => {
	const { createCharacters } = await import(
		sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
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
	let ordinaryWrites = 0;
	const gpu = {
		upload( g ) {
			const deferred = !!g.material.deferredParticle;
			if ( !deferred ) ordinaryWrites++;
			return { deferred, instances: [ ...g.instances ] };
		},
		release( d ) {
			reference.release( d );
		},
		updateInstances( d, v ) {
			if ( !d.deferred ) ordinaryWrites++;
			d.instances = [ ...v ];
			return d;
		},
		presentParticles( d, particles ) {
			if ( !d.deferred ) ordinaryWrites++;
			d.instances = [ ...reference.present( d, particles ).matrices ];
		},
		updateBones( d ) {
			if ( !d.deferred ) ordinaryWrites++;
		}
	};
	function frame( time, visible ) {
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
		owner.prepare( gpu, {}, 257, undefined, false, time );
		const writes = ordinaryWrites;
		const plan = owner.deferredPlan( 257, [ 0, 0, 30 ] );
		owner.completeDeferred( plan.query ? [ visible ] : undefined );
		const draws = owner.prepare( gpu, {}, 257, undefined, false, time, true );
		assert.equal( ordinaryWrites, writes, "deferred continuation repeated ordinary uploads" );
		return draws.map( d => d.instances[12] );
	}
	// Each drawn particle sits at its tick position plus less than one tick of
	// motion: presentation carries the pending fraction (particle-presentation.ts).
	const atTick = ( drawn, ticks ) => {
		assert.equal( drawn.length, ticks.length );
		drawn.forEach( ( value, i ) =>
			assert.ok( value >= ticks[i] - 1e-4 && value < ticks[i] + 1, `${value} vs ${ticks[i]}` )
		);
	};
	try {
		assert.deepEqual( frame( 0 ), [] );
		assert.deepEqual( frame( .5 ), [] );
		atTick( frame( .501, true ), [ 0 ] );
		atTick( frame( .601 ), [ 2 ] );
		atTick( frame( .801 ), [ 6 ] );
		assert.deepEqual( frame( 1.002, false ), [] );
		assert.deepEqual( frame( 1.302 ), [] );
		atTick( frame( 1.503, true ), [ 10 ] );
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
	assert.equal( owner.plan( [ 1 ], new Set( [ 1 ] ) ), false );
	owner.complete();
	assert.equal( owner.sample( 1 ).alpha, 0 );
	owner.begin( 1.2, [ actor ] );
	assert.equal( owner.plan( [ 1 ], new Set( [ 1 ] ) ), true );
	owner.complete( [ true ] );
	assert.equal( owner.sample( 1 ).instanceAlpha, 255 );
	owner.begin( 1.8, [ actor ] );
	owner.plan( [ 1 ], new Set( [ 1 ] ) );
	owner.complete( [ false ] );
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
