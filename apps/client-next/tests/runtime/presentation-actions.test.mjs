/*
===========================================================================

presentation-actions.test.mjs - cast completion releases base animation

A skill decoration can outlive its READY/WAIT/SHOT motion. Retaining that
decoration must not keep the actor's idle and movement animations suppressed.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPresentationActions } = await import(
	"../../src/engine/runtime/characters/presentation-actions.ts"
);

/*
================
fixture
================
*/
function fixture() {
	const actions = createPresentationActions();
	const definition = { durationMs: 1000, trackEvents: [], timeWarpCurve: { scale: 0, records: [] }, soundEvents: [] };
	const body = {
		refObjId: 1,
		codename: "CASTER",
		glb: "/caster.glb",
		clips: [ "ready", "wait", "shot" ],
		animationStates: { ready: definition, wait: definition, shot: definition }
	};
	const entity = {
		gid: 1,
		name: "Caster",
		refObjId: 1,
		kind: "local-player",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		movementMode: 2,
		appearanceState: [ 1, 0, 0 ]
	};
	const output = { failure: null };
	const published = {
		catalog: new Map(),
		nativeMotionUrls: new Map(),
		animationStates: new Map(),
		soundProfiles: new Map(),
		skillSounds: new Map()
	};
	return {
		/*
		================
		step
		================
		*/
		step( seconds, shotAtMs, extraCasts = [] ) {
			const frame = {
				entities: [ entity ],
				entitiesByGid: new Map( [ [ 1, entity ] ] ),
				local: undefined,
				gameplay: {
					revision: 0,
					localGid: 1,
					pose: null,
					authoritativePose: null,
					pendingMoves: 0,
					acknowledgedMove: 0,
					target: 0,
					targetPending: 0,
					inventory: [],
					inventoryPending: false,
					vitals: [],
					error: null,
					casts: [ {
						token: 1,
						caster: 1,
						skill: 1,
						target: 0,
						damage: 0,
						fatal: false,
						receivedAtMs: 0,
						shotAtMs
					}, ...extraCasts ]
				},
				seconds,
				simulationMs: seconds * 1000,
				castTokens: new Set( [ 1, ...extraCasts.map( cast => cast.token ) ] ),
				vitalsByGid: new Map(),
				groundClocks: new Map(),
				combatStanceEnds: new Map(),
				resourceFor: () => body,
				appearanceRef: () => 1,
				logicalPose: () => ({ regionId: 1, x: 0, y: 0, z: 0, angle: 0 }),
				wornEquipment: () => [],
				referenceAppearances: new Map(),
				random: { range: () => 0 },
				resources: { animation: () => true },
				effects: {
					loaded: () => true,
					phases: () => [ [ "ready" ], [ "wait" ], [ "shot" ] ],
					structureShake() {}
				},
				feedback: { pendingDeaths: () => new Set() },
				health: undefined,
				structureVisuals: { step: () => [] },
				sounds: { emit: () => true }
			};
			const result = actions.step(
				/** @type {Parameters<typeof actions.step>[0]} */ (frame),
				output,
				published
			);
			assert.equal( output.failure, null );
			return result;
		}
	};
}

test("a completed buff motion releases idle while its cast token remains published", () => {
	const f = fixture();
	assert.equal( f.step( 0, undefined ).waitingActors.has( 1 ), true );
	assert.equal( f.step( 1, 1000 ).waitingActors.has( 1 ), true );
	const complete = f.step( 3, 1000 );
	assert.equal( complete.actionLayersByActor.get( 1 )?.length, 0 );
	assert.equal( complete.waitingActors.has( 1 ), false, "completed SHOT must restore base animation" );
});

test("a frame stall across the whole released cast restores base animation immediately", () => {
	const f = fixture();
	f.step( 0, undefined );
	assert.equal( f.step( 10, 1000 ).waitingActors.has( 1 ), false );
});

test("an unreleased wall keeps WAIT and suppresses locomotion after a stall", () => {
	const f = fixture();
	f.step( 0, undefined );
	const held = f.step( 10, undefined );
	assert.equal( held.waitingActors.has( 1 ), true );
	assert.ok( held.actionLayersByActor.get( 1 )?.some( layer => layer.clip === "wait" && layer.weight > 0 ) );
});

test("a replaced WAIT cannot suppress idle after the replacement cast completes", () => {
	const f = fixture();
	f.step( 0, undefined );
	f.step( 1.5, undefined );
	const next = { token: 2, caster: 1, skill: 1, receivedAtMs: 2000, shotAtMs: 3000 };
	f.step( 2, undefined, [ next ] );
	f.step( 3, undefined, [ next ] );
	const complete = f.step( 5, undefined, [ next ] );
	assert.equal( complete.actionLayersByActor.get( 1 )?.length, 0 );
	assert.equal( complete.waitingActors.has( 1 ), false, "superseded WAIT must not own the base action" );
});
