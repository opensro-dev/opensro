/*
===========================================================================

moving-stage.test.mjs - tests for moving-stage.ts, effects.ts, random.ts,
damage-feedback.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { movingTargets, radialDestination, stepMoving } = await import(
	"../../src/engine/foundation/animation/moving-stage.ts"
);
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const pose = ( [x, y, z] ) => ({ regionId: 257, x, y, z, yaw: 0 });
test("per-frame movement matches 36 executions of original x86 including endpoint equality and zero distance", () => {
	const reference = JSON.parse( readFileSync( "tests/fixtures/native/moving-step-reference.json", "utf8" ) );
	assert.equal( reference.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	for ( const row of reference.cases ) {
		const actual = stepMoving( pose( row.start ), pose( row.end ), row.step );
		assert.equal( actual.arrived, row.arrived, JSON.stringify( row ) );
		assert.deepEqual( [ actual.pose.x, actual.pose.y, actual.pose.z ], row.position, JSON.stringify( row ) );
	}
});
test("48 radial endpoints and orientation angles match the original x86 and CRT trigonometry", () => {
	const reference = JSON.parse( readFileSync( "tests/fixtures/native/moving-step-reference.json", "utf8" ) );
	assert.equal( reference.radial.length, 48 );
	for ( const row of reference.radial ) {
		const actual = radialDestination( { ...pose( row.start ), yaw: row.yaw }, row.degrees, row.distance );
		assert.deepEqual( [ actual.x, actual.y, actual.z ], row.position, JSON.stringify( row ) );
		assert.equal( actual.yaw, row.angle );
	}
});
test("count distribution is count total, cyclic row order, with no damage on visual duplicates", () => {
	assert.deepEqual( movingTargets( 1, 9, [ 2, 3 ] ), [ { target: 9, owns: true, all: true } ] );
	assert.deepEqual( movingTargets( 4, 9, [ 3, 2 ] ), [
		{ target: 3, owns: true, all: false },
		{ target: 2, owns: true, all: false },
		{ target: 3, owns: false, all: false },
		{ target: 2, owns: false, all: false }
	] );
	assert.deepEqual( movingTargets( 3, 9, [] ), [] );
	assert.throws( () => movingTargets( 129, 9, [ 2 ] ), /count/ );
});
const stage = {
	resource: "bolt.efp",
	phase: "SHOT",
	startEvent: 1,
	damageEvent: true,
	action: "AT_MOV_SPLASH",
	move: "MOV_STRAIGHT",
	movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 },
	bone: null,
	targetBone: null,
	offset: [ 0, 0, 0 ],
	targetOffset: [ 0, 0, 0 ],
	life: 0,
	count: 1,
	scripts: []
};
function fixture( stages = [ stage ], options = {} ) {
	let serial = 0;
	const jobs = new Map(), sounds = [], random = createPresentationRandom( 7, 1, 100 );
	const catalog = { 1: { clips: [], stages } },
		owner = createCharacterEffects(
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
			v => sounds.push( v ),
			random
		);
	const entities = [ 1, 2, 3 ].map( ( gid, i ) => ({
		gid,
		kind: gid === 1 ? "local-player" : "monster",
		regionId: 257,
		x: i * 10,
		y: 0,
		z: 0,
		heading: 0
	}) );
	const cast = {
			token: 1,
			caster: 1,
			target: 2,
			skill: 1,
			results: [ 2, 3 ].map( target => ({ target, impacts: [ { damage: 10 } ] }) )
		},
		trigger = { cast, phase: "SHOT", event: 1, at: .25 };
	const step = ( now, events = [], ready = true, casts = [ cast ] ) =>
		owner.step( entities, { casts }, now, () => ready, () => 10, events, options.socket );
	step( 0 );
	step( .125 );
	const initial = step( .25, [ trigger ], options.ready ?? true );
	return { owner, entities, cast, trigger, step, initial, random, sounds };
}
test("splash arrives once before selecting its first result, then samples each next target live", () => {
	const f = fixture(),
		feedback = createDamageFeedback(),
		transfer = () => feedback.take( [ f.cast ], [], () => 0, 0, 0, f.owner.takeImpacts() );
	assert.equal( f.initial.length, 1 );
	assert.deepEqual( transfer(), [] );
	f.step( .375 );
	assert.deepEqual( transfer(), [], "initial arrival has no pending entry" );
	f.entities[2].x = 40;
	f.step( .5 );
	assert.deepEqual( transfer().map( h => h.target ), [ 2 ] );
	assert.equal( f.step( .625 )[0].pose.x, 22.5 );
	f.entities[2].x = 150;
	f.step( .75 );
	assert.deepEqual( transfer(), [] );
	f.step( .875 );
	assert.deepEqual( transfer().map( h => h.target ), [ 3 ], "destination remains captured during the hop" );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("splash owns transferred results after cancellation and caster disappearance, even while assets are cold", () => {
	const f = fixture( [ stage ], { ready: false } ), feedback = createDamageFeedback();
	assert.deepEqual( feedback.take( [ f.cast ], [ f.trigger ], () => 0, .25, 250, f.owner.takeImpacts() ), [] );
	f.cast.cancelledAtMs = 300;
	assert.deepEqual( feedback.take( [ f.cast ], [], () => 0, .3, 300 ), [] );
	f.entities.shift();
	f.step( 10, [], false, [] );
	assert.deepEqual( f.owner.takeImpacts(), [], "no multi-hop catch-up" );
	f.step( 11, [], false, [] );
	assert.deepEqual( feedback.take( [], [], () => 0, 11, 11000, f.owner.takeImpacts() ).map( h => h.target ), [ 2 ] );
	f.entities.pop();
	f.step( 12, [], false, [] );
	assert.deepEqual(
		feedback.take( [], [], () => 0, 12, 12000, f.owner.takeImpacts() ),
		[],
		"removed target is skipped"
	);
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("live target sockets are re-evaluated at retarget and chain has no default arrival camera or end sound", () => {
	const calls = [];
	const f = fixture( [ {
		...stage,
		targetBone: "joint",
		soundEnd: "end.wav",
		scripts: [ "SCT_SHAKECAM_MOV0" ],
		arrivalResource: "arrival.efp"
	} ], {
		socket: ( gid, bone, offset, event ) => {
			calls.push( { gid, event } );
			return pose( [ gid === 2 ? 10 : 50, 0, 0 ] );
		}
	} );
	f.step( .375 );
	f.step( .5 );
	assert.deepEqual( calls.map( c => c.gid ), [ 2, 2, 3 ] );
	assert.ok( calls.every( c => c.event.sampleCurrent ) );
	assert.equal( calls.at( -1 ).event.at, .5 );
	assert.deepEqual( f.owner.takeCameraScripts(), [] );
	assert.deepEqual( f.sounds, [] );
	assert.equal( f.step( .625 ).filter( actor => actor.model.includes( "arrival.efp" ) ).length, 2 );
	f.owner.reset();
	assert.deepEqual( f.step( .75 ), [] );
	f.owner.dispose();
});
test("multiple moving instances choose cyclic targets but only one instance owns each target result", () => {
	const f = fixture( [ { ...stage, action: "AT_MOV_1TAR", count: 4 } ] );
	assert.equal( f.initial.length, 4 );
	const launch = f.owner.takeImpacts();
	assert.deepEqual( launch.map( e => e.target ), [ 2, 3 ] );
	assert.ok( launch.every( e => !e.allTargets ) );
	f.step( .375 );
	const arrived = f.owner.takeImpacts();
	assert.deepEqual( arrived.map( e => e.target ), [ 2 ] );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("losing the selected destination does not discard the other rows owned by a single default mover", () => {
	const f = fixture( [ { ...stage, action: "AT_MOV_1TAR" } ] ), feedback = createDamageFeedback();
	feedback.take( [ f.cast ], [ f.trigger ], () => 0, .25, 250, f.owner.takeImpacts() );
	f.entities.splice( 1, 1 );
	f.step( .5 );
	const hits = feedback.take( [], [], () => 0, .5, 500, f.owner.takeImpacts() );
	assert.ok( hits.some( hit => hit.target === 3 ) );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("MOV_ROUND follows the native default mover, while splash consumes UPR spin without applying its default arc", () => {
	const round = fixture( [ { ...stage, action: "AT_MOV_1TAR", move: "MOV_ROUND" } ] );
	const actor = round.step( .3 )[0];
	assert.ok( Math.abs( actor.pose.x - 5 ) < 1e-6 );
	assert.equal( actor.pose.z, 0 );
	assert.equal( round.owner.error(), null );
	round.owner.dispose();
	const chain = fixture( [ { ...stage, move: "MOV_UPR", parameters: [ 1000, 90, 90 ] } ] );
	assert.equal( chain.random.takeTrace().length, 2, "speed then spin" );
	assert.equal( chain.step( .3 )[0].pose.y, 0 );
	assert.equal( chain.step( .325 )[0].pose.z, 0 );
	assert.equal( chain.random.takeTrace().length, 0 );
	chain.owner.dispose();
});
const radial = {
	...stage,
	action: "AT_MOV_OPTION",
	damageEvent: false,
	native: {
		fadeInMs: 300,
		fadeOutMs: 300,
		actionOptions: { enabled: true, direction: 90, distance: 50, residualDistance: 10 }
	},
	life: 3
};
test("radial movers emit once on strict spacing, use full delta on delay expiry and fade residual family", () => {
	const f = fixture( [ { ...radial, movement: { ...stage.movement, delayMs: 200 } } ] );
	assert.equal( f.initial.length, 0 );
	assert.equal( f.step( .375 ).length, 0 );
	const a = f.step( .5 );
	assert.equal( a.length, 2, "full .125 second step emits despite only .05 seconds past delay" );
	const fixed = a.find( actor => actor.gid !== -1 ), b = f.step( .625 );
	assert.deepEqual( defined( b.find( actor => actor.gid === defined( fixed ).gid ) ).pose, defined( fixed ).pose );
	f.step( 1 );
	const fading = f.step( 1.125 );
	assert.ok( fading.length > 0 );
	assert.ok( fading.every( actor => actor.opacity === 148 / 255 ) );
	assert.deepEqual( f.step( 1.375 ), [], "authored lifeMs is not an independent 3 second timer" );
	assert.deepEqual( f.owner.takeImpacts(), [] );
	f.owner.dispose();
	const equal = fixture( [ radial ] );
	assert.equal( equal.step( .35 ).length, 1, "exactly ten units does not emit" );
	assert.equal( equal.step( .375 ).length, 2 );
	equal.owner.dispose();
});
test("radial angles use original source and round once at the native float stores", () => {
	const start = { ...pose( [ 10, 20, 30 ] ), yaw: 0 };
	assert.deepEqual( radialDestination( start, 0, 50 ), pose( [ 10, 20, -20 ] ) );
	assert.equal( radialDestination( start, 90, 50 ).x, 60 );
	const f = fixture( [ { ...radial, offset: [ 0, 30, 0 ] } ] );
	assert.equal( f.initial[0].pose.y, 30 );
	assert.ok( f.step( .5 )[0].pose.y < 30, "endpoint is not offset with source bone" );
	f.owner.dispose();
});
test("zero fade sentinel stops emission; negative sentinel removes the entire residual family", () => {
	for ( const fadeOutMs of [ 0, -1 ] ) {
		const f = fixture( [ { ...radial, native: { ...radial.native, fadeOutMs } } ] );
		f.step( .375 );
		f.step( .5 );
		const arrived = f.step( 1 );
		if ( fadeOutMs < 0 ) assert.deepEqual( arrived, [] );
		else {
			assert.ok( arrived.length > 0 );
			assert.ok( arrived.every( actor => actor.emissionEnd !== undefined ) );
		}
		f.owner.reset();
		assert.deepEqual( f.step( 2 ), [] );
		f.owner.dispose();
	}
});
test("published 88 plain splash and 96 radial stages admit; arrow scripts admit", () => {
	const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json", "utf8" ) ),
		catalog = createEffectDecoder().decode( new TextEncoder().encode( JSON.stringify( raw ) ) );
	let splash = 0, option = 0;
	for ( const record of Object.values( catalog ) ) {
		for ( const row of record.stages ) {
			if ( ![ "AT_MOV_SPLASH", "AT_MOV_OPTION" ].includes( row.action ) || row.scripts.length ) continue;
			const f = fixture( [ { ...row, phase: "SHOT", startEvent: 1 } ], {
				socket: ( gid ) => pose( [ gid === 1 ? 0 : 10, 0, 0 ] )
			} );
			assert.equal( f.owner.error(), null, JSON.stringify( row ) );
			f.owner.dispose();
			if ( row.action === "AT_MOV_SPLASH" ) splash++;
			else option++;
		}
	}
	assert.equal( splash, 88 );
	assert.equal( option, 96 );
	const blocked = fixture( [ { ...stage, scripts: [ "SCT_ARROW" ] } ] );
	assert.equal( blocked.owner.error(), null );
	blocked.owner.dispose();
});
