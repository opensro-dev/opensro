/*
===========================================================================

christmas-carols.test.mjs - the carols follow the Christmas event group

Port-only, not native (Experimental "Christmas carols", on by default):
while the row is on and the Christmas event group (1) is on, during a
snowfall, the music takes the carol mode; the fortress war's music keeps
its latch; the row off is the native region music. So-Ok's Christmas
Gift's town carols group (2006) plays them in the six towns (Jangan,
Donwhang, Hotan, Samarkand, Constantinople, Alexandria) only, the whole
window, and the region's own track everywhere else.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { eventMusicMode, regionMusicMode, TOWN_CAROL_MUSIC_MODE } = await import(
	"../../src/engine/foundation/gameplay/event-environment.ts"
);
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

test("the town carols group plays the carols in the towns only, snow or not", () => {
	const town = eventMusicMode( 0, { 2006: 1, 7: 1 }, true );
	assert.equal( town, TOWN_CAROL_MUSIC_MODE );
	assert.equal(
		eventMusicMode( 0, { 2006: 1, 7: 1, 1: 1 }, true ),
		TOWN_CAROL_MUSIC_MODE,
		"the snow does not spread them to the fields"
	);
	for ( const profile of [ "장안", "돈황", "호탄", "사마르칸트", "콘스탄티노플", "알렉산드리아" ] ) {
		assert.equal( regionMusicMode( town, profile ), 1, profile );
	}
	for ( const profile of [ "장안필드", "요새_장안", "돈황필드", "호탄필드", "중앙아시아", "동유럽", "델타지역" ] ) {
		assert.equal( regionMusicMode( town, profile ), 0, "outside the towns: " + profile + "'s own music" );
	}
	assert.equal( regionMusicMode( town, null ), 0 );
	assert.equal( eventMusicMode( 0, { 2006: 1 }, false ), 0, "the row off is the native region music" );
	assert.equal( eventMusicMode( 3, { 2006: 1 }, true ), 3, "the fortress war keeps its music" );
	assert.equal( regionMusicMode( 1, "장안필드" ), 1, "the winter event's carols keep playing everywhere" );
	assert.equal( regionMusicMode( 3, "장안" ), 3 );
});
