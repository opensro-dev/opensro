/*
===========================================================================

scenery-particles.test.mjs - tests for scenery-particles.ts,
scenery-emission.ts, deferred-particles.ts, program.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
import { collectSceneryParticleReferences } from "../../../../scripts/build/effects/buildEffectPrograms.mjs";
import { defined } from "../helpers/defined.mjs";
const { sceneryParticles, sceneryOrientation, holderSphere, placedSphere } = await import(
	"../../src/engine/foundation/rendering/scenery-particles.ts"
);
const { createSceneryEmission } = await import( "../../src/engine/foundation/animation/scenery-emission.ts" );
const { createDeferredParticles } = await import( "../../src/engine/foundation/animation/deferred-particles.ts" );
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );
const { createTextureAtlas } = await import( "../../src/engine/foundation/rendering/texture-atlas.ts" );
const { createMaterialTimeline } = await import( "../../src/engine/foundation/rendering/material-timeline.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );

test("every installed outdoor particle record projects and every referenced EFP compiles", () => {
	const index = JSON.parse(
			readPublishedAssetBytesSync( "/assets/world/outdoor/object-resources.json", publicRoot )
		),
		warnings = [];
	let count = 0;
	for ( const root of index.bsr ) {
		for ( const [i, b] of (root.branches ?? [ root ]).entries() ) {
			count += sceneryParticles( b.modifiers?.particleModifiers, String( root.objectId ), i, 257, identity(), m =>
				warnings.push( [ b.sourcePath, m ] ) ).length;
		}
	}
	assert.deepEqual( warnings, [] );
	assert.equal(
		count,
		index.bsr.flatMap( r => r.branches ?? [ r ] ).flatMap( r => r.modifiers?.particleModifiers ?? [] ).reduce(
			( n, m ) => n + m.entries.length,
			0
		)
	);
	assert.ok( count > 0 );
	const bytes = readPublishedAssetBytesSync( "/assets/effects/programs.json", publicRoot ),
		decoder = createEffectPrograms(),
		refs = new Set( collectSceneryParticleReferences( index ).map( r => r.effectPath ) );
	assert.ok( refs.size >= 47 );
	// An EFP the archive proves empty or absent (v1.696 ships
	// dun/petra_flame_yellow_glow.efp at 0 bytes) fails B1F270's header read;
	// AFF730 keeps the reset object, so that emitter draws nothing. The
	// catalogue names exactly those (buildEffectPrograms.mjs); every other
	// referenced EFP must draw.
	const unavailable = new Set( JSON.parse( new TextDecoder().decode( bytes ) ).nativeUnavailable?.effects ?? [] );
	for ( const path of refs ) {
		const result = decoder.decode( bytes, path );
		if ( unavailable.has( path ) ) assert.equal( result.model.primitives.length, 0, path );
		else assert.ok( result.model.primitives.length > 0, path );
	}
});
test("emitter placement, night gating, resource admission, origin crossing and reset retain independent identity", () => {
	const m = identity();
	m[12] = 10;
	m[13] = 20;
	m[14] = 30;
	const modifiers = [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ {
			field00: 1,
			field4c: 0,
			flags50: [ 14, 1, 0 ],
			flag53: 0,
			vector3c: [ 1, 2, 3 ],
			effectPath: "map/fire.efp"
		} ]
	} ];
	const emitters = sceneryParticles( modifiers, "object", 0, 257, m, () => assert.fail() );
	assert.deepEqual( emitters[0].pose, { regionId: 257, x: 11, y: 22, z: 33, yaw: 0 } );
	assert.deepEqual( [ ...sceneryOrientation( [ 0, 0, 0 ] ) ].map( v => v || 0 ), [ ...identity() ] );
	const owner = createSceneryEmission();
	assert.equal( owner.step( { emitters, night: false }, 0, () => true, 10 ).length, 0 );
	assert.equal(
		owner.step( { emitters, night: true }, .5, () => true, 10 ).length,
		0,
		"night setter does not retry a refused native activation"
	);
	owner.reset();
	assert.equal( owner.step( { emitters, night: true }, 1, () => false, 10 ).length, 0 );
	const first = owner.step( { emitters, night: true }, 5, () => true, 10 )[0];
	assert.equal( first.time, 0 );
	assert.deepEqual( first.deferredParticle, { offset: 14, nightOnly: true } );
	const crossed = emitters.map( e => ({ ...e, pose: { ...e.pose, regionId: 258, x: e.pose.x - 1920 } }) );
	const next = owner.step( { emitters: crossed, night: true }, 6, () => true, 10 )[0];
	assert.equal( next.gid, first.gid );
	assert.equal( next.time, 1 );
	assert.equal( next.pose.x, -1909 );
	const daylight = owner.step( { emitters, night: false }, 7, () => true, 10 )[0];
	assert.equal( daylight.gid, first.gid, "daylight retains the instance for renderer gating" );
	const resumed = owner.step( { emitters, night: true }, 8, () => true, 10 )[0];
	assert.equal( resumed.gid, first.gid );
	assert.equal( resumed.time, 3, "source time does not restart; renderer owns paused EFP time" );
	owner.reset();
	assert.deepEqual( owner.step( null, 9, () => true, 10 ), [] );
});
test("a bone emitter stands on its published bind matrix, an absent bone on the object root", () => {
	const m = identity();
	m[12] = 10;
	m[13] = 20;
	m[14] = 30;
	// Bone bind: +X turned onto +Y, standing 5 above the model origin.
	const bone = [ 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1, 0, 0, 5, 0, 1 ];
	const authored = {
		field00: 0,
		boneName: "Bone02",
		field4c: 0,
		flags50: [ 11, 0, 0 ],
		flag53: 0,
		vector3c: [ 1, 2, 3 ],
		effectPath: "map/cj_pal_potal_blue.efp"
	};
	const modifiers = entries => [ { kind: 2, stateId: -1, animationSetName: "ambient", entries } ];
	const [onBone] = sceneryParticles(
		modifiers( [ { ...authored, boneMatrix: bone } ] ),
		"o",
		0,
		257,
		m,
		() => assert.fail()
	);
	// AEC870: the bone matrix owns the translation; vector3c applies only at the root.
	assert.deepEqual( onBone.pose, { regionId: 257, x: 10, y: 25, z: 30, yaw: 0 } );
	assert.deepEqual( onBone.basis, [ 0, 1, 0, -1, 0, 0, 0, 0, 1 ] );
	const warnings = [];
	const [absent] = sceneryParticles( modifiers( [ authored ] ), "o", 0, 257, m, w => warnings.push( w ) );
	assert.deepEqual( absent.pose, { regionId: 257, x: 11, y: 22, z: 33, yaw: 0 } );
	assert.equal( warnings.length, 1 );
	assert.throws( () =>
		sceneryParticles( modifiers( [ { ...authored, boneMatrix: [ 1, 2 ] } ] ), "o", 0, 257, m, () => {} )
	);
});
test("night-only ordinary particles retain renderer identity and freeze native ticks through daylight", () => {
	const source = {
		id: "night",
		model: "/assets/effects/programs.json#map/fire.efp",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		basis: [ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
		renderPriority: 0,
		nightOnly: true
	};
	const owner = createSceneryEmission(), clock = createDeferredParticles();
	let gid;
	const step = ( seconds, night ) => {
		const actors = owner.step( { emitters: [ source ], night }, seconds, () => true, 10 );
		assert.equal( actors.length, 1 );
		gid ??= actors[0].gid;
		assert.equal( actors[0].gid, gid );
		clock.begin( seconds, actors, true, night );
		return clock.sample( gid );
	};
	assert.equal( defined( step( 0, true ) ).time, 0 );
	assert.equal( defined( step( .1, true ) ).time, .1 );
	assert.equal( defined( step( .2, false ) ).draw, false );
	assert.equal( defined( step( .3, false ) ).time, .1 );
	const resumed = step( .4, true );
	assert.equal( defined( resumed ).draw, true );
	assert.equal( defined( resumed ).deferred, false );
	assert.equal( defined( resumed ).time, .2 );
});

test("atlas preserves forward/reverse, looping, one-shot and native long-update reflection", () => {
	const make = ( start, end, flags ) => createTextureAtlas( { start, end, flags, fps: 10, rows: 2, columns: 4 } );
	const frame = a => Math.round( a.matrix[2] * 4 ) + Math.round( a.matrix[6] * 2 ) * 4;
	for ( const [start, end] of [ [ 0, 3 ], [ 3, 0 ] ] ) {
		const once = make( start, end, 0 );
		once.step( 0 );
		once.step( .5 );
		assert.equal( frame( once ), end );
		once.step( 1 );
		assert.equal( frame( once ), end );
		const loop = make( start, end, 1 );
		loop.step( 0 );
		loop.step( .4 );
		assert.equal( frame( loop ), start );
		const ping = make( start, end, 3 );
		ping.step( 0 );
		ping.step( .4 );
		assert.equal( frame( ping ), start === 0 ? 2 : 1 );
		ping.step( 10 );
		assert.equal( frame( ping ), end );
	}
});
test("material timelines preserve native wrap, clamped bounce and terminal colors", () => {
	const make = mode =>
		createMaterialTimeline( {
			mode,
			duration: 1000,
			flags: 3,
			colors: [ { time: 0, value: [ 1, 0, 0, 1 ] }, { time: 1000, value: [ 0, 1, 0, 1 ] } ]
		} );
	for ( const mode of [ 0, 1, 2 ] ) {
		const clock = make( mode );
		clock.step( 0 );
		clock.step( .5 );
		assert.deepEqual( [ ...clock.rgb ], [ .5, .5, 0 ] );
		clock.step( 1.5 );
		assert.deepEqual( [ ...clock.rgb ], mode === 0 ? [ .5, .5, 0 ] : [ 0, 1, 0 ] );
		clock.step( 2 );
		assert.deepEqual( [ ...clock.rgb ], mode === 0 ? [ 1, 0, 0 ] : mode === 1 ? [ .5, .5, 0 ] : [ 0, 1, 0 ] );
	}
});

test("scenery emitters carry whether their holder is in view, in view by default", () => {
	const m = identity();
	const modifiers = [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ {
			field00: 1,
			field4c: 0,
			flags50: [ 0, 0, 0 ],
			flag53: 0,
			vector3c: [ 0, 0, 0 ],
			effectPath: "map/fire.efp"
		} ]
	} ];
	const emitters = sceneryParticles( modifiers, "object", 0, 257, m, () => assert.fail() );
	const owner = createSceneryEmission();
	assert.equal( owner.step( { emitters, night: true }, 0, () => true, 10 )[0].holderVisible, true );
	const asked = [];
	const hidden = owner.step(
		{
			emitters,
			night: true,
			holderVisible: emitter => {
				asked.push( emitter );
				return false;
			}
		},
		1,
		() => true,
		10
	)[0];
	assert.equal( hidden.holderVisible, false );
	assert.deepEqual( asked, [ emitters[0] ] );
	assert.equal( hidden.time, 1, "the source clock runs on: the renderer stops the emitter's own" );
});

test("a compound's holder sphere holds every mesh box and follows its placement", () => {
	const meshes = new Map( [
		[ "a.bms", { bounds: { min: [ -1, 0, -1 ], max: [ 1, 4, 1 ] } } ],
		[ "b.bms", { bounds: { min: [ 0, 0, 0 ], max: [ 3, 1, 1 ] } } ]
	] );
	assert.equal( holderSphere( [ { meshPaths: [ "absent.bms" ] } ], meshes ), null );
	// Two branches; paths are matched lower-case.
	const local = holderSphere( [ { meshPaths: [ "A.bms" ] }, {
		meshPaths: [ "x.bms" ],
		renderMeshSection: { paths: [ "b.BMS" ] }
	} ], meshes );
	// The union box [-1, 0, -1]..[3, 4, 1]: centre (1, 2, 0), half diagonal 3.
	assert.deepEqual( local, [ 1, 2, 0, 3 ] );
	// Placed at (10, 20, 30), turned a quarter about y, scaled by 2.
	const matrix = Float32Array.of( 0, 0, -2, 0, 0, 2, 0, 0, 2, 0, 0, 0, 10, 20, 30, 1 );
	assert.deepEqual( placedSphere( local, matrix ), [ 10, 24, 28, 6 ] );
});
