/*
===========================================================================

target-follow-effects.test.mjs - moving-target native effect review probe

Native 8DE7A4 attaches AT_TARGET_F to the target's decoration owner.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );

/*
================
movingTargetEffect

Casts one stage of the given action from a caster at the origin onto a
target at (40, 5, 30), then moves the target. options.firstStep returns the
actors of the first frame that draws any (once the stage's assets load)
instead of the moved ones.
================
*/
function movingTargetEffect( action, options = {} ) {
	let requestId = 0;
	const jobs = new Map();
	const effect = createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			install() {},
			dispose() {
				jobs.clear();
			},
			request( url, limit, decode ) {
				const result = decode === "effects" ?
					{
						kind: "effects",
						catalog: {
							"1": {
								clips: [ "attack1" ],
								stages: [ {
									resource: options.resource ?? "hit.efp",
									damageEvent: false,
									startEvent: 1,
									action,
									move: options.move ?? "MOV_NONE",
									...(options.movement ? { movement: options.movement } : {}),
									bone: null,
									offset: options.startOffset ?? [ 0, 0, 0 ],
									targetBone: options.bone ?? null,
									targetOffset: options.offset ?? [ 0, 0, 0 ],
									life: 0,
									sound: null,
									count: 1,
									scripts: []
								} ]
							}
						}
					} :
					{
						kind: "bytes",
						buffer: new TextEncoder().encode( JSON.stringify( {
							format: "sro-skill-stage-models",
							models: {}
						} ) ).buffer
					};
				jobs.set( ++requestId, result );
				return requestId;
			},
			take( id ) {
				const result = jobs.get( id );
				jobs.delete( id );
				return result;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const caster = {
		gid: 1,
		refObjId: 1,
		kind: "player",
		name: "caster",
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: options.casterHeading ?? 0
	};
	const target = {
		gid: 2,
		refObjId: 1,
		kind: "monster",
		name: "target",
		regionId: 257,
		x: 40,
		y: 5,
		z: 30,
		heading: options.targetHeading ?? 0
	};
	const cast = { token: 1, caster: 1, target: 2, skill: 1, damage: 0, fatal: false };
	const gameplay = {
		casts: [ cast ],
		localGid: 1,
		revision: 0,
		pose: null,
		authoritativePose: null,
		pendingMoves: 0,
		acknowledgedMove: 0,
		target: 2,
		targetPending: 0,
		inventory: [],
		inventoryPending: false,
		vitals: [],
		error: null
	};
	for ( const time of [ 0, 0.1, 0.2, 0.3 ] ) {
		const born = effect.step( [ caster, target ], gameplay, time, () => true, () => 0.5, [ {
			cast,
			phase: "SHOT",
			event: 1,
			at: time
		} ] );
		if ( options.firstStep && born.length ) {
			assert.equal( effect.error(), null );
			effect.dispose();
			return born;
		}
	}
	target.x = 80;
	if ( options.close ) gameplay.casts = [];
	const entities = options.removeTarget ? [ caster ] : [ caster, target ];
	const actors = effect.step( entities, gameplay, 0.4, () => true, () => 0.5, [] );
	assert.equal( effect.error(), null );
	const result = actors;

	effect.dispose();
	return result;
}

test("native AT_TARGET remains at its sampled position", () => {
	assert.equal( movingTargetEffect( "AT_TARGET" )[0].pose.x, 40 );
});

test("native AT_TARGET_F follows the target", () => {
	assert.equal( movingTargetEffect( "AT_TARGET_F" )[0].pose.x, 80 );
});

test("target decoration keeps its bone and offset after caster closure", () => {
	const actors = movingTargetEffect( "AT_TARGET_F", { close: true, bone: "Bip01 Head", offset: [ 1, 2, 3 ] } );
	assert.equal( actors.length, 1 );
	// The native offset turns with the target's root (8D6880): at heading 0
	// (1, 2, 3) is (-3, 2, 1) in the world, and the renderer gets it raw.
	assert.equal( actors[0].pose.x, 77 );
	assert.deepEqual( actors[0].attachment, {
		gid: 2,
		bone: "Bip01 Head",
		root: false,
		basis: "native",
		offset: [ 1, 2, 3 ]
	} );
});

test("target decoration retires with the target", () => {
	assert.equal( movingTargetEffect( "AT_TARGET_F", { removeTarget: true } ).length, 0 );
});

test("target follow accepts only EFP and does not create a mover", () => {
	assert.equal( movingTargetEffect( "AT_TARGET_F", { resource: "hit.bsr" } ).length, 0 );
	assert.equal( movingTargetEffect( "AT_TARGET_F", { move: "MOV_STRAIGHT" } )[0].pose.x, 80 );
});

// Snow storm - Ice rain's shards: MOV_STRAIGHT at start speed 200 from 100
// units above a point beside the target down to that point.
const ICE_RAIN = {
	move: "MOV_STRAIGHT",
	movement: { delayMs: 0, startSpeed: 200, endSpeed: 300 },
	startOffset: [ 26, 100, -14 ],
	offset: [ 26, 0, -14 ]
};

test("native AT_TARGET mover starts at the world-axis offset and faces -pi/2 whatever the target faces", () => {
	// A quarter-turned target must not turn the offset or the shard (8DE6C7..8DE774).
	const [shard] = movingTargetEffect( "AT_TARGET", { ...ICE_RAIN, targetHeading: 16384, firstStep: true } );
	assert.ok( shard, "the mover is born" );
	// Falling from (66, 105, 16) toward (66, 5, 16).
	assert.deepEqual( [ shard.pose.x, shard.pose.z ], [ 66, 16 ] );
	assert.ok( shard.pose.y > 5 && shard.pose.y < 105, `in flight at y ${shard.pose.y}` );
	assert.equal( shard.pose.yaw, Math.fround( -Math.PI / 2 ) );
});

test("native AT_TARGET mover keeps its captured path when the target walks away", () => {
	// Every shard still falls straight down beside where the target stood.
	const actors = movingTargetEffect( "AT_TARGET", { ...ICE_RAIN, targetHeading: 16384 } );
	assert.ok( actors.length > 0 );
	for ( const shard of actors ) assert.deepEqual( [ shard.pose.x, shard.pose.z ], [ 66, 16 ] );
});

test("native AT_TARGET at start speed 0 is a static effect facing the caster's yaw", () => {
	for ( const move of [ "MOV_NONE", "MOV_STRAIGHT" ] ) {
		const [effect] = movingTargetEffect( "AT_TARGET", {
			move,
			movement: { delayMs: 0, startSpeed: 0, endSpeed: 0 },
			casterHeading: 16384,
			targetHeading: 0,
			firstStep: true
		} );
		assert.ok( effect, `${move} at speed 0 draws on the target` );
		assert.deepEqual( [ effect.pose.x, effect.pose.y, effect.pose.z ], [ 40, 5, 30 ], move );
		// 8DE67A: [caster+0x88], the caster's model yaw, not the target's.
		assert.equal( effect.pose.yaw, (16384 / 65535 * 2 * Math.PI + Math.PI / 2) % (2 * Math.PI), move );
	}
});
