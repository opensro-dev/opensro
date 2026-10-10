/*
===========================================================================

shield-skill-visuals.test.mjs - retail Chinese shield buffs through the effect owner

Use the published v1.150 records and EasyFX programs, rather than substitute
stages or particles. Every book must play its hand cast, native sound and
defensive impact; a retained buff does not invent a continuous flame loop.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";
import { createSkillVisualFixture } from "../helpers/skill-visual-fixture.mjs";
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

const FIRE_SHIELD_BOOKS = [
	{ name: "Phoenix", group: "a", ids: [ 127, 1394, 1395, 1396, 1397, 1398 ] },
	{ name: "Flower", group: "a", ids: [ 128, 1399, 1400, 1401, 1402, 1403 ] },
	{ name: "King", group: "b", ids: [ 129, 1404, 1405, 1406, 1407, 1408 ] },
	{ name: "Emperor", group: "b", ids: [ 3431 ] }
];
const CAST_SOUND = "/assets/audio/sfx/prim/snd/skill/csk_fire_gigong_hand.wav";
const decoder = createEffectDecoder();
const records = decoder.decode( readPublishedAssetBytesSync( "/assets/skill/effectRecords.json", CLIENT_PUBLIC_ROOT ) );

test("all 19 Fire Shield ranks retain the retail animation, hand binding, sound and two visual groups", () => {
	for ( const book of FIRE_SHIELD_BOOKS ) {
		for ( const id of book.ids ) {
			const record = records[id];
			assert.ok( record, `${book.name} ${id}: missing record` );
			assert.deepEqual( record.clips, [ "native:default:30" ] );
			assert.equal( record.stages.length, 1 );
			const stage = record.stages[0];
			assert.equal( stage.phase, "SHOT" );
			assert.equal( stage.startEvent, 1 );
			assert.equal( stage.action, "AT_ONE_FOLLOW" );
			assert.equal( stage.move, "MOV_NONE" );
			assert.equal( stage.bone, "Bip01 L Hand" );
			assert.deepEqual( stage.offset, [ 0, 0, 0 ] );
			assert.equal( stage.keepRotation, true );
			assert.equal( stage.count, 1 );
			assert.equal( stage.life, 0 );
			assert.equal( stage.sound, CAST_SOUND );
			assert.equal( stage.resource, `skill/china/fire_shield_effect_${book.group}.efp` );
			assert.equal(
				defined( record.attachedAction ).defense,
				`skill/china/fire_shield_damage_${book.group}.efp`
			);
			assert.equal( record.damageEffect, null );
		}
	}
});

test("the four retail Fire Shield EasyFX programs and every texture and sound are published", () => {
	const bytes = readPublishedAssetBytesSync( "/assets/effects/programs.json", CLIENT_PUBLIC_ROOT );
	try {
		for ( const group of [ "a", "b" ] ) {
			for ( const kind of [ "effect", "damage" ] ) {
				const resource = `skill/china/fire_shield_${kind}_${group}.efp`;
				const decoded = decoder.model( bytes, resource );
				assert.equal( decoded.model.primitives.length, kind === "effect" ? 8 : 4 );
				assert.equal( decoded.model.clips[0].duration, kind === "effect" ? 2 : .95 );
				assert.ok( decoded.imagePaths.length > 0 );
				for ( const image of decoded.imagePaths ) {
					assert.ok( readPublishedAssetBytesSync( image, CLIENT_PUBLIC_ROOT ).byteLength > 0, image );
				}
			}
		}
		assert.ok( readPublishedAssetBytesSync( CAST_SOUND, CLIENT_PUBLIC_ROOT ).byteLength > 0 );
	} finally {
		decoder.dispose();
	}
});

test("each rank plays one hand cast and defensive hits only while attached, including restored buffs", () => {
	for ( const book of FIRE_SHIELD_BOOKS ) {
		for ( const skill of book.ids ) {
			const f = createSkillVisualFixture( { records } );
			try {
				const cast = { token: skill, caster: 1, target: 1, skill, damage: 0, fatal: false, receivedAtMs: 1000 };
				f.game.casts = [ cast ];
				f.game.attachedEffects = [ { gid: 1, token: skill + 10000, skill, phase: 2, receivedAtMs: 1000 } ];
				assert.deepEqual( f.frame( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] ), [] );
				const actors = f.frame( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
				assert.equal( actors.length, 1, `${book.name} ${skill}: missing cast visual` );
				const hand = actors[0];
				assert.equal(
					decodeURIComponent( hand.model.split( "#" )[1] ),
					`skill/china/fire_shield_effect_${book.group}.efp`
				);
				assert.equal( defined( hand.attachment ).gid, 1 );
				assert.equal( defined( hand.attachment ).bone, "Bip01 L Hand" );
				assert.equal( hand.loop, false );
				assert.deepEqual( f.sounds.map( sound => sound.path ), [ CAST_SOUND ] );
				assert.equal( f.frame( 1.5 ).length, 1 );
				assert.deepEqual( f.frame( 3.1 ), [], "buff must not retain a continuous cast flame" );
				f.game.casts = [];
				const incoming = {
					token: 20000,
					caster: 2,
					target: 1,
					skill: 1,
					damage: 0,
					fatal: false,
					receivedAtMs: 4000
				};
				const route = f.owner.impactSource( 2, 1, f.game.attachedEffects, incoming );
				assert.deepEqual( route, { gid: 1, skill, defensive: true } );
				const pose = { regionId: 257, x: 10, y: 20, z: 30, yaw: radians( 0 ) };
				f.owner.damage(
					1,
					2,
					0,
					route.skill,
					route.defensive,
					false,
					false,
					pose,
					[ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
					undefined,
					true,
					4
				);
				const hit = f.frame( 4 );
				assert.equal( hit.length, 1 );
				assert.equal(
					decodeURIComponent( hit[0].model.split( "#" )[1] ),
					`skill/china/fire_shield_damage_${book.group}.efp`
				);
				assert.equal( hit[0].loop, false );
				assert.equal( f.frame( 4.94 ).length, 1 );
				assert.deepEqual( f.frame( 4.96 ), [] );
				f.game.attachedEffects = [];
				assert.deepEqual( f.owner.impactSource( 2, 1, [], incoming ), { gid: 2, skill: 1, defensive: false } );
				f.game.attachedEffects = [ { gid: 1, token: 0, skill, phase: 2, restored: true } ];
				assert.deepEqual( f.owner.impactSource( 2, 1, f.game.attachedEffects, incoming ), route );
				assert.deepEqual( f.frame( 600 ), [], "restoration must not replay the cast" );
				assert.equal( f.owner.error(), null );
			} finally {
				f.owner.dispose();
			}
		}
	}
});

const FLYING_HEAVEN_BOOKS = [
	{ name: "Ice Flame", first: 30541, count: 6, group: "a" },
	{ name: "Wind Cloud", first: 30547, count: 6, group: "a" },
	{ name: "Hundred Ghosts", first: 30553, count: 6, group: "b" },
	{ name: "Mountain Sea", first: 30559, count: 4, group: "b" }
];
const SHIELD_TRADEOFF_SOUNDS = [
	"/assets/audio/sfx/prim/snd/skill/csk_sword_shieldpd_a.wav",
	"/assets/audio/sfx/prim/snd/skill/csk_sword_shieldpd_b.wav"
];

test("all 22 Flying Heaven Art ranks retain the three original cast stages and two sounds", () => {
	for ( const book of FLYING_HEAVEN_BOOKS ) {
		for ( let rank = 0; rank < book.count; rank++ ) {
			const record = defined( records[book.first + rank] );
			assert.deepEqual( record.clips, [ "native:default:30" ] );
			assert.equal( record.secondaryEffect, true );
			assert.equal( record.damageEffect, null );
			assert.equal( record.stages.length, 3 );
			const resources = [
				"sword_shieldpd_ready_a",
				"sword_shieldpd_ready_b",
				"sword_special_force_" + book.group
			];
			for ( let event = 0; event < 3; event++ ) {
				const stage = record.stages[event];
				assert.equal( stage.phase, "SHOT" );
				assert.equal( stage.startEvent, event );
				assert.equal( stage.resource, "skill/china/" + resources[event] + ".efp" );
				assert.equal( stage.sound, event < 2 ? SHIELD_TRADEOFF_SOUNDS[event] : null );
				assert.equal( stage.action, "AT_ONE_FOLLOW" );
				assert.equal( stage.move, "MOV_NONE" );
				assert.equal( stage.bone, null );
				assert.deepEqual( stage.offset, event < 2 ? [ -1, 17, 2 ] : [ 0, 10, 8 ] );
				assert.equal( stage.life, 0 );
			}
		}
	}
});

test("Flying Heaven Art programs, textures and sounds are published with their original durations", () => {
	const decoder = createEffectDecoder();
	const bytes = readPublishedAssetBytesSync( "/assets/effects/programs.json", CLIENT_PUBLIC_ROOT );
	try {
		for (
			const program of [
				{ name: "sword_shieldpd_ready_a", primitives: 4, duration: 1.45 },
				{ name: "sword_shieldpd_ready_b", primitives: 8, duration: 1.5 },
				{ name: "sword_special_force_a", primitives: 2, duration: 1.45 },
				{ name: "sword_special_force_b", primitives: 2, duration: 1.4 }
			]
		) {
			const decoded = decoder.model( bytes, "skill/china/" + program.name + ".efp" );
			assert.equal( decoded.model.primitives.length, program.primitives );
			assert.equal( decoded.model.clips[0].duration, program.duration );
			for ( const image of decoded.imagePaths ) {
				assert.ok( readPublishedAssetBytesSync( image, CLIENT_PUBLIC_ROOT ).byteLength > 0, image );
			}
		}
		for ( const sound of SHIELD_TRADEOFF_SOUNDS ) {
			assert.ok( readPublishedAssetBytesSync( sound, CLIENT_PUBLIC_ROOT ).byteLength > 0 );
		}
	} finally {
		decoder.dispose();
	}
});

test("every Flying Heaven Art rank plays all three event stages once through the effect owner", () => {
	for ( const book of FLYING_HEAVEN_BOOKS ) {
		for ( let rank = 0; rank < book.count; rank++ ) {
			const skill = book.first + rank;
			const f = createSkillVisualFixture( { records } );
			try {
				const cast = { token: skill, caster: 1, target: 1, skill, damage: 0, fatal: false, receivedAtMs: 1000 };
				f.game.casts = [ cast ];
				f.game.attachedEffects = [ { gid: 1, token: skill + 10000, skill, phase: 2, receivedAtMs: 1000 } ];
				const models = [
					"sword_shieldpd_ready_a",
					"sword_shieldpd_ready_b",
					"sword_special_force_" + book.group
				];
				for ( let event = 0; event < 3; event++ ) {
					const actors = f.frame( 1, [ { cast, phase: "SHOT", event, at: 1 } ] );
					assert.equal( actors.length, event + 1, book.name + " " + skill );
					const effect = actors.find( actor =>
						decodeURIComponent( actor.model.split( "#" )[1] ) === "skill/china/" + models[event] + ".efp"
					);
					assert.ok( effect );
					assert.equal( defined( effect.attachment ).gid, 1 );
					assert.equal( effect.loop, false );
					assert.equal( f.frame( 1, [ { cast, phase: "SHOT", event, at: 1 } ] ).length, event + 1 );
				}
				assert.deepEqual( f.sounds.map( sound => sound.path ), SHIELD_TRADEOFF_SOUNDS );
				assert.deepEqual( f.frame( 3.1 ), [] );
				f.game.casts = [];
				f.game.attachedEffects = [ { gid: 1, token: 0, skill, phase: 2, restored: true } ];
				assert.deepEqual( f.frame( 600 ), [], "restoration must not replay cast stages" );
				assert.equal( f.owner.error(), null );
			} finally {
				f.owner.dispose();
			}
		}
	}
});
