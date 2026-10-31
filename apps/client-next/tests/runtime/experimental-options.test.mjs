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

const experimentalModule = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const {
	experimentalGameplay,
	experimentalOptions,
	experimentalVideo,
	phoneDisplay,
	PHONE_SCENE_PIXEL_BUDGET,
	renderScales,
	sceneRenderScale,
	uiScaleFactor
} = experimentalModule;
const { createExperimentalHud, EXPERIMENTAL_TABS } = await import(
	"../../src/engine/runtime/ui/hud/experimental-hud.ts"
);

const OFF = Object.freeze( {
	renderScale: 100,
	// On by default (owner 2026-10-27, #1806): a phone caps the scene's pixels;
	// every other display draws at full resolution.
	phoneRenderCap: true,
	// Native 1:1 interface size.
	uiScale: 100,
	chatTimestamps: false,
	// On by default (owner 2026-10-28, #1976): event notices get their
	// own chat tab; an explicit false restores the native four tabs.
	eventChatTab: true,
	safeLoadingScreen: false,
	monsterGuide: false,
	developerDiagnostics: false,
	postProcessing: false,
	anisotropicFiltering: false,
	heightFog: false,
	dynamicSun: false,
	floatBloom: false,
	hdrToneMap: false,
	sunShadow: false,
	perPixelLighting: false,
	sunRays: false,
	// On by default (owner 2026-10-25): a press walks at once and stops
	// where the server casts.
	predictedRunUp: true,
	// On by default (owner 2026-10-15): the camera eases its height and release.
	smoothCamera: true,
	// On by default (owner 2026-10-17): +8 and higher weapons and shields wear enchant sparks.
	enchantSparks: true,
	// On by default (owner 2026-10-14); it changes only devices without BC.
	compressedTextures: true,
	// On by default (owner 2026-10-14): near effects outrank far characters.
	sharedActorRoom: true,
	// On by default (owner 2026-10-25): the own walk is stepped on the main thread.
	mainThreadOwnWalk: true,
	// On by default (owner 2026-10-15): a disabled tab is dimmed.
	greyDisabledTabs: true,
	// On by default (owner 2026-10-15): the shard's job-kill notices show.
	jobKillNotices: true,
	// On by default (owner 2026-10-15): the shard's drop and plus notices show.
	dropPlusNotices: true,
	// On by default (owner 2026-10-19): the shard's +7 notices show.
	plusSevenNotices: true,
	// On by default: the shard's item break notices show (#1738).
	breakNotices: true,
	// Off by default: each player opts in to the unique damage ranking.
	uniqueDamageRank: false,
	// Off by default: V hides only monsters and players, as the original does.
	vHidesCompanions: false,
	// On by default (port-only, not native): hides characters in dungeon blocks the active block does not draw.
	dungeonCharacterPvs: true,
	// On by default (owner 2026-10-15): labels and lists stay inside their window.
	fitWindowLabels: true,
	// On by default (owner 2026-10-15): a drop on a bag tab moves the item there.
	dropOnBagTab: true,
	// On by default (owner 2026-10-24): click shortcuts stage into an exchange.
	exchangeClickShortcuts: true,
	// On by default (owner 2026-10-15): a notice's player and item are links.
	noticeLinks: true,
	// On by default (owner 2026-10-15): the notice banner shows for 5 s.
	shortNotices: true,
	attackPetTraining: false,
	// On by default (owner 2026-10-15): Withdraw asks before a guild ban.
	confirmGuildBan: true,
	// Off by default: straight projectiles keep their launch-captured endpoint (8D8580).
	homingProjectiles: false,
	// Off by default: the automatic potion watches only the player (70C550).
	transportAutoPotion: false,
	// On by default (owner 2026-10-18, BUG-086): clicking CIFPetMiniInfo opens CIFCos;
	// off restores the native no-op click (6B3760).
	petClickOpensWindow: true,
	// On by default (owner 2026-10-20): keep bit 0x80 enabled on 0xB4/0xB5 so the
	// pet keeps picking up gold when full; off restores the native 0x0077DD61 disable.
	smartPetPickup: true,
	// Off by default (#841): the pickup pet proposes the native categories
	// until the item filter is turned on.
	petPickupFilter: false,
	// The filter's kinds include everything unless one is explicitly off;
	// they are read only while the filter itself is on.
	petFilterGold: true,
	petFilterEquipment: true,
	petFilterAlchemy: true,
	petFilterPotions: true,
	petFilterOther: true,
	// On by default (owner 2026-10-22, #1243): a touch tap keeps an item or skill tooltip up.
	touchInspect: true,
	// On by default (#1451 step 3): WebTransport is the
	// default connect and the WebSocket the fallback; off is one setting.
	webTransport: true,
	// Off by default: the M window shows only the five native towns and 28 x 8 overview.
	expandedWorldMap: false,
	// Off by default (#840): the maps mark the tracked quest's targets only.
	questTrackerMarkers: false,
	// Off by default (#1640): only the native plate and no ground ring.
	sunDropMarker: false
} );

test("touch inspection defaults on and only an explicit false turns it off", () => {
	for (
		const value of [ undefined, null, {}, { touchInspect: true }, { touchInspect: "false" }, { touchInspect: 0 } ]
	) {
		assert.equal( experimentalOptions( value ).touchInspect, true, JSON.stringify( value ) );
	}
	assert.equal( experimentalOptions( { touchInspect: false } ).touchInspect, false );
});

test("only an explicit boolean enables chat timestamps", () => {
	for ( const value of [ undefined, null, [], {}, true, { chatTimestamps: "true" }, { chatTimestamps: 1 } ] ) {
		assert.equal( experimentalOptions( value ).chatTimestamps, false );
	}
	assert.equal( experimentalOptions( { chatTimestamps: true, developerDiagnostics: false } ).chatTimestamps, true );
});

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
		phoneRenderCap: true,
		postProcessing: false,
		anisotropicFiltering: false,
		heightFog: false,
		dynamicSun: false,
		floatBloom: false,
		hdrToneMap: false,
		sunShadow: false,
		perPixelLighting: false,
		sunRays: false,
		// Smooth camera is on by default (owner 2026-10-15).
		smoothCamera: true,
		// Enchant sparks are on by default (owner 2026-10-17).
		enchantSparks: true,
		// The shared actor room is on by default (owner 2026-10-14).
		sharedActorRoom: true,
		vHidesCompanions: false,
		dungeonCharacterPvs: true
	} );
	for (
		const key of [
			"postProcessing",
			"anisotropicFiltering",
			"heightFog",
			"dynamicSun",
			"floatBloom",
			"hdrToneMap",
			"sunShadow",
			"perPixelLighting",
			"sunRays"
		]
	) {
		assert.equal( experimentalOptions( { [key]: 1 } )[key], false );
		assert.equal( experimentalOptions( { [key]: "true" } )[key], false );
		assert.equal( experimentalOptions( { [key]: true } )[key], true );
		assert.equal( experimentalVideo( experimentalOptions( { [key]: true } ) )[key], true );
	}
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
			"sunRays"
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
		[ "Image", "World", "Lighting", "Game", "Notices", "Other", "Pets", "Pickup" ]
	);
	// Lighting keeps the direct-light stages; render scale joins Image without
	// changing the indices used to reopen the existing tabs.
	assert.deepEqual(
		EXPERIMENTAL_TABS[2].rows.map( row => row.key ),
		[ "sunShadow", "perPixelLighting", "sunRays", "enchantSparks", "dungeonCharacterPvs" ]
	);
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
Phone resolution (#1806)
================
*/
// The report's iPhone: a 2250 x 1206 backing store at device pixel ratio 3.
const IPHONE_VIEWPORT = { width: 2250, height: 1206 };

test("a phone is a coarse pointer on a short screen; touch laptops and tablets are not", () => {
	assert.equal( phoneDisplay( { coarsePointer: true, shortSide: 402 } ), true );
	assert.equal( phoneDisplay( { coarsePointer: true, shortSide: 360 } ), true );
	assert.equal( phoneDisplay( { coarsePointer: true, shortSide: 768 } ), false, "tablet" );
	assert.equal( phoneDisplay( { coarsePointer: false, shortSide: 402 } ), false, "fine pointer" );
	assert.equal( phoneDisplay( { coarsePointer: true, shortSide: 0 } ), false, "no screen reported" );
});

test("phone resolution defaults on and only an explicit false turns it off", () => {
	for ( const value of [ undefined, null, {}, { phoneRenderCap: "false" }, { phoneRenderCap: 0 } ] ) {
		assert.equal( experimentalOptions( value ).phoneRenderCap, true, JSON.stringify( value ) );
	}
	const off = experimentalOptions( JSON.parse( JSON.stringify( { phoneRenderCap: false } ) ) );
	assert.equal( off.phoneRenderCap, false );
	assert.equal( experimentalVideo( off ).phoneRenderCap, false );
});

test("on a phone the cap picks the largest scale inside the pixel budget", () => {
	const on = experimentalVideo( experimentalOptions() );
	assert.equal( sceneRenderScale( on, IPHONE_VIEWPORT, true ), 50 );
	// A smaller backing store keeps more of its resolution.
	assert.equal( sceneRenderScale( on, { width: 1280, height: 720 }, true ), 100 );
	assert.equal( sceneRenderScale( on, { width: 1600, height: 900 }, true ), 75 );
	// Past the budget even at the smallest scale, the smallest scale stays.
	assert.equal( sceneRenderScale( on, { width: 4000, height: 3000 }, true ), 50 );
	for ( const viewport of [ IPHONE_VIEWPORT, { width: 1600, height: 900 } ] ) {
		const scale = sceneRenderScale( on, viewport, true );
		assert.ok( viewport.width * viewport.height * scale * scale / 10000 <= PHONE_SCENE_PIXEL_BUDGET );
	}
});

test("the phone cap leaves desktops, the off switch and a picked scale alone", () => {
	const on = experimentalVideo( experimentalOptions() );
	// Not a phone: the native full resolution.
	assert.equal( sceneRenderScale( on, IPHONE_VIEWPORT, false ), 100 );
	// Switched off on a phone: full resolution.
	const off = experimentalVideo( experimentalOptions( { phoneRenderCap: false } ) );
	assert.equal( sceneRenderScale( off, IPHONE_VIEWPORT, true ), 100 );
	// A scale the player picked below 100 wins, also above the cap's choice.
	const picked = experimentalVideo( experimentalOptions( { renderScale: 75 } ) );
	assert.equal( sceneRenderScale( picked, IPHONE_VIEWPORT, true ), 75 );
	assert.equal( sceneRenderScale( picked, IPHONE_VIEWPORT, false ), 75 );
});

/*
================
UI size preferences
================
*/
test("UI size defaults to the native 100 and only an in-range stop restores", () => {
	assert.equal( experimentalOptions().uiScale, 100 );
	for ( const uiScale of [ undefined, null, true, false, "150", 0, 49, 201, 999, NaN, Infinity, [], {} ] ) {
		assert.equal( experimentalOptions( { uiScale } ).uiScale, 100 );
	}
	for ( const uiScale of [ 50, 55, 75, 100, 125, 150, 200 ] ) {
		const saved = experimentalOptions( JSON.parse( JSON.stringify( { uiScale } ) ) );
		assert.equal( saved.uiScale, uiScale );
	}
	// A stored value between stops snaps to the nearest one.
	assert.equal( experimentalOptions( { uiScale: 137 } ).uiScale, 135 );
	assert.equal( experimentalOptions( { uiScale: 138 } ).uiScale, 140 );
	// The platform factor is exactly 1 at the native size.
	assert.equal( uiScaleFactor( experimentalOptions() ), 1 );
	assert.equal( uiScaleFactor( experimentalOptions( { uiScale: 200 } ) ), 2 );
	assert.equal( uiScaleFactor( experimentalOptions( { uiScale: 50 } ) ), 0.5 );
	assert.equal( "uiScale" in experimentalVideo( experimentalOptions( { uiScale: 150 } ) ), false );
	assert.equal( "uiScale" in experimentalGameplay( experimentalOptions( { uiScale: 150 } ) ), false );
});

/*
================
UI size draft selection
================
*/
test("UI size selections remain drafts until Confirm and reject values off the slider", () => {
	const hud = createExperimentalHud();
	hud.restore( experimentalOptions( { uiScale: 150 } ) );
	hud.open();
	assert.equal( hud.state().draft.uiScale, 150 );
	hud.setUiScale( 75 );
	assert.equal( hud.state().draft.uiScale, 75 );
	assert.equal( hud.state().saved.uiScale, 150 );
	hud.open();
	assert.equal( hud.state().draft.uiScale, 150 );
	hud.setUiScale( 999 );
	hud.setUiScale( 25 );
	hud.setUiScale( NaN );
	assert.equal( hud.state().draft.uiScale, 150 );
	hud.setUiScale( 200 );
	assert.equal( hud.confirm().uiScale, 200 );
	hud.reset();
	assert.equal( hud.state().draft.uiScale, 100 );
	assert.equal( hud.state().saved.uiScale, 200 );
	assert.equal( hud.confirm().uiScale, 100 );
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

/*
================
Gameplay rows
================
*/
test("the predicted run-up defaults on and reaches only the worker's slice", () => {
	// Owner 2026-10-25: on by default. The native press (6FCD50) never
	// walks before the server answers, so the row remains the off switch.
	assert.deepEqual( experimentalGameplay( experimentalOptions() ), {
		predictedRunUp: true,
		webTransport: true,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true,
		homingProjectiles: false,
		smartPetPickup: true,
		sunDropMarker: false,
		petPickupKinds: undefined
	} );
	for ( const value of [ 1, "true", null, {} ] ) {
		assert.equal( experimentalOptions( { predictedRunUp: value } ).predictedRunUp, true );
	}
	assert.equal( experimentalOptions( { predictedRunUp: false } ).predictedRunUp, false );
	const on = experimentalOptions( { predictedRunUp: true } );
	assert.deepEqual( experimentalGameplay( on ), {
		predictedRunUp: true,
		webTransport: true,
		jobKillNotices: true,
		dropPlusNotices: true,
		plusSevenNotices: true,
		breakNotices: true,
		homingProjectiles: false,
		smartPetPickup: true,
		sunDropMarker: false,
		petPickupKinds: undefined
	} );
	assert.equal( "predictedRunUp" in experimentalVideo( on ), false );
});

test("the WebTransport connection defaults on and one setting turns it off", () => {
	// #1451 step 3: WebTransport is the default connect; the WebSocket
	// stays the connect-time fallback, and the row remains the switch
	// back. Like the other default-on rows, only an explicit false
	// turns it off.
	assert.equal( experimentalOptions().webTransport, true );
	assert.equal( experimentalGameplay( experimentalOptions() ).webTransport, true );
	for ( const value of [ 1, "true", null, {}, "false", 0 ] ) {
		assert.equal( experimentalOptions( { webTransport: value } ).webTransport, true, JSON.stringify( value ) );
	}
	const off = experimentalOptions( { webTransport: false } );
	assert.equal( off.webTransport, false );
	assert.equal( experimentalGameplay( off ).webTransport, false );
});

test("the pickup pet item filter is off by default and only narrows inside itself", () => {
	// ground-item's PICKUP_KIND bits: gold 1, equipment 2, alchemy 4,
	// potions 8, other 16; the mask reaches the worker only while the
	// filter row is on, so off is the native loop.
	for ( const value of [ undefined, null, {}, { petPickupFilter: "true" }, { petPickupFilter: 1 } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.petPickupFilter, false, JSON.stringify( value ) );
		assert.equal( experimentalGameplay( options ).petPickupKinds, undefined );
	}
	const on = experimentalOptions( { petPickupFilter: true } );
	assert.equal( experimentalGameplay( on ).petPickupKinds, 31 );
	// Kinds narrow only on an explicit false, never on a stored non-boolean.
	for ( const value of [ "false", 0, null ] ) {
		assert.equal(
			experimentalGameplay( experimentalOptions( { petPickupFilter: true, petFilterGold: value } ) )
				.petPickupKinds,
			31,
			JSON.stringify( value )
		);
	}
	assert.equal(
		experimentalGameplay( experimentalOptions( { petPickupFilter: true, petFilterGold: false } ) )
			.petPickupKinds,
		30
	);
	assert.equal(
		experimentalGameplay( experimentalOptions( {
			petPickupFilter: true,
			petFilterGold: false,
			petFilterEquipment: false,
			petFilterAlchemy: false,
			petFilterPotions: false,
			petFilterOther: false
		} ) ).petPickupKinds,
		0
	);
	// Kind switches stored without the master row still change nothing.
	assert.equal(
		experimentalGameplay( experimentalOptions( { petFilterGold: false } ) ).petPickupKinds,
		undefined
	);
});

test("compressed textures are on unless explicitly false, and the stored reader parses the saved key", () => {
	const { storedExperimentalOptions, EXPERIMENTAL_STORAGE_KEY } = experimentalModule;
	for ( const value of [ undefined, null, {}, { compressedTextures: 1 }, { compressedTextures: "false" } ] ) {
		assert.equal( experimentalOptions( value ).compressedTextures, true );
	}
	assert.equal( experimentalOptions( { compressedTextures: false } ).compressedTextures, false );
	const saved = new Map( [ [ EXPERIMENTAL_STORAGE_KEY, JSON.stringify( { compressedTextures: false } ) ] ] );
	const storage = { getItem: key => saved.get( key ) ?? null };
	assert.equal( storedExperimentalOptions( storage ).options.compressedTextures, false );
	assert.equal( storedExperimentalOptions( { getItem: () => null } ).options.compressedTextures, true );
	assert.equal( storedExperimentalOptions( undefined ).options.compressedTextures, true );
	assert.throws( () => storedExperimentalOptions( { getItem: () => "{" } ) );
});

test("a profile saved before the flip loads with WebTransport on; a choice under the current key is kept", () => {
	const { storedExperimentalOptions, EXPERIMENTAL_STORAGE_KEY, PREVIOUS_EXPERIMENTAL_STORAGE_KEY } =
		experimentalModule;
	const storageOf = entries => {
		const saved = new Map( entries );
		return { getItem: key => saved.get( key ) ?? null };
	};
	// Saved before the flip (platform.ts wrote the whole options object,
	// so a Confirm under the old default stored webTransport: false):
	// the flip reaches it, and its other choices carry over.
	const before = storedExperimentalOptions( storageOf( [
		[ PREVIOUS_EXPERIMENTAL_STORAGE_KEY, JSON.stringify( { webTransport: false, chatTimestamps: true } ) ]
	] ) );
	assert.equal( before.options.webTransport, true );
	assert.equal( before.options.chatTimestamps, true );
	assert.ok( before.raw && typeof before.raw === "object" && !("webTransport" in before.raw) );
	// Stored under the current key after the flip: an explicit off stays off.
	const after = storedExperimentalOptions( storageOf( [
		[ PREVIOUS_EXPERIMENTAL_STORAGE_KEY, JSON.stringify( { chatTimestamps: true } ) ],
		[ EXPERIMENTAL_STORAGE_KEY, JSON.stringify( { webTransport: false } ) ]
	] ) );
	assert.equal( after.options.webTransport, false );
	// The current key wins even when the previous one is also present.
	const both = storedExperimentalOptions( storageOf( [
		[ PREVIOUS_EXPERIMENTAL_STORAGE_KEY, JSON.stringify( { webTransport: false, chatTimestamps: false } ) ],
		[ EXPERIMENTAL_STORAGE_KEY, JSON.stringify( { webTransport: false, chatTimestamps: true } ) ]
	] ) );
	assert.equal( both.options.webTransport, false );
	assert.equal( both.options.chatTimestamps, true );
});

test("the shared actor room is on unless explicitly false, and reaches the renderer's video options", () => {
	for ( const value of [ undefined, null, {}, { sharedActorRoom: 1 }, { sharedActorRoom: "false" } ] ) {
		assert.equal( experimentalOptions( value ).sharedActorRoom, true );
	}
	const off = experimentalOptions( { sharedActorRoom: false } );
	assert.equal( off.sharedActorRoom, false );
	assert.equal( experimentalVideo( off ).sharedActorRoom, false );
	assert.equal( experimentalVideo( experimentalOptions( {} ) ).sharedActorRoom, true );
});

test("grey disabled tabs is on unless explicitly false and stays out of the renderer's video options", () => {
	for ( const value of [ undefined, null, {}, { greyDisabledTabs: 1 }, { greyDisabledTabs: "false" } ] ) {
		assert.equal( experimentalOptions( value ).greyDisabledTabs, true );
	}
	assert.equal( experimentalOptions( { greyDisabledTabs: false } ).greyDisabledTabs, false );
	assert.equal( "greyDisabledTabs" in experimentalVideo( experimentalOptions( {} ) ), false );
});

test("job kill notices are on unless explicitly false and reach the worker's gameplay slice", () => {
	for ( const value of [ undefined, null, {}, { jobKillNotices: 1 }, { jobKillNotices: "false" } ] ) {
		assert.equal( experimentalOptions( value ).jobKillNotices, true );
	}
	const off = experimentalOptions( { jobKillNotices: false } );
	assert.equal( off.jobKillNotices, false );
	assert.equal( experimentalGameplay( off ).jobKillNotices, false );
	assert.equal( "jobKillNotices" in experimentalVideo( experimentalOptions( {} ) ), false );
});

test("drop and plus notices are on unless explicitly false and reach the worker's gameplay slice", () => {
	for ( const value of [ undefined, null, {}, { dropPlusNotices: 1 }, { dropPlusNotices: "false" } ] ) {
		assert.equal( experimentalOptions( value ).dropPlusNotices, true );
	}
	const off = experimentalOptions( { dropPlusNotices: false } );
	assert.equal( off.dropPlusNotices, false );
	assert.equal( off.jobKillNotices, true );
	assert.equal( experimentalGameplay( off ).dropPlusNotices, false );
	assert.equal( "dropPlusNotices" in experimentalVideo( experimentalOptions( {} ) ), false );
});

test("plus seven notices are on unless explicitly false, stay out of the video slice, and never widen the drop/plus row", () => {
	for ( const value of [ undefined, null, {}, { plusSevenNotices: 1 }, { plusSevenNotices: "false" } ] ) {
		assert.equal( experimentalOptions( value ).plusSevenNotices, true );
	}
	const off = experimentalOptions( { plusSevenNotices: false } );
	assert.equal( off.plusSevenNotices, false );
	assert.equal( off.dropPlusNotices, true );
	assert.equal( experimentalGameplay( off ).plusSevenNotices, false );
	assert.equal( experimentalGameplay( experimentalOptions( { dropPlusNotices: false } ) ).plusSevenNotices, true );
	assert.equal( "plusSevenNotices" in experimentalVideo( experimentalOptions( {} ) ), false );
});

test("item break notices are on unless explicitly false, stay out of the video slice, and never widen another notice row", () => {
	for ( const value of [ undefined, null, {}, { breakNotices: 1 }, { breakNotices: "false" } ] ) {
		assert.equal( experimentalOptions( value ).breakNotices, true );
	}
	const off = experimentalOptions( { breakNotices: false } );
	assert.equal( off.breakNotices, false );
	assert.equal( off.jobKillNotices, true );
	assert.equal( off.dropPlusNotices, true );
	assert.equal( off.plusSevenNotices, true );
	assert.equal( experimentalGameplay( off ).breakNotices, false );
	assert.equal( experimentalGameplay( experimentalOptions( { dropPlusNotices: false } ) ).breakNotices, true );
	assert.equal( "breakNotices" in experimentalVideo( experimentalOptions( {} ) ), false );
});

test("V hides pets and transports only when explicitly on, and reaches the renderer's slice", () => {
	for ( const value of [ undefined, null, {}, { vHidesCompanions: 1 }, { vHidesCompanions: "true" } ] ) {
		assert.equal( experimentalOptions( value ).vHidesCompanions, false );
	}
	const on = experimentalOptions( { vHidesCompanions: true } );
	assert.equal( on.vHidesCompanions, true );
	assert.equal( experimentalVideo( on ).vHidesCompanions, true );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "vHidesCompanions" ) ) );
});

test("fit window labels is on unless explicitly false and stays out of the worker's slices", () => {
	for ( const value of [ undefined, null, {}, { fitWindowLabels: 1 }, { fitWindowLabels: "false" } ] ) {
		assert.equal( experimentalOptions( value ).fitWindowLabels, true );
	}
	assert.equal( experimentalOptions( { fitWindowLabels: false } ).fitWindowLabels, false );
	assert.equal( "fitWindowLabels" in experimentalVideo( experimentalOptions( {} ) ), false );
	assert.equal( "fitWindowLabels" in experimentalGameplay( experimentalOptions( {} ) ), false );
});

test("drop on bag tabs is on unless explicitly false and stays out of the worker's slices", () => {
	for ( const value of [ undefined, null, {}, { dropOnBagTab: 1 }, { dropOnBagTab: "false" } ] ) {
		assert.equal( experimentalOptions( value ).dropOnBagTab, true );
	}
	assert.equal( experimentalOptions( { dropOnBagTab: false } ).dropOnBagTab, false );
	assert.equal( "dropOnBagTab" in experimentalVideo( experimentalOptions( {} ) ), false );
	assert.equal( "dropOnBagTab" in experimentalGameplay( experimentalOptions( {} ) ), false );
});

test("exchange click shortcuts are on unless explicitly false and stay out of the worker's slices", () => {
	for (
		const value of [ undefined, null, {}, { exchangeClickShortcuts: 1 }, { exchangeClickShortcuts: "false" } ]
	) {
		assert.equal( experimentalOptions( value ).exchangeClickShortcuts, true );
	}
	assert.equal( experimentalOptions( { exchangeClickShortcuts: false } ).exchangeClickShortcuts, false );
	assert.equal( "exchangeClickShortcuts" in experimentalVideo( experimentalOptions( {} ) ), false );
	assert.equal( "exchangeClickShortcuts" in experimentalGameplay( experimentalOptions( {} ) ), false );
});

test("smooth camera is on unless explicitly false and reaches the renderer's slice", () => {
	for ( const value of [ undefined, null, {}, { smoothCamera: 1 }, { smoothCamera: "false" } ] ) {
		assert.equal( experimentalOptions( value ).smoothCamera, true );
	}
	const off = experimentalOptions( { smoothCamera: false } );
	assert.equal( off.smoothCamera, false );
	assert.equal( experimentalVideo( off ).smoothCamera, false );
});

test("enchant sparks are on unless explicitly false and reach the renderer's slice", () => {
	for ( const value of [ undefined, null, {}, { enchantSparks: 1 }, { enchantSparks: "false" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.enchantSparks, true );
		assert.equal( experimentalVideo( options ).enchantSparks, true );
	}
	const off = experimentalOptions( { enchantSparks: false } );
	assert.equal( off.enchantSparks, false );
	assert.equal( experimentalVideo( off ).enchantSparks, false );
});

test("homing projectiles default off (native captured endpoint) and reach the worker's gameplay slice when enabled", () => {
	for ( const value of [ undefined, null, {}, { homingProjectiles: 1 }, { homingProjectiles: "true" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.homingProjectiles, false );
		assert.equal( experimentalGameplay( options ).homingProjectiles, false );
		assert.equal( "homingProjectiles" in experimentalVideo( options ), false );
	}
	const on = experimentalOptions( { homingProjectiles: true } );
	assert.equal( on.homingProjectiles, true );
	assert.equal( experimentalGameplay( on ).homingProjectiles, true );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "homingProjectiles" ) ) );
});

test("pet panel click-to-open defaults on (BUG-086) and stays out of the worker's slices", () => {
	for ( const value of [ undefined, null, {}, { petClickOpensWindow: 1 }, { petClickOpensWindow: "true" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.petClickOpensWindow, true );
		assert.equal( "petClickOpensWindow" in experimentalGameplay( options ), false );
		assert.equal( "petClickOpensWindow" in experimentalVideo( options ), false );
	}
	const off = experimentalOptions( { petClickOpensWindow: false } );
	assert.equal( off.petClickOpensWindow, false );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "petClickOpensWindow" ) ) );
});

test("smart pet pickup defaults on unless explicitly false and reaches the worker's gameplay slice", () => {
	for ( const value of [ undefined, null, {}, { smartPetPickup: 1 }, { smartPetPickup: "false" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.smartPetPickup, true );
		assert.equal( experimentalGameplay( options ).smartPetPickup, true );
		assert.equal( "smartPetPickup" in experimentalVideo( options ), false );
	}
	const off = experimentalOptions( { smartPetPickup: false } );
	assert.equal( off.smartPetPickup, false );
	assert.equal( experimentalGameplay( off ).smartPetPickup, false );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "smartPetPickup" ) ) );
});

test("expanded world map defaults off (native five towns and 28 x 8 overview) and stays out of the worker's slices", () => {
	for ( const value of [ undefined, null, {}, { expandedWorldMap: 1 }, { expandedWorldMap: "true" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.expandedWorldMap, false );
		assert.equal( "expandedWorldMap" in experimentalGameplay( options ), false );
		assert.equal( "expandedWorldMap" in experimentalVideo( options ), false );
	}
	const on = experimentalOptions( { expandedWorldMap: true } );
	assert.equal( on.expandedWorldMap, true );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "expandedWorldMap" ) ) );
});

test("interpolated own movement has no option, no row, and no video slice (#1564)", () => {
	for ( const value of [ undefined, null, {}, { interpolatedOwnWalk: false }, { interpolatedOwnWalk: "false" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( "interpolatedOwnWalk" in options, false );
		assert.equal( "interpolatedOwnWalk" in experimentalVideo( options ), false );
	}
	assert.ok( !EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => String( row.key ) === "interpolatedOwnWalk" ) ) );
});

test("main-thread own movement defaults on, turns off only on an explicit false, and stays out of the worker's and renderer's slices", () => {
	for ( const value of [ undefined, null, {}, { mainThreadOwnWalk: 1 }, { mainThreadOwnWalk: "false" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.mainThreadOwnWalk, true );
		assert.equal( "mainThreadOwnWalk" in experimentalGameplay( options ), false );
		assert.equal( "mainThreadOwnWalk" in experimentalVideo( options ), false );
	}
	assert.equal( experimentalOptions( { mainThreadOwnWalk: false } ).mainThreadOwnWalk, false );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "mainThreadOwnWalk" ) ) );
});

test("quest target markers default off (tracked quest only) and stay out of the worker's slices", () => {
	for ( const value of [ undefined, null, {}, { questTrackerMarkers: 1 }, { questTrackerMarkers: "true" } ] ) {
		const options = experimentalOptions( value );
		assert.equal( options.questTrackerMarkers, false );
		assert.equal( "questTrackerMarkers" in experimentalGameplay( options ), false );
		assert.equal( "questTrackerMarkers" in experimentalVideo( options ), false );
	}
	const on = experimentalOptions( { questTrackerMarkers: true } );
	assert.equal( on.questTrackerMarkers, true );
	assert.ok( EXPERIMENTAL_TABS.some( tab => tab.rows.some( row => row.key === "questTrackerMarkers" ) ) );
});
