/*
===========================================================================

fire-buff-visuals.test.mjs - the original Fire buff presentation lifecycles

Verify every shipped rank with the published v1.150 records and EasyFX
programs. Persistent protection and imbues keep their authored attachments;
Flame Body and detection only play their cast effects. Fire Shield has its
own hit-routing coverage in shield-skill-visuals.test.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";
import { createSkillVisualFixture } from "../helpers/skill-visual-fixture.mjs";
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const decoder = createEffectDecoder();
const records = decoder.decode( readPublishedAssetBytesSync( "/assets/skill/effectRecords.json" ) );
const programs = readPublishedAssetBytesSync( "/assets/effects/programs.json" );
const CAST_SOUND = "/assets/audio/sfx/prim/snd/skill/csk_fire_fiecraze_swing.wav";

/*
================
ranks
================
*/
function ranks( first, count ) {
	return Array.from( { length: count }, ( _, i ) => first + i );
}

const PROTECTION = [
	{ book: "a", ids: [ 133, ...ranks( 1415, 8 ) ], hit: "a" },
	{ book: "b", ids: [ 134, ...ranks( 1423, 8 ) ], hit: "a" },
	{ book: "c", ids: [ 135, ...ranks( 1431, 8 ) ], hit: "b" },
	{ book: "d", ids: ranks( 19724, 7 ), hit: "b" }
];
const IMBUES = [
	{ book: "a", ids: [ 124, ...ranks( 1370, 8 ) ], motion: "a" },
	{ book: "b", ids: [ 125, ...ranks( 1378, 8 ) ], motion: "a" },
	{ book: "c", ids: [ 126, ...ranks( 1386, 8 ) ], motion: "b" },
	{ book: "d", ids: ranks( 3416, 13 ), motion: "b" }
];
const WALLS = [
	{ book: "a", ids: [ 136, ...ranks( 1439, 5 ) ], group: "a" },
	{ book: "b", ids: [ 137, ...ranks( 1444, 5 ) ], group: "a" },
	{ book: "c", ids: ranks( 3441, 6 ), group: "b" },
	{ book: "d", ids: ranks( 19745, 3 ), group: "b" }
];
const SHORT_CASTS = [
	{ ids: [ 130, 131, ...ranks( 1409, 4 ) ], resource: "fire_gongup_effect_a" },
	{ ids: [ 132, ...ranks( 1413, 2 ), ...ranks( 19718, 2 ) ], resource: "fire_gongup_effect_b" },
	{ ids: [ ...ranks( 19812, 5 ), ...ranks( 24937, 7 ) ], resource: "fire_detect_stealth_a" },
	{ ids: ranks( 19817, 2 ), resource: "fire_detect_invisible_a" }
];
const SHORT_CAST_IDS = SHORT_CASTS.flatMap( group => group.ids );

/*
================
resourceOf
================
*/
function resourceOf( actor ) {
	return decodeURIComponent( actor.model.split( "#" )[1] );
}

/*
================
fixture

Use actual program durations when the owner retires one-shot stages.
================
*/
function fixture( skill ) {
	const record = defined( records[skill] );
	const durations = new Map();
	for ( const stage of record.stages ) {
		if ( !stage.resource ) continue;
		durations.set( stage.resource, decoder.model( programs, stage.resource ).model.clips[0].duration );
	}
	return createSkillVisualFixture( { records, durations } );
}

/*
================
retire

Native retirement stops emission; the already emitted particles finish
their original lifetime before the actor disappears.
================
*/
function retire( f, at ) {
	f.game.attachedEffects = [];
	for ( const actor of f.frame( at ) ) {
		assert.equal( actor.loop, false );
		assert.ok( actor.emissionEnd !== undefined );
	}
	assert.deepEqual( f.frame( at + 5 ), [] );
}

test("all 34 Fire Protection ranks keep a body aura, restore only the loop, and retire it", () => {
	for ( const book of PROTECTION ) {
		for ( const skill of book.ids ) {
			const record = defined( records[skill] );
			assert.deepEqual( defined( record.phaseClips ).slice( 0, 3 ), [
				[ "native:default:43" ],
				[ "native:default:94" ],
				[ "native:default:29" ]
			] );
			assert.equal( defined( record.attachedAction ).defense, `skill/china/fire_ganggi_damage_${book.hit}.efp` );
			const active = record.stages.filter( stage => stage.phase === "ACT_S" || stage.phase === "ACT_L" );
			assert.deepEqual( active.map( stage => [ stage.phase, stage.action, stage.bone, stage.resource ] ), [
				[ "ACT_S", "AT_ONE_FOLLOW", "Bip01", `skill/china/fire_ganggi_keep_${book.book}.efp` ],
				[ "ACT_L", "AT_LOOP", "Bip01", `skill/china/fire_ganggi_keep_${book.book}.efp` ]
			] );
			const f = fixture( skill );
			try {
				f.game.attachedEffects = [ { gid: 1, token: skill, skill, phase: 2, receivedAtMs: 1000 } ];
				f.frame( 1 );
				assert.deepEqual( f.sounds.map( sound => sound.path ), [ CAST_SOUND ] );
				const kept = f.frame( 30 );
				assert.equal( kept.length, 1, `Fire Protection ${skill}: missing or duplicated lasting aura` );
				assert.equal( kept[0].loop, true );
				assert.equal( defined( kept[0].attachment ).bone, "Bip01" );
				retire( f, 31 );
				f.sounds.length = 0;
				f.game.attachedEffects = [ { gid: 1, token: 0, skill, phase: 2, restored: true } ];
				assert.equal( f.frame( 60 ).length, 1 );
				assert.deepEqual( f.sounds, [], "restoring the protection must not replay its cast sound" );
				assert.equal( f.frame( 90 ).length, 1 );
				assert.equal( f.owner.error(), null );
			} finally {
				f.owner.dispose();
			}
		}
	}
});

test("all 40 Fire Force ranks keep the original weapon effect and select their own impact group", () => {
	for ( const book of IMBUES ) {
		for ( const skill of book.ids ) {
			const record = defined( records[skill] );
			assert.equal( record.damageEffect, `hiteffect/hit_4_fire_hit_${book.book}.efp` );
			assert.deepEqual( record.stages.map( stage => [ stage.phase, stage.bone, stage.resource ] ), [
				[ "ACT_S", "Bip01 R Finger2", `skill/china/fire_gigongta_motion_${book.motion}.efp` ],
				[ "ACT_L", "Bip01 R Finger2", `skill/china/fire_gigongta_keep_${book.book}.efp` ]
			] );
			const f = fixture( skill );
			try {
				f.game.attachedEffects = [ { gid: 1, token: skill, skill, phase: 2, receivedAtMs: 1000 } ];
				assert.equal( f.frame( 1 ).length, 2 );
				const kept = f.frame( 4 );
				assert.equal( kept.length, 1 );
				assert.equal( resourceOf( kept[0] ), `skill/china/fire_gigongta_keep_${book.book}.efp` );
				assert.equal( defined( kept[0].attachment ).bone, "Bip01 R Finger2" );
				const cast = { token: skill + 10000, caster: 1, target: 2, skill: 1, damage: 1, fatal: false };
				assert.deepEqual( f.owner.impactSource( 1, 2, f.game.attachedEffects, cast ), {
					gid: 1,
					skill,
					defensive: false
				} );
				retire( f, 5 );
				f.sounds.length = 0;
				f.game.attachedEffects = [ { gid: 1, token: 0, skill, phase: 2, restored: true } ];
				assert.equal( f.frame( 20 ).length, 1 );
				assert.deepEqual( f.sounds, [] );
				assert.equal( f.owner.error(), null );
			} finally {
				f.owner.dispose();
			}
		}
	}
});

test("all 21 Fire Wall ranks keep the original ground wall until their attachment ends", () => {
	for ( const book of WALLS ) {
		for ( const skill of book.ids ) {
			const record = defined( records[skill] );
			const stage = defined( record.stages.find( stage => stage.phase === "ACT_L" ) );
			assert.equal( stage.resource, `skill/china/fire_hwabyeok_keep_${book.group}.efp` );
			assert.equal( stage.action, "AT_LOOP" );
			assert.equal( stage.bone, null );
			const f = fixture( skill );
			try {
				f.game.attachedEffects = [ { gid: 1, token: skill, skill, phase: 2, receivedAtMs: 1000 } ];
				assert.equal( f.frame( 1 ).length, 1 );
				const kept = f.frame( 60 );
				assert.equal( kept.length, 1 );
				assert.equal( resourceOf( kept[0] ), stage.resource );
				assert.equal( kept[0].loop, true );
				assert.equal( defined( kept[0].attachment ).ground, true );
				retire( f, 61 );
				assert.equal( f.owner.error(), null );
			} finally {
				f.owner.dispose();
			}
		}
	}
});

test("Flame Body and both detection families never invent an active-buff aura or replay casts on restore", () => {
	for ( const { ids, resource } of SHORT_CASTS ) {
		for ( const skill of ids ) {
			const record = defined( records[skill] );
			assert.ok( record.stages.every( stage => !defined( stage.phase ).startsWith( "ACT_" ) ) );
			const f = fixture( skill );
			try {
				const cast = { token: skill, caster: 1, target: 1, skill, damage: 0, fatal: false, receivedAtMs: 1000 };
				f.game.casts = [ cast ];
				f.game.attachedEffects = [ { gid: 1, token: skill + 10000, skill, phase: 2, receivedAtMs: 1000 } ];
				const visual = f.frame( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
				assert.equal( visual.length, 1 );
				assert.equal( resourceOf( visual[0] ), `skill/china/${resource}.efp` );
				assert.equal( visual[0].loop, false );
				assert.deepEqual( f.frame( 5 ), [] );
				f.game.casts = [];
				f.game.attachedEffects = [ { gid: 1, token: 0, skill, phase: 2, restored: true } ];
				assert.deepEqual( f.frame( 600 ), [] );
				assert.equal( f.owner.error(), null );
			} finally {
				f.owner.dispose();
			}
		}
	}
});

test("every authored Fire buff resource, texture and sound is published", () => {
	const resources = new Set(), sounds = new Set();
	const activeIds = [ ...PROTECTION, ...IMBUES, ...WALLS ].flatMap( book => book.ids );
	for ( const skill of [ ...activeIds, ...SHORT_CAST_IDS ] ) {
		const record = defined( records[skill] );
		for ( const stage of record.stages ) {
			if ( stage.resource ) resources.add( stage.resource );
			if ( stage.sound ) sounds.add( stage.sound );
		}
		for (
			const resource of [ record.damageEffect, record.attachedAction?.defense, record.attachedAction?.attack ]
		) {
			if ( resource ) resources.add( resource );
		}
	}
	for ( const resource of resources ) {
		const program = decoder.model( programs, resource );
		assert.ok( program.model.primitives.length > 0, resource );
		for ( const image of program.imagePaths ) {
			assert.ok( readPublishedAssetBytesSync( image ).byteLength > 0, image );
		}
	}
	for ( const sound of sounds ) assert.ok( readPublishedAssetBytesSync( sound ).byteLength > 0, sound );
	for ( const skill of [ 142, ...ranks( 1500, 8 ) ] ) {
		assert.equal( records[skill], undefined, "the Flame Devil passive has no authored particle record" );
	}
});
