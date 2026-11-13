/*
===========================================================================

camera-script.test.mjs - tests for effect-script.ts, camera.ts, random.ts,
effects.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { effectScript, localHitFlash } = await import( "../../src/engine/foundation/animation/effect-script.ts" );
const { createCameraScripts } = await import( "../../src/engine/runtime/world/camera/camera.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { resolveFollowCamera } = await import( "../../src/engine/foundation/rendering/follow-camera.ts" );
const { createWorldStream } = await import( "../../src/engine/runtime/world/world.ts" );
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );
const command = ( atMs = 0, amplitude = 50 ) => ({
	atMs,
	amplitude,
	durationMs: 500,
	periodMs: amplitude === 50 ? 20 : 25
});
test("all captured timer samples and CRT states match execution of the original x86 instructions", () => {
	const native = JSON.parse( readFileSync( "tests/fixtures/native/camera-script-reference.json", "utf8" ) );
	assert.equal( native.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	for ( const row of native.cases ) {
		const random = createPresentationRandom( row.seed, 1, 100 ), camera = createCameraScripts( random );
		camera.step( 0, [ command( 0, row.amplitude ) ] );
		for ( const sample of row.samples ) {
			assert.deepEqual(
				camera.step( sample.now, [] ),
				sample.offset,
				JSON.stringify( { seed: row.seed, amplitude: row.amplitude, now: sample.now } )
			);
			const draws = random.takeTrace();
			assert.equal( draws.length, 2 );
			assert.equal( defined( draws.at( -1 ) ).stateAfter, sample.random );
		}
	}
});
test("all eight native camera scripts decode; malformed parameters stay rejected", () => {
	for ( const [i, amplitude] of [ 50, 200, 300, 400 ].entries() ) {
		for ( const arrival of [ false, true ] ) {
			assert.deepEqual( effectScript( [ `SCT_SHAKECAM${arrival ? "_MOV" : ""}${i}` ] ), {
				kind: "camera",
				arrival,
				amplitude,
				durationMs: 500,
				periodMs: i ? 25 : 20
			} );
		}
	}
	assert.throws( () => effectScript( [ "SCT_SHAKECAM0", "5" ] ), /parameter/ );
	assert.equal( effectScript( [ "SCT_SHAKECAM4" ] ).kind, "unsupported" );
});
test("retail timer decay, replacement, expiration and skipped polls consume the shared RNG exactly", () => {
	const rng = createPresentationRandom( 7, 1, 100 ),
		control = createPresentationRandom( 7 ),
		camera = createCameraScripts( rng );
	assert.deepEqual( camera.step( 0, [ command() ] ), [ 0, 0 ] );
	assert.equal( rng.takeTrace().length, 0 );
	assert.deepEqual( camera.step( 19, [] ), [ 0, 0 ] );
	// float32(.04)*50 truncates to 1, not 2.
	assert.deepEqual( camera.step( 20, [] ), [
		Math.fround( control.range( -49, 49 ) / 100 ),
		Math.fround( control.range( -49, 49 ) / 100 )
	] );
	assert.equal( rng.takeTrace().length, 2 );
	camera.step( 119, [] );
	assert.equal( rng.takeTrace().length, 2, "one poll, no catch-up RNG draws" );
	assert.deepEqual( camera.step( 120, [ command( 120, 400 ) ] ), [ 0, 0 ] );
	camera.step( 145, [] );
	assert.equal( rng.takeTrace().length, 2 );
	assert.deepEqual( camera.step( 620, [] ), [ 0, 0 ] );
	assert.equal( rng.takeTrace().length, 0, "end timer precedes sample timer" );
	camera.step( 700, [ command( 700 ) ] );
	camera.reset();
	assert.deepEqual( camera.step( 750, [] ), [ 0, 0 ] );
	assert.equal( rng.takeTrace().length, 0 );
});
test("a rewound maintenance frame no longer kills the session; the clock ratchet survives reset", () => {
	const rng = createPresentationRandom( 11, 1, 100 ), camera = createCameraScripts( rng );
	camera.step( 1000, [] );
	// Every producer stamps the clamped presentation clock (1000) while the
	// host frame clock reads a few milliseconds less: animation-frame and
	// maintenance performance.now() readings interleave (#2561). The event is
	// the shake of the local player's own landing hit on that frame.
	let shook = null;
	assert.doesNotThrow( () => (shook = camera.step( 995, [ command( 1000 ) ] )) );
	assert.deepEqual( shook, [ 0, 0 ] );
	// The ratchet, not the raw reading, drives the decay: 996 stays clamped
	// at 1000 and does not sample a period early.
	assert.deepEqual( camera.step( 996, [] ), [ 0, 0 ] );
	assert.equal( rng.takeTrace().length, 0 );
	// reset() drops the shake but never rewinds the clock.
	camera.reset();
	assert.doesNotThrow( () => camera.step( 997, [ command( 1000 ) ] ) );
	// A stamp genuinely beyond every reading the camera has ever seen stays
	// a producer defect and stays fatal.
	assert.throws( () => camera.step( 1005, [ command( 1500 ) ] ), /Invalid camera script event/ );
});
test("shake moves native world X/Y target and collision segment without accumulating or rotating with yaw", () => {
	const base = {
		originRegion: 257,
		eye: [ 0, 0, 0 ],
		target: [ 10, 20, 30 ],
		fov: 1,
		near: 1,
		far: 100,
		follow: { yaw: Math.PI / 2, pitch: 0, distance: 80, height: 20 }
	};
	const a = resolveFollowCamera( base, [], null ).camera;
	const b = resolveFollowCamera( { ...base, follow: { ...base.follow, offset: [ 2, -3 ] } }, [], null ).camera;
	assert.equal( b.target[0] - a.target[0], 2 );
	assert.ok( Math.abs( b.target[1] - a.target[1] + 3 ) < 1e-5 );
	assert.equal( b.target[2], a.target[2] );
	assert.equal( b.eye[0] - a.eye[0], 2 );
	assert.equal( b.eye[2], a.eye[2] );
	assert.deepEqual( resolveFollowCamera( base, [], null ).camera, a );
});
function fixture( stages, kind = "local-player", overrides = {} ) {
	let serial = 0;
	const jobs = new Map(), catalog = { 1: { clips: [], stages } };
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
							).buffer
						}
				);
				return serial;
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
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 1, kind, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
		gid: 2,
		kind: "monster",
		regionId: 257,
		x: 100,
		y: 0,
		z: 0,
		heading: 0
	} ];
	const cast = { token: 1, caster: 1, target: 2, skill: 1, ...overrides },
		trigger = { cast, phase: "SHOT", event: 1, at: .2 };
	const step = ( now, events = [], ready = false, casts = [ cast ] ) =>
		owner.step( entities, { casts }, now, () => ready, () => 1, events );
	step( 0 );
	step( .1 );
	step( .2, [ trigger ] );
	return { owner, entities, cast, trigger, step };
}
const stage = {
	resource: null,
	phase: "SHOT",
	startEvent: 1,
	damageEvent: false,
	action: "AT_ONE_FOLLOW",
	move: "MOV_NONE",
	bone: null,
	offset: [ 0, 0, 0 ],
	life: 0,
	count: 1,
	scripts: [ "SCT_SHAKECAM1" ]
};
test("script-only stages fire once, cold models do not gate them, and only remote CICUser is exempt", () => {
	for ( const kind of [ "local-player", "monster", "cos", "player" ] ) {
		const f = fixture( [ stage ], kind ), events = f.owner.takeCameraScripts();
		assert.equal( events.length, kind === "player" ? 0 : 1 );
		f.step( .3, [ f.trigger ] );
		assert.deepEqual( f.owner.takeCameraScripts(), [] );
		f.owner.dispose();
	}
});
test("AT_TARGET moving camera scripts launch above target and fire at arrival, even with cold assets or a finalized cast", () => {
	const moving = {
		...stage,
		resource: "ice.efp",
		action: "AT_TARGET",
		move: "MOV_STRAIGHT",
		movement: { delayMs: 100, startSpeed: 100, endSpeed: 100 },
		offset: [ 0, 100, 0 ],
		targetOffset: [ 0, 0, 0 ],
		scripts: [ "SCT_SHAKECAM_MOV0" ]
	};
	const f = fixture( [ moving ] );
	assert.equal( f.owner.error(), null );
	assert.deepEqual( f.owner.takeCameraScripts(), [] );
	const rows = f.step( .8, [], true, [] );
	assert.equal( rows.length, 1 );
	assert.equal( rows[0].pose.x, 100 );
	assert.ok( Math.abs( rows[0].pose.y - 50 ) < 1e-6 );
	f.step( 1.31, [], false, [] );
	const events = f.owner.takeCameraScripts();
	assert.equal( events.length, 1 );
	assert.ok(
		Math.abs( events[0].atMs - 1310 ) < 1e-6,
		"native timer starts when arrival is dispatched, not at an interpolated past time"
	);
	f.step( 2, [], false, [] );
	assert.deepEqual( f.owner.takeCameraScripts(), [] );
	f.owner.dispose();
	const peer = fixture( [ moving ], "player" );
	peer.step( 2 );
	assert.deepEqual( peer.owner.takeCameraScripts(), [] );
	peer.owner.dispose();
});
test("arrival-only bits on stationary stages are not incorrectly dispatched as immediate shakes; reset clears queued scripts", () => {
	const f = fixture( [ { ...stage, scripts: [ "SCT_SHAKECAM_MOV1" ] } ] );
	assert.deepEqual( f.owner.takeCameraScripts(), [] );
	f.owner.dispose();
	const other = fixture( [ stage ] );
	other.owner.reset();
	assert.deepEqual( other.owner.takeCameraScripts(), [] );
	other.owner.dispose();
});
test("target-local batch has one arrival/camera owner and retains its whole result vector", () => {
	const impacts = [ { damage: 10 } ],
		results = [ { target: 2, impacts }, { target: 3, impacts } ],
		moving = {
			...stage,
			damageEvent: true,
			resource: "ice.efp",
			action: "AT_TARGET",
			move: "MOV_STRAIGHT",
			movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 },
			offset: [ 0, 100, 0 ],
			scripts: [ "SCT_SHAKECAM_MOV0" ]
		};
	const f = fixture( [ moving ], "local-player", { results } ), feedback = createDamageFeedback();
	f.entities.push( { ...f.entities[1], gid: 3, x: 150 } );
	const launch = f.owner.takeImpacts();
	assert.equal( launch.length, 1 );
	assert.equal( launch[0].allTargets, true );
	assert.deepEqual(
		feedback.take( [ f.cast ], [ f.trigger ], () => 0, .2, 200, launch ),
		[],
		"callback cannot flush transferred hits early"
	);
	assert.equal(
		f.step( .7, [], true, [] ).length,
		1,
		"one effect at the resolved target, not a duplicate per result row"
	);
	f.step( 1.3, [], false, [] );
	const hits = feedback.take( [], [], () => 0, 1.3, 1300, f.owner.takeImpacts() );
	assert.deepEqual( hits.map( h => h.target ), [ 2, 3 ] );
	assert.equal( f.owner.takeCameraScripts().length, 1 );
	f.owner.dispose();
});
test("world owner pumps camera RNG before action RNG, publishes offsets and clears them on reset", () => {
	const random = createPresentationRandom( 1, 1, 20 ), cameras = [];
	const world = createWorldStream(
		{ available: () => 0 },
		{ setWorldCamera: v => cameras.push( v ), cancelWorldUpdate() {}, setWorld() {} },
		"http://localhost",
		random
	);
	const pose = { regionId: 257, x: 10, y: 20, z: 30, angle: 0 };
	world.pumpCameraScripts( 0 );
	world.step( pose, undefined, undefined, undefined, 0, [ command() ] );
	world.pumpCameraScripts( 20 );
	random.range( 0, 10 );
	world.step( pose, undefined, undefined, undefined, 20, [] );
	const trace = random.takeTrace();
	assert.equal( trace.length, 3, "publishing the camera after action update does not consume another timer sample" );
	assert.notDeepEqual( cameras.at( -1 ).follow.offset, [ 0, 0 ] );
	world.reset();
	world.step( pose, undefined, undefined, undefined, 40, [] );
	assert.deepEqual( cameras.at( -1 ).follow.offset, [ 0, 0 ] );
	assert.equal( random.takeTrace().length, 0 );
	world.dispose();
});

test("proved 8D5440 local-caster HWAN and critical branches dispatch existing camera timers", () => {
	for ( const local of [ false, true ] ) {
		for ( const hwan of [ false, true ] ) {
			for ( const flags of [ 0, 1, 2, 3, 0x10 ] ) {
				const expected = local && ((flags & 2) || hwan);
				const event = localHitFlash( local, hwan, flags, 1000 );
				assert.equal( event !== null, !!expected );
				if ( event ) {
					const strong = hwan && !!(flags & 2);
					assert.deepEqual( event, {
						atMs: 1000,
						amplitude: strong ? 200 : 50,
						durationMs: 500,
						periodMs: strong ? 25 : 20
					} );
				}
			}
		}
	}
	const owner = createCharacterEffects(
		{ available: () => 0 },
		"http://fixture.invalid",
		() => {},
		createPresentationRandom( 1 )
	);
	owner.hitFlash( true, true, 2, 1000 );
	assert.equal( owner.takeCameraScripts()[0].amplitude, 200 );
	assert.deepEqual( owner.takeCameraScripts(), [] );
	owner.dispose();
});
