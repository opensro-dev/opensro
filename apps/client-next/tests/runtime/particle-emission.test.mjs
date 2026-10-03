/*
===========================================================================

particle-emission.test.mjs - tests for particle-emission.ts, characters.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { particleEmission, particleBirthFrames } = await import(
	"../../src/engine/foundation/animation/particle-emission.ts"
);
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createParticleReference } = await import( "../helpers/particle-reference.mjs" );
const emitter = { start: 2, duration: 10, period: 2, limit: 3, rate: .5 };
test("fractional emission accumulates across opportunities and stops at its cumulative cap", () => {
	assert.deepEqual( particleBirthFrames( emitter, 20 ), [ 4, 8 ] );
	assert.deepEqual( particleBirthFrames( { ...emitter, duration: 30 }, 40 ), [ 4, 8, 12 ] );
	assert.deepEqual( particleEmission( emitter, 0, 1 ), { emitted: 0, total: 0, closed: false } );
	assert.deepEqual( particleEmission( emitter, 2.5, 12 ), { emitted: 0, total: 2.5, closed: true } );
});
test("emission quality changes the period only below the window and halves positive limits", () => {
	assert.equal( particleEmission( emitter, 0, 4, 2 ).total, 0 );
	assert.equal( particleEmission( emitter, 0, 4, 5 ).total, .5 );
	assert.equal( particleEmission( { ...emitter, rate: 2 }, 0, 2, 1, 3 ).total, 2 );
	assert.deepEqual( particleBirthFrames( { ...emitter, rate: 0 }, 20 ), [] );
	assert.deepEqual( particleBirthFrames( { ...emitter, period: 0 }, 20 ), [] );
});
test("finite particle populations reject malformed schedules and cumulative expansion", () => {
	assert.throws( () => particleBirthFrames( { ...emitter, rate: NaN }, 20 ), /Invalid/ );
	assert.throws( () => particleBirthFrames( { ...emitter, rate: 100, limit: 100 }, 20, 10 ), /population budget/ );
	assert.throws( () => particleBirthFrames( emitter, 1201 ), /schedule budget/ );
});
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
function fixture( frames = true ) {
	const c = createCharacters(), matrices = [], colors = [], opacities = [], reference = createParticleReference();
	let uploads = 0, releases = 0;
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		images: [],
		clips: [ { name: "effect", duration: 2, channels: [] } ],
		primitives: [ {
			name: "particle",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			emission: { births: [ 0, .1, .2 ], lifetime: .25 },
			materialFrames: {
				fps: 4,
				colors: new Float32Array( [ 1, 0, 0, 1, 0, 0, 1, 0 ] ),
				windows: new Float32Array( [ 1, 1, 0, 0, 1, 1, 0, 0 ] )
			},
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	};
	if ( !frames ) delete model.primitives[0].materialFrames;
	c.model( "effect", model, [] );
	const gpu = {
		upload() {
			uploads++;
			return {};
		},
		presentParticles( draw, particles ) {
			const drawn = reference.present( draw, particles );
			matrices.push( drawn.matrices );
			colors.push( drawn.appearance );
			opacities.push( drawn.opacities );
		},
		release( draw ) {
			reference.release( draw );
			releases++;
		}
	};
	const actor = ( gid, time, x ) => ({
		gid,
		model: "effect",
		pose: { regionId: 1, x, y: 0, z: 0, yaw: 0 },
		clip: "effect",
		time,
		loop: false,
		scale: 1
	});
	return {
		c,
		gpu,
		actor,
		get uploads() {
			return uploads;
		},
		get releases() {
			return releases;
		},
		get matrices() {
			return matrices.at( -1 );
		},
		get colors() {
			return colors.at( -1 );
		},
		get opacities() {
			return opacities.at( -1 );
		},
		step( rows, origin = 1, view ) {
			c.actors( rows );
			return c.prepare(
				gpu,
				{
					upload() {
						return {};
					},
					release() {}
				},
				origin,
				view
			);
		}
	};
}
test("arrival stops new births while existing particles retain transforms, fade packing and natural expiration", () => {
	const f = fixture();
	f.step( [ f.actor( -2, 0, 5 ) ] );
	f.step( [ { ...f.actor( -2, .2, 55 ), emissionEnd: .1, opacity: .5 } ] );
	assert.equal( f.matrices.length, 16 );
	assert.equal( f.matrices[12], 5 );
	assert.deepEqual( [ ...f.opacities ], [ .5 ] );
	f.step( [ { ...f.actor( -2, .3, 85 ), emissionEnd: .1, opacity: .25 } ] );
	assert.equal( f.matrices.length, 0 );
	assert.equal( f.opacities.length, 0 );
	f.c.dispose( f.gpu, null );
});

test("an emission stop invalidates retained instances even when actor time and pose are unchanged", () => {
	const f = fixture( false ), actor = f.actor( -2, .2, 5 );
	f.step( [ actor ] );
	f.step( [ actor ] );
	assert.equal( f.matrices.length, 48 );
	f.step( [ { ...actor, emissionEnd: .1 } ] );
	assert.equal( f.matrices.length, 16 );
	assert.throws( () => f.step( [ { ...actor, emissionEnd: NaN } ] ), /emission end/ );
	f.c.dispose( f.gpu, null );
});

test("live births retain their own transforms and ages while sharing one GPU draw", () => {
	const f = fixture();
	f.step( [ f.actor( -2, 0, 0 ) ] );
	assert.equal( f.matrices.length, 16 );
	f.step( [ f.actor( -2, .1, 10 ) ] );
	assert.equal( f.matrices.length, 32 );
	assert.equal( f.matrices[12], 0 );
	assert.equal( f.matrices[28], 10 );
	assert.ok( f.colors[0] < f.colors[8] );
	f.step( [ f.actor( -2, .2, 20 ) ] );
	assert.deepEqual( [ f.matrices[12], f.matrices[28], f.matrices[44] ], [ 0, 10, 20 ] );
	f.step( [ f.actor( -2, .3, 30 ) ] );
	assert.deepEqual( [ f.matrices[12], f.matrices[28] ], [ 10, 20 ] );
	assert.equal( f.uploads, 1 );
	f.step( [ f.actor( -2, .5, 50 ) ] );
	assert.equal( f.matrices.length, 0 );
	assert.equal( f.uploads, 1 );
	f.c.dispose( f.gpu, null );
});
test("birth state survives batch membership and origin changes, resets on rewind and despawn", () => {
	const f = fixture();
	f.step( [ f.actor( -2, 0, 3 ) ] );
	f.step( [ f.actor( -2, .1, 13 ), f.actor( -3, 0, 100 ) ] );
	f.step( [ f.actor( -2, .2, 23 ) ], 2 );
	assert.deepEqual( [ f.matrices[12], f.matrices[28], f.matrices[44] ], [ 3 - 1920, 13 - 1920, 23 - 1920 ] );
	f.c.invalidate();
	f.step( [ f.actor( -2, .21, 24 ) ], 1 );
	assert.deepEqual( [ f.matrices[12], f.matrices[28], f.matrices[44] ], [ 3, 13, 23 ] );
	f.step( [ f.actor( -2, 0, 90 ) ] );
	assert.equal( f.matrices[12], 90 );
	f.step( [] );
	f.step( [ f.actor( -2, 0, 99 ) ] );
	assert.equal( f.matrices[12], 99 );
	f.c.dispose( f.gpu, null );
});
test("particle age changes draw membership even without animated channels or material tables", () => {
	const f = fixture( false );
	f.step( [ f.actor( -2, 0, 0 ) ] );
	f.step( [ f.actor( -2, .1, 0 ) ] );
	assert.equal( f.matrices.length, 32 );
	f.c.dispose( f.gpu, null );
});
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
test("frozen scheduling candidate matches retail x86 callback deltas and float stores", () => {
	const reference = JSON.parse(
		readFileSync( "tests/fixtures/native/native-particle-emission-reference.json", "utf8" )
	);
	assert.equal(
		createHash( "sha256" ).update( readFileSync( "src/engine/foundation/animation/particle-emission.ts" ) ).digest(
			"hex"
		),
		reference.sourceSha256
	);
	assert.equal(
		createHash( "sha256" ).update( readFileSync( "tests/fixtures/native/particle-emission-policy.json" ) ).digest(
			"hex"
		),
		reference.policySha256
	);
	assert.equal( reference.cases.length, 1056 );
	for ( const row of reference.cases ) {
		assert.deepEqual(
			particleEmission( row.emitter, row.total, row.frame, ...row.quality ),
			row.result,
			JSON.stringify( row )
		);
	}
});

test("looped particle effects reset birth positions even when a frame skips whole cycles", () => {
	const f = fixture();
	f.step( [ { ...f.actor( -2, .1, 5 ), loop: true } ] );
	f.step( [ { ...f.actor( -2, 4.1, 55 ), loop: true } ] );
	assert.ok( Math.abs( f.matrices[12] - 55 ) < 1e-6 );
	f.c.dispose( f.gpu, null );
});

test("BAN parent motion evaluates hierarchy at actor.time without smearing late births across particle ages", () => {
	const c = createCharacters(), matrices = [], bones = [], reference = createParticleReference();
	let uploads = 0;
	const model = {
		nodes: [
			{ name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "ban-parent", parent: 0, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "emitter", parent: 1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] }
		],
		images: [],
		clips: [ {
			name: "effect",
			duration: 1,
			channels: [ {
				node: 1,
				path: "translation",
				interpolation: "LINEAR",
				times: new Float32Array( [ 0, 1 ] ),
				values: new Float32Array( [ 0, 0, 0, 100, 0, 0 ] )
			} ]
		} ],
		primitives: [ {
			name: "particle",
			node: 2,
			joints: [ 2 ],
			inverseBind: identity(),
			image: -1,
			emission: { births: [ 0, .1, .2 ], lifetime: .3 },
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	};
	c.model( "ban-effect", model, [] );
	const gpu = {
		upload() {
			uploads++;
			return {};
		},
		presentParticles( draw, particles ) {
			const drawn = reference.present( draw, particles );
			matrices.push( drawn.matrices );
			bones.push( drawn.palettes );
		},
		release( draw ) {
			reference.release( draw );
		}
	};
	const actor = ( time, x ) => ({
		gid: -10,
		model: "ban-effect",
		pose: { regionId: 1, x, y: 0, z: 0, yaw: 0 },
		clip: "effect",
		time,
		loop: false,
		scale: 1
	});
	c.actors( [ actor( 0, 0 ) ] );
	c.prepare( gpu, {
		upload() {
			return {};
		},
		release() {}
	}, 1 );
	c.actors( [ actor( .1, 10 ) ] );
	c.prepare( gpu, {
		upload() {
			return {};
		},
		release() {}
	}, 1 );
	c.actors( [ actor( .2, 20 ) ] );
	c.prepare( gpu, {
		upload() {
			return {};
		},
		release() {}
	}, 1 );

	const lastMatrices = matrices.at( -1 );
	const lastBones = bones.at( -1 );
	// At actor.time = 0.2: 3 active particles.
	// Instance matrices inherit birth positions: [0, 10, 20] along X.
	assert.equal( lastMatrices.length, 48 );
	assert.deepEqual( [ lastMatrices[12], lastMatrices[28], lastMatrices[44] ], [ 0, 10, 20 ] );
	// Bone matrices for all 3 particles inherit the live BAN parent position at actor.time = 0.2:
	// X = 0.2 * 100 = 20. Neither particle is smeared back to age 0.1 (X=10) or age 0.0 (X=0).
	assert.equal( lastBones.length, 48 );
	assert.ok( Math.abs( lastBones[12] - 20 ) < 1e-5 );
	assert.ok( Math.abs( lastBones[28] - 20 ) < 1e-5 );
	assert.ok( Math.abs( lastBones[44] - 20 ) < 1e-5 );
	c.dispose( gpu, null );
});
