/*
===========================================================================

projectile-facing.test.mjs - a flying .efp faces the way it flies

A Wizard's bolts are .efp programs carried by a straight mover. Natively
the mover keeps the yaw its Initialize caller hands it (the source object's
yaw float, CICharactor +0x88; the TR client's a0ebf3 reads it at +0x80) and
only a .bsr mesh gets the travel basis (8D9233; in the TR client a06c4c
writes it into +0xDC, the mesh, while an .efp lives in +0xD8). The caster
faces its target, so the program's head leads along the flight.

The port drew a hand-launched bolt with the launch socket's yaw. A socket
is a drawn actor's pose, whose yaw is characterHeadingYaw (pi minus the
native yaw), so the bolt flew mirrored: right only on the east-west line,
backwards north or south and sideways between. These tests launch Cold
Bolt (a hand bone) and Fire Bolt (no bone) through the renderer's real
socket query toward eight bearings and pin each flight's yaw to the native
yaw of its direction of travel (atan2(dz, dx) + pi/2: native-movement.ts
movementHeading and angles.ts nativeHeadingYaw). The yaw is the drawn
body's: a caster whose logical heading still points away, but whose body
the cast turned to its target, launches along the body (the mover reads
the object's live yaw, which CIDecoSkill_Update 8DC440 turns).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { characterHeadingYaw } = await import( "../../src/engine/foundation/math/angles.ts" );
const { movementHeading } = await import( "../../src/engine/foundation/gameplay/native-movement.ts" );

const REGION = 257;
const RANGE = 150;
const BEARINGS = 8;
// Float yaw stores and the heading word's 1/65536 turn.
const YAW_EPSILON = 5e-4;
const COLD_BOLT = 8620;
const FIRE_BOLT = 8748;
const COLD_RESOURCE = "skill/europe/wizard_cold_bolt_a.efp";
const FIRE_RESOURCE = "skill/europe/wizard_fire_bolt_a.efp";

/*
================
encode
================
*/
function encode( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

/*
================
boltStage

One Wizard bolt row (effectRecords 8620 and 8748): a straight
AT_MOV_1TAR .efp at 250 units/s, from the left hand or from an offset.
The landing offset drops the rows' -2 so the flight runs on the bearing.
================
*/
function boltStage( resource, bone, offset ) {
	return {
		startKeepRotation: !!bone,
		startAddHeight: false,
		targetKeepRotation: false,
		targetAddHeight: false,
		animationPhase: "SHOT",
		startEvent: 1,
		actionType: "AT_MOV_1TAR",
		objectResourcePath: resource,
		startBone: bone,
		startOffset: offset,
		targetBone: null,
		targetOffset: [ 0, 12, 0 ],
		move: { kind: "MOV_STRAIGHT", delay: 0, startSpeed: 250, endSpeed: 250 },
		createCount: 1
	};
}

/*
================
body

A caster model whose left hand sits above the root and on its forward
line, so a socket launch is told apart from a root one while the flight
still runs along the bearing.
================
*/
function body() {
	const node = ( name, parent, translation ) => ({
		name,
		parent,
		translation,
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	});
	return {
		nodes: [ node( "root", -1, [ 0, 0, 0 ] ), node( "Bip01 L Hand", 0, [ 0, 12, 2 ] ) ],
		primitives: [],
		images: [],
		clips: []
	};
}

/*
================
makeEffects

An effect owner over a stub asset owner serving the two bolt rows and
their program models.
================
*/
function makeEffects() {
	const catalog = createEffectDecoder().decode( encode( {
		[COLD_BOLT]: {
			authoredShotAnimationNames: [ "ANI_SKILL_1" ],
			authoredStages: [ boltStage( COLD_RESOURCE, "Bip01 L Hand", [ 0, 0, -2 ] ) ]
		},
		[FIRE_BOLT]: {
			authoredShotAnimationNames: [ "ANI_SKILL_1" ],
			authoredStages: [ boltStage( FIRE_RESOURCE, null, [ 0, 8, -8 ] ) ]
		}
	} ) );
	const models = {
		[COLD_RESOURCE]: { glb: "/assets/cold.glb", clips: [ "effect" ], clipLoop: true },
		[FIRE_RESOURCE]: { glb: "/assets/fire.glb", clips: [ "effect" ], clipLoop: true }
	};
	let serial = 0;
	const jobs = new Map();
	return createCharacterEffects(
		{
			available: () => 4,
			progress: () => null,
			health: () => ({ phase: "running" }),
			install: () => {},
			textureFeatures() {},
			hdTextures() {},
			dispose: () => jobs.clear(),
			/*
			================
			request
			================
			*/
			request( url, limit, kind ) {
				jobs.set(
					++serial,
					kind === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models } ).buffer }
				);
				return serial;
			},
			/*
			================
			take
			================
			*/
			take( id ) {
				const result = jobs.get( id );
				jobs.delete( id );
				return result;
			},
			/*
			================
			cancel
			================
			*/
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://fixture.invalid",
		() => {},
		createPresentationRandom( 1 )
	);
}

/*
================
wrap

An angle folded into (-pi, pi].
================
*/
function wrap( angle ) {
	return Math.PI - (((Math.PI - angle) % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI);
}

/*
================
flight

Casts one bolt from a caster at the origin, facing a target at the given
bearing, and returns the bolt's first two flying poses. A stale caster's
logical heading still points the other way while its drawn body, which
8DC440 turned to the target, faces it; the step then gets the drawn rows
as presentation-events hands them.
================
*/
function flight( skill, bearing, stale = false ) {
	const effects = makeEffects(), renderer = createCharacters();
	renderer.model( "body", body(), [] );
	const at = { regionId: REGION, x: 960, y: 0, z: 960 };
	const there = {
		regionId: REGION,
		x: at.x + Math.cos( bearing ) * RANGE,
		y: 0,
		z: at.z + Math.sin( bearing ) * RANGE
	};
	const heading = movementHeading( { ...at, angle: 0 }, { ...there, angle: 0 } );
	// The drawn rows the socket query reads: characterHeadingYaw, as
	// actor-motion.ts and presentation-events.ts build them.
	const actors = [
		{ gid: 1, model: "body", pose: { ...at, yaw: characterHeadingYaw( heading ) } },
		{ gid: 2, model: "body", pose: { ...there, yaw: characterHeadingYaw( 0 ) } }
	].map( row => ({ ...row, scale: 1, clip: "", time: 0, loop: true }) );
	const entities = [
		{
			gid: 1,
			kind: "player",
			name: "Caster",
			refObjId: 1,
			...at,
			heading: stale ? (heading + 32768) % 65536 : heading
		},
		{ gid: 2, kind: "player", name: "Target", refObjId: 1, ...there, heading: 0 }
	];
	const cast = { token: 1, caster: 1, target: 2, skill, damage: 0, fatal: false };
	const gameplay = { localGid: 3, pose: null, casts: [ cast ], vitals: [], inventory: [] };
	const step = ( now, triggers = [] ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 10,
			triggers,
			( gid, bone, offset ) => renderer.socket( actors, gid, { name: bone, fallback: "mount-root" }, offset ),
			stale ? actors : []
		);
	step( 0 );
	step( .1 );
	step( .2 );
	const launched = step( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
	const flown = step( 1.1 );
	assert.equal( effects.error(), null );
	assert.equal( launched.length, 1, `skill ${skill} launched` );
	assert.equal( flown.length, 1, `skill ${skill} flies` );
	effects.dispose();
	return { from: launched[0].pose, to: flown[0].pose, effectBasis: flown[0].effectBasis };
}

for (
	const [skill, lane] of [ [ COLD_BOLT, "hand-launched Cold Bolt" ], [ FIRE_BOLT, "offset-launched Fire Bolt" ] ]
) {
	for ( const stale of [ false, true ] ) {
		test(`a ${lane} faces its flight on every bearing${stale ? " from a turned body" : ""}`, () => {
			for ( let i = 0; i < BEARINGS; i++ ) {
				const bearing = i / BEARINGS * 2 * Math.PI;
				const { from, to, effectBasis } = flight( skill, bearing, stale );
				const dx = to.x - from.x, dz = to.z - from.z;
				assert.ok( Math.hypot( dx, dz ) > 10, `bearing ${i}: the bolt moved` );
				// An .efp keeps a pose yaw; only a .bsr carries the travel basis.
				assert.equal( effectBasis, undefined );
				const travel = Math.atan2( dz, dx ) + Math.PI / 2;
				const error = wrap( to.yaw - travel );
				assert.ok(
					Math.abs( error ) < YAW_EPSILON,
					`bearing ${i * 360 / BEARINGS} deg: yaw ${to.yaw} is ${error * 180 / Math.PI} deg off the flight`
				);
				assert.equal( from.yaw, to.yaw, "a straight mover keeps its spawn yaw" );
			}
		});
	}
}
