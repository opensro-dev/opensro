/*
===========================================================================

drawn-effect-anchors.test.mjs - effect anchors sit on the drawn body (#1545)

While the local player walks, its presented (frame-clock) pose trails the
movement owner's logical pose, and its entity row still holds the wire
position. The effect step's local anchors - cast launch points, stage
sounds and statics, entity-attached visuals - must sit on the frame's
drawn pose, which the step now receives as its own input (#1442). The
hawk's mechanical holder is game logic and stays on the logical pose.
Loads the shipped modules through the shared native loader.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { readFileSync } from "node:fs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterEffects } = await import(
	sourceFileUrl( "src/engine/runtime/characters/effects/effects.ts" ).href
);
const { createEffectDecoder } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/effects/effects.ts" ).href
);
const { createPresentationRandom } = await import(
	sourceFileUrl( "src/engine/runtime/random/random.ts" ).href
);
const { worldAudioPosition } = await import(
	sourceFileUrl( "src/engine/foundation/world/world-space.ts" ).href
);
const { nativeHeadingYaw } = await import(
	sourceFileUrl( "src/engine/foundation/math/angles.ts" ).href
);

const REGION = 257;
// The local player's entity row still holds its wire position; the
// movement owner's logical pose has run ahead of the drawn body.
const WIRE = { regionId: REGION, x: 40, y: 10, z: 440 };
const LOGICAL = { regionId: REGION, x: 100, y: 10, z: 500, angle: 0 };
const DRAWN = { regionId: REGION, x: 90, y: 10, z: 480, angle: 0 };
const encode = value => new TextEncoder().encode( JSON.stringify( value ) );

/*
================
makeEffects

An effect owner over a stub asset owner serving one decoded catalog and
no stage models, capturing every sound it is asked to play.
================
*/
function makeEffects( catalog, sounds, models = {} ) {
	let id = 0;
	const jobs = new Map();
	return createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: encode( { format: "sro-skill-stage-models", models } ).buffer
						}
				);
				return id;
			},
			take( jobId ) {
				const result = jobs.get( jobId );
				jobs.delete( jobId );
				return result;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://fixture.invalid",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
}

/*
================
frame

The two entity rows, the logical gameplay and the frame's drawn poses
for one walking-local-player step.
================
*/
function frame( gameplay ) {
	const entities = [
		{ gid: 1, kind: "local-player", ...WIRE, heading: 0 },
		{ gid: 2, kind: "player", regionId: REGION, x: 140, y: 10, z: 540, heading: 0 }
	];
	const drawnPoses = new Map( [
		[ 1, { ...DRAWN } ],
		[ 2, { regionId: REGION, x: 137, y: 10, z: 536, angle: 0 } ]
	] );
	return { entities, gameplay: { localGid: 1, pose: { ...LOGICAL }, ...gameplay }, drawnPoses };
}

/*
================
stepper

A step that hands the effect owner the frame's drawn poses, as
presentation-events does.
================
*/
function stepper( effects, entities, gameplay, drawnPoses ) {
	return ( now, triggers = [] ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 0.5,
			triggers,
			undefined,
			[],
			2,
			true,
			new Map(),
			drawnPoses
		);
}

test("a cast launches from the walking local player's presented pose", () => {
	const catalog = createEffectDecoder().decode( encode( {
		"2": {
			authoredShotAnimationNames: [ "ANI_SKILL_1" ],
			authoredStages: [ {
				startKeepRotation: true,
				startAddHeight: false,
				targetKeepRotation: true,
				targetAddHeight: false,
				animationPhase: "SHOT",
				startEvent: 1,
				actionType: "AT_MOV_1TAR",
				objectResourcePath: "bolt.bsr",
				startOffset: [ 0, 0, 0 ],
				targetOffset: [ 0, 0, 0 ],
				move: { kind: "MOV_STRAIGHT", delay: 0, startSpeed: 250, endSpeed: 250 },
				createCount: 1
			} ]
		}
	} ) );
	const sounds = [];
	const effects = makeEffects( catalog, sounds, {
		"bolt.bsr": { glb: "/assets/bolt.glb", clips: [ "stand" ], clipLoop: true }
	} );
	const { entities, gameplay, drawnPoses } = frame( { casts: [] } );
	const step = stepper( effects, entities, gameplay, drawnPoses );
	step( 0 );
	step( .1 );
	step( .2 );
	const cast = { token: 1, caster: 1, target: 2, skill: 2 };
	gameplay.casts = [ cast ];
	step( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
	const actors = step( 1.1 );
	assert.equal( actors.length, 1 );
	// A tenth of a second at 250 u/s from the drawn body toward the
	// target: (90, 480) + 25 * (50, 60) / |(50, 60)|.
	assert.ok( Math.abs( actors[0].pose.x - 106.0 ) < 0.75, `x ${actors[0].pose.x}` );
	assert.ok( Math.abs( actors[0].pose.z - 499.2 ) < 0.75, `z ${actors[0].pose.z}` );
	// Neither the logical pose's launch line nor the wire row's.
	assert.ok( Math.hypot( actors[0].pose.x - 117.7, actors[0].pose.z - 517.7 ) > 5 );
	assert.ok( Math.hypot( actors[0].pose.x - WIRE.x, actors[0].pose.z - WIRE.z ) > 5 );
	assert.equal( effects.error(), null );
	effects.dispose();
});

test("a stage sound and its static stage sit on the presented pose", () => {
	const catalog = {
		"1": {
			clips: [ "attack1" ],
			stages: [ {
				resource: "hit.efp",
				damageEvent: true,
				startEvent: 1,
				action: "AT_DMG_POS",
				move: "MOV_NONE",
				bone: null,
				offset: [ 0, 10, 0 ],
				life: 0,
				sound: "stage.wav",
				count: 1,
				scripts: []
			} ]
		}
	};
	const sounds = [];
	const effects = makeEffects( catalog, sounds );
	const cast = { token: 1, caster: 2, target: 1, skill: 1 };
	const { entities, gameplay, drawnPoses } = frame( { casts: [ cast ] } );
	const step = ( now ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 0.5,
			gameplay.casts.map( row => ({ cast: row, phase: "SHOT", event: 1, at: now }) ),
			undefined,
			[],
			2,
			true,
			new Map(),
			drawnPoses
		);
	step( 0 );
	step( .1 );
	step( .2 );
	const actors = step( .3 );
	assert.equal( actors.length, 1 );
	assert.equal( actors[0].pose.x, DRAWN.x );
	assert.equal( actors[0].pose.z, DRAWN.z );
	assert.equal( actors[0].pose.y, DRAWN.y + 10 );
	const sound = sounds.find( event => event.path === "stage.wav" );
	assert.ok( sound, "the stage sound played" );
	const [x, y, z] = worldAudioPosition( DRAWN );
	assert.deepEqual( [ sound.x, sound.y, sound.z ], [ x, y, z ] );
	assert.notDeepEqual( [ sound.x, sound.y, sound.z ], [ ...worldAudioPosition( LOGICAL ) ] );
	assert.equal( effects.error(), null );
	effects.dispose();
});

test("an entity-attached follow visual rides the presented pose", () => {
	const catalog = {
		"3": {
			clips: [ "attack1" ],
			stages: [ {
				resource: "hit.efp",
				startEvent: 1,
				action: "AT_ONE_FOLLOW",
				move: "MOV_NONE",
				bone: null,
				offset: [ 0, 10, 0 ],
				life: 0,
				count: 1,
				scripts: []
			} ]
		}
	};
	const sounds = [];
	const effects = makeEffects( catalog, sounds );
	const cast = { token: 1, caster: 1, target: 2, skill: 3 };
	const { entities, gameplay, drawnPoses } = frame( { casts: [ cast ] } );
	const step = ( now ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 0.5,
			gameplay.casts.map( row => ({ cast: row, phase: "SHOT", event: 1, at: now }) ),
			undefined,
			[],
			2,
			true,
			new Map(),
			drawnPoses
		);
	step( 0 );
	step( .1 );
	step( .2 );
	const actors = step( .3 );
	assert.equal( actors.length, 1 );
	assert.equal( actors[0].pose.x, DRAWN.x );
	assert.equal( actors[0].pose.z, DRAWN.z );
	assert.equal( actors[0].pose.y, DRAWN.y + 10 );
	assert.equal( effects.error(), null );
	effects.dispose();
});

test("the hawk's mechanical holder stays on the logical pose", () => {
	const catalog = createEffectDecoder().decode(
		readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" )
	);
	const manifest = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skillfx/manifest.json", "utf8" );
	let serial = 0;
	const jobs = new Map();
	const effects = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: new TextEncoder().encode( manifest ).buffer }
				);
				return serial;
			},
			take( id ) {
				const row = jobs.get( id );
				jobs.delete( id );
				return row;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	// The holder walks: logical pose ahead at +60, drawn body behind at
	// -60, wire row at the origin. The bird steers by the logical holder.
	const entities = [
		{ gid: 1, kind: "local-player", regionId: REGION, x: 0, y: 0, z: 0, heading: 0 },
		{ gid: 2, kind: "monster", regionId: REGION, x: 120, y: 0, z: 0, heading: 0 }
	];
	const gameplay = {
		localGid: 1,
		pose: { regionId: REGION, x: 60, y: 0, z: 0, angle: 0 },
		casts: [],
		attachedEffects: [ { gid: 1, skill: 78, token: 88, phase: 0, restored: true } ]
	};
	const drawnPoses = new Map( [ [ 1, { regionId: REGION, x: -60, y: 0, z: 0, angle: 0 } ] ] );
	const step = now =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 2,
			[],
			undefined,
			entities.map( entity => ({
				gid: entity.gid,
				model: "/assets/actor.glb",
				clip: "stand",
				time: 0,
				loop: true,
				scale: 1,
				heightFactor: 1,
				effectAnchor: { bone: null, offset: [ 0, 10, 1 ] },
				pose: { ...entity, yaw: Math.PI }
			}) ),
			2,
			true,
			new Map(),
			drawnPoses
		);
	step( 0 );
	step( .01 );
	assert.equal( step( .02 ).length, 1 );
	// Send the bird at the monster; it travels, attacks, and comes home
	// to its mechanical holder - the logical pose at +60, not the drawn
	// body at -60 and not the wire row at the origin.
	gameplay.attachedEffects = [
		{ ...gameplay.attachedEffects[0], hawk: { revision: 1, target: 2, damage: 0x8123 } }
	];
	let now = .03, rows = step( now );
	assert.equal( rows[0].clip, "state-7" );
	while ( rows[0].clip !== "state-2" && now < 5 ) {
		now = Math.round( (now + .01) * 1000 ) / 1000;
		rows = step( now );
	}
	assert.equal( rows[0].clip, "state-2" );
	const attack = now;
	step( attack + 6 );
	rows = step( attack + 8 );
	assert.ok( rows[0].pose.x > 30, `hawk x ${rows[0].pose.x} steered to the drawn holder` );
	assert.equal( rows[0].pose.y, 21 );
	assert.equal( effects.error(), null );
	effects.dispose();
});

test("an attached follow visual turns with the mover's heading, not the presenter's", () => {
	const catalog = {
		"3": {
			clips: [ "attack1" ],
			stages: [ {
				resource: "hit.efp",
				startEvent: 1,
				action: "AT_ONE_FOLLOW",
				move: "MOV_NONE",
				bone: null,
				offset: [ 0, 10, 0 ],
				life: 0,
				count: 1,
				scripts: []
			} ]
		}
	};
	const sounds = [];
	const effects = makeEffects( catalog, sounds );
	const cast = { token: 1, caster: 1, target: 2, skill: 3 };
	const { entities, gameplay, drawnPoses } = frame( { casts: [ cast ] } );
	// The presenter drew the body mid-turn (heading word 16384, a
	// quarter turn); the mover's snapped heading is 0. The drawn map
	// supplies the position only.
	drawnPoses.set( 1, { ...DRAWN, angle: 16384 } );
	const step = ( now ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 0.5,
			gameplay.casts.map( row => ({ cast: row, phase: "SHOT", event: 1, at: now }) ),
			undefined,
			[],
			2,
			true,
			new Map(),
			drawnPoses
		);
	step( 0 );
	step( .1 );
	step( .2 );
	const actors = step( .3 );
	assert.equal( actors.length, 1 );
	assert.equal( actors[0].pose.x, DRAWN.x );
	assert.equal( actors[0].pose.z, DRAWN.z );
	assert.equal( actors[0].pose.yaw, nativeHeadingYaw( LOGICAL.angle ) );
	assert.notEqual( actors[0].pose.yaw, nativeHeadingYaw( 16384 ) );
	assert.equal( effects.error(), null );
	effects.dispose();
});

test("the actor budget trims around the drawn body, not the logical pose", () => {
	const catalog = {
		"7": {
			clips: [],
			stages: [ {
				phase: "ACT_L",
				resource: "hit.efp",
				startEvent: 0,
				action: "AT_LOOP",
				move: "MOV_NONE",
				bone: null,
				offset: [ 0, 0, 0 ],
				count: 1,
				scripts: []
			} ]
		}
	};
	const sounds = [];
	const effects = makeEffects( catalog, sounds );
	const entities = [];
	const presented = [];
	const attachedEffects = [];
	for ( let gid = 1; gid <= 2100; gid++ ) {
		const x = gid <= 2050 ? 10 : 990;
		entities.push( {
			gid,
			kind: gid === 1 ? "local-player" : "player",
			regionId: REGION,
			x,
			y: 0,
			z: 0,
			heading: 0
		} );
		presented.push( {
			gid,
			model: "/assets/actor.glb",
			clip: "stand",
			time: 0,
			loop: true,
			scale: 1,
			heightFactor: 1,
			pose: { regionId: REGION, x, y: 0, z: 0, yaw: Math.PI }
		} );
		attachedEffects.push( { gid, skill: 7, token: gid, phase: 0 } );
	}
	const gameplay = {
		localGid: 1,
		// The movement owner's logical pose sits by the far crowd; the
		// drawn body stands at x=0. The budget keeps what is near the
		// body as drawn.
		pose: { regionId: REGION, x: 1000, y: 0, z: 0, angle: 0 },
		casts: [],
		attachedEffects
	};
	const drawnPoses = new Map( [ [ 1, { regionId: REGION, x: 0, y: 0, z: 0, angle: 0 } ] ] );
	const step = now =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 0.5,
			[],
			undefined,
			presented,
			2,
			true,
			new Map(),
			drawnPoses
		);
	step( 0 );
	step( .05 );
	step( .1 );
	const rows = step( .15 );
	// 2100 attached visuals, 2048 slots: the 52 farthest from the drawn
	// body give way - the whole far crowd at x=990 and two at x=10.
	// Around the logical pose the far crowd would be the nearest and
	// the near crowd would give way instead.
	assert.equal( rows.length, 2048 );
	assert.equal( rows.filter( row => row.pose.x === 990 ).length, 0 );
	assert.equal( rows.filter( row => row.pose.x === 10 ).length, 2048 );
	assert.equal( effects.error(), null );
	effects.dispose();
});
