/*
===========================================================================

drawn-anchor-presentation.test.mjs - drawn anchors sit on the drawn body (#1442)

While the local player walks, its presented (frame-clock) pose trails the
movement owner's logical pose. Everything drawn or heard at the body -
effect anchors, damage text, positional sound, the camera target - must
anchor at the presented pose, or it slides against the body. Loads the
shipped modules through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createPresentationEvents } = await import(
	sourceFileUrl( "src/engine/runtime/characters/presentation-events.ts" ).href
);
const { selectCameraTarget } = await import(
	sourceFileUrl( "src/engine/runtime/characters/presentation-finalize.ts" ).href
);
const { worldAudioPosition } = await import(
	sourceFileUrl( "src/engine/foundation/world/world-space.ts" ).href
);

const REGION = 24744, SECONDS = 10;
// The walk's logical pose (the movement owner's latest) and the pose the
// presenter has drawn of the same walk: a few units behind, mid-stride.
const LOGICAL = { regionId: REGION, x: 100, y: 10, z: 500, angle: 0 };
const DRAWN = { regionId: REGION, x: 97, y: 10, z: 496, angle: 0 };

/*
================
entity

One roster row. The local player's row still holds its spawn point, as
real rows do until the next entity update: nothing may anchor there.
================
*/
function entity( gid, x, z ) {
	return {
		gid,
		refObjId: gid,
		kind: gid === 1 ? "local-player" : "player",
		appearanceState: [ 1, 0, 0 ],
		regionId: REGION,
		x,
		y: 10,
		z,
		heading: 0,
		movementMode: 0
	};
}

/*
================
driveEvents

One presentation-events step over a walking local player (gid 1) and a
second player (gid 2), with one hit from the local player on gid 2. The
frame's drawn poses trail the logical ones. Returns what the effect
step received, the damage texts placed and the flush's position resolver.
================
*/
function driveEvents( heldPoses = new Map() ) {
	const local = entity( 1, 40, 440 ), other = entity( 2, 120, 520 );
	const entities = [ local, other ];
	const gameplay = {
		localGid: 1,
		pose: { ...LOGICAL },
		casts: [],
		vitals: [],
		questMarkers: [],
		inventory: [],
		attachedEffects: []
	};
	/** @type {{ effectsPose: any, effectsEntities: any, effectsDrawn: any, resolve: any }} */
	const seen = { effectsPose: null, effectsEntities: null, effectsDrawn: null, resolve: null };
	const effects = {
		step( rows, effectGameplay, ...rest ) {
			seen.effectsEntities = rows;
			seen.effectsPose = effectGameplay?.pose;
			// The step's last argument is the frame's drawn poses (#1545).
			seen.effectsDrawn = rest.at( -1 );
			return [];
		},
		attachedInstances: () => [],
		takeActivations: () => [],
		takeHawkImpacts: () => [],
		takeImpacts: () => [],
		impactIndex: () => 0,
		hitFlash() {},
		damage: () => []
	};
	const sounds = {
		emit: () => true,
		impact() {},
		flush( _now, resolve ) {
			seen.resolve = resolve;
		}
	};
	const events = createPresentationEvents();
	events.step(
		{
			entities,
			gameplay,
			seconds: SECONDS,
			simulationMs: undefined,
			entitiesByGid: new Map( entities.map( row => [ row.gid, row ] ) ),
			actionClocks: new Map(),
			combatStanceEnds: new Map(),
			triggers: [],
			samples: new Map( [ [ 1, {} ], [ 2, {} ] ] ),
			heldPoses,
			localMover: gid => gid === 1,
			logicalPose: row =>
				row.gid === 1 ?
					gameplay.pose :
					{ regionId: row.regionId, x: row.x, y: row.y, z: row.z, angle: row.heading },
			appearanceRef: row => row.refObjId,
			soundContext: () => ({}),
			// Mid-walk, the drawn pose trails the logical one: the frame's
			// drawn poses, as presentPoses resolved them (#1442c).
			drawnPoses: new Map( [
				[ 1, { ...gameplay.pose, x: gameplay.pose.x - 3, z: gameplay.pose.z - 4 } ],
				[ 2, { regionId: other.regionId, x: other.x - 3, y: other.y, z: other.z - 4, angle: other.heading } ]
			] ),
			referenceAppearances: { step() {} },
			effects,
			effectDetail: 0,
			bloodEnabled: false,
			// The /id byte ships standing (0xCCB5D8).
			damageNumbers: true,
			resources: { ready: () => false, duration: () => 0 },
			renderer: { characterSocket: () => null, characterMatrix: () => null, characterLocalMatrix: () => null },
			feedback: {
				take: () => [ {
					cast: { token: "hit-1", caster: 1, target: 2, skill: 100 },
					target: 2,
					impact: { type: 0, flags: 0, damage: 10, fatal: false, secondaryAmount: 0 },
					key: "hit-1",
					at: SECONDS
				} ],
				pendingDeaths: () => new Set()
			},
			health: undefined,
			sounds
		},
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
	return { seen, texts: events.state.damageTexts };
}

test("effect anchors sit on the walking local player's presented pose", () => {
	const { seen, texts } = driveEvents();
	// The effect step reads the local player's anchor from the frame's
	// drawn poses (#1545); the gameplay view keeps the logical pose.
	assert.equal( seen.effectsDrawn.get( 1 ).x, DRAWN.x );
	assert.equal( seen.effectsDrawn.get( 1 ).z, DRAWN.z );
	assert.notEqual( seen.effectsDrawn.get( 1 ).x, LOGICAL.x );
	assert.equal( seen.effectsPose.x, LOGICAL.x );
	// So does the local player's row among the effect entities.
	const local = seen.effectsEntities.find( row => row.gid === 1 );
	assert.equal( local.x, DRAWN.x );
	assert.equal( local.z, DRAWN.z );
	// And the damage text placed on the victim (CIDamageText 8D3920
	// copies the impact position): the victim's presented pose.
	assert.equal( texts.length, 1 );
	assert.equal( texts[0].anchor.x, 120 - 3 );
	assert.equal( texts[0].anchor.z, 520 - 4 );
});

test("a held pose moves the effect step's local anchor, keeping the mover's heading", () => {
	const HELD = { regionId: REGION, x: 85, y: 10, z: 470, angle: 0.25 };
	const { seen } = driveEvents( new Map( [ [ 1, HELD ] ] ) );
	const pose = seen.effectsDrawn.get( 1 );
	// On a cast-burst frame actor-motion takes the hold later this step,
	// so the composed map lays the held pose over the frame's drawn
	// poses: the effect step anchors at the held body, not the drawn one.
	assert.equal( pose.x, HELD.x );
	assert.equal( pose.z, HELD.z );
	assert.notEqual( pose.x, DRAWN.x );
	// The hold moves the body, it does not turn it: the heading stays
	// the movement owner's, not the held row's own angle.
	assert.equal( pose.angle, LOGICAL.angle );
	assert.notEqual( pose.angle, HELD.angle );
});

test("sounds at the walking local player sit on its presented pose", () => {
	const { seen } = driveEvents();
	assert.ok( seen.resolve );
	assert.deepEqual( seen.resolve( 1 ), worldAudioPosition( DRAWN ) );
	assert.notDeepEqual( seen.resolve( 1 ), worldAudioPosition( LOGICAL ) );
	// Another emitter keeps its entity row: only the local player's
	// anchor moves to the presented pose.
	assert.deepEqual( seen.resolve( 2 ), worldAudioPosition( { regionId: REGION, x: 120, y: 10, z: 520 } ) );
});

test("the camera target sits on the drawn body while walking", () => {
	const output = {};
	selectCameraTarget(
		{
			local: entity( 1, 40, 440 ),
			gameplay: { localGid: 1, pose: { ...LOGICAL, angle: 1.5 } },
			next: new Map( [ [ 1, { pose: { ...DRAWN, yaw: 0.7 } } ] ] ),
			entitiesByGid: new Map()
		},
		output,
		{ heights: new Map( [ [ "CHAR_1", 42 ] ] ), catalog: new Map( [ [ 1, { codename: "CHAR_1" } ] ] ) }
	);
	assert.equal( output.cameraTarget.pose.x, DRAWN.x );
	assert.equal( output.cameraTarget.pose.z, DRAWN.z );
	assert.notEqual( output.cameraTarget.pose.x, LOGICAL.x );
	// The camera follows the drawn body's yaw, while pose.angle stays the
	// mover's snapped heading (FollowCameraTarget).
	assert.equal( output.cameraTarget.yaw, Math.PI - 0.7 );
	assert.equal( output.cameraTarget.pose.angle, 1.5 );
	assert.equal( output.cameraTarget.height, 42 );
	assert.equal( output.cameraTarget.mounted, false );
});

test("a riding camera target sits on the drawn mount", () => {
	const rider = entity( 1, 40, 440 );
	rider.mountedOn = 9;
	const mount = entity( 9, 40, 440 );
	const output = {};
	selectCameraTarget(
		{
			local: rider,
			gameplay: { localGid: 1, pose: { ...LOGICAL, angle: 1.5 } },
			next: new Map( [ [ 9, { pose: { regionId: REGION, x: 90, y: 20, z: 490, yaw: 0.7 } } ] ] ),
			entitiesByGid: new Map( [ [ 9, mount ] ] )
		},
		output,
		{
			heights: new Map( [ [ "CHAR_1", 42 ], [ "MOUNT_9", 30 ] ] ),
			catalog: new Map( [ [ 1, { codename: "CHAR_1" } ], [ 9, { codename: "MOUNT_9" } ] ] )
		}
	);
	assert.equal( output.cameraTarget.mounted, true );
	assert.equal( output.cameraTarget.pose.x, 90 );
	assert.equal( output.cameraTarget.pose.z, 490 );
	// The rider floats at the drawn mount's height less the native seat.
	assert.equal( output.cameraTarget.pose.y, Math.fround( Math.fround( 20 + 30 ) - 13 ) );
	assert.equal( output.cameraTarget.pose.angle, 1.5 );
});
