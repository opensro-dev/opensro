/*
===========================================================================

efp-projectile-facing.test.mjs - every staggered .efp bolt takes the
caster's turned yaw, not the logical heading

Frozen Spear (skill 8635) fires three wizard_cold_bolt_b.efp movers at
SHOT events 2, 4 and 6 of one cast. Natively a mover's Initialize caller
hands it the source object's yaw float (CICharactor +0x88), which
CIDecoSkill_Update (8DC440) keeps turned toward the target for the whole
cast; the travel basis (8D9233) is written only into the mesh child
(+0xE0), while an .efp program (AttachVisualModel's kind 2) lives in
+0xDC and never receives it. The port's carrier of the object yaw is the
presented body actor, so each bolt's spawn yaw must be that turned yaw
at every staggered spawn - a bolt that instead reads the caster's
logical heading (his run direction, some 80 degrees off the flight in
BR-261127-0018-210B) leaves the staff lying across its flight line,
leader and followers alike.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { characterHeadingYaw } = await import( "../../src/engine/foundation/math/angles.ts" );
const { movementHeading } = await import( "../../src/engine/foundation/gameplay/native-movement.ts" );

const FROZEN_SPEAR = 8635;
const BOLT = "skill/europe/wizard_cold_bolt_b.efp";
const TARGET = { x: 90, z: 120 };
// Float yaw stores and the heading word's 1/65536 turn.
const YAW_EPSILON = 5e-3;

/*
================
boltStage

One Frozen Spear bolt row as published in effectRecords 8635 (stages at
SHOT events 2, 4 and 6): a straight AT_MOV_1TAR .efp at 250 units/s from
the left hand, landing 12 up on the victim with a small authored sway.
================
*/
function boltStage( event, targetOffset ) {
	return {
		startKeepRotation: true,
		startAddHeight: false,
		targetKeepRotation: false,
		targetAddHeight: false,
		animationPhase: "SHOT",
		startEvent: event,
		damageEvent: true,
		actionType: "AT_MOV_1TAR",
		objectResourcePath: BOLT,
		startBone: "Bip01 L Hand",
		startOffset: [ 0, 0, -2 ],
		targetBone: null,
		targetOffset,
		move: { kind: "MOV_STRAIGHT", delay: 0, startSpeed: 250, endSpeed: 250 },
		createCount: 1
	};
}

/*
================
spearCast

Casts Frozen Spear from a wizard whose logical heading points some 80
degrees off the flight (his run direction when the cast began), while
the presented body - the port's carrier of the native object yaw - is
turned onto the target, and returns the three bolt actors.
================
*/
function spearCast() {
	const catalog = createEffectDecoder().decode(
		new TextEncoder().encode( JSON.stringify( {
			[FROZEN_SPEAR]: {
				authoredShotAnimationNames: [ "ANI_SKILL_1" ],
				authoredStages: [
					boltStage( 2, [ 0, 12, -2 ] ),
					boltStage( 4, [ 1, 13, -2 ] ),
					boltStage( 6, [ -1, 12, -2 ] )
				]
			}
		} ) )
	);
	const models = { [BOLT]: { glb: "/assets/bolt.glb", clips: [ "effect" ], clipLoop: true } };
	let serial = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		/** @type {any} */ ({
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models } )
							).buffer
						}
				);
				return serial;
			},
			take( id ) {
				const job = jobs.get( id );
				jobs.delete( id );
				return job;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		}),
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const casterAt = { regionId: 257, x: 0, y: 0, z: 0, angle: 0 };
	const targetAt = { regionId: 257, x: TARGET.x, y: 0, z: TARGET.z, angle: 0 };
	const travelWord = movementHeading( casterAt, targetAt );
	// The run direction the logical heading still holds: 80 degrees off.
	const logicalAt = {
		regionId: 257,
		x: Math.cos( 2.3 ) * 100,
		y: 0,
		z: Math.sin( 2.3 ) * 100,
		angle: 0
	};
	const logicalWord = movementHeading( casterAt, logicalAt );
	const entities = [
		{ gid: 1, kind: "local-player", regionId: 257, x: 0, y: 0, z: 0, heading: logicalWord },
		{ gid: 2, kind: "monster", regionId: 257, x: TARGET.x, y: 0, z: TARGET.z, heading: 0 }
	];
	const cast = {
		token: 1,
		caster: 1,
		target: 2,
		skill: FROZEN_SPEAR,
		receivedAtMs: 0,
		results: [ { target: 2, impacts: [ { damage: 10 } ] } ]
	};
	const game = {
		localGid: 1,
		pose: { regionId: 257, x: 0, y: 0, z: 0, angle: logicalWord },
		inventory: [],
		casts: [ cast ]
	};
	// The drawn body 8DC440 turned onto the target: its presented row is
	// what the mover's Initialize reads in this port.
	const presented = [ {
		gid: 1,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: characterHeadingYaw( travelWord ) },
		scale: 1,
		clip: "",
		time: 0,
		loop: true
	} ];
	const step = ( now, events = [] ) =>
		owner.step(
			entities,
			game,
			now,
			() => true,
			() => 10,
			events,
			gid => ({
				regionId: 257,
				x: gid === 1 ? 0 : TARGET.x,
				y: 0,
				z: gid === 1 ? 0 : TARGET.z,
				yaw: 0
			}),
			presented,
			2
		);
	step( 0 );
	step( .1 );
	const spawned = [], seen = new Set();
	for ( const [index, event] of [ 2, 4, 6 ].entries() ) {
		const actors = step( .2 + index * .15, [ { cast, phase: "SHOT", event, at: .2 + index * .15 } ] );
		for ( const actor of actors ) {
			if (
				!decodeURIComponent( actor.model ?? "" ).endsWith( "wizard_cold_bolt_b.efp" ) || seen.has( actor.gid )
			) {
				continue;
			}
			seen.add( actor.gid );
			spawned.push( actor );
		}
	}
	owner.dispose();
	return spawned;
}

test("every Frozen Spear bolt spawns facing its flight, leader and followers alike", () => {
	const bolts = spearCast();
	assert.equal( bolts.length, 3, "the three bolt stages did not all spawn" );
	// The flight's native yaw (atan2(dz, dx) + pi/2, as projectile-facing
	// pins it): what a bolt drawn across its line gets wrong by the angle
	// between the caster's logical heading and the flight.
	const travel = Math.atan2( TARGET.z, TARGET.x ) + Math.PI / 2;
	const wrap = angle => Math.PI - (((Math.PI - angle) % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI);
	for ( const [index, bolt] of bolts.entries() ) {
		// An .efp carries no travel basis; the mesh child (+0xE0) alone
		// receives 8D9233's rows.
		assert.equal( bolt.effectBasis, undefined, `bolt ${index + 1} carries a mesh basis` );
		const error = wrap( bolt.pose.yaw - travel );
		assert.ok(
			Math.abs( error ) < YAW_EPSILON,
			`bolt ${index + 1} yaw is ${error * 180 / Math.PI} deg off the flight line`
		);
	}
});
