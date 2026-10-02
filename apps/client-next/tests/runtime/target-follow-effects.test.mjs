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
									bone: null,
									offset: [ 0, 0, 0 ],
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
	const caster = { gid: 1, refObjId: 1, kind: "player", name: "caster", regionId: 257, x: 0, y: 0, z: 0, heading: 0 };
	const target = {
		gid: 2,
		refObjId: 1,
		kind: "monster",
		name: "target",
		regionId: 257,
		x: 40,
		y: 5,
		z: 30,
		heading: 0
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
		effect.step( [ caster, target ], gameplay, time, () => true, () => 0.5, [ {
			cast,
			phase: "SHOT",
			event: 1,
			at: time
		} ] );
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
	assert.equal( actors[0].pose.x, 81 );
	assert.deepEqual( actors[0].attachment, {
		gid: 2,
		bone: "Bip01 Head",
		root: false,
		basis: "native",
		offset: [ 1, 2, -3 ]
	} );
});

test("target decoration retires with the target", () => {
	assert.equal( movingTargetEffect( "AT_TARGET_F", { removeTarget: true } ).length, 0 );
});

test("target follow accepts only EFP and does not create a mover", () => {
	assert.equal( movingTargetEffect( "AT_TARGET_F", { resource: "hit.bsr" } ).length, 0 );
	assert.equal( movingTargetEffect( "AT_TARGET_F", { move: "MOV_STRAIGHT" } )[0].pose.x, 80 );
});
