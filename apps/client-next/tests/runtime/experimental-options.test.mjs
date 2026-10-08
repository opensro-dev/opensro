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

const { experimentalOptions, experimentalVideo } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { createExperimentalHud, EXPERIMENTAL_TABS } = await import(
	"../../src/engine/runtime/ui/hud/experimental-hud.ts"
);

const OFF = Object.freeze( {
	chatTimestamps: false,
	developerDiagnostics: false,
	postProcessing: false,
	anisotropicFiltering: false,
	heightFog: false,
	dynamicSun: false,
	terrainRelief: false,
	texturedHorizon: false,
	floatBloom: false
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
		postProcessing: false,
		anisotropicFiltering: false,
		heightFog: false,
		dynamicSun: false,
		terrainRelief: false,
		texturedHorizon: false,
		floatBloom: false
	} );
	for (
		const key of [
			"postProcessing",
			"anisotropicFiltering",
			"heightFog",
			"dynamicSun",
			"terrainRelief",
			"texturedHorizon",
			"floatBloom"
		]
	) {
		assert.equal( experimentalOptions( { [key]: 1 } )[key], false );
		assert.equal( experimentalOptions( { [key]: "true" } )[key], false );
		assert.equal( experimentalOptions( { [key]: true } )[key], true );
		assert.equal( experimentalVideo( experimentalOptions( { [key]: true } ) )[key], true );
	}
});

test("new environment preferences persist only on Confirm and Default remains a draft", () => {
	for ( const key of /** @type {const} */ ([ "dynamicSun", "terrainRelief", "texturedHorizon", "floatBloom" ]) ) {
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
	assert.deepEqual( EXPERIMENTAL_TABS.map( tab => tab.title ), [ "Image", "World", "Chat", "Developer" ] );
	const keys = EXPERIMENTAL_TABS.flatMap( tab => tab.rows.map( row => row.key ) ).sort();
	assert.deepEqual( keys, Object.keys( OFF ).sort() );
	const ids = EXPERIMENTAL_TABS.flatMap( tab => tab.rows.map( row => row.id ) );
	assert.equal( new Set( ids ).size, ids.length );
	const hud = createExperimentalHud();
	hud.selectTab( 3 );
	assert.equal( hud.state().tab, 3 );
	hud.open();
	assert.equal( hud.state().tab, 0 );
	assert.throws( () => hud.selectTab( EXPERIMENTAL_TABS.length ) );
	assert.throws( () => hud.selectTab( -1 ) );
});
