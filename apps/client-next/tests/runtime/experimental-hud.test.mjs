/*
===========================================================================

experimental-hud.test.mjs - the Experimental window: rows, drafts, tabs

The window side of the Experimental feature: drafts that only Confirm
persists, Default that only edits the draft, the tab census that covers
every preference once, rows that list only while their set ships, and
the list's scroll and drag.
The option-value semantics live in experimental-options.test.mjs.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { OFF } from "../helpers/experimental-off.mjs";

const experimentalModule = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { experimentalOptions, experimentalVideo } = experimentalModule;
const { createExperimentalHud, experimentalRows, experimentalTabs, EXPERIMENTAL_TABS } = await import(
	"../../src/engine/runtime/ui/hud/experimental-hud.ts"
);

test("safe loading defaults off and persists only after Confirm", () => {
	for ( const value of [ undefined, null, {}, { safeLoadingScreen: "true" }, { safeLoadingScreen: 1 } ] ) {
		assert.equal( experimentalOptions( value ).safeLoadingScreen, false );
	}
	assert.equal( experimentalOptions( { safeLoadingScreen: true } ).safeLoadingScreen, true );
	const hud = createExperimentalHud();
	hud.open();
	hud.toggle( "safeLoadingScreen" );
	assert.equal( hud.state().saved.safeLoadingScreen, false );
	hud.open();
	assert.equal( hud.state().draft.safeLoadingScreen, false, "closing discards the unconfirmed toggle" );
	hud.toggle( "safeLoadingScreen" );
	const saved = hud.confirm();
	const restored = createExperimentalHud();
	restored.restore( experimentalOptions( JSON.parse( JSON.stringify( saved ) ) ) );
	assert.equal( restored.state().saved.safeLoadingScreen, true );
	restored.reset();
	assert.equal( restored.state().saved.safeLoadingScreen, true, "Default only changes the draft" );
	assert.equal( restored.confirm().safeLoadingScreen, false );
});

test("the HD row is listed only while the published build carries the set (#2800)", () => {
	const world = /** @type {import("../../src/engine/contracts/session").SessionState} */ ({
		phase: "world",
		huntingGuide: { url: "http://localhost/hunting.json", bytes: 1 }
	});
	const keys = tabs => tabs.flatMap( tab => experimentalRows( tab ).map( row => row.key ) );
	assert.ok( keys( experimentalTabs( world, true ) ).includes( "hdTextures" ), "the set is published" );
	assert.ok( keys( experimentalTabs( null, true ) ).includes( "hdTextures" ), "also outside the world list" );
	for ( const session of [ world, null, undefined ] ) {
		const tabs = experimentalTabs( session, false );
		assert.ok( !keys( tabs ).includes( "hdTextures" ), "no set, no row to toggle dead" );
		assert.ok(
			tabs.every( tab => tab.sections.length > 0 && tab.sections.every( section => section.rows.length > 0 ) ),
			"no section or tab is left empty"
		);
	}
});

test("experimental drafts require Confirm and Default only changes the draft", () => {
	const hud = createExperimentalHud();
	assert.equal( hud.state().saved.chatTimestamps, false );
	hud.open();
	hud.toggle( "chatTimestamps" );
	assert.equal( hud.state().draft.chatTimestamps, true );
	assert.equal( hud.state().saved.chatTimestamps, false );
	hud.open();
	assert.equal( hud.state().draft.chatTimestamps, false );
	hud.toggle( "chatTimestamps" );
	assert.deepEqual( hud.confirm(), { ...OFF, chatTimestamps: true } );
	hud.reset();
	assert.equal( hud.state().draft.chatTimestamps, false );
	assert.equal( hud.state().saved.chatTimestamps, true );
	hud.open();
	assert.equal( hud.state().draft.chatTimestamps, true );
	hud.reset();
	assert.deepEqual( hud.confirm(), OFF );
	hud.restore( { ...OFF, chatTimestamps: true } );
	assert.deepEqual( hud.state(), {
		saved: { ...OFF, chatTimestamps: true },
		draft: { ...OFF, chatTimestamps: true },
		tab: 0,
		scroll: 0
	} );
});

/*
================
Developer diagnostics opt-in
================
*/
test("diagnostics default off and follow the same draft lifecycle", () => {
	for ( const value of [ undefined, null, [], {}, { developerDiagnostics: "true" }, { developerDiagnostics: 1 } ] ) {
		assert.equal( experimentalOptions( value ).developerDiagnostics, false );
	}
	const hud = createExperimentalHud();
	hud.toggle( "developerDiagnostics" );
	assert.equal( hud.state().draft.developerDiagnostics, true );
	assert.equal( hud.state().saved.developerDiagnostics, false );
	hud.open();
	assert.equal( hud.state().draft.developerDiagnostics, false );
	hud.toggle( "developerDiagnostics" );
	assert.equal( hud.confirm().developerDiagnostics, true );
	hud.reset();
	assert.equal( hud.state().saved.developerDiagnostics, true );
	hud.open();
	assert.equal( hud.state().draft.developerDiagnostics, true );
	hud.reset();
	assert.equal( hud.confirm().developerDiagnostics, false );
});

test("a saved preference holding the retired relief and horizon rows loads cleanly and ignores them", () => {
	// Both rows were removed at the owner's request (2026-10-11): a browser
	// that saved them on still loads, the keys are dropped, and the other
	// stages keep their saved values.
	const saved = { terrainRelief: true, texturedHorizon: true, heightFog: true, renderScale: 75 };
	const options = experimentalOptions( saved );
	assert.deepEqual( options, { ...OFF, heightFog: true, renderScale: 75 } );
	assert.equal( "terrainRelief" in options, false );
	assert.equal( "texturedHorizon" in experimentalVideo( options ), false );
	const ids = /** @type {string[]} */ (EXPERIMENTAL_TABS.flatMap( experimentalRows ).map( row => row.key ));
	assert.ok( !ids.includes( "terrainRelief" ) && !ids.includes( "texturedHorizon" ) );
});

test("new environment preferences persist only on Confirm and Default remains a draft", () => {
	for (
		const key of /** @type {const} */ ([
			"dynamicSun",
			"floatBloom",
			"hdrToneMap",
			"sunShadow",
			"perPixelLighting",
			"ambientOcclusion",
			"waterRefraction"
		])
	) {
		const hud = createExperimentalHud();
		hud.open();
		hud.toggle( key );
		assert.deepEqual( hud.state().saved, OFF );
		const saved = hud.confirm();
		assert.deepEqual( saved, { ...OFF, [key]: true } );
		const restored = createExperimentalHud();
		restored.restore( experimentalOptions( JSON.parse( JSON.stringify( saved ) ) ) );
		restored.open();
		assert.equal( restored.state().draft[key], true );
		restored.reset();
		assert.deepEqual( restored.state().draft, OFF );
		assert.equal( restored.state().saved[key], true );
		restored.open();
		assert.equal( restored.state().draft[key], true, "Unconfirmed Default must be discarded" );
		restored.reset();
		assert.deepEqual( restored.confirm(), OFF );
		restored.open();
		assert.deepEqual( restored.state().draft, OFF );
	}
});

/*
================
Tabs
================
*/
test("six tabs group every preference once, in the spec's sections", () => {
	assert.deepEqual( EXPERIMENTAL_TABS.map( tab => tab.title ), [
		"Graphics",
		"Interface",
		"Game",
		"Map",
		"Notices",
		"Pets"
	] );
	assert.deepEqual( EXPERIMENTAL_TABS[0].sections.map( section => section.title ), [
		"Image",
		"Light and sky",
		"Textures and effects"
	] );
	// The 4x multisampled scene sits beside the post-process anti-aliasing.
	const image = EXPERIMENTAL_TABS[0].sections[0].rows.map( row => row.key );
	assert.equal( image.indexOf( "msaa" ), image.indexOf( "postProcessing" ) + 1 );
	// Scene FXAA, the cheap edge smoothing, follows MSAA.
	assert.equal( image.indexOf( "sceneFxaa" ), image.indexOf( "msaa" ) + 1 );
	assert.deepEqual( EXPERIMENTAL_TABS[5].sections[1].rows.map( row => row.key ), [
		"smartPetPickup",
		"petPickupFilter",
		"petFilterGold",
		"petFilterEquipment",
		"petFilterAlchemy",
		"petFilterPotions",
		"petFilterOther"
	] );
	const keys = EXPERIMENTAL_TABS.flatMap( tab => experimentalRows( tab ).map( row => row.key ) );
	// Every option has a row; the own-walk presenter has none (owner 2026-10-25).
	// 64: the Entire-map level checkbox (#3258) left when the v1.188
	// window's own foot checkbox became the map's native switch.
	assert.equal( keys.length, 64 );
	assert.deepEqual( [ ...keys ].sort(), Object.keys( OFF ).sort() );
	const kinds = EXPERIMENTAL_TABS.flatMap( experimentalRows ).filter( row => row.parent );
	assert.deepEqual(
		kinds.map( row => [ row.key, row.parent ] ),
		[ "petFilterGold", "petFilterEquipment", "petFilterAlchemy", "petFilterPotions", "petFilterOther" ].map(
			key => [
				key,
				"petPickupFilter"
			]
		)
	);
	const ids = EXPERIMENTAL_TABS.flatMap( tab => experimentalRows( tab ).map( row => row.id ) );
	assert.equal( new Set( ids ).size, ids.length );
	const hud = createExperimentalHud();
	hud.selectTab( 4 );
	assert.equal( hud.state().tab, 4 );
	hud.open();
	assert.equal( hud.state().tab, 0 );
	assert.throws( () => hud.selectTab( EXPERIMENTAL_TABS.length ) );
	assert.throws( () => hud.selectTab( -1 ) );
});

test("a drag moves whole rows past half a row and keeps the rest", () => {
	const hud = createExperimentalHud();
	hud.scroll( 2, 5 );
	// A 1-pixel jitter toward the top leaves the list where it is.
	hud.drag( -1, 46, 5 );
	assert.equal( hud.state().scroll, 2 );
	hud.drag( 30, 46, 5 );
	assert.equal( hud.state().scroll, 3 );
	hud.drag( -30, 46, 5 );
	assert.equal( hud.state().scroll, 2 );
	assert.equal( hud.endDrag(), false, "it moved rows: not a click" );
});

test("a gesture under the click slop is a click and moves nothing", () => {
	const hud = createExperimentalHud();
	hud.drag( 3, 46, 5 );
	hud.drag( -2, 46, 5 );
	assert.equal( hud.endDrag(), true );
	assert.equal( hud.state().scroll, 0 );
	hud.drag( 20, 46, 5 );
	assert.equal( hud.endDrag(), false, "past the slop it is a drag, even without a row step" );
});

test("gating drops the row, then an emptied section", () => {
	// No world session: no hunting guide, so its row goes and World map
	// (M), its only row gone, goes with it; no HD set.
	const map = experimentalTabs( null, false ).find( tab => tab.title === "Map" );
	assert.ok( !map?.sections.some( section => section.title === "World map (M)" ) );
	const world = /** @type {any} */ ({ phase: "world", huntingGuide: { url: "fixture", bytes: 1 } });
	assert.ok(
		experimentalTabs( world, false ).find( tab => tab.title === "Map" )?.sections.some(
			section => section.title === "World map (M)"
		),
		"with a session the guide's row keeps the section"
	);
	assert.ok( !experimentalRows( experimentalTabs( null, false )[0] ).some( row => row.key === "hdTextures" ) );
});

test("scroll clamps to its range and resets on tab change and open", () => {
	const hud = createExperimentalHud();
	hud.scroll( 5, 3 );
	assert.equal( hud.state().scroll, 3 );
	hud.scroll( -10, 3 );
	assert.equal( hud.state().scroll, 0 );
	hud.scroll( 1.5, 3 );
	hud.selectTab( 2 );
	assert.equal( hud.state().scroll, 0 );
	hud.scroll( 2, 3 );
	hud.open();
	assert.equal( hud.state().scroll, 0 );
});
