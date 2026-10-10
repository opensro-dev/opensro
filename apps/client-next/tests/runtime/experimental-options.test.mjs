/*
===========================================================================

experimental-options.test.mjs - explicit opt-in and draft cancellation

Browser additions stay off until a valid saved preference or Confirm enables
them. Opening again discards changes that were never confirmed. Every video
stage defaults off, which is the native frame.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { experimentalOptions, experimentalVideo, renderScales } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { createExperimentalHud, EXPERIMENTAL_TABS } = await import(
	"../../src/engine/runtime/ui/hud/experimental-hud.ts"
);

const OFF = Object.freeze( {
	renderScale: 100,
	chatTimestamps: false,
	monsterGuide: false,
	developerDiagnostics: false,
	postProcessing: false,
	anisotropicFiltering: false,
	heightFog: false,
	dynamicSun: false,
	terrainRelief: false,
	texturedHorizon: false,
	floatBloom: false,
	hdrToneMap: false,
	sunShadow: false,
	perPixelLighting: false
} );

test("only an explicit boolean enables chat timestamps", () => {
	for ( const value of [ undefined, null, [], {}, true, { chatTimestamps: "true" }, { chatTimestamps: 1 } ] ) {
		assert.equal( experimentalOptions( value ).chatTimestamps, false );
	}
	assert.equal( experimentalOptions( { chatTimestamps: true, developerDiagnostics: false } ).chatTimestamps, true );
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

/*
================
Video stages
================
*/
test("every video stage defaults off and only an explicit true enables it", () => {
	assert.deepEqual( experimentalOptions(), OFF );
	assert.deepEqual( experimentalVideo( experimentalOptions() ), {
		renderScale: 100,
		postProcessing: false,
		anisotropicFiltering: false,
		heightFog: false,
		dynamicSun: false,
		terrainRelief: false,
		texturedHorizon: false,
		floatBloom: false,
		hdrToneMap: false,
		sunShadow: false,
		perPixelLighting: false
	} );
	for (
		const key of [
			"postProcessing",
			"anisotropicFiltering",
			"heightFog",
			"dynamicSun",
			"terrainRelief",
			"texturedHorizon",
			"floatBloom",
			"hdrToneMap",
			"sunShadow",
			"perPixelLighting"
		]
	) {
		assert.equal( experimentalOptions( { [key]: 1 } )[key], false );
		assert.equal( experimentalOptions( { [key]: "true" } )[key], false );
		assert.equal( experimentalOptions( { [key]: true } )[key], true );
		assert.equal( experimentalVideo( experimentalOptions( { [key]: true } ) )[key], true );
	}
});

test("new environment preferences persist only on Confirm and Default remains a draft", () => {
	for (
		const key of /** @type {const} */ ([
			"dynamicSun",
			"terrainRelief",
			"texturedHorizon",
			"floatBloom",
			"hdrToneMap",
			"sunShadow",
			"perPixelLighting"
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
		[ "Image", "World", "Lighting", "Chat", "Developer" ]
	);
	// Lighting keeps the direct-light stages; render scale joins Image without
	// changing the indices used to reopen the existing tabs.
	assert.deepEqual(
		EXPERIMENTAL_TABS[2].rows.map( row => row.key ),
		[ "sunShadow", "perPixelLighting" ]
	);
	for ( const tab of EXPERIMENTAL_TABS ) assert.ok( tab.rows.length <= 5, "At most five rows a tab" );
	const keys = EXPERIMENTAL_TABS.flatMap( tab => tab.rows.map( row => row.key ) ).sort();
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

/*
================
Render scale preferences
================
*/
test("render scale accepts only explicit numeric selections and reaches the renderer slice", () => {
	assert.deepEqual( renderScales(), [ 100, 75, 50 ] );
	for ( const renderScale of renderScales() ) {
		const saved = experimentalOptions( JSON.parse( JSON.stringify( { renderScale } ) ) );
		assert.equal( saved.renderScale, renderScale );
		assert.equal( experimentalVideo( saved ).renderScale, renderScale );
	}
	for ( const renderScale of [ undefined, null, true, false, "75", "50", 0, 25, 74.9, 101, NaN, Infinity, [], {} ] ) {
		assert.equal( experimentalOptions( { renderScale } ).renderScale, 100 );
	}
	for ( const legacy of [ undefined, null, [], {}, { chatTimestamps: true } ] ) {
		assert.equal( experimentalOptions( legacy ).renderScale, 100 );
	}
});

/*
================
Render scale restoration and reset
================
*/
test("restored render scale survives toggles and unconfirmed Default but confirmed Default restores native", () => {
	for ( const renderScale of renderScales() ) {
		const hud = createExperimentalHud();
		hud.restore( experimentalOptions( { renderScale } ) );
		hud.open();
		hud.toggle( "chatTimestamps" );
		assert.equal( hud.confirm().renderScale, renderScale );
		hud.reset();
		assert.equal( hud.state().draft.renderScale, 100 );
		assert.equal( hud.state().saved.renderScale, renderScale );
		hud.open();
		assert.equal( hud.state().draft.renderScale, renderScale );
		hud.reset();
		assert.equal( hud.confirm().renderScale, 100 );
	}
});

/*
================
Retired Video render scale
================
*/
test("legacy Video render scale is discarded without changing native Video records", async () => {
	const { defaultVideoOptions, videoOptions } = await import(
		"../../src/engine/foundation/rendering/video-options.ts"
	);
	const defaults = defaultVideoOptions();
	for ( const renderScale of [ 100, 75, 50, "50", null, -1 ] ) {
		const restored = videoOptions( { ...defaults, renderScale } );
		assert.deepEqual( restored, defaults );
		assert.equal( Object.hasOwn( restored, "renderScale" ), false );
	}
});

/*
================
Render scale draft selection
================
*/
test("render scale selections remain drafts until Confirm and reject unsupported values", () => {
	const hud = createExperimentalHud();
	for ( const renderScale of renderScales() ) {
		hud.open();
		hud.selectRenderScale( renderScale );
		assert.equal( hud.state().draft.renderScale, renderScale );
		assert.equal( hud.state().saved.renderScale, 100 );
		hud.open();
		assert.equal( hud.state().draft.renderScale, 100 );
		hud.selectRenderScale( renderScale );
		assert.equal( hud.confirm().renderScale, renderScale );
		hud.selectRenderScale( 25 );
		hud.selectRenderScale( NaN );
		hud.selectRenderScale( Infinity );
		assert.equal( hud.state().draft.renderScale, renderScale );
		hud.reset();
		hud.confirm();
	}
});
