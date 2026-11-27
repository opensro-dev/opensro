/*
===========================================================================

knockback-at-impact.test.mjs - a struck character recoils when its hit lands

CIDecoDamageEffect_Initialize (8D5440), run by 8DB770 at the skill's damage
event, starts a knockback's 250/s travel (8D56F6 -> 8DDBD0) and, for type
5, its SYSTEM_KNOCKBACK decoration. The result packet arrives earlier: the
drawn body waits where it stood until the hit is presented.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createKnockbackPresentation } = await import(
	"../../src/engine/runtime/characters/knockback-presentation.ts"
);
const { createDamageFeedback } = await import( "../../src/engine/runtime/characters/damage-feedback.ts" );
const { createPresentationEvents } = await import( "../../src/engine/runtime/characters/presentation-events.ts" );
const { SYSTEM_KNOCKBACK } = await import( "../../src/engine/contracts/orb.ts" );

const REGION = 24744;
const FROM = { regionId: REGION, x: 100, y: 10, z: 500, angle: 0 };
const TO = { regionId: REGION, x: 100, y: 10, z: 450, angle: 0 };
// 50 units at 250 units/s.
const PATH = { from: FROM, to: TO, durationMs: 200, displacement: true, startedAtMs: 0 };
const DRAWN = { ...TO, angle: 1.5 };

test("the drawn body holds until the knockback hit lands, then recoils from that second", () => {
	const knockbacks = createKnockbackPresentation();
	assert.deepEqual(
		knockbacks.pose( 7, PATH, true, 10, DRAWN ),
		{ ...FROM, angle: DRAWN.angle },
		"held where it stood, still facing as drawn"
	);
	knockbacks.land( 7, 10.5 );
	const halfway = knockbacks.pose( 7, PATH, false, 10.6, DRAWN );
	assert.equal( halfway?.z, 475, "half the leg 0.1 s after the hit" );
	assert.equal( halfway?.angle, DRAWN.angle );
	assert.equal( knockbacks.pose( 7, PATH, false, 10.7, DRAWN ), undefined, "the presenter takes over at the end" );
	assert.equal( knockbacks.pose( 7, { ...PATH, displacement: false }, true, 11, DRAWN ), undefined );
});

test("a knockback or knockdown impact is pending until consumed or retired", () => {
	const feedback = createDamageFeedback();
	const impact = type => ({
		type,
		flags: 0,
		damage: 1,
		fatal: false,
		displacement: { regionId: REGION, x: 1, y: 2, z: 3 }
	});
	const cast = ( token, target, hit ) => ({
		token,
		caster: 1,
		target,
		skill: 1,
		results: [ { target, impacts: [ hit ] } ]
	});
	const casts =
		/** @type {any[]} */ ([ cast( 1, 7, impact( 5 ) ), cast( 2, 8, impact( 4 ) ), cast( 3, 9, impact( 0 ) ) ]);
	assert.deepEqual( [ ...feedback.pendingKnockbacks( casts ) ].sort(), [ 7, 8 ] );
	assert.deepEqual(
		[ ...feedback.pendingKnockbacks( casts, gid => gid !== 7 ) ],
		[ 8 ],
		"a retired result holds nothing"
	);
	assert.deepEqual(
		[ ...feedback.pendingKnockbacks( [ { ...casts[0], discardPendingResults: true } ] ) ],
		[],
		"a discarded result holds nothing"
	);
});

/*
================
presentHit

One presentation-events step presenting a single hit of the given impact
type from gid 1 onto gid 2 at second 10. Returns the landings and the
system decorations it started.
================
*/
function presentHit( type ) {
	const row = gid => ({ gid, refObjId: gid, kind: "player", appearanceState: [ 1, 0, 0 ], ...FROM, heading: 0 });
	const entities = [ row( 1 ), row( 2 ) ];
	const landed = [], decorated = [];
	createPresentationEvents().step(
		/** @type {any} */ ({
			entities,
			selectedGids: new Set( [ 1, 2 ] ),
			gameplay: {
				localGid: 1,
				pose: FROM,
				casts: [],
				vitals: [],
				questMarkers: [],
				inventory: [],
				attachedEffects: []
			},
			seconds: 10,
			simulationMs: undefined,
			entitiesByGid: new Map( entities.map( e => [ e.gid, e ] ) ),
			actionClocks: new Map(),
			combatStanceEnds: new Map(),
			triggers: [],
			samples: new Map( [ [ 1, {} ], [ 2, {} ] ] ),
			heldPoses: new Map(),
			localMover: gid => gid === 1,
			logicalPose: e => ({ regionId: e.regionId, x: e.x, y: e.y, z: e.z, angle: e.heading }),
			appearanceRef: e => e.refObjId,
			soundContext: () => ({}),
			drawnPoses: new Map(),
			referenceAppearances: { step() {} },
			effects: {
				step: () => [],
				attachedInstances: () => [],
				takeActivations: () => [],
				takeHawkImpacts: () => [],
				takeImpacts: () => [],
				impactIndex: () => 0,
				hitFlash() {},
				system: ( gid, effect ) => decorated.push( [ gid, effect ] ),
				damage: () => []
			},
			effectDetail: 0,
			bloodEnabled: false,
			damageNumbers: false,
			resources: { ready: () => false, duration: () => 0 },
			renderer: { characterSocket: () => null, characterMatrix: () => null, characterLocalMatrix: () => null },
			feedback: {
				take: () => [ {
					cast: { token: 1, caster: 1, target: 2, skill: 100 },
					target: 2,
					impact: {
						type,
						flags: 0,
						damage: 10,
						fatal: false,
						displacement: { regionId: REGION, x: 100, y: 10, z: 450 }
					},
					key: "1:2:0",
					at: 10
				} ],
				pendingDeaths: () => new Set()
			},
			health: undefined,
			sounds: { emit: () => true, impact() {}, flush() {} },
			knockbacks: { land: ( gid, at ) => landed.push( [ gid, at ] ) }
		}),
		{ displayed: new Map() },
		{
			catalog: new Map(),
			tradeBandits: new Map(),
			soundProfiles: new Map(),
			effectAnchors: new Map(),
			riderModes: new Map(),
			bloodEffects: new Map()
		}
	);
	return { landed, decorated };
}

test("the hit starts the recoil; a type-5 knockback also starts SYSTEM_KNOCKBACK (8D5440)", () => {
	assert.deepEqual( presentHit( 5 ), { landed: [ [ 2, 10 ] ], decorated: [ [ 2, SYSTEM_KNOCKBACK ] ] } );
	assert.deepEqual( presentHit( 4 ), { landed: [ [ 2, 10 ] ], decorated: [] }, "a knockdown is undecorated" );
	assert.deepEqual( presentHit( 0 ), { landed: [], decorated: [] } );
});

test("the frame's drawn pose holds a struck monster until its hit, and leaves the local player alone", async () => {
	const { resolveDrawnPoses } = await import( "../../src/engine/runtime/characters/presentation-poses.ts" );
	const knockbacks = createKnockbackPresentation();
	const monster = { gid: 7, refObjId: 7, kind: "monster", ...TO, heading: 0, movementPath: PATH };
	const local = { ...monster, gid: 1, kind: "local-player" };
	/*
	================
	drawn
	================
	*/
	const drawn = ( pending, seconds ) =>
		resolveDrawnPoses(
			/** @type {any} */ ({
				selected: [ monster, local ],
				gameplay: null,
				seconds,
				logicalPose: e => ({ regionId: e.regionId, x: e.x, y: e.y, z: e.z, angle: e.heading }),
				localMover: gid => gid === 1,
				states: new Map(),
				vitalsByGid: new Map(),
				health: undefined,
				pendingDeaths: new Set(),
				pendingKnockbacks: new Set( pending ),
				knockbacks,
				statusOwner: { view: () => ({ poseLocked: false }) },
				posePresentation: { pose: () => DRAWN, carry() {} }
			})
		);
	const held = drawn( [ 1, 7 ], 10 );
	assert.equal( held.get( 7 )?.z, FROM.z, "the monster waits where it stood" );
	assert.equal( held.get( 1 ), DRAWN, "the local player keeps its own mover" );
	knockbacks.land( 7, 10 );
	assert.equal( drawn( [], 10.1 ).get( 7 )?.z, 475 );
	assert.equal( drawn( [], 10.3 ).get( 7 ), DRAWN );
});
