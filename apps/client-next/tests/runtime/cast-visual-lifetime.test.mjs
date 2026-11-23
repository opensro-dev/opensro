/*
===========================================================================

cast-visual-lifetime.test.mjs - a self-cast's visuals end with its cast

#3609: after casting a buff on themselves, the effect stayed on screen.
The wire half (a cast bracket that never closes) is pinned by
effect-cast-teardown.test.mjs and skill-cast-lifecycle.test.mjs; this file
pins the render half with the real skill data: the windup a cast draws,
the casting loop, and a retained buff's loop each end when the native
client ends them.

Native evidence (SRO_Client.exe v1.150, read with revtool): the 0xB505
processor CPSMission_OnSkillCastControl0xB505 (0x7754F0) forwards to
0x8E2C90, which reads the kind byte and token; kind 1 runs the
continuation parser 0x8E0440 then CIDecoSkill_ReleaseStep (0x8DF180,
stage state 2 -> 3 and the stage machine advances), kind 2 runs
CIDecoSkill_CancelCast (0x8DCF40), which marks the deco cancelled and
tears down every remaining stage's spawned objects through 0x8DB770.
CIDecoSkill_Update (0x8DC440) returns 0 once the deco's +0xD0 flag is set,
so a cast's effect objects live exactly as long as its bracket.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const manifest = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skillfx/manifest.json", "utf8" ) );
const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json", "utf8" ) );

/*
================
harness

Builds an effects presenter fed with the real decoded records of the
given skills. The caster is the local player; durations are stubbed at
one second, so a released visual is due out within one second of its
bracket event (its fade, when the stage authors one, is shorter).
================
*/
function harness( skillIds ) {
	const records = {};
	for ( const id of skillIds ) records[id] = raw[id];
	const catalog = createEffectDecoder().decode(
		new TextEncoder().encode( JSON.stringify( records ) )
	);
	let serial = 0;
	const jobs = new Map();
	// The asset owner, the entity and the gameplay state are partial
	// fixtures: the presenter reads only the members given here, so the
	// casts state the contract types without inventing the rest (the
	// gameplay type is module-private, hence any).
	const assets = /** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */ (
		/** @type {unknown} */ ({
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
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
		})
	);
	const owner = createCharacterEffects( assets, "http://localhost", () => {}, createPresentationRandom( 1 ) );
	const entities = /** @type {import("../../src/engine/contracts/world.ts").EntityState[]} */ (
		/** @type {unknown} */ ([ { gid: 1, kind: "local-player", regionId: 257, x: 0, y: 0, z: 0, heading: 0 } ])
	);
	const game = /** @type {any} */ ({ localGid: 1, inventory: [], casts: [], attachedEffects: [] });
	const step = ( now, events = [] ) =>
		owner.step(
			entities,
			game,
			now,
			() => true,
			() => 1,
			events,
			() => ({ regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) }),
			[ {
				gid: 1,
				model: "/assets/body.glb",
				pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
				clip: "stand",
				time: 0,
				loop: true,
				scale: 1,
				heightFactor: 1
			} ],
			2
		);
	return { owner, game, step };
}

/*
================
names

The drawn effect resources of one frame, without their bundle prefix.
================
*/
const names = actors => actors.map( a => decodeURIComponent( a.model ?? "" ).split( "#" ).pop() );

test("a teleport windup ends when its cast bracket closes", () => {
	const { owner, game, step } = harness( [ 9063 ] );
	const cast = { token: 7, caster: 1, target: 1, skill: 9063, receivedAtMs: 0 };
	game.casts = [ cast ];
	step( 0, [ { cast, phase: "READY", event: 0, at: 0 } ] );
	step( .1 );
	step( .2 );
	assert.ok(
		names( step( .3 ) ).some( n => n.includes( "wizard_light_teleport_a" ) ),
		"the windup is drawn while the cast is open"
	);
	// The release, then the finalize: the cast leaves the table.
	cast.shotAtMs = 200;
	cast.cancelledAtMs = 300;
	game.casts = [];
	step( .35 );
	const tail = names( step( 2 ) );
	assert.ok(
		!tail.some( n => n.includes( "wizard_light_teleport_a" ) ),
		`the windup outlives its bracket: ${tail}`
	);
	assert.equal( owner.error(), null );
	owner.dispose();
});

test("a casting loop ends when the shot fires", () => {
	const { owner, game, step } = harness( [ 10228 ] );
	const cast = { token: 9, caster: 1, target: 1, skill: 10228, receivedAtMs: 0 };
	game.casts = [ cast ];
	step( 0, [ { cast, phase: "READY", event: 0, at: 0 } ] );
	step( .1 );
	step( .2 );
	assert.ok(
		names( step( .3 ) ).some( n => n.includes( "skill_cast_foot_heal_a" ) ),
		"the casting loop is drawn while the cast charges"
	);
	// Native advances the stage machine on the release (0x8DF180); the
	// READY loop leaves with the shot, not with the bracket close.
	cast.shotAtMs = 200;
	step( .35 );
	const tail = names( step( 1 ) );
	assert.ok(
		!tail.some( n => n.includes( "skill_cast_foot_heal_a" ) ),
		`the casting loop outlives its shot: ${tail}`
	);
	assert.equal( owner.error(), null );
	owner.dispose();
});

test("a retained buff's loop lives while the buff does and ends with it", () => {
	const { owner, game, step } = harness( [ 27 ] );
	const cast = { token: 11, caster: 1, target: 1, skill: 27, receivedAtMs: 0, shotAtMs: 100 };
	game.casts = [ cast ];
	game.attachedEffects = [ { gid: 1, skill: 27, token: 11, phase: 2 } ];
	step( 0 );
	step( .1 );
	step( .2 );
	assert.ok(
		names( step( 1 ) ).some( n => n.includes( "sword_shield_keep_a" ) ),
		"the buff loop is drawn"
	);
	assert.ok(
		names( step( 30 ) ).some( n => n.includes( "sword_shield_keep_a" ) ),
		"the buff loop lives while the buff is active"
	);
	// The teardown takes the row; the loop leaves with it.
	game.attachedEffects = [];
	step( 31 );
	const tail = names( step( 33 ) );
	assert.ok(
		!tail.some( n => n.includes( "sword_shield_keep_a" ) ),
		`the buff loop outlives the buff: ${tail}`
	);
	assert.equal( owner.error(), null );
	owner.dispose();
});
