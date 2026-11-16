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
/*
================
identity
================
*/
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
fixture
================
*/
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
test("particle batches retain GPU storage while retired rows become empty and replacements get fresh births", () => {
	const f = fixture();
	for ( const gids of [ [ 1, 2, 3 ], [ 1, 2, 3, 4 ], [ 1, 2, 3 ], [ 1, 2, 3, 5 ] ] ) {
		f.step( gids.map( gid => f.actor( gid, 0, gid * 10 ) ) );
		const positions = [];
		for ( let offset = 12; offset < f.matrices.length; offset += 16 ) positions.push( f.matrices[offset] );
		assert.deepEqual( positions, gids.map( gid => gid * 10 ) );
		assert.equal( f.uploads, 1, "membership changes inside a capacity band must retain the draw" );
		assert.equal( f.releases, 0 );
	}
	f.step( [] );
	assert.equal( f.releases, 1 );
});

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

/*
================
crowdFixture

A renderer holding bodies and the glow emitters attached to them, one
model per actor so every admitted actor is its own draw. step() returns
the gids whose geometry the frame drew; a model's first vertex carries its
actor's gid, since admission copies the geometry. Each glow reserves the
largest particle capacity, so a few dozen fill the render budget.
================
*/
function crowdFixture( body ) {
	const c = createCharacters(), reference = createParticleReference(), births = new Map();
	const gpu = {
		upload( data ) {
			return { gid: data.positions[0], indexCount: 3, instanceCount: 1 };
		},
		updateInstances( draw ) {
			return draw;
		},
		updateBones() {},
		presentParticles( draw, particles ) {
			births.set( draw.gid, reference.present( draw, particles ).matrices[12] );
		},
		release( draw ) {
			reference.release( draw );
		}
	};
	const images = {
		upload() {
			return {};
		},
		release() {}
	};

	const node = name => ({ name, parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] });
	const geometry = ( gid, vertices = 3 ) => {
		const positions = new Float32Array( vertices * 3 );
		positions[0] = gid;
		return { positions, indices: new Uint32Array( [ 0, 1, 2 ] ), transform: identity() };
	};
	return {
		c,
		// The x of each glow's first live particle, as the last frame drew it.
		births,
		/*
		================
		addBody

		A body of nodes joints, drawing vertices vertices.
		================
		*/
		addBody( gid, { nodes = 1, vertices = 3 } = body ) {
			const inverseBind = new Float32Array( nodes * 16 );
			for ( let i = 0; i < nodes; i++ ) inverseBind.set( identity(), i * 16 );
			c.model( "body" + gid, {
				nodes: Array.from(
					{ length: nodes },
					( _, i ) => ({ ...node( i ? "n" + i : "root" ), parent: i ? 0 : -1 })
				),
				images: [],
				clips: [],
				primitives: [ {
					name: "mesh",
					node: 0,
					joints: Array.from( { length: nodes }, ( _, i ) => i ),
					inverseBind,
					image: -1,
					geometry: geometry( gid, vertices )
				} ]
			}, [] );
			return {
				gid,
				model: "body" + gid,
				pose: { regionId: 1, x: gid * 10, y: 0, z: 0, yaw: 0 },
				clip: "",
				time: 0,
				loop: true,
				scale: 1
			};
		},
		/*
		================
		addGlow
		================
		*/
		addGlow( gid, holder ) {
			c.model( "glow" + gid, {
				nodes: [ node( "root" ) ],
				images: [],
				clips: [ { name: "effect", duration: 30, channels: [] } ],
				primitives: [ {
					name: "particle",
					node: 0,
					joints: [ 0 ],
					inverseBind: identity(),
					image: -1,
					emission: { births: [ 0 ], lifetime: 30, capacity: 8192 },
					geometry: geometry( gid )
				} ]
			}, [] );
			return {
				gid,
				model: "glow" + gid,
				pose: { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 },
				attachment: { gid: holder, bone: "root", offset: [ 0, 0, 0 ] },
				clip: "effect",
				time: 1,
				loop: true,
				scale: 1
			};
		},
		step( rows ) {
			c.actors( rows );
			return new Set( c.prepare( gpu, images, 1 ).map( draw => draw.gid ) );
		},
		dispose() {
			c.dispose( gpu, images );
		}
	};
}

test("a crowd's glows never take the render budget its bodies need: bodies plan first, glows share the rest", () => {
	// 16 bodies of 1024 joints (about 390 KB of pose each), two glows each.
	const f = crowdFixture( { nodes: 1024 } ), bodies = [], glows = [];
	for ( let gid = 1; gid <= 16; gid++ ) bodies.push( f.addBody( gid ) );
	for ( let i = 0; i < 32; i++ ) glows.push( f.addGlow( 1000 + i, bodies[i % 16].gid ) );
	const drawn = f.step( [ ...bodies, ...glows ] );
	assert.deepEqual( bodies.filter( b => !drawn.has( b.gid ) ).map( b => b.gid ), [], "every body draws" );
	const glowsDrawn = glows.filter( g => drawn.has( g.gid ) ).length;
	assert.ok( glowsDrawn > 0 && glowsDrawn < glows.length, "the budget holds only some glows" );
	// The glows that draw are the first in admission order (nearest first).
	assert.deepEqual( glows.map( g => drawn.has( g.gid ) ), glows.map( ( _, i ) => i < glowsDrawn ) );
	assert.equal( f.c.stats().deferredActors, glows.length - glowsDrawn );
	assert.equal( f.c.stats().deferredBodies, 0 );
	assert.ok( f.c.stats().renderBytes <= 64 << 20 );
	// A refused glow keeps no particle history, so frame after frame the
	// same bodies still draw.
	assert.deepEqual( f.step( [ ...bodies, ...glows ] ), drawn );
	f.dispose();
});

test("a glow whose holder the render budget refused is not admitted, even when it would fit", () => {
	// Each body's batch takes about 34 MB of the 64 MB budget: one fits.
	const f = crowdFixture( { vertices: 300000 } );
	const near = f.addBody( 1 ), far = f.addBody( 2 ), nearGlow = f.addGlow( 1001, 1 ), farGlow = f.addGlow( 1002, 2 );
	const drawn = f.step( [ near, far, nearGlow, farGlow ] );
	assert.ok( drawn.has( near.gid ) && drawn.has( nearGlow.gid ) );
	assert.ok( !drawn.has( far.gid ) );
	assert.equal( f.c.stats().deferredBodies, 1 );
	assert.ok( !drawn.has( farGlow.gid ), "a glow never draws without its holder" );
	f.dispose();
});

test("a glow the render budget refused restarts when it is admitted again", () => {
	const f = crowdFixture( { nodes: 1 } ), holder = f.addBody( 1 ), crowd = [];
	const glows = Array.from( { length: 11 }, ( _, i ) => f.addGlow( 1000 + i, 1 ) );
	assert.ok( f.step( [ holder, ...glows ] ).has( glows[10].gid ) );
	assert.equal( f.births.get( glows[10].gid ), 10, "born at its holder" );
	// Nearer bodies arrive and take the budget: the eleventh glow gives way.
	for ( let gid = 2; gid <= 17; gid++ ) crowd.push( f.addBody( gid, { nodes: 1024 } ) );
	assert.ok( !f.step( [ holder, ...crowd, ...glows ] ).has( glows[10].gid ) );
	// They leave, and the holder has moved: the glow starts over where its
	// holder is now, not from the particles it had before it gave way.
	const moved = { ...holder, pose: { ...holder.pose, x: 50 } };
	assert.ok( f.step( [ moved, ...glows ] ).has( glows[10].gid ) );
	assert.equal( f.births.get( glows[10].gid ), 50 );
	f.dispose();
});
