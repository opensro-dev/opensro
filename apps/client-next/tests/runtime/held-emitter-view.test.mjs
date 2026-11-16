/*
===========================================================================

held-emitter-view.test.mjs - an emitter held out of view stops its clock

The original ticks an object attached to a compound only on a frame the
compound is in view, by that frame's time (A9EFB0 -> A956F0). These tests
drive the production character renderer (characters.ts) through hidden
frames, with the scenery flag (holderVisible) and with an attachment whose
holder leaves the view, and require the frame after them to draw exactly
what an emitter never hidden draws once the hidden time is taken off its
clock: no particle drawn while hidden, no catch-up after.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacters } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
);
const { createParticleReference } = await import( "../helpers/particle-reference.mjs" );

const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
// A wide orthographic view around the origin, and the same view slid far to
// the side, so that nothing near the origin is in it.
const VIEW = Float32Array.of( .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, .001, 0, 0, 0, .5, 1 );
const AWAY = Float32Array.of( .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, .001, 0, 50, 0, .5, 1 );
const POSE = { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 };

// One particle a tick, moving one unit along x each tick.
const EFFECT = {
	nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
	images: [],
	clips: [ { name: "effect", duration: 20, channels: [] } ],
	particleGraph: [ {
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
	} ],
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
const BODY = {
	...EFFECT,
	particleGraph: undefined,
	clips: [ { name: "idle", duration: 1, channels: [] } ],
	primitives: EFFECT.primitives.map( p => ({ ...p, particleEmitter: undefined, emission: undefined }) )
};

/*
================
renderer

A character renderer with the effect and a body to hold it, on a fake
device whose particle pass is the JavaScript reference.
================
*/
function renderer() {
	const owner = createCharacters(), reference = createParticleReference();
	owner.model( "effect", EFFECT, [] );
	owner.model( "body", BODY, [] );
	const gpu = {
		upload( g ) {
			return { particles: false, instances: [ ...g.instances ] };
		},
		release( d ) {
			reference.release( d );
		},
		updateInstances( d, v ) {
			d.instances = [ ...v ];
			return d;
		},
		presentParticles( d, particles ) {
			d.particles = true;
			d.instances = [ ...reference.present( d, particles ).matrices ];
		},
		updateBones() {}
	};
	return {
		owner,
		/*
		================
		frame

		Draws one frame; returns the x of every drawn particle. view "none"
		draws without a view.
		================
		*/
		/** @param {any[]} actors @param {number} time @param {Float32Array | "none"} [view] */
		frame( actors, time, view = VIEW ) {
			owner.actors( actors );
			const draws = owner.prepare( gpu, {}, 257, view === "none" ? undefined : view, false, time );
			return draws.filter( d => d.particles ).map( d => d.instances[12] );
		},
		dispose() {
			owner.dispose( gpu, null );
		}
	};
}

/*
================
scenery

A scenery emitter whose placement is or is not in view.
================
*/
function scenery( time, holderVisible ) {
	return [ { gid: 1, model: "effect", pose: POSE, clip: "effect", time, loop: true, scale: 1, holderVisible } ];
}

/*
================
held

An emitter attached to a body at the origin.
================
*/
function held( time ) {
	return [
		{ gid: 2, model: "body", pose: POSE, clip: "idle", time, loop: true, scale: 1 },
		{
			gid: 1,
			model: "effect",
			pose: POSE,
			clip: "effect",
			time,
			loop: true,
			scale: 1,
			attachment: { gid: 2, bone: "root", offset: [ 0, 0, 0 ] }
		}
	];
}

test("a scenery emitter whose placement is out of view stops its clock and resumes without catch-up", () => {
	const paused = renderer(), never = renderer();
	try {
		for ( const time of [ 0, .501, .601, .801 ] ) {
			assert.deepEqual( paused.frame( scenery( time, true ), time ), never.frame( scenery( time, true ), time ) );
		}
		assert.ok(
			paused.frame( scenery( .901, true ), .901 ).length,
			"the emitter draws while its placement is in view"
		);
		never.frame( scenery( .901, true ), .901 );
		// Out of view: nothing drawn, and the clock stands still.
		assert.deepEqual( paused.frame( scenery( 1.002, false ), 1.002 ), [] );
		assert.deepEqual( paused.frame( scenery( 1.302, false ), 1.302 ), [] );
		assert.deepEqual( paused.frame( scenery( 1.603, false ), 1.603 ), [] );
		// Back in view 0.902 s later: the frame draws what the emitter drew one
		// frame after .901 had it never left.
		assert.deepEqual( paused.frame( scenery( 1.904, true ), 1.904 ), never.frame( scenery( 1.002, true ), 1.002 ) );
		assert.deepEqual( paused.frame( scenery( 2.004, true ), 2.004 ), never.frame( scenery( 1.102, true ), 1.102 ) );
	} finally {
		paused.dispose();
		never.dispose();
	}
});

test("an emitter held by a body out of view stops its clock and resumes without catch-up", () => {
	const paused = renderer(), never = renderer();
	try {
		for ( const time of [ 0, .501, .601, .801 ] ) {
			assert.deepEqual( paused.frame( held( time ), time ), never.frame( held( time ), time ) );
		}
		assert.ok( paused.frame( held( .901 ), .901 ).length, "the held emitter draws while its holder is in view" );
		never.frame( held( .901 ), .901 );
		// The camera turns away from the holder: nothing drawn, no time owed.
		assert.deepEqual( paused.frame( held( 1.002 ), 1.002, AWAY ), [] );
		assert.deepEqual( paused.frame( held( 1.402 ), 1.402, AWAY ), [] );
		assert.deepEqual( paused.frame( held( 1.503 ), 1.503 ), never.frame( held( 1.002 ), 1.002 ) );
	} finally {
		paused.dispose();
		never.dispose();
	}
});

test("a frame without a view holds nothing out of view", () => {
	const flagged = renderer(), shown = renderer();
	try {
		for ( const time of [ 0, .501, .601, .801, 1.002, 1.302 ] ) {
			assert.deepEqual(
				flagged.frame( scenery( time, false ), time, "none" ),
				shown.frame( scenery( time, true ), time, "none" )
			);
		}
		assert.ok( shown.frame( scenery( 1.402, true ), 1.402, "none" ).length );
	} finally {
		flagged.dispose();
		shown.dispose();
	}
});
