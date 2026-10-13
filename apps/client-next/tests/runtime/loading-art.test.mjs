/*
===========================================================================

loading-art.test.mjs - only the loading art that can show next stays decoded

All nine loading images were in every frame's demand: 18 MiB decoded for
screens that could not appear. loadingArtDemand keeps the chrome, the
europe variant the next load draws, the rebirth art while dead and the
character-select art outside the world or after a leave request.
loadingArtHeld is the backstop: a screen whose art is not decoded yet holds
the retained world frame instead of drawing an empty background.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { loadingArtDemand, loadingArtHeld, LOADING_ART_HOLD_MS, travelLoadingBackground } = await import(
	"../../src/engine/foundation/ui/mission-loading.ts"
);

const ROOT = "/assets/images/Media_extracted/interface/loading/";
const CHROME = [ "loading_form", "gauge_loading", "nowloading" ];
const names = paths => paths.map( path => path.slice( ROOT.length, -".png".length ) ).sort();
const ALIVE = { inWorld: true, dead: false, leaving: false, nextVariant: 1 };

test("alive in the world, only the chrome and the next variant stay decoded", () => {
	assert.deepEqual( names( loadingArtDemand( ALIVE ) ), [ ...CHROME, "loading_europe_1" ].sort() );
	assert.deepEqual(
		names( loadingArtDemand( { ...ALIVE, nextVariant: 2 } ) ),
		[ ...CHROME, "loading_europe_2" ].sort()
	);
});

test("death warms the rebirth art before its teleport", () => {
	assert.ok( names( loadingArtDemand( { ...ALIVE, dead: true } ) ).includes( "loading_rebirth" ) );
	assert.ok( !names( loadingArtDemand( ALIVE ) ).includes( "loading_rebirth" ) );
});

test("a leave request or the frontend warms the character-select art", () => {
	for ( const state of [ { ...ALIVE, leaving: true }, { ...ALIVE, inWorld: false } ] ) {
		const demanded = names( loadingArtDemand( state ) );
		assert.ok(
			demanded.includes( "loading_charactercustom" ) && demanded.includes( "loading_charactercustom_europe" )
		);
	}
	assert.ok( !names( loadingArtDemand( ALIVE ) ).includes( "loading_charactercustom_europe" ) );
});

test("art no lead time foresaw waits instead of drawing empty", () => {
	// Travel mode 4 (never set today) and a destination region are demanded on use.
	const thief = travelLoadingBackground( { mode: 4, region: 0 }, 1 );
	assert.equal( thief, ROOT + "loading_thief2.png" );
	assert.ok( !loadingArtDemand( ALIVE ).includes( thief ) );
	const waiting = { background: thief, startup: false, resident: false, worldShown: true, heldForMs: 0 };
	assert.equal( loadingArtHeld( waiting ), true, "held while its art decodes" );
	assert.equal( loadingArtHeld( { ...waiting, resident: true } ), false, "shown once decoded" );
	assert.equal(
		loadingArtHeld( { ...waiting, heldForMs: LOADING_ART_HOLD_MS } ),
		false,
		"never frozen by art that never comes"
	);
});

test("only a retained world frame is held", () => {
	const waiting = {
		background: ROOT + "loading_rebirth.png",
		startup: false,
		resident: false,
		worldShown: true,
		heldForMs: 0
	};
	assert.equal( loadingArtHeld( { ...waiting, worldShown: false } ), false, "the title or dock draws as before" );
	assert.equal( loadingArtHeld( { ...waiting, startup: true } ), false, "the startup title draws as before" );
	assert.equal( loadingArtHeld( { ...waiting, background: null } ), false, "a screen with no art draws as before" );
});
