/*
===========================================================================

experimental-hud.test.mjs - the Experimental window: rows, drafts, tabs

The window side of the Experimental feature: drafts that only Confirm
persists, Default that only edits the draft, the tab census that covers
every preference once, and rows that list only while their set ships.
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
const { createExperimentalHud, experimentalTabs, EXPERIMENTAL_TABS } = await import(
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
	const keys = tabs => tabs.flatMap( tab => tab.rows.map( row => row.key ) );
	assert.ok( keys( experimentalTabs( world, true ) ).includes( "hdTextures" ), "the set is published" );
	assert.ok( keys( experimentalTabs( null, true ) ).includes( "hdTextures" ), "also outside the world list" );
	for ( const session of [ world, null, undefined ] ) {
		const tabs = experimentalTabs( session, false );
		assert.ok( !keys( tabs ).includes( "hdTextures" ), "no set, no row to toggle dead" );
		assert.ok( tabs.every( tab => tab.rows.length > 0 ), "the emptied HD tab goes with its row" );
		assert.ok( !tabs.some( tab => tab.title === "HD" ) );
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
		tab: 0
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
	const ids = /** @type {string[]} */ (EXPERIMENTAL_TABS.flatMap( tab => tab.rows ).map( row => row.key ));
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
			"ambientOcclusion"
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
test("the window's tabs cover every preference once and Open returns to Image", () => {
	assert.deepEqual(
		EXPERIMENTAL_TABS.map( tab => tab.title ),
		[ "Image", "World", "Lighting", "Game", "Notices", "Other", "Pets", "Pickup", "Effects", "HD", "Chat" ]
	);
	// Chat, appended last, holds the chat-log links: the notice links moved
	// from Notices, and the port-only item links (SRO_CHAT_ITEM_LINKS).
	assert.deepEqual( EXPERIMENTAL_TABS[10].rows.map( row => row.key ), [ "noticeLinks", "chatItemLinks" ] );
	// Lighting keeps the direct-light stages, the moving sun among them,
	// and holds the silk history row for room (#2269); render scale joins
	// Image without changing the indices used to reopen the existing tabs.
	assert.deepEqual(
		EXPERIMENTAL_TABS[2].rows.map( row => row.key ),
		[
			"sunShadow",
			"dynamicSun",
			"perPixelLighting",
			"enchantSparks",
			"sealUnderHeading",
			"dungeonCharacterPvs",
			"silkHistory"
		]
	);
	// Effects holds the port-only image effects - the sky detail and the
	// ambient occlusion (#2094), here for room; HD, appended last, holds
	// the HD texture set's row (#2061).
	assert.deepEqual( EXPERIMENTAL_TABS[8].rows.map( row => row.key ), [
		"skyDetail",
		"ambientOcclusion",
		"setBonusTooltip"
	] );
	assert.deepEqual( EXPERIMENTAL_TABS[9].rows.map( row => row.key ), [ "hdTextures" ] );
	for ( const tab of EXPERIMENTAL_TABS ) assert.ok( tab.rows.length <= 7, "At most seven rows a tab" );
	const keys = EXPERIMENTAL_TABS.flatMap( tab => tab.rows.map( row => row.key ) ).sort();
	// Every option has a row; the own-walk presenter has none (owner 2026-10-25).
	assert.deepEqual( keys, Object.keys( OFF ).sort() );
	const ids = EXPERIMENTAL_TABS.flatMap( tab => tab.rows.map( row => row.id ) );
	assert.equal( new Set( ids ).size, ids.length );
	const hud = createExperimentalHud();
	hud.selectTab( 4 );
	assert.equal( hud.state().tab, 4 );
	hud.open();
	assert.equal( hud.state().tab, 0 );
	assert.throws( () => hud.selectTab( EXPERIMENTAL_TABS.length ) );
	assert.throws( () => hud.selectTab( -1 ) );
});
