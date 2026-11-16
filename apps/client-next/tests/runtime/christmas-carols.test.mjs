/*
===========================================================================

christmas-carols.test.mjs - the carols follow the Christmas event group

Port-only, not native (Experimental "Christmas carols", on by default):
while the row is on and the Christmas event group (1) is on, during a
snowfall, the music takes the carol mode; the fortress war's music keeps
its latch; the row off is the native region music.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { eventMusicMode } = await import( "../../src/engine/foundation/gameplay/event-environment.ts" );
const { createMusicSelection } = await import( "../../src/engine/foundation/audio/music.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

test("the carols row is on by default and only an explicit false turns it off", () => {
	assert.equal( experimentalOptions().christmasCarols, true );
	assert.equal( experimentalOptions( { christmasCarols: "false" } ).christmasCarols, true );
	assert.equal( experimentalOptions( { christmasCarols: false } ).christmasCarols, false );
});

test("the carols play only with the row on and the Christmas group on", () => {
	assert.equal( eventMusicMode( 0, { 1: 1 }, true ), 1 );
	assert.equal( eventMusicMode( 0, { 1: 1 }, false ), 0, "the row off is the native region music" );
	assert.equal( eventMusicMode( 0, { 1: 0 }, true ), 0, "the event over" );
	assert.equal(
		eventMusicMode( 0, { 5: 1, 7: 1 }, true ),
		0,
		"Halloween and the decoration groups are not Christmas"
	);
	assert.equal( eventMusicMode( 3, { 1: 1 }, true ), 3, "the fortress war keeps its music" );
});

test("the carol mode cycles the four native carols", () => {
	const select = createMusicSelection();
	const played = [];
	for ( let i = 0; i < 5; i++ ) played.push( select( 1, "/assets/audio/music/jangan.mp3", false, "" )?.path );
	assert.deepEqual( played, [
		"/assets/audio/music/event_carol_01.mp3",
		"/assets/audio/music/event_carol_02.mp3",
		"/assets/audio/music/event_carol_03.mp3",
		"/assets/audio/music/event_carol_04.mp3",
		"/assets/audio/music/event_carol_01.mp3"
	] );
});
