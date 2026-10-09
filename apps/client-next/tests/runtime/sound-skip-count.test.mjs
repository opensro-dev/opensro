/*
===========================================================================

sound-skip-count.test.mjs - the effectsound rule's native skip countdown

CGEffSoundBody_PlayNamedSound (8F9280) decrements a rule's countdown on
every trigger and swallows the trigger while it stays at or above zero;
below zero it resets to the rule's skip count (effectsound column 7) and
plays. A pet cat's stand clip loops every 1.6 s with snd_stand at its
start, and its rule's skip of 23 keeps the meow to one loop in 24.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterSounds } = await import(
	sourceFileUrl( "src/engine/runtime/characters/sounds/sounds.ts" ).href
);

const CONTEXT = { player: false, berserk: false };
const AT = [ 0, 0, 0 ];

/*
================
rule
================
*/
function rule( object, skip ) {
	return {
		object,
		handle: "SND_STAND",
		event1: "-",
		publicPath: `/assets/audio/sfx/${object}.wav`,
		volume: 80,
		...(skip === undefined ? {} : { skip })
	};
}

/*
================
triggers

Which of n stand triggers from one profile were heard.
================
*/
function triggers( sounds, heard, profile, n, gid = 1 ) {
	const played = [];
	for ( let i = 0; i < n; i++ ) {
		const before = heard.length;
		sounds.emit( `${gid}:${i}`, profile, [ "SND_STAND" ], CONTEXT, AT, i );
		if ( heard.length > before ) played.push( i );
	}
	return played;
}

test("a rule plays once every skip + 1 triggers, starting with the first", () => {
	const heard = [], sounds = createCharacterSounds( event => heard.push( event ) );
	sounds.catalog( [ rule( "COS_P_CAT", 23 ), rule( "COS_T_HORSE1", 3 ), rule( "ITEM_BELL" ) ] );
	assert.deepEqual( triggers( sounds, heard, "COS_P_CAT", 50 ), [ 0, 24, 48 ] );
	assert.deepEqual( triggers( sounds, heard, "COS_T_HORSE1", 9 ), [ 0, 4, 8 ] );
	assert.deepEqual( triggers( sounds, heard, "ITEM_BELL", 3 ), [ 0, 1, 2 ] );
});

test("the countdown belongs to the rule, shared by every actor using it", () => {
	const heard = [], sounds = createCharacterSounds( event => heard.push( event ) );
	sounds.catalog( [ rule( "COS_P_CAT", 2 ) ] );
	const first = triggers( sounds, heard, "COS_P_CAT", 2, 1 );
	const second = triggers( sounds, heard, "COS_P_CAT", 2, 2 );
	assert.deepEqual( [ first, second ], [ [ 0 ], [ 1 ] ] );
	// A new catalog is a new rule table: its countdowns start again.
	sounds.catalog( [ rule( "COS_P_CAT", 2 ) ] );
	assert.deepEqual( triggers( sounds, heard, "COS_P_CAT", 1, 3 ), [ 0 ] );
});

test("a skip count outside the native int16 range rejects the catalog", () => {
	const sounds = createCharacterSounds( () => {} );
	for ( const skip of [ -1, 1.5, 0x8000 ] ) {
		assert.throws( () => sounds.catalog( [ rule( "COS_P_CAT", skip ) ] ), /Invalid sound rule/ );
	}
});

test("a positional trigger beyond 600 units is dropped before it counts", () => {
	const heard = [];
	let listener = [ 0, 0, 0 ];
	const sounds = createCharacterSounds( event => heard.push( event ), () => 0, () => listener );
	sounds.catalog( [ rule( "COS_P_CAT", 1 ) ] );
	// Far away: handled, silent, and the countdown is untouched.
	listener = [ 601, 0, 0 ];
	assert.equal( sounds.emit( "far", "COS_P_CAT", [ "SND_STAND" ], CONTEXT, AT, 0 ), true );
	assert.equal( heard.length, 0 );
	// Within reach the first trigger still plays, then one is skipped.
	listener = [ 600, 0, 0 ];
	assert.deepEqual( triggers( sounds, heard, "COS_P_CAT", 3 ), [ 0, 2 ] );
});

test("the volume column is clamped to 0..100 before it becomes a gain", () => {
	const heard = [], sounds = createCharacterSounds( event => heard.push( event ) );
	sounds.catalog( [ { ...rule( "LOUD", 0 ), volume: 140 }, { ...rule( "QUIET", 0 ), volume: -5 } ] );
	triggers( sounds, heard, "LOUD", 1 );
	triggers( sounds, heard, "QUIET", 1 );
	assert.deepEqual( heard.map( event => event.gain ), [ 1, 0 ] );
});
