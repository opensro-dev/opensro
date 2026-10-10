/*
===========================================================================

ui-behavior.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
import { uiFixture, fontAtlas } from "../helpers/ui-fixture.mjs";
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( file ).href );
}
const { createUiAssets } = await load( "src/engine/runtime/ui/resources/resources.ts" );
const { topmostControlAt } = await load( "src/engine/foundation/ui/hit-test.ts" );
const { refreshNameColor } = await load( "src/engine/foundation/gameplay/name-color.ts" );
const { createGameplay } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" );
const { createQuests } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/quests/quests.ts" );
/*
================
assetFixture
================
*/
function assetFixture() {
	let id = 0;
	const requests = [], results = new Map(), cancelled = [], published = [];
	const assets = {
		available: () => 8,
		/*
		================
		request
		================
		*/
		request( url ) {
			requests.push( url );
			return ++id;
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const r = results.get( id );
			results.delete( id );
			return r ?? null;
		},
		cancel: id => cancelled.push( id )
	};
	const resources = createUiAssets( assets, ( ...args ) => published.push( args ), "https://fixture.invalid/" );
	return { assets, resources, requests, results, cancelled, published };
}
/*
================
UI demand compares by content across arrays

The UI publishes a frozen demand array per layout and hands it back on the
frames between. A fresh array of the same length that names a different
image must still be noticed; the same contents in a fresh array must not
reload anything. (In-place edits of an unfrozen array are covered below.)
================
*/
test("UI demand is compared by content, whichever array carries it", () => {
	const f = assetFixture();
	f.resources.step( [ "/a.png", "/b.png" ], 0 );
	assert.deepEqual( f.requests.length, 2 );
	f.results.set( 1, { kind: "image", image: { width: 1, height: 1 } } );
	f.results.set( 2, { kind: "image", image: { width: 1, height: 1 } } );
	const settled = Object.freeze( [ "/a.png", "/b.png" ] );
	f.resources.step( settled, 1 );
	assert.equal( f.resources.step( settled, 2 ), false, "the same frozen array again is the same demand" );
	assert.equal(
		f.resources.step( [ "/a.png", "/b.png" ], 3 ),
		false,
		"equal contents in a new array change nothing"
	);
	f.resources.step( [ "/a.png", "/c.png" ], 4 );
	assert.equal( f.requests.length, 3, "a same-length demand naming a new image requests it" );
	assert.match( f.requests[2], /c\.png$/ );
	// Edited, then frozen: frozen now, but not when it was handed in.
	const late = [ "/a.png", "/c.png" ];
	f.resources.step( late, 5 );
	late[1] = "/d.png";
	Object.freeze( late );
	f.resources.step( late, 6 );
	assert.equal( f.requests.length, 4, "an array frozen after an edit is still read" );
	assert.match( f.requests[3], /d\.png$/ );
});
test("UI textures recover with backoff, release demand and cannot restart after disposal", () => {
	const f = assetFixture(), paths = [ "/button.png" ];
	f.resources.step( paths, 0 );
	f.results.set( 1, { kind: "error", error: "HTTP 503" } );
	assert.equal( f.resources.step( paths, 10 ), true, "failure invalidates retained UI error presentation" );
	for ( let now = 11; now < 1010; now++ ) f.resources.step( paths, now );
	assert.equal( f.requests.length, 1 );
	assert.match( f.resources.error(), /503/ );
	f.resources.step( paths, 1010 );
	assert.equal( f.requests.length, 2 );
	f.results.set( 2, { kind: "error", error: "HTTP 503" } );
	f.resources.step( paths, 1020 );
	f.resources.step( paths, 3019 );
	assert.equal( f.requests.length, 2 );
	f.resources.step( paths, 3020 );
	assert.equal( f.requests.length, 3 );
	const image = { width: 32, height: 32 };
	f.results.set( 3, { kind: "image", image } );
	f.resources.step( paths, 3021 );
	assert.equal( f.resources.has( paths[0] ), true );
	assert.equal( f.resources.error(), null );
	f.resources.step( [], 3022 );
	assert.deepEqual( f.published, [ [ paths[0], image ] ] );
	f.resources.step( paths, 3023 );
	f.resources.dispose();
	f.resources.dispose();
	assert.deepEqual( f.cancelled, [] );
	assert.deepEqual( f.published, [ [ paths[0], image ], [ paths[0], null ] ] );
	f.resources.step( paths, 99999 );
	assert.equal( f.requests.length, 3 );
});
/*
================
residentListEviction

A baseline list found resident is remembered, and the memo must not
outlive an eviction: a 4096 x 4096 image is 64 MiB, over the 48 MiB cap,
so it is evicted as soon as demand stops wanting it.
================
*/
test("a resident image list stops reading resident once one of its images is evicted", () => {
	const f = assetFixture(), baseline = [ "/a.png" ];
	assert.equal( f.resources.residentAll( baseline ), false );
	f.resources.step( baseline, 0 );
	f.results.set( 1, { kind: "image", image: { width: 4096, height: 4096 } } );
	f.resources.step( baseline, 1 );
	assert.equal( f.resources.residentAll( baseline ), true );
	assert.equal( f.resources.residentAll( baseline ), true, "remembered answer" );
	f.resources.step( [ "/b.png" ], 2 );
	assert.equal( f.resources.has( "/a.png" ), false );
	assert.equal( f.resources.residentAll( baseline ), false );
	f.resources.dispose();
});
test("UI request exceptions are recoverable and released failures do not poison re-entry", () => {
	const f = assetFixture();
	const request = f.assets.request;
	f.assets.request = () => {
		throw Error( "queue unavailable" );
	};
	f.resources.step( [ "/a.png" ], 0 );
	assert.match( f.resources.error(), /queue unavailable/ );
	f.assets.request = request;
	f.resources.step( [ "/a.png" ], 999 );
	assert.equal( f.requests.length, 0 );
	f.resources.step( [], 1000 );
	assert.equal( f.resources.error(), null );
	f.resources.step( [ "/a.png" ], 1001 );
	assert.equal( f.requests.length, 1 );
	f.resources.dispose();
});

test("released image failures invalidate retained error UI and report attributed recovery without retry spam", () => {
	const events = [], results = new Map();
	let id = 0;
	const owner = createUiAssets(
		{
			available: () => 8,
			request: () => ++id,
			take: id => {
				const r = results.get( id );
				results.delete( id );
				return r;
			},
			/*
			================
			cancel
			================
			*/
			cancel() {}
		},
		() => {},
		"https://fixture.invalid",
		event => events.push( event )
	);
	owner.step( [ "/failed.png" ], 0 );
	results.set( 1, { kind: "error", error: "Asset HTTP 503" } );
	assert.equal( owner.step( [ "/failed.png" ], 1 ), true );
	assert.match( owner.error(), /\/failed.png: Asset HTTP 503/ );
	owner.step( [ "/failed.png" ], 1001 );
	results.set( 2, { kind: "error", error: "Asset HTTP 503" } );
	owner.step( [ "/failed.png" ], 1002 );
	assert.equal( events.length, 1, "same retry failure is not repeated in the console" );
	assert.equal( owner.step( [], 1003 ), true, "clearing failed demand must rebuild the displayed error dialog" );
	assert.equal( owner.error(), null );
	assert.equal( events[1].kind, "released" );
	assert.equal( events[1].attempts, 2 );
	owner.step( [ "/failed.png" ], 1004 );
	results.set( 3, { kind: "error", error: "decode failed" } );
	owner.step( [ "/failed.png" ], 1005 );
	owner.step( [ "/failed.png" ], 2005 );
	results.set( 4, { kind: "image", image: { width: 1, height: 1 } } );
	owner.step( [ "/failed.png" ], 2006 );
	assert.equal( events.at( -1 ).kind, "recovered" );
	assert.equal( owner.error(), null );
	owner.dispose();
});

test("settled UI demand notices in-place path edits, waits for completion and retries after release", () => {
	const f = assetFixture(), paths = [ "/a.png" ];
	f.resources.step( paths, 0 );
	f.results.set( 1, { kind: "image", image: { width: 2, height: 2 } } );
	assert.equal( f.resources.step( paths, 1 ), true );
	for ( let i = 2; i < 30; i++ ) assert.equal( f.resources.step( paths, i ), false );
	paths[0] = "/b.png";
	assert.equal( f.resources.step( paths, 30 ), false );
	assert.equal( f.requests.length, 2 );
	assert.equal( f.resources.step( paths, 31 ), false );
	f.results.set( 2, { kind: "error", error: "temporary" } );
	assert.equal( f.resources.step( paths, 32 ), true );
	paths.length = 0;
	f.resources.step( paths, 33 );
	assert.equal( f.resources.error(), null );
	paths.push( "/b.png" );
	f.resources.step( paths, 34 );
	assert.equal( f.requests.length, 3 );
	f.results.set( 3, { kind: "image", image: { width: 2, height: 2 } } );
	assert.equal( f.resources.step( paths, 35 ), true );
	paths[0] = "/a.png";
	assert.equal( f.resources.step( paths, 36 ), false );
	assert.equal( f.requests.length, 3 );
	assert.equal( f.resources.has( "/a.png" ), true );
	f.resources.dispose();
});

test("UI image disposal drains all releases even when cancellation and renderer release fail", () => {
	let next = 0;
	const results = new Map(), cancelled = [], released = [];
	const owner = createUiAssets( {
		available: () => 8,
		request: () => ++next,
		take: id => {
			const value = results.get( id );
			results.delete( id );
			return value;
		},
		/*
		================
		cancel
		================
		*/
		cancel( id ) {
			cancelled.push( id );
			if ( id === 3 ) throw Error( "cancel failed" );
		}
	}, ( path, image ) => {
		if ( !image ) {
			released.push( path );
			if ( path === "/a.png" ) throw Error( "renderer release failed" );
		}
	}, "https://fixture.invalid" );
	const paths = [ "/a.png", "/b.png", "/c.png", "/d.png" ];
	owner.step( paths, 0 );
	results.set( 1, { kind: "image", image: { width: 10, height: 20 } } );
	results.set( 2, { kind: "image", image: { width: 20, height: 30 } } );
	owner.step( paths, 1 );
	assert.throws( () => owner.dispose(), error => error instanceof AggregateError && error.errors.length === 2 );
	assert.deepEqual( cancelled, [ 3, 4 ] );
	assert.deepEqual( released, [ "/a.png", "/b.png" ] );
	assert.deepEqual( owner.stats(), { pending: 0, failed: [] } );
	assert.equal( owner.size( "/a.png" ), undefined );
	owner.dispose();
	owner.step( paths, 100 );
	assert.equal( next, 4 );
});

/*
================
absentIconFallback

CIFSlotWithHelp (55B450) loads icon\icon_default.ddj when the requested icon
fails: an icon the manifest lacks is served the default under its own path,
without the blocking retry. Any other absent image settles blank, and a
transient fault on an icon still retries.
================
*/
test("an absent slot icon draws icon_default and an absent image settles blank", () => {
	const f = assetFixture();
	const icon = "/assets/images/Media_extracted/icon/item/etc/archemy_reinforce_recipe_a.png";
	const art = "/assets/images/Media_extracted/interface/missing.png";
	const paths = [ icon, art ];
	f.resources.step( paths, 0 );
	f.results.set( 1, { kind: "error", error: "Asset absent from published manifest", absent: true } );
	f.results.set( 2, { kind: "error", error: "Asset absent from published manifest", absent: true } );
	f.resources.step( paths, 1 );
	assert.equal( f.resources.error(), null, "absence never blocks the interface" );
	assert.deepEqual( f.resources.stats().failed, [] );
	assert.equal( f.requests.at( -1 ), "https://fixture.invalid/assets/images/Media_extracted/icon/icon_default.png" );
	assert.equal( f.requests.length, 3, "the blank image is never requested again" );
	const image = { width: 32, height: 32 };
	f.results.set( 3, { kind: "image", image } );
	f.resources.step( paths, 2 );
	assert.deepEqual( f.published, [ [ icon, image ] ], "the default is published under the icon's own path" );
	for ( let now = 3; now < 60_000; now += 1000 ) f.resources.step( paths, now );
	assert.equal( f.requests.length, 3 );
	f.resources.dispose();
	const g = assetFixture();
	g.resources.step( [ icon ], 0 );
	g.results.set( 1, { kind: "error", error: "HTTP 503" } );
	g.resources.step( [ icon ], 1 );
	assert.match( g.resources.error(), /503/, "a transient fault keeps the retry" );
	g.resources.step( [ icon ], 1001 );
	assert.equal( g.requests.at( -1 ), "https://fixture.invalid" + icon );
	g.resources.dispose();
});

test("UI image dimensions are captured before ownership is transferred to the renderer", () => {
	const image = { width: 32, height: 64 };
	let result = null;
	const owner = createUiAssets( {
		available: () => 8,
		request: () => 1,
		take: () => {
			const value = result;
			result = null;
			return value;
		},
		/*
		================
		cancel
		================
		*/
		cancel() {}
	}, ( _path, bitmap ) => {
		if ( bitmap ) {
			bitmap.width = 0;
			bitmap.height = 0;
		}
	}, "https://fixture.invalid" );
	owner.step( [ "/a.png" ], 0 );
	result = { kind: "image", image };
	owner.step( [ "/a.png" ], 1 );
	assert.deepEqual( owner.size( "/a.png" ), [ 32, 64 ] );
	owner.dispose();
});

test("retail GM prefix colors player names gold without granting permission", () => {
	const f = uiFixture();
	try {
		f.state.entities[0].name = "[GM]Test2";
		f.state.session.character = "[GM]Test2";
		f.state.entities[0].nameColor = refreshNameColor( f.state.entities[0], {
			local: f.state.entities[0],
			social: { leader: 0, members: [] },
			fortress: { worldId: 0, worlds: [], fortresses: [], wars: [], registered: [] },
			capeTeam: () => undefined
		} );
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, i * 100 );
		const glyphs = f.scenes.at( -1 ).quads.filter( q => q.characterAnchor === 1 && q.texture === fontAtlas.image );
		assert.ok( glyphs.some( q => q.color.join( "," ) === [ 1, 216 / 255, 122 / 255, 1 ].join( "," ) ) );
		f.ui.event( { kind: "key", code: "Backquote", shift: true } );
		f.ui.step( f.state, 1700 );
		assert.ok(
			!f.scenes.at( -1 ).quads.some( q => q.rect[2] === 600 && q.rect[3] === 112 ),
			"name prefix does not authorize console"
		);
	} finally {
		f.dispose();
	}
});

test("the berserk entry flash fades every frame, not in 50 ms steps", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 1000 );
		f.state.entities = [ { ...f.state.entities[0], appearanceState: [ 0, 0, 1 ] } ];
		f.state = { ...f.state };
		const flash = () =>
			f.scenes.at( -1 )?.quads.find( q =>
				!q.texture && q.rect[2] === 1600 && q.rect[3] === 900 &&
				q.color[0] === 1 && q.color[1] === 1 && q.color[2] === 1
			)?.color[3];
		const seen = [];
		// 60 Hz frames through the 700 ms flash: each one must publish a new opacity.
		for ( let i = 1; i <= 40; i++ ) {
			f.ui.step( f.state, 1000 + i * 16 );
			seen.push( flash() );
		}
		const distinct = new Set( seen.filter( alpha => alpha !== undefined ) ).size;
		assert.ok( distinct >= 30, `only ${distinct} distinct flash opacities across 40 frames` );
	} finally {
		f.dispose();
	}
});

test("F10 opens the Item Mall and a world transfer closes it, as the native reset does", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 1000 );
		const open = () => {
			let result;
			// A retained frame returns null; keep the last published controls.
			for ( let i = 0; i < 20; i++ ) result = f.ui.step( f.state, 1100 + i * 50 ) ?? result;
			return !!result?.controls.some( control => control.id === "item-mall-close" );
		};
		assert.equal( open(), false );
		f.ui.event( { kind: "key", code: "F10" } );
		assert.equal( open(), true, "UIIT_STT_SILKMALL_SHORT_KEY: Item Mall(F10)" );
		// Windows are hidden while the transfer loads; the mall must stay closed after it.
		f.state.travel = { mode: 1, region: 25000 };
		open();
		f.state.travel = null;
		assert.equal( open(), false, "0x366A reset closes the ItemMall section" );
	} finally {
		f.dispose();
	}
});

test("the underbar's up arrow selects the next quickslot bar, as native control 13 does", () => {
	// CIFUnderBar's message map (BB2C90): control 13, GDR_BTN_QUICKSLOTUP, runs
	// 572760 (page + 1); control 12, GDR_BTN_QUICKSLOTDOWN, runs 572750 (page - 1).
	const f = uiFixture();
	try {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */
		let last;
		for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, 1100 + i * 50 ) ?? last;
		const next = last?.controls.find( control => control.id === "hotbar-next" );
		const prev = last?.controls.find( control => control.id === "hotbar-prev" );
		assert.ok( next && prev, "both quickslot arrows publish" );
		assert.ok( next.rect[1] < prev.rect[1], "the upper arrow (QUICKSLOTUP) is next, the lower previous" );
	} finally {
		f.dispose();
	}
});

test("the underbar mall button is a clickable button and F10 toggles the mall", () => {
	const f = uiFixture();
	try {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */
		let last;
		const frame = () => {
			for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, 1100 + i * 50 ) ?? last;
			return last;
		};
		const button = frame()?.controls.find( control => control.id === "item-mall" );
		// The platform layer activates buttons only: a region click did nothing.
		assert.equal( button?.kind, "button", "CIFButton 7 must publish as a button" );
		const open = () => !!frame()?.controls.some( control => control.id === "item-mall-close" );
		f.ui.event( { kind: "activate", id: "item-mall" } );
		assert.equal( open(), true, "the button opens the mall" );
		f.ui.event( { kind: "key", code: "F10" } );
		assert.equal( open(), false, "F10 closes the mall it opened" );
		f.ui.event( { kind: "key", code: "F10" } );
		assert.equal( open(), true, "and opens it again" );
	} finally {
		f.dispose();
	}
});

test("the Item Mall notice text keeps its static's authored dark FontColor, as 6CC490", () => {
	const f = uiFixture();
	try {
		for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, 1100 + i * 50 );
		const face = fontAtlas.fonts["0"],
			uv = ( /** @type {string} */ c ) => {
				const g = face.glyphs[c.codePointAt( 0 ) ?? 0];
				return [ g.x / fontAtlas.atlasWidth, g.y / fontAtlas.atlasHeight ].join( "," );
			},
			glyphs = defined( f.scenes.at( -1 ) ).quads.filter( q => q.texture === fontAtlas.image ),
			uvs = glyphs.map( q => q.uv.slice( 0, 2 ).join( "," ) ),
			word = Array.from( "stocked", uv ),
			at = uvs.findIndex( ( _, i ) => word.every( ( u, k ) => uvs[i + k] === u ) );
		assert.ok( at >= 0, "the notice text is drawn" );
		// ifmallnotifywnd.txt GDR_MALL_NOTIFY_CONTENTS FontColor=255,61,34,0.
		assert.deepEqual( glyphs[at]?.color, [ 61 / 255, 34 / 255, 0, 1 ] );
	} finally {
		f.dispose();
	}
});

test("the Item Mall notice waits for its frame instead of drawing bare text over the world", () => {
	// A teleport or world entry cold-loads the frame; until it arrives the
	// window (text and buttons) is withheld as one admission.
	let held = true;
	const f = uiFixture( undefined, path => held && path.endsWith( "/interface/mall/mall_communicate.png" ) );
	try {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */
		let last;
		const shown = () => {
			for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, 1100 + i * 50 ) ?? last;
			return !!last?.controls.some( control => control.id === "mall-notice-close" );
		};
		assert.equal( shown(), false, "the notice showed before its frame loaded" );
		held = false;
		assert.equal( shown(), true, "the notice did not appear once its frame loaded" );
	} finally {
		f.dispose();
	}
});

test("the Item Mall notice opens once per session at world entry, as 683B40", () => {
	const f = uiFixture();
	try {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */
		let last;
		const shown = ( /** @type {string} */ id ) => {
			for ( let i = 0; i < 20; i++ ) last = f.ui.step( f.state, 1100 + i * 50 ) ?? last;
			return !!last?.controls.some( control => control.id === id );
		};
		assert.equal( shown( "mall-notice-close" ), true, "CIFMallNotifyWnd after the first world entry" );
		f.ui.event( { kind: "activate", id: "mall-notice-close" } );
		assert.equal( shown( "mall-notice-close" ), false, "button 5 closes the notice" );
		// A world transfer keeps the +0x6FC latch: the notice does not return.
		f.state.travel = { mode: 1, region: 25000 };
		shown( "mall-notice-close" );
		f.state.travel = null;
		assert.equal( shown( "mall-notice-close" ), false );
	} finally {
		f.dispose();
	}
	const g = uiFixture();
	try {
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */
		let last;
		const shown = ( /** @type {string} */ id ) => {
			for ( let i = 0; i < 20; i++ ) last = g.ui.step( g.state, 1100 + i * 50 ) ?? last;
			return !!last?.controls.some( control => control.id === id );
		};
		assert.equal( shown( "mall-notice-enter" ), true );
		g.ui.event( { kind: "activate", id: "mall-notice-enter" } );
		assert.equal( shown( "item-mall-close" ), true, "button 4 enters the mall" );
	} finally {
		g.dispose();
	}
});

test("window hotkeys sound on retarget, close once, and preserve sidebar click-only selection", () => {
	const f = uiFixture();
	f.ui.step( f.state, 1000 );
	f.sounds.length = 0;
	for ( const code of [ "KeyC", "KeyS", "KeyI", "KeyA", "KeyP", "KeyQ" ] ) {
		f.ui.event( { kind: "key", code } );
		assert.equal( f.sounds.at( -1 ), "open", code );
		assert.equal( f.sounds.length, [ "KeyC", "KeyS", "KeyI", "KeyA", "KeyP", "KeyQ" ].indexOf( code ) + 1 );
	}
	f.ui.event( { kind: "key", code: "KeyQ" } );
	assert.deepEqual( f.sounds, [ "open", "open", "open", "open", "open", "open", "close" ] );
	f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
	assert.equal( f.sounds.at( -1 ), "open" );
	const count = f.sounds.length;
	f.ui.event( { kind: "activate", id: "select-window:Skills" } );
	f.ui.event( { kind: "activate", id: "select-window:Skills" } );
	assert.equal( f.sounds.length, count, "sidebar uses the button sound without an extra window sound" );
	f.ui.event( { kind: "activate", id: "open-window:Character" } );
	assert.equal( f.sounds.length, count + 1 );
	f.ui.event( { kind: "activate", id: "open-window:Character" } );
	assert.equal( f.sounds.length, count + 1, "idempotent open is silent" );
	f.ui.event( { kind: "key", code: "Escape" } );
	assert.equal( f.sounds.at( -1 ), "close" );
	f.dispose();
});

test("NPC UI uses authored talk bounds and native fonts through option, accept, close and reopen", () => {
	const frames = [], game = createGameplay( frame => frames.push( frame ) );
	const f = uiFixture( command => {
		if ( command.kind === "gameplay" ) game.command( command.command, now, npc );
	} );
	const npc = { gid: 7, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0, name: "General Sonhyeon" };
	f.state.entities.push( npc );
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
	let now = 100;
	game.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	/*
	================
	settle
	================
	*/
	function settle() {
		const state = game.take();
		if ( state ) f.state.gameplay = state;
		for ( let i = 0; i < 50; i++ ) semantics = f.ui.step( f.state, now++ ) ?? semantics;
	}
	/*
	================
	select
	================
	*/
	function select() {
		game.command( { kind: "select", gid: 7 }, now, npc );
		game.receive( { opcode: 0xb45a, payload: Buffer.from( "0107000000000200000000", "hex" ) }, now );
		settle();
	}
	/*
	================
	reply
	================
	*/
	function reply( kind, prompt, options = [] ) {
		const str = s => {
			const b = Buffer.from( s ), n = Buffer.alloc( 2 );
			n.writeUInt16LE( b.length );
			return Buffer.concat( [ n, b ] );
		};
		game.receive( {
			opcode: 0x3773,
			payload: Buffer.concat( [
				Buffer.of( kind ),
				str( prompt ),
				...(kind === 4 ? [ Buffer.of( options.length ), ...options.map( str ) ] : [])
			] )
		}, now );
		settle();
	}
	try {
		select();
		const talk = defined( semantics ).controls.find( c => c.id === "npc-talk" );
		assert.ok( talk );
		assert.deepEqual( talk.rect, [ 234, 271, 302, 18 ] );
		assert.ok( f.hasText( "1. Start to converse." ) );
		f.ui.event( { kind: "activate", id: "npc-talk" } );
		settle();
		assert.equal( frames.at( -1 ).opcode, 0x7338 );
		assert.ok( defined( semantics ).controls.find( c => c.id === "npc-talk" ).disabled );
		reply( 4, "SN_NPC_CH_SOLDIER_EA1_BS", [ "SN_QNO_CH_SOLDIER_EA1_1" ] );
		const option = defined( semantics ).controls.find( c => c.id === "npc-choice:5" );
		assert.ok( option && !option.disabled );
		f.ui.event( { kind: "activate", id: option.id } );
		settle();
		assert.deepEqual( [ ...frames.at( -1 ).payload ], [ 5 ] );
		reply( 3, "SN_TALK_QNO_CH_SOLDIER_EA1_1_01" );
		// Long authored prose may place Yes/No below the viewport. Wheel and arrows
		// must make the same actionable rows reachable without changing choice IDs.
		for ( let i = 0; i < 40 && !defined( semantics ).controls.some( c => c.id === "npc-choice:2" ); i++ ) {
			f.ui.event( { kind: "activate", id: "npc-scroll-down" } );
			settle();
		}
		assert.ok( defined( semantics ).controls.some( c => c.id === "npc-choice:2" && !c.disabled ) );
		assert.ok( f.hasText( "1. Yes" ) );
		f.ui.event( { kind: "activate", id: "npc-choice:2" } );
		settle();
		assert.deepEqual( [ ...frames.at( -1 ).payload ], [ 2 ] );
		game.receive( { opcode: 0x31ed, payload: Buffer.from( "010500000000000802", "hex" ) }, now );
		reply( 1, "SN_NPC_CH_SOLDIER_EA1_BS" );
		assert.equal( f.state.gameplay.quests[0].refId, 5 );
		assert.ok( defined( semantics ).controls.some( c => c.id === "npc-choice:1" ) );
		assert.ok( f.hasText( "1. Confirm" ) );
		if ( process.env.NPC_UI_CAPTURE ) {
			mkdirSync( "temp/artifacts/npc-conversation", { recursive: true } );
			writeFileSync(
				"temp/artifacts/npc-conversation/ui.json",
				JSON.stringify( { scene: f.scenes.at( -1 ), semantics }, null, 2 )
			);
		}
		f.ui.event( { kind: "activate", id: "npc-close" } );
		settle();
		assert.equal( frames.at( -1 ).opcode, 0x74b3 );
		assert.ok(
			!defined( semantics ).controls.some( c => c.id === "npc-talk" || c.id.startsWith( "npc-choice:" ) )
		);
		game.receive( { opcode: 0xb4b3, payload: Buffer.of( 1 ) }, now );
		select();
		assert.ok( defined( semantics ).controls.some( c => c.id === "npc-talk" ) );
		assert.equal( f.state.gameplay.quests.length, 1 );
	} finally {
		f.dispose();
		game.dispose();
	}
});

test("quest objectives repaint native progress, per-node status and color after updates and reopen", () => {
	const f = uiFixture(), quests = createQuests( () => {} );
	const captures = [],
		subLayout = JSON.parse(
			readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifquestslotsub.json", "utf8" )
		)
			.controlsByName;
	const symbol = "SN_CON_QNO_CH_SOLDIER_EA1_1";
	const record = {
		refId: 2,
		u08: 0,
		u09: 0,
		flags: 24,
		u10: 2,
		contents: [ { tag: 1, kind: 1, description: symbol, objectiveSentinel: false, objectiveValues: [ 0 ] } ],
		targetIds: []
	};
	const strings =
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/text/textuisystem.en.json", "utf8" ) ).entries;
	/*
	================
	update
	================
	*/
	function update( kind, count ) {
		const name = Buffer.from( symbol ), p = Buffer.alloc( 5 + 3 + 1 + 1 + 2 + 2 + name.length + 1 + 4 );
		let o = 0;
		p[o++] = 2;
		p.writeUInt32LE( 2, o );
		o += 4;
		p[o++] = 0;
		p[o++] = 0;
		p[o++] = 24;
		p[o++] = 2;
		p[o++] = 1;
		p[o++] = 1;
		p[o++] = kind;
		p.writeUInt16LE( name.length, o );
		o += 2;
		name.copy( p, o );
		o += name.length;
		p[o++] = 1;
		p.writeUInt32LE( count, o );
		quests.receive( { opcode: 0x31ed, payload: p } );
		f.state.gameplay = { ...f.state.gameplay, ...quests.state() };
	}
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
	/*
	================
	settle
	================
	*/
	function settle( now ) {
		for ( let i = 0; i < 100; i++ ) semantics = f.rawStep( { ...f.state }, now + i ) ?? semantics;
	}
	/*
	================
	glyphRun
	================
	*/
	function glyphRun( value, color ) {
		const glyphs = f.scenes.at( -1 ).quads.filter( q => q.texture === fontAtlas.image );
		const expected = Array.from( value, c => {
			const g = fontAtlas.fonts["0"].glyphs[c.codePointAt( 0 )] ?? fontAtlas.fonts["0"].glyphs["63"];
			return [
				g.x / fontAtlas.atlasWidth,
				g.y / fontAtlas.atlasHeight,
				g.width / fontAtlas.atlasWidth,
				g.height / fontAtlas.atlasHeight
			];
		} );
		const start = glyphs.findIndex( ( _, i ) =>
			expected.every( ( uv, j ) => JSON.stringify( glyphs[i + j]?.uv ) === JSON.stringify( uv ) )
		);
		assert.ok( start >= 0, "Missing rendered text: " + value );
		const run = glyphs.slice( start, start + expected.length );
		for ( const quad of run ) assert.deepEqual( quad.color, color );
		return run;
	}
	try {
		quests.bootstrap( { character: { activeQuests: [ record ] } } );
		f.state.gameplay = { ...f.state.gameplay, ...quests.state() };
		settle( 1000 );
		f.ui.event( { kind: "activate", id: "open-window:Quests" } );
		settle( 1100 );
		assert.ok(
			defined( semantics ).controls.some( c => c.id === "quest-expand:2" ),
			"Production quest row must exist"
		);
		f.ui.event( { kind: "activate", id: "quest-expand:2" } );
		let firstRect;
		for ( const [index, [kind, count]] of [ [ 1, 0 ], [ 0, 40 ], [ 1, 7 ], [ 2, 40 ], [ 1, 8 ] ].entries() ) {
			update( kind, count );
			settle( 1200 + index * 100 );
			const complete = kind === 0 || kind === 2,
				color = complete ? [ 1, 156 / 255, 104 / 255, 1 ] : [ 239 / 255, 218 / 255, 164 / 255, 1 ];
			const description = "Hunt 40 Weasel (" + count + ")",
				status = strings[complete ? "UIIT_STT_QUEST_END" : "UIIT_STT_QUEST_ING"];
			const run = glyphRun( description, color ), statusRun = glyphRun( status, color );
			const background = f.scenes.at( -1 ).quads.find( q =>
				q.texture === subLayout.GDR_QUESTSLOT_SUB_WND.ddj.publicPath
			);
			assert.ok( background, "Expanded row background must be drawn" );
			captures.push( {
				kind,
				count,
				description,
				status,
				origin: background.rect.slice( 0, 2 ),
				quads: [ ...run, ...statusRun ]
			} );
			firstRect ??= run[0].rect;
			assert.deepEqual( run[0].rect, firstRect, "Progress cannot move the authored text origin" );
		}
		f.ui.event( { kind: "activate", id: "close" } );
		settle( 1800 );
		f.ui.event( { kind: "activate", id: "open-window:Quests" } );
		settle( 1900 );
		glyphRun( "Hunt 40 Weasel (8)", [ 239 / 255, 218 / 255, 164 / 255, 1 ] );
		assert.equal( quests.state().quests[0].contents[0].kind, 1, "Rendering must not mutate the quest owner" );
		if ( process.env.QUEST_ROW_CAPTURE ) {
			mkdirSync( "temp/artifacts/quest-objectives", { recursive: true } );
			writeFileSync( "temp/artifacts/quest-objectives/ui-rows.json", JSON.stringify( captures, null, 2 ) );
		}
	} finally {
		f.dispose();
		quests.clear();
	}
});

test("pose journals retain chat and status layouts while text and viewport changes rebuild them", () => {
	const f = uiFixture();
	f.state.gameplay.chat = { lines: [ { channel: "all", name: "Player", gid: 1, text: "Hello", outgoing: true } ] };
	for ( let i = 0; i < 20; i++ ) f.ui.step( { ...f.state }, 1000 );
	const before = f.ui.stats().layoutRetention;
	assert.ok( before.chat.rebuilds > 0 );
	assert.ok( before.status.rebuilds > 0 );
	for ( let i = 0; i < 100; i++ ) {
		f.state.entities = f.state.entities.map( entity => ({ ...entity, x: i }) );
		f.state.gameplay = {
			...f.state.gameplay,
			pose: { ...f.state.gameplay.pose, x: i },
			chat: { lines: f.state.gameplay.chat.lines.map( line => ({ ...line }) ) }
		};
		f.ui.step( { ...f.state }, 1000 + i );
	}
	const moved = f.ui.stats().layoutRetention;
	assert.equal( moved.chat.rebuilds, before.chat.rebuilds );
	assert.equal( moved.status.rebuilds, before.status.rebuilds );
	assert.ok( moved.chat.reuses > before.chat.reuses );
	assert.ok( moved.status.reuses > before.status.reuses );
	for ( const part of [ "player", "bar" ] ) {
		assert.equal( moved[part].rebuilds, before[part].rebuilds );
		assert.ok( moved[part].reuses > before[part].reuses );
	}
	f.state.gameplay = {
		...f.state.gameplay,
		chat: { lines: [ { channel: "all", name: "Player", gid: 1, text: "Changed", outgoing: true } ] }
	};
	f.ui.step( { ...f.state }, 1100 );
	assert.equal( f.ui.stats().layoutRetention.chat.rebuilds, moved.chat.rebuilds + 1 );
	assert.equal( f.ui.stats().layoutRetention.status.rebuilds, moved.status.rebuilds );
	f.state.width = 1400;
	f.ui.step( { ...f.state }, 1101 );
	assert.equal( f.ui.stats().layoutRetention.status.rebuilds, moved.status.rebuilds + 1 );
	f.dispose();
});

test("quest progress publishes native green chrome and bold glyphs independently of warning and journal", () => {
	const f = uiFixture(),
		game = createGameplay( () => {} ),
		fixture = JSON.parse( readFileSync( "../server/internal/game/quest/graesp_wire_fixture.json", "utf8" ) );
	const step = now => {
		for ( let i = 0; i < 30; i++ ) f.ui.step( { ...f.state }, now );
	};
	const chrome = family => f.scenes.at( -1 ).quads.filter( q => q.texture.includes( "/com_" + family + "_" ) );
	const receive = n => {
		game.receive( { opcode: 0x31ed, payload: Buffer.from( fixture.frames[n].payloadHex, "hex" ) }, 0 );
		f.state.gameplay = { ...f.state.gameplay, ...game.take() };
	};
	try {
		game.bootstrap( { character: { activeQuests: [] } } );
		receive( 0 );
		step( 1000 );
		assert.equal( chrome( "quest" ).length, 0 );
		receive( 1 );
		f.state.gameplay = {
			...f.state.gameplay,
			notices: [ {
				sequence: 1,
				key: "UIIT_MSG_ANYONE_DEAD_UNIC",
				value: 0,
				arguments: [ "asd2", "Cerberus" ],
				banner: true
			} ]
		};
		step( 1100 );
		assert.equal( chrome( "quest" ).length, 8 );
		assert.equal( chrome( "warning" ).length, 8 );
		assert.ok( f.hasText( "Hunt 20 Graesps (1)", "0", 2 ) );
		const face = fontAtlas.fonts["0"].styles["2"],
			width = Array.from( "Hunt 20 Graesps (1)", c => face.glyphs[c.codePointAt( 0 )].advanceX ).reduce(
				( a, b ) => a + b,
				0
			);
		assert.deepEqual( chrome( "quest" )[6].rect, [ 800 - (width >> 1), 152, width, 8 ] );
		const fill = f.scenes.at( -1 ).quads.find( q => !q.texture && q.rect[1] === 160 && q.rect[2] === width );
		assert.deepEqual( fill.color, [ 0, 91 / 255, 66 / 255, 128 / 255 ] );
		assert.equal( chrome( "quest" )[4].rect[2], 40 );
		const x = chrome( "quest" )[0].rect[0];
		f.state.width = 1200;
		step( 1200 );
		assert.equal( chrome( "quest" )[0].rect[0], x - 200 );
		receive( 2 );
		step( 2100 );
		assert.ok( f.hasText( "Hunt 20 Graesps (2)", "0", 2 ) );
		receive( 2 );
		step( 8100 );
		assert.equal( chrome( "warning" ).length, 0 );
		assert.ok( chrome( "quest" ).every( q => q.color[3] === 127 / 255 ) );
		step( 9100 );
		assert.equal( chrome( "quest" ).length, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Quests" } );
		step( 9200 );
		assert.equal( chrome( "quest" ).length, 0, "opening journal does not replay feedback" );
		f.state.session = { phase: "signed-out", revision: 2 };
		step( 9300 );
		f.state.session = { phase: "world", revision: 3 };
		game.bootstrap( { character: { activeQuests: [] } } );
		receive( 0 );
		step( 9400 );
		assert.equal( chrome( "quest" ).length, 0 );
		receive( 1 );
		step( 9500 );
		assert.equal( chrome( "quest" ).length, 8, "fresh session starts a new event sequence" );
	} finally {
		f.dispose();
		game.dispose();
	}
});

test("unique announcement publishes native warning chrome and glyphs, recenters and expires", () => {
	const f = uiFixture();
	try {
		for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, 1000 );
		f.state.gameplay = {
			...f.state.gameplay,
			notices: [ {
				sequence: 1,
				key: "UIIT_MSG_ANYONE_DEAD_UNIC",
				value: 0,
				arguments: [ "asd2", "Cerberus" ],
				banner: true
			} ]
		};
		for ( let i = 0; i < 8; i++ ) f.ui.step( f.state, 1100 );
		const chrome = () => f.scenes.at( -1 ).quads.filter( q => q.texture.includes( "/com_warning_" ) );
		assert.equal( chrome().length, 8 );
		assert.ok(
			f.hasText( "[asd2]has killed [Cerberus].", "0", 2 ),
			"CIFNotify overrides the authored font with style 2"
		);
		const face = fontAtlas.fonts["0"].styles["2"], message = "[asd2]has killed [Cerberus].";
		const width = Array.from( message, c => face.glyphs[c.codePointAt( 0 )].advanceX ).reduce(
			( a, b ) => a + b,
			0
		);
		assert.deepEqual(
			chrome()[6].rect,
			[ 800 - (width >> 1), 122, width, 8 ],
			"border measures the same bold glyphs it draws"
		);
		assert.ok(
			chrome()[4].texture.endsWith( "com_warning_edge2.png" ),
			"retail first resource argument is the wide left side fade"
		);
		assert.ok(
			chrome()[6].texture.endsWith( "com_warning_edge.png" ),
			"retail second resource argument is the top strip"
		);
		assert.equal( chrome()[4].rect[2], 40 );
		assert.equal( chrome()[4].rect[0], chrome()[0].rect[0], "left edge meets the corner across its full width" );
		assert.equal( chrome()[5].rect[2], 40 );
		assert.deepEqual( chrome()[5].uv, [ 1, 0, -1, 1 ] );
		assert.deepEqual( chrome()[7].uv, [ 0, 1, 1, -1 ] );
		const left = chrome()[0].rect[0];
		f.state = { ...f.state, width: 1200 };
		f.ui.step( f.state, 1200 );
		assert.equal( chrome()[0].rect[0], left - 200, "resize recenters native notice" );
		f.ui.step( f.state, 7100 );
		assert.ok( chrome().every( q => q.color[3] === 127 / 255 ) );
		f.ui.step( f.state, 8100 );
		assert.equal( chrome().length, 0 );
	} finally {
		f.dispose();
	}
});

test("prepared popup opens on the next step, sidebar selection stays open, hotkeys still toggle", () => {
	const f = uiFixture();
	try {
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		const published = f.textures.length;
		f.ui.event( { kind: "key", code: "KeyC" } );
		let scene = f.rawStep( f.state, 1300 );
		assert.ok( scene, JSON.stringify( f.ui.stats() ) );
		assert.ok( scene.controls.some( c => c.id === "main-popup-drag" ) );
		for ( const name of [ "Inventory", "Character", "Party", "Inventory" ] ) {
			f.ui.event( { kind: "activate", id: "select-window:" + name } );
			scene = f.rawStep( f.state, 1400 );
			assert.ok( scene.controls.some( c => c.id === "main-popup-drag" ), name + " switches in one step" );
			assert.equal(
				scene.controls.find( c => c.id === "main-popup-drag" ).label,
				name,
				"the new panel is committed, not retained old content"
			);
			f.ui.event( { kind: "activate", id: "select-window:" + name } );
			scene = f.rawStep( f.state, 1401 ) ?? scene;
			assert.ok(
				scene.controls.some( c => c.id === "main-popup-drag" ),
				"same-tab selection keeps the popup open"
			);
		}
		assert.equal(
			f.textures.length,
			published,
			"switching prepared panels causes no upload or release: " +
				f.textures.slice( published ).map( ( [p] ) => p ).join( "," )
		);
		f.ui.event( { kind: "focus", id: "select-window:Inventory" } );
		f.ui.event( { kind: "key", code: "KeyI" } );
		scene = f.rawStep( f.state, 1500 );
		assert.ok( !scene.controls.some( c => c.id === "main-popup-drag" ) );
		f.ui.event( { kind: "key", code: "KeyI" } );
		scene = f.rawStep( f.state, 1501 );
		assert.ok( scene.controls.some( c => c.id === "main-popup-drag" ) );
		assert.equal( f.textures.length, published );
	} finally {
		f.dispose();
	}
});

test("inactive UI textures evict by residency pressure and rehydrate after eviction", () => {
	const f = assetFixture();
	for ( let i = 0; i < 4; i++ ) {
		const path = "/" + i + ".png";
		f.resources.step( [ path ], i * 2 );
		f.results.set( i + 1, { kind: "image", image: { width: 2048, height: 2048 } } );
		f.resources.step( [ path ], i * 2 + 1 );
	}
	assert.equal( f.resources.has( "/0.png" ), false );
	assert.equal( f.resources.has( "/1.png" ), true );
	assert.deepEqual( f.published.filter( ( [, image] ) => image === null ).map( ( [path] ) => path ), [ "/0.png" ] );
	f.resources.step( [ "/0.png" ], 10 );
	assert.equal( f.requests.length, 5 );
	f.resources.dispose();
	assert.deepEqual( f.cancelled, [ 5 ] );
});

test("Academy main popup and matching board have separate native entry lifecycles", () => {
	/** @type {any[]} */ const sent = [];
	const f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = { ...f.state.gameplay, academy: { member: false, request: null, rows: [], page: 0 } };
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyL" } );
		f.ui.step( f.state, 1300 );
		assert.equal( f.ui.stats().panel, "Academy" );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "activate", id: "open-window:Academy Matching" } );
		f.ui.step( f.state, 1400 );
		assert.equal( sent.filter( c => c.command?.kind === "academy-page" ).length, 1 );
		f.ui.event( { kind: "activate", id: "close" } );
		f.state.gameplay = {
			...f.state.gameplay,
			academy: { ...f.state.gameplay.academy, request: { kind: "page", id: 0 } }
		};
		f.ui.step( f.state, 1500 );
		f.ui.event( { kind: "activate", id: "open-window:Academy Matching" } );
		assert.equal( sent.filter( c => c.command?.kind === "academy-page" ).length, 1 );
	} finally {
		f.dispose();
	}
});

test("the player panel selects the local character and frames itself while selected", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	const framed = () => f.scenes.at( -1 ).quads.some( q => q.texture?.endsWith( "/playerminiinfo/pmi_select.png" ) );
	try {
		let semantics;
		for ( let t = 0; t < 1200; t += 100 ) semantics = f.ui.step( f.state, t ) ?? semantics;
		const panel = semantics.controls.find( c => c.id === "self-target" );
		assert.ok( panel );
		// The panel's own buttons stay above it.
		const order = id => semantics.controls.findIndex( c => c.id === id );
		assert.ok( order( "ability-details" ) > order( "self-target" ) );
		assert.equal( framed(), false );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: "self-target" } );
		assert.deepEqual( sent.map( c => c.command ), [ { kind: "select", gid: f.state.gameplay.localGid } ] );
		f.state.gameplay = { ...f.state.gameplay, target: f.state.gameplay.localGid };
		for ( let t = 1200; t < 2400; t += 100 ) f.ui.step( f.state, t );
		assert.equal( framed(), true );
	} finally {
		f.dispose();
	}
});

test("contextual Shop entry leaves Alchemy through the same lifecycle; locked Magic Pop rejects entry", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = { ...f.state.gameplay, target: 99 };
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: "open-window:Alchemy" } );
		f.ui.event( { kind: "activate", id: "open-window:Alchemy" } );
		assert.deepEqual( sent.map( c => c.command?.kind ), [ "alchemy-open" ] );
		f.ui.event( { kind: "activate", id: "shop-open" } );
		assert.deepEqual(
			sent.map( c => c.command?.kind ),
			[ "alchemy-open", "shop-open" ],
			"opening waits for authoritative shop admission"
		);
		f.state.gameplay = { ...f.state.gameplay, shop: { npc: 99, offers: [] }, shopCompletionRevision: 1 };
		f.ui.step( f.state, 1250 );
		assert.deepEqual( sent.map( c => c.command?.kind ), [ "alchemy-open", "shop-open", "alchemy-close" ] );
		f.ui.event( { kind: "activate", id: "open-window:Magic Pop" } );
		f.state.gameplay = { ...f.state.gameplay, gacha: { visible: true, phase: "rolling" } };
		f.ui.step( f.state, 1300 );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: "shop-open" } );
		f.ui.event( { kind: "activate", id: "open-window:Alchemy" } );
		assert.deepEqual( sent, [], "rejected transitions must not send entry/exit commands" );
	} finally {
		f.dispose();
	}
});

test("a new inventory icon keeps the previous popup visible and navigation available until ready", () => {
	let blocked = true;
	const f = uiFixture( () => {}, path => blocked && path.endsWith( "/hp_potion_01.png" ) );
	try {
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyC" } );
		f.ui.step( f.state, 1300 );
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, quantity: 1, refObjId: 1, icon: "item/etc/hp_potion_01.ddj" } ]
		};
		f.ui.event( { kind: "activate", id: "select-window:Inventory" } );
		let scene = f.ui.step( f.state, 1400 );
		assert.equal( scene.controls.find( c => c.id === "main-popup-drag" ).label, "Character" );
		assert.ok( !scene.controls.find( c => c.id === "select-window:Character" ).disabled );
		assert.ok( !scene.controls.find( c => c.id === "close" ).disabled );
		blocked = false;
		for ( let t = 1500; t < 1900; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		assert.equal( scene.controls.find( c => c.id === "main-popup-drag" ).label, "Inventory" );
		assert.ok( scene.controls.some( c => c.id === "slot:13" && !c.disabled ) );
	} finally {
		f.dispose();
	}
});

test("System menu inserts Experimental below Options using the authored buttons", () => {
	const f = uiFixture();
	try {
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "Escape" } );
		const scene = f.ui.step( f.state, 1300 );
		const buttons = scene.controls.filter( c =>
			[
				"open-window:Option",
				"open-window:Experimental",
				"open-window:Game Guide",
				"system-restart",
				"system-exit"
			].includes( c.id )
		);
		assert.deepEqual( buttons.map( c => c.label ), [ "Option", "Experimental", "Help", "Restart", "Exit" ] );
		assert.deepEqual( buttons.map( c => c.rect ), [ 385, 419, 453, 487, 521 ].map( y => [ 724, y, 152, 24 ] ) );
		assert.ok( !scene.controls.some( c => c.id === "disconnect" || c.id === "logout" ) );
		f.ui.event( { kind: "activate", id: "system-restart" } );
		const restarted = f.ui.step( f.state, 1400 );
		assert.equal(
			restarted.controls.filter( c => c.id === "system-restart" ).length,
			0,
			"Restart hides the System menu like retail sub_5d03e0"
		);
	} finally {
		f.dispose();
	}
});

test("a cold System window admits its frame, buttons and input together after the last texture", () => {
	let blocked = true;
	const f = uiFixture( () => {}, path => blocked && path.endsWith( "/system/sys_button.png" ) );
	try {
		let scene;
		for ( let t = 0; t < 1200; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		f.ui.event( { kind: "key", code: "Escape" } );
		scene = f.ui.step( f.state, 1300 ) ?? scene;
		assert.ok( !scene.controls.some( c => c.id === "system-drag" || c.id === "open-window:Option" ) );
		assert.ok( !f.hasText( "Restart" ) );
		blocked = false;
		for ( let t = 1400; t < 1800; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		assert.ok( scene.controls.some( c => c.id === "system-drag" ) );
		assert.equal(
			scene.controls.filter( c =>
				[ "open-window:Option", "open-window:Game Guide", "system-restart", "system-exit" ].includes( c.id )
			).length,
			4
		);
	} finally {
		f.dispose();
	}
});

test("monster names respect native distance and ownership gates, with hover and selection styles", () => {
	const f = uiFixture();
	try {
		const monster = {
			gid: 99,
			regionId: 1,
			x: 299,
			y: 0,
			z: 0,
			heading: 0,
			kind: "monster",
			name: "Mangyang",
			appearanceState: [ 1, 0, 0 ]
		};
		f.state.entities.push( monster );
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		const glyphs = () =>
			f.scenes.at( -1 ).quads.filter( q => q.characterAnchor === 99 && q.texture === fontAtlas.image );
		assert.ok( glyphs().length > 0 );
		const normal = glyphs().map( q => q.uv );
		f.state.gameplay = { ...f.state.gameplay, target: 99 };
		f.ui.step( f.state, 1300 );
		assert.notDeepEqual( glyphs().map( q => q.uv ), normal, "selection uses the bold font" );
		monster.x = 300;
		f.ui.step( f.state, 1400 );
		assert.equal( glyphs().length, 0 );
		f.state.hoveredEntity = 99;
		f.ui.step( f.state, 1500 );
		assert.ok( glyphs().length > 0 );
		assert.ok(
			f.scenes.at( -1 ).quads.some( q => q.characterAnchor === 99 && q.texture === "" && q.rect[3] === 1 )
		);
		f.state.hoveredEntity = null;
		monster.x = 100;
		monster.appearanceState = [ 2, 0, 0 ];
		f.ui.step( f.state, 1600 );
		assert.ok( glyphs().length > 0, "native name gate is ownership, not LIFE" );
		// 85E2E0 hides a monster or COS name only while it is ridden
		// (CICharactor_GetMountedHorseOrVehicle): an owned pet keeps its name.
		monster.ownerGid = 1;
		f.ui.step( f.state, 1700 );
		assert.ok( glyphs().length > 0, "an owned, unridden COS keeps its name" );
		f.state.entities[0] = { ...f.state.entities[0], mountedOn: 99 };
		f.ui.step( f.state, 1800 );
		assert.equal( glyphs().length, 0, "the ridden COS hides its name" );
	} finally {
		f.dispose();
	}
});

test("inventory separates equipment and bag pages and double-click uses the selected item", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 58,
			equipmentSlotCount: 13,
			inventory: [ {
				slot: 13,
				refObjId: 1,
				typeFlags: 0x6c,
				quantity: 3,
				name: "Potion",
				icon: "item/etc/hp_potion_01.ddj"
			} ]
		};
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyI" } );
		let scene = f.ui.step( f.state, 1300 );
		assert.equal( scene.controls.filter( c => c.id.startsWith( "slot:" ) && !c.disabled ).length, 45 );
		assert.ok( scene.controls.some( c => c.id === "slot:13" && c.rect[2] === 32 ) );
		assert.ok( !scene.controls.some( c => c.id === "use" ) );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( sent.at( -1 ), { kind: "gameplay", command: { kind: "item-use", slot: 13 } } );
		f.ui.event( { kind: "activate", id: "inventory-page:1" } );
		scene = f.ui.step( f.state, 1400 );
		assert.deepEqual(
			scene.controls.filter( c => c.id.startsWith( "slot:" ) && !c.disabled && Number( c.id.slice( 5 ) ) >= 13 )
				.map( c => Number( c.id.slice( 5 ) ) ),
			Array.from( { length: 13 }, ( _, i ) => 45 + i )
		);
	} finally {
		f.dispose();
	}
});

test("a beta skill point count stays inside the underbar's 48-pixel GDR_STATIC_SP", () => {
	const f = uiFixture();
	try {
		f.state.gameplay = { ...f.state.gameplay, progression: { level: 10, masteries: [], skillPoints: 123456789 } };
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		assert.equal( f.hasText( "123456789" ), false, "the full nine digits overflow the gauge" );
		// With the shipped font "123456K" is wider than 48 pixels, so the first
		// candidate that fits is whole millions.
		assert.equal( f.hasText( "123456K" ), false );
		assert.equal( f.hasText( "123M" ), true, "the first shortened form that fits" );
		f.state.gameplay = { ...f.state.gameplay, progression: { level: 10, masteries: [], skillPoints: 1234 } };
		for ( let t = 1200; t < 2400; t += 100 ) f.ui.step( f.state, t );
		assert.equal( f.hasText( "1234" ), true, "a count that fits is the native %d" );
	} finally {
		f.dispose();
	}
});

test("death prompt opens 3 s after the death state, never at LIFE ingress, and routes native choices", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		// LIFE=dead arms only CICharactor timer 0xE (5000 ms, 777BFE). The fatal impact, or zero HP with no
		// pending fatal (77A33A), enters the death state; CICPlayer timer 0xF opens the prompt 3000 ms later.
		const vitals = deathState => {
			f.state.gameplay = {
				...f.state.gameplay,
				vitals: [ { gid: 1, hp: 0, mp: 0, ...(deathState ? { deathState: true } : {}) } ]
			};
		};
		const life = state => {
			f.state.entities = [ { ...f.state.entities[0], appearanceState: [ state, 0, 0 ] } ];
		};
		// step() returns null when a frame renders identically; keep the latest rendered semantics.
		/** @type {any} */ let latest = null;
		const step = now => (latest = f.ui.step( f.state, now ) ?? latest),
			prompt = now => step( now ).controls.some( c => c.id === "rebirth-point" );
		f.state.gameplay = { ...f.state.gameplay, progression: { level: 10, masteries: [] } };
		vitals( false );
		for ( let t = 0; t < 1200; t += 100 ) step( t );
		assert.equal( prompt( 1200 ), false, "zero HP alone is not LIFE-dead" );
		life( 2 );
		assert.equal( prompt( 1300 ), false, "LIFE arrives with the windup, before the blow lands" );
		f.ui.event( { kind: "activate", id: "rebirth-point" } );
		assert.deepEqual( sent, [], "an unopened prompt accepts no choice" );
		vitals( true );
		assert.equal( prompt( 2000 ), false, "the fatal impact lands and enters the death state" );
		assert.equal( prompt( 4999 ), false );
		assert.equal( prompt( 5000 ), true, "timer 0xF: 3000 ms after the death state" );
		let scene = latest;
		assert.deepEqual( defined( scene ).controls.filter( c => c.id.startsWith( "rebirth-" ) ).map( c => c.id ), [
			"rebirth-body",
			"rebirth-drag",
			"rebirth-point",
			"rebirth-alternate"
		] );
		assert.ok( defined( scene ).controls.some( c => c.id === "chat-text" ) );
		assert.equal( f.ui.blocks( 500, 300 ), false, "camera gestures outside the dialog remain available" );
		assert.equal( f.ui.blocks( 610, 400 ), true, "the dialog itself consumes pointer input" );
		assert.equal( f.hasText( "<sml2>" ), false );
		assert.ok( f.hasText( "paralyzed." ) );
		assert.ok(
			f.scenes.at( -1 ).quads.some( q =>
				q.color[0] === 1 && q.color[1] === 217 / 255 && q.color[2] === 83 / 255
			),
			"authored gold color is retained"
		);
		f.ui.event( { kind: "drag", id: "rebirth-drag", dx: 100, dy: 40 } );
		scene = step( 5010 );
		assert.deepEqual( defined( scene ).controls.find( c => c.id === "rebirth-body" ).rect, [ 700, 385, 400, 210 ] );
		f.ui.event( { kind: "drag", id: "rebirth-drag", dx: 9999, dy: -9999 } );
		scene = step( 5020 );
		assert.deepEqual( defined( scene ).controls.find( c => c.id === "rebirth-body" ).rect, [ 1200, 0, 400, 210 ] );
		f.ui.event( { kind: "key", code: "Enter" } );
		scene = step( 5030 );
		assert.equal( defined( scene ).focusRequest.id, "chat-text" );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "activate", id: "rebirth-alternate" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "rebirth", choice: 2 } } ] );
		scene = step( 5100 );
		assert.ok(
			defined( scene ).controls.filter( c => c.id === "rebirth-point" || c.id === "rebirth-alternate" ).every(
				c => !c.disabled
			)
		);
		f.ui.event( { kind: "activate", id: "rebirth-point" } );
		assert.equal( sent.length, 2 );
		life( 1 );
		vitals( false );
		f.state.gameplay = { ...f.state.gameplay, progression: { level: 11, masteries: [] } };
		step( 5200 );
		life( 2 );
		vitals( true );
		assert.equal( prompt( 5300 ), false );
		assert.equal( prompt( 8300 ), true );
		f.ui.event( { kind: "activate", id: "rebirth-alternate" } );
		scene = step( 8400 );
		assert.equal( sent.length, 2, "high-level alternate waits for rescue without sending choice 2" );
		assert.ok( !defined( scene ).controls.some( c => c.id === "rebirth-point" ) );
		f.ui.event( { kind: "world-select", gid: 2 } );
		assert.equal( prompt( 8410 ), false, "another corpse cannot reopen the local prompt" );
		f.ui.event( { kind: "world-select", gid: 1 } );
		assert.equal( prompt( 8420 ), true, "selecting self reopens the dismissed rescue prompt" );
		assert.deepEqual( defined( latest ).controls.find( c => c.id === "rebirth-body" ).rect, [
			600,
			345,
			400,
			210
		] );
		f.ui.event( { kind: "drag", id: "rebirth-drag", dx: 100, dy: 40 } );
		step( 8430 );
		f.ui.event( { kind: "world-select", gid: 1 } );
		step( 8440 );
		assert.deepEqual(
			defined( latest ).controls.find( c => c.id === "rebirth-body" ).rect,
			[ 700, 385, 400, 210 ],
			"an existing type 3 dialog is retained"
		);
		f.ui.event( { kind: "activate", id: "rebirth-alternate" } );
		assert.equal( prompt( 8450 ), false );
		step( 8460 );
		f.ui.event( { kind: "world-select", gid: 1 } );
		assert.equal( prompt( 8470 ), true, "a silent resurrection refusal must not block corpse selection" );
		step( 8480 );
		f.ui.event( { kind: "world-select", gid: 1 } );
		assert.equal( prompt( 8490 ), true );
		assert.deepEqual(
			defined( latest ).controls.find( c => c.id === "rebirth-body" ).rect,
			[ 600, 345, 400, 210 ],
			"a new prompt is centered"
		);
		assert.equal( sent.length, 2, "corpse selection and high-level rescue dismissal are local UI actions" );
		life( 1 );
		vitals( false );
		step( 8500 );
		f.ui.event( { kind: "world-select", gid: 1 } );
		assert.equal( prompt( 8510 ), false, "living self-selection does not open rebirth" );
		life( 2 );
		assert.equal( prompt( 8600 ), false );
		assert.equal( prompt( 16599 ), false );
		assert.equal( prompt( 16600 ), true, "no presented impact: LIFE + 5000 ms (timer 0xE) + 3000 ms" );
		assert.deepEqual(
			defined( latest ).controls.find( c => c.id === "rebirth-body" ).rect,
			[ 600, 345, 400, 210 ],
			"each new death resets the dialog position"
		);
		life( 1 );
		step( 16700 );
		life( 2 );
		assert.equal( prompt( 16800 ), false );
		f.ui.event( { kind: "world-select", gid: 1 } );
		assert.equal( prompt( 16801 ), true, "explicit selection bypasses the automatic death timer" );
	} finally {
		f.dispose();
	}
});

test("portrait selection immediately restores the dead player's prompt without requesting resurrection", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		/** @type {any} */ let latest;
		const step = now => (latest = f.ui.step( f.state, now ) ?? latest);
		const prompt = () => latest.controls.find( control => control.id === "rebirth-body" );
		f.state.gameplay = { ...f.state.gameplay, progression: { level: 11, masteries: [] } };
		for ( let now = 0; now <= 1100; now += 100 ) step( now );
		f.ui.event( { kind: "activate", id: "self-target" } );
		step( 1200 );
		assert.equal( prompt(), undefined, "living portrait selection cannot open rebirth" );
		f.state.entities = [ { ...f.state.entities[0], appearanceState: [ 2, 0, 0 ] } ];
		f.state.gameplay = { ...f.state.gameplay, vitals: [ { gid: 1, hp: 0, mp: 0 } ] };
		step( 1300 );
		assert.equal( prompt(), undefined, "the automatic death timer has not elapsed" );
		f.ui.event( { kind: "activate", id: "self-target" } );
		step( 1301 );
		assert.ok( prompt(), "dead portrait selection must bypass the automatic timer" );
		assert.deepEqual( prompt().rect, [ 600, 345, 400, 210 ] );
		f.ui.event( { kind: "drag", id: "rebirth-drag", dx: 100, dy: 40 } );
		step( 1310 );
		f.ui.event( { kind: "activate", id: "self-target" } );
		step( 1320 );
		assert.deepEqual( prompt().rect, [ 700, 385, 400, 210 ], "an existing prompt keeps its position" );
		f.ui.event( { kind: "activate", id: "rebirth-alternate" } );
		step( 1330 );
		assert.equal( prompt(), undefined, "the level-11 alternate choice dismisses locally" );
		f.ui.event( { kind: "activate", id: "self-target" } );
		step( 1340 );
		assert.deepEqual( prompt().rect, [ 600, 345, 400, 210 ], "reopening creates a centered prompt" );
		f.state.entities = [ { ...f.state.entities[0], appearanceState: [ 1, 0, 0 ] } ];
		f.state.gameplay = { ...f.state.gameplay, vitals: [ { gid: 1, hp: 100, mp: 0 } ] };
		step( 1400 );
		f.ui.event( { kind: "activate", id: "self-target" } );
		step( 1410 );
		assert.equal( prompt(), undefined, "revival clears the explicit prompt request" );
		assert.ok( sent.length > 0 );
		assert.ok(
			sent.every( command =>
				command.kind === "gameplay" && command.command.kind === "select" &&
				command.command.gid === 1
			),
			"portrait activation never requests resurrection"
		);
	} finally {
		f.dispose();
	}
});

test("a resurrection question retires the death box at native geometry; selecting oneself restores it", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		/** @type {any} */ let latest = null;
		const step = now => (latest = f.ui.step( f.state, now ) ?? latest),
			ids = () => defined( latest ).controls.map( c => c.id );
		f.state.gameplay = {
			...f.state.gameplay,
			progression: { level: 10, masteries: [] },
			vitals: [ { gid: 1, hp: 0, mp: 0, deathState: true } ]
		};
		f.state.entities = [ { ...f.state.entities[0], appearanceState: [ 2, 0, 0 ] } ];
		for ( let t = 0; t <= 3000; t += 100 ) step( t );
		assert.ok( ids().includes( "rebirth-point" ) );
		const confirmCaption = defined( latest ).controls.find( c => c.id === "rebirth-body" ).label;
		// 7644E0 case 3: ClearStateTimer(0xF), retire kinds 3 and 4, open kind 4.
		f.state.gameplay = { ...f.state.gameplay, social: { invitation: null, resurrection: { gid: 2 } } };
		step( 3100 );
		assert.deepEqual( ids(), [
			"resurrection-body",
			"resurrection-drag",
			"resurrection-accept",
			"resurrection-refuse"
		], "the question retires the death box and holds the HUD" );
		const body = defined( latest ).controls.find( c => c.id === "resurrection-body" );
		// 5C82D0 centres 308x148 (1600x900: 646,376); 52F460 case 3 resizes to 400x210.
		assert.deepEqual( body.rect, [ 646, 376, 400, 210 ] );
		assert.ok( body.label && body.label !== confirmCaption, "kind 4 carries the agreement caption" );
		assert.deepEqual(
			defined( latest ).controls.filter( c => c.id.startsWith( "resurrection-" ) && c.kind === "button" ).map(
				c => [ c.id, c.label, c.rect ]
			),
			[ [ "resurrection-accept", "Yes", [ 769, 544, 76, 24 ] ], [ "resurrection-refuse", "No", [
				849,
				544,
				76,
				24
			] ] ]
		);
		assert.ok( f.hasText( "The warm light is hovering around your body." ) );
		assert.ok( f.hasText( "You feel the soul entering your body." ) );
		assert.ok( f.hasText( "Will you resurrect yourself to venture again?" ) );
		f.ui.event( { kind: "drag", id: "resurrection-drag", dx: 40, dy: -30 } );
		step( 3110 );
		assert.deepEqual( defined( latest ).controls.find( c => c.id === "resurrection-accept" ).rect, [
			809,
			514,
			76,
			24
		] );
		f.ui.event( { kind: "activate", id: "logout" } );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "activate", id: "resurrection-refuse" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "resurrection-consent", accept: false }
		} );
		// The refusal closes the slot; nothing reopens the death box by itself.
		f.state.gameplay = { ...f.state.gameplay, social: { invitation: null } };
		for ( let t = 3200; t <= 9000; t += 200 ) step( t );
		assert.ok( !ids().includes( "rebirth-point" ), "the retired death box stays closed" );
		// 6813E0: selecting oneself while dead opens kind 3 again.
		f.ui.event( { kind: "world-select", gid: 1 } );
		step( 9100 );
		assert.ok( ids().includes( "rebirth-point" ), "selecting oneself restores the death box" );
		// 7644E0 case 7: an rmut revival asks the mutation question instead.
		f.state.gameplay = {
			...f.state.gameplay,
			social: { invitation: null, resurrection: { gid: 3, mutation: true } }
		};
		step( 9200 );
		assert.ok( !ids().includes( "rebirth-point" ), "the mutation question retires the death box too" );
		assert.ok( ids().includes( "resurrection-accept" ) );
		assert.ok( !f.hasText( "The warm light is hovering around your body." ) );
		f.ui.event( { kind: "activate", id: "resurrection-accept" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "resurrection-consent", accept: true }
		} );
	} finally {
		f.dispose();
	}
});

test("an invitation box and the resurrection question stay open and answerable together", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.ui.step( f.state, 0 );
		const invited = { invitation: { type: 2, gid: 1, options: 0 } },
			both = { invitation: { type: 2, gid: 1, options: 0 }, resurrection: { gid: 2 } },
			at = ( social, now ) => f.ui.step( { ...f.state, gameplay: { ...f.state.gameplay, social } }, now ),
			buttons = semantic => semantic.controls.filter( c => c.kind === "button" ).map( c => [ c.id, c.rect ] );
		const alone = buttons( at( invited, 100 ) );
		assert.deepEqual( alone.map( ( [id] ) => id ), [ "invite-accept", "invite-refuse" ] );
		const raced = buttons( at( both, 200 ) );
		assert.deepEqual( raced.map( ( [id] ) => id ), [
			"invite-accept",
			"invite-refuse",
			"resurrection-accept",
			"resurrection-refuse"
		] );
		assert.deepEqual( raced.slice( 0, 2 ), alone, "the invitation box keeps its native controls" );
		f.ui.event( { kind: "activate", id: "invite-refuse" } );
		f.ui.event( { kind: "activate", id: "resurrection-accept" } );
		assert.deepEqual( sent, [
			{ kind: "gameplay", command: { kind: "social-consent", accept: false } },
			{ kind: "gameplay", command: { kind: "resurrection-consent", accept: true } }
		] );
		const question = buttons( at( { invitation: null, resurrection: { gid: 2 } }, 300 ) );
		assert.deepEqual( question.map( ( [id] ) => id ), [ "resurrection-accept", "resurrection-refuse" ] );
		assert.deepEqual( buttons( at( invited, 400 ) ), alone, "the question leaves the invitation box as it was" );
	} finally {
		f.dispose();
	}
});

test("mission overlay visibility follows the loading presentation, including its completed frame hold", () => {
	const f = uiFixture();
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			academy: { member: false },
			progression: { level: 1 },
			guide: { event: 2, seenMask: 2, country: 0 }
		};
		f.state.entities[0].visualFlags = 1;
		const loading = {
			...f.state,
			frontend: { phase: "loading-world", generation: 1 },
			worldReady: false,
			loadingProgress: .5
		};
		for ( let now = 0; now < 100; now += 10 ) f.ui.step( loading, now );
		assert.ok( !f.scenes.at( -1 ).quads.some( q => q.characterAnchor !== undefined ) );
		let semantic = f.ui.step( { ...loading, worldReady: true, loadingProgress: 1 }, 100 );
		assert.ok( !semantic?.controls.some( c => c.id === "academy-open" || c.id === "guide-indicator" ) );
		const ready = { ...f.state, frontend: { phase: "world", generation: 1 } };
		for ( let now = 200; now < 1200; now += 100 ) semantic = f.ui.step( ready, now ) ?? semantic;
		assert.ok( semantic.controls.some( c => c.id === "academy-open" ) );
		assert.ok( f.hasText( "Player" ) );
		assert.ok( f.scenes.at( -1 ).quads.some( q => q.characterAnchor === 1 && q.texture === fontAtlas.image ) );
	} finally {
		f.dispose();
	}
});

test("chat caret remains inside the edit for long text, selection and composition; tab captions clear the lamps", () => {
	const f = uiFixture();
	try {
		let semantic;
		for ( let now = 0; now < 1000; now += 100 ) semantic = f.ui.step( f.state, now ) ?? semantic;
		const edit = semantic.controls.find( c => c.id === "chat-text" ).rect;
		f.ui.event( { kind: "focus", id: "chat-text" } );
		for (
			const [start, end, composing] of [ [ 100, 100, false ], [ 90, 100, false ], [ 90, 100, true ], [
				0,
				0,
				false
			] ]
		) {
			f.ui.event( { kind: "edit", id: "chat-text", value: "W".repeat( 100 ), start, end, composing } );
			f.ui.step( { ...f.state }, 1100 );
			const caret = f.scenes.at( -1 ).quads.find( q =>
				q.texture === "" && q.rect[2] === 2 && q.rect[3] === 11 && q.clip[0] === edit[0]
			);
			assert.ok( caret );
			assert.ok( caret.rect[0] >= edit[0] && caret.rect[0] < edit[0] + edit[2] );
			assert.deepEqual( caret.clip, edit );
		}
		for ( const control of semantic.controls.filter( c => c.id.startsWith( "chat-tab:" ) ) ) {
			const captions = f.scenes.at( -1 ).quads.filter( q =>
				q.texture === fontAtlas.image && q.clip[1] === control.rect[1] + 4 && q.clip[0] >= control.rect[0] &&
				q.clip[0] < control.rect[0] + control.rect[2]
			);
			assert.ok( captions.length );
			assert.ok(
				captions.every( q =>
					q.rect[0] >= control.rect[0] + 9 && q.clip[0] + q.clip[2] <= control.rect[0] + control.rect[2]
				)
			);
		}
	} finally {
		f.dispose();
	}
});

test("chat tabs focus their prefixed draft; missing membership emits native status text without sending", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		for ( let now = 0; now < 1000; now += 100 ) f.ui.step( f.state, now );
		for (
			const [tab, prefix, key] of [ [ 1, "#", "Cannot find the relevant party" ], [
				2,
				"@",
				"Cannot carry this function out because you do not belong to the guild."
			] ]
		) {
			f.ui.event( { kind: "activate", id: "chat-tab:" + tab } );
			const semantic = f.ui.step( { ...f.state }, 1100 );
			assert.equal( semantic.controls.find( c => c.id === "chat-text" ).value, prefix );
			assert.equal( semantic.focusRequest.id, "chat-text" );
			f.ui.event( {
				kind: "edit",
				id: "chat-text",
				value: prefix + "hello",
				start: 6,
				end: 6,
				composing: false
			} );
			f.ui.event( { kind: "activate", id: "chat-send" } );
			f.ui.step( f.state, 1200 );
			assert.ok( f.hasText( key.split( " " ).slice( 0, 4 ).join( " " ) ) );
			assert.equal( sent.filter( c => c.command?.kind === "chat" ).length, 0 );
		}
	} finally {
		f.dispose();
	}
});

test("guild-war notice modal owns input, dismisses locally and reopens only for a new notice", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		for ( let now = 0; now < 1000; now += 100 ) f.rawStep( { ...f.state }, now );
		const notice = {
			sequence: 1,
			key: "",
			value: 0,
			bannerOnly: true,
			dialog: {
				title: "UIIT_STT_EVENTGUIDE",
				lines: [ "UIIT_MSG_GUILDWAR_SUGGESTIONS_01", "UIIT_MSG_GUILDWAR_SUGGESTIONS_02" ]
			}
		};
		f.state.gameplay = { ...f.state.gameplay, notices: [ notice ] };
		let scene = f.rawStep( { ...f.state }, 1100 );
		const confirm = () => scene.controls.find( c => c.id === "notice-dialog-confirm" );
		assert.ok( confirm() );
		assert.equal( f.ui.blocks( 20, 20 ), true );
		const original = [ ...confirm().rect ];
		for ( const code of [ "Enter", "NumpadEnter", "Escape", "KeyI" ] ) f.ui.event( { kind: "key", code } );
		for ( const id of [ "logout", "chat-send", "open-window:Inventory" ] ) f.ui.event( { kind: "activate", id } );
		scene = f.rawStep( { ...f.state }, 1110 ) ?? scene;
		assert.ok( confirm() );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "drag", id: "notice-dialog-drag", dx: 20, dy: 10 } );
		scene = f.rawStep( { ...f.state }, 1120 );
		assert.deepEqual( confirm().rect, [ original[0] + 20, original[1] + 10, 76, 24 ] );
		f.ui.event( { kind: "activate", id: "notice-dialog-confirm" } );
		scene = f.rawStep( { ...f.state }, 1130 );
		assert.equal( confirm(), undefined );
		assert.deepEqual( sent, [] );
		scene = f.rawStep( { ...f.state }, 1140 ) ?? scene;
		assert.equal( confirm(), undefined );
		f.state.gameplay = { ...f.state.gameplay, notices: [ notice, { ...notice, sequence: 2 } ] };
		scene = f.rawStep( { ...f.state }, 1150 );
		assert.deepEqual( confirm().rect, original );
	} finally {
		f.dispose();
	}
});

test("disconnect uses native confirmation geometry and blocks stale world controls without raw error text", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		for ( let now = 0; now < 1000; now += 100 ) f.ui.step( f.state, now );
		const state = {
			...f.state,
			session: { ...f.state.session, phase: "disconnected", error: "Error: Server ended transport session" }
		};
		const result = f.ui.step( state, 1100 );
		assert.deepEqual( result.controls.filter( c => c.kind === "button" ).map( c => [ c.id, c.rect ] ), [ [
			"disconnect-confirm",
			[ 758, 475, 76, 24 ]
		] ] );
		f.ui.event( { kind: "drag", id: "disconnect-drag", dx: 80, dy: 30 } );
		const moved = f.ui.step( state, 1110 );
		assert.deepEqual( moved.controls.find( c => c.id === "disconnect-confirm" ).rect, [ 838, 505, 76, 24 ] );
		assert.equal( result.message, "Disconnected from the server." );
		assert.ok( f.hasText( "Confirmation window" ) );
		assert.ok( f.hasText( "Disconnected from the server." ) );
		assert.equal( f.hasText( "Error: Server ended transport session" ), false );
		assert.equal( f.ui.stats().error, state.session.error );
		assert.equal( f.ui.blocks( 20, 20 ), true );
		for ( const id of [ "reconnect", "logout", "chat-send" ] ) f.ui.event( { kind: "activate", id } );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "key", code: "NumpadEnter" } );
		assert.deepEqual( sent, [ { kind: "logout" } ] );
	} finally {
		f.dispose();
	}
});

test("world and chat implementation errors remain diagnostic, not floating game text", () => {
	const f = uiFixture();
	try {
		const state = {
			...f.state,
			gameplay: {
				...f.state.gameplay,
				error: "Internal movement failure",
				chat: { lines: [], error: "Internal chat failure" }
			}
		};
		let result;
		for ( let now = 0; now < 1000; now += 100 ) result = f.ui.step( state, now ) ?? result;
		assert.equal( f.ui.stats().error, "Internal movement failure" );
		assert.equal( f.hasText( "Internal movement failure" ), false );
		assert.equal( f.hasText( "Internal chat failure" ), false );
		assert.doesNotMatch( result.message, /Internal/ );
	} finally {
		f.dispose();
	}
});
test("message scrolling uses current layout and a modal blocks captured background drags", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.chat = {
			lines: Array.from(
				{ length: 40 },
				( _, i ) => ({ channel: 1, text: "Message " + i, name: "Player", outgoing: false })
			)
		};
		let semantic;
		for ( let now = 0; now < 1000; now += 100 ) semantic = f.ui.step( f.state, now ) ?? semantic;
		const thumb = () => semantic.controls.find( c => c.id === "chat-scroll-thumb" ).rect[1];
		const initial = thumb();
		for ( let i = 0; i < 20; i++ ) f.ui.event( { kind: "drag", id: "chat-scroll-thumb", dx: 0, dy: -1 } );
		semantic = f.ui.step( f.state, 1100 ) ?? semantic;
		assert.ok( thumb() < initial, "sub-row drag input moves the real HUD thumb" );
		const scrolled = thumb();
		const modal = { ...f.state, gameplay: { ...f.state.gameplay, social: { invitation: { type: 1, gid: 1 } } } };
		f.ui.step( modal, 1200 );
		f.ui.event( { kind: "drag", id: "chat-scroll-thumb", dx: 0, dy: 50 } );
		semantic = f.ui.step( f.state, 1300 ) ?? semantic;
		assert.equal( thumb(), scrolled, "modal owns input even if an old thumb was captured" );
	} finally {
		f.dispose();
	}
});

test("party proposal projects native controls, blocks background input and preserves response ownership", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		const invite = { ...f.state, gameplay: { ...f.state.gameplay, social: { invitation: { type: 2, gid: 1 } } } };
		let semantic = f.ui.step( invite, 100 );
		assert.deepEqual(
			semantic.controls.filter( c => c.kind === "button" ).map( c => [ c.id, c.label, c.rect[2], c.rect[3] ] ),
			[ [ "invite-accept", "Accept", 76, 24 ], [ "invite-refuse", "Refuse", 76, 24 ] ]
		);
		f.ui.event( { kind: "drag", id: "invite-drag", dx: 90, dy: -20 } );
		semantic = f.ui.step( invite, 110 );
		assert.deepEqual( semantic.controls.find( c => c.id === "invite-accept" ).rect, [ 812, 481, 76, 24 ] );
		assert.ok( f.hasText( "[Player]has" ) );
		assert.ok( f.hasText( "Party application success. Will you accept it?" ) );
		assert.equal( f.ui.blocks( 100, 100 ), true );
		f.ui.event( { kind: "activate", id: "logout" } );
		f.ui.event( { kind: "activate", id: "submit" } );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "activate", id: "invite-accept" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "social-consent", accept: true } } ] );
		semantic = f.ui.step( invite, 200 );
		assert.ok(
			semantic === null || semantic.controls.some( c => c.id === "invite-accept" ),
			"UI does not fabricate server dismissal"
		);
		semantic = f.ui.step( { ...f.state, gameplay: { ...f.state.gameplay, social: { invitation: null } } }, 300 );
		assert.ok( semantic.controls.some( c => c.id === "close" ), "underlying inventory remains open" );
		f.ui.step( invite, 400 );
		f.ui.event( { kind: "activate", id: "invite-refuse" } );
		assert.deepEqual( sent.at( -1 ), { kind: "gameplay", command: { kind: "social-consent", accept: false } } );
	} finally {
		f.dispose();
	}
});
test("guild proposal uses native agreement text, guild admission and independent drag lifetime", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		for ( let t = 0; t < 1000; t += 100 ) f.ui.step( f.state, t );
		f.state.gameplay = { ...f.state.gameplay, social: { invitation: { type: 5, gid: 1 } } };
		let scene = f.ui.step( { ...f.state }, 1100 );
		assert.ok( !scene?.controls.some( c => c.id === "invite-accept" ), "native requires inviter guild data" );
		f.state.entities = [ { ...f.state.entities[0], guildName: "Silkroad" } ];
		scene = f.ui.step( f.state, 1200 );
		assert.ok( f.hasText( "[Player]has applied for your guild." ) );
		assert.ok( f.hasText( "Do you want to join [Silkroad]guild?" ) );
		assert.deepEqual( scene.controls.filter( c => c.kind === "button" ).map( c => [ c.id, c.label, c.rect ] ), [ [
			"invite-accept",
			"Yes",
			[ 718, 475, 76, 24 ]
		], [ "invite-refuse", "No", [ 798, 475, 76, 24 ] ] ] );
		f.ui.event( { kind: "drag", id: "invite-drag", dx: 50, dy: 25 } );
		scene = f.ui.step( f.state, 1210 );
		assert.equal( scene.controls.find( c => c.id === "invite-accept" ).rect[0], 768 );
		f.ui.event( { kind: "activate", id: "logout" } );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "activate", id: "invite-refuse" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "social-consent", accept: false } } ] );
		f.state.gameplay = { ...f.state.gameplay, social: { invitation: null } };
		f.ui.step( f.state, 1300 );
		f.state.gameplay = { ...f.state.gameplay, social: { invitation: { type: 5, gid: 1 } } };
		scene = f.ui.step( f.state, 1400 );
		assert.equal( scene.controls.find( c => c.id === "invite-accept" ).rect[0], 718 );
	} finally {
		f.dispose();
	}
});

test("modal admission retires a captured System title drag before its handler runs", () => {
	const f = uiFixture();
	try {
		for ( let t = 0; t < 1000; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "Escape" } );
		let scene = f.ui.step( { ...f.state }, 1100 );
		const start = scene.controls.find( c => c.id === "system-drag" ).rect;
		const modal = { ...f.state, gameplay: { ...f.state.gameplay, social: { invitation: { type: 1, gid: 1 } } } };
		f.ui.step( modal, 1200 );
		f.ui.event( { kind: "drag", id: "system-drag", dx: 100, dy: 100 } );
		scene = f.ui.step( f.state, 1300 );
		assert.deepEqual( scene.controls.find( c => c.id === "system-drag" ).rect, start );
	} finally {
		f.dispose();
	}
});

test("title discovers servers once, preselects an operating row and submits with Enter without List", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const state = {
			...f.state,
			session: { phase: "signed-out", revision: 1 },
			gameplay: null,
			entities: [],
			frontend: { phase: "login", generation: 1, elapsed: 1, alpha: 1, logoAlpha: 0, error: null }
		};
		f.ui.step( state, 0 );
		assert.deepEqual( sent, [ { kind: "servers", apiBase: "https://fixture.invalid/" } ] );
		f.ui.step( state, 100 );
		assert.equal( sent.length, 1 );
		f.ui.step( {
			...state,
			session: {
				phase: "signed-out",
				revision: 2,
				servers: [ { id: "offline", operating: false }, { id: "first", operating: true }, {
					id: "second",
					operating: true
				} ]
			}
		}, 200 );
		f.ui.event( { kind: "activate", id: "submit" } );
		assert.equal( sent.length, 1, "empty native edits do not send or invent a message" );
		for ( const [id, value] of [ [ "account", "fixture" ], [ "password", "fixture-only" ] ] ) {
			f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
		}
		f.ui.event( { kind: "activate", id: "submit" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "login",
			apiBase: "https://fixture.invalid/",
			id: "fixture",
			password: "fixture-only",
			serverId: "first"
		} );
		f.ui.event( { kind: "activate", id: "submit" } );
		assert.equal( sent.length, 2, "held/repeated submit cannot duplicate login" );
	} finally {
		f.dispose();
	}
});
test("inventory/action assignment and numeric activation share the gameplay routes", () => {
	const sent = [], f = uiFixture( command => sent.push( command.command ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventoryPending: false,
			inventory: [ { slot: 15, refObjId: 7, typeFlags: 0x8ec, quantity: 3 } ]
		};
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		f.ui.step( f.state, 100 );
		f.ui.event( { kind: "activate", id: "slot:15" } );
		const itemBar = f.ui.step( f.state, 200 ).controls.find( c => c.id === "hotbar:0" );
		f.ui.event( { kind: "drag-end", id: "slot:15", x: itemBar.rect[0] + 2, y: itemBar.rect[1] + 2 } );
		assert.deepEqual( sent.pop(), { kind: "quickslot-set", binding: { slot: 0, kind: 0x46, payload: 2 } } );
		f.state.gameplay.quickSlots = [ { slot: 1, kind: 0x46, payload: 2 } ];
		f.ui.event( { kind: "activate", id: "close" } );
		f.ui.step( f.state, 300 );
		f.ui.event( { kind: "key", code: "Digit1" } );
		assert.deepEqual( sent.pop(), { kind: "item-use", slot: 15 } );
		f.ui.event( { kind: "activate", id: "open-window:Actions" } );
		f.ui.step( f.state, 400 );
		f.ui.event( { kind: "activate", id: "action:4001" } );
		const actionBar = f.ui.step( f.state, 500 ).controls.find( c => c.id === "hotbar:2" );
		f.ui.event( { kind: "drag-end", id: "action:4001", x: actionBar.rect[0] + 2, y: actionBar.rect[1] + 2 } );
		assert.deepEqual( sent.pop(), { kind: "quickslot-set", binding: { slot: 2, kind: 0x4a, payload: 4001 } } );
		f.state.gameplay.quickSlots.push( { slot: 2, kind: 0x4a, payload: 4001 } );
		f.ui.event( { kind: "activate", id: "close" } );
		f.ui.step( f.state, 600 );
		f.ui.event( { kind: "key", code: "Digit2" } );
		assert.deepEqual( sent.pop(), { kind: "action-command", id: 4001 } );
	} finally {
		f.dispose();
	}
});
test("displayed changes redraw without a dependency list and unchanged output stays retained", () => {
	const f = uiFixture();
	try {
		f.state.gameplay = { ...f.state.gameplay, progression: { level: 1, masteries: [] } };
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Character" } );
		for ( let i = 1; i <= 5; i++ ) f.ui.step( f.state, i * 20 );
		assert.ok( f.hasText( "Lv 1" ) );
		const mounted = { ...f.state, gameplay: { ...f.state.gameplay, progression: { level: 2, masteries: [] } } };
		assert.ok( f.ui.step( mounted, 200 ) );
		assert.ok( f.hasText( "Lv 2" ) );
		const selected = {
			...mounted,
			gameplay: { ...mounted.gameplay, target: 2 },
			entities: [ ...mounted.entities, { ...mounted.entities[0], gid: 2, name: "Old NPC", kind: "npc" } ]
		};
		f.ui.step( selected, 300 );
		const renamed = {
			...selected,
			entities: [ selected.entities[0], { ...selected.entities[1], name: "Renamed NPC" } ]
		};
		assert.ok( f.ui.step( renamed, 400 ) );
		assert.ok( f.hasText( "Renamed NPC" ) );
		const count = f.scenes.length, uploads = f.textures.length;
		for ( let now = 500; now < 5000; now += 100 ) assert.equal( f.ui.step( renamed, now ), null );
		assert.equal( f.scenes.length, count );
		assert.equal( f.textures.length, uploads );
	} finally {
		f.dispose();
	}
});
test("Clear hides the granted target immediately and retains its server release barrier", () => {
	let blocked = false;
	const sent = [],
		game = createGameplay( frame => {
			if ( blocked ) throw Error( "backpressure" );
			sent.push( frame );
		} );
	const f = uiFixture( command => game.command( command.command, 0, undefined ) );
	try {
		game.bootstrap( {} );
		game.seed( f.state.entities[0] );
		const target = { ...f.state.entities[0], gid: 9, kind: "npc", name: "NPC" };
		game.command( { kind: "select", gid: 9 }, 0, target );
		const grant = Buffer.alloc( 11 );
		grant[0] = 1;
		grant.writeUInt32LE( 9, 1 );
		game.receive( { opcode: 0xb45a, payload: grant }, 0 );
		f.state.gameplay = game.take();
		f.state.entities.push( target );
		f.ui.step( f.state, 0 );
		blocked = true;
		assert.throws( () => f.ui.event( { kind: "activate", id: "clear-target" } ), /backpressure/ );
		assert.equal( game.take().targetPending, 0 );
		blocked = false;
		f.ui.event( { kind: "activate", id: "clear-target" } );
		assert.equal( sent.at( -1 ).opcode, 0x74b3 );
		assert.deepEqual( [ ...sent.at( -1 ).payload ], [ 9, 0, 0, 0 ] );
		const waiting = game.take();
		assert.equal( waiting.target, 0 );
		assert.equal( waiting.targetPending, 9 );
		assert.throws( () => game.command( { kind: "select", gid: 9 }, 0, target ), /pending/ );
		game.receive( { opcode: 0xb4b3, payload: Uint8Array.of( 1 ) }, 0 );
		assert.equal( game.take().target, 0 );
		const count = sent.length;
		game.command( { kind: "release-target" }, 0, undefined );
		assert.equal( sent.length, count );
	} finally {
		f.dispose();
		game.dispose();
	}
});

test("GPU management controls send typed intent and preserve the selected guild member", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		f.state.gameplay.social = {
			localName: "Player",
			self: 11,
			leader: 11,
			options: 0,
			members: [ { id: 11, name: "Player", level: 1 }, { id: 22, name: "Other", level: 1 } ],
			guild: {
				id: 7,
				name: "Guild",
				level: 4,
				gp: 50,
				subject: "Notice",
				contents: "Body",
				members: [ {
					id: 11,
					name: "Player",
					level: 1,
					grade: 0,
					permissions: 0x1000001f,
					grant: "Master",
					donated: 0,
					model: 1907,
					role: 0,
					offline: 0
				}, {
					id: 22,
					name: "Other",
					level: 1,
					grade: 1,
					permissions: 0,
					grant: "",
					donated: 0,
					model: 1907,
					role: 0,
					offline: 0
				} ]
			},
			invitation: { type: 5, gid: 2 },
			error: null
		};
		f.state.gameplay.target = 2;
		f.state.entities.push( { ...f.state.entities[0], gid: 2, name: "Other" } );
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "invite-refuse" } );
		assert.deepEqual( sent.pop(), { kind: "social-consent", accept: false } );
		f.ui.event( { kind: "activate", id: "open-window:Party" } );
		let output = f.ui.step( f.state, 100 );
		assert.equal( output.controls.find( c => c.id === "party-invite" ).disabled, false );
		f.ui.event( { kind: "activate", id: "social-member:22" } );
		f.ui.step( f.state, 200 );
		f.ui.event( { kind: "activate", id: "party-kick" } );
		assert.equal( sent.length, 0 );
		f.ui.step( f.state, 300 );
		f.ui.event( { kind: "activate", id: "party-kick" } );
		assert.deepEqual( sent.pop(), { kind: "party-kick", id: 22 } );
		f.ui.event( { kind: "activate", id: "open-window:Guild" } );
		f.ui.step( f.state, 400 );
		f.ui.event( { kind: "activate", id: "social-member:22" } );
		f.ui.event( { kind: "activate", id: "guild-dialog:title" } );
		f.ui.step( f.state, 450 );
		f.ui.event( { kind: "edit", id: "social-name", value: "Officer", start: 7, end: 7, composing: false } );
		output = f.ui.step( f.state, 500 );
		assert.equal( output.controls.find( c => c.id === "guild-title" ).disabled, false );
		f.ui.event( { kind: "activate", id: "guild-title" } );
		assert.deepEqual( sent.pop(), { kind: "guild-title", id: 22, name: "Officer" } );
	} finally {
		f.dispose();
	}
});
test("hotbar page keys, clearing and IME suppression are distinct from skill execution", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		f.state.gameplay.skills = [ 7 ];
		f.state.gameplay.quickSlots = [ { slot: 31, kind: 0x49, payload: 7 } ];
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "F4" } );
		f.ui.step( f.state, 100 );
		f.ui.event( { kind: "key", code: "Digit1" } );
		assert.equal( sent.pop().skillId, 7 );
		f.ui.event( { kind: "edit", id: "chat-text", value: "x", start: 1, end: 1, composing: true } );
		f.ui.event( { kind: "key", code: "Digit1" } );
		assert.equal( sent.length, 0 );
		f.ui.event( { kind: "edit", id: "chat-text", value: "x", start: 1, end: 1, composing: false } );
		f.ui.event( { kind: "activate", id: "hotbar-clear" } );
		f.ui.step( f.state, 200 );
		f.ui.event( { kind: "activate", id: "hotbar:31" } );
		assert.deepEqual( sent.pop(), { kind: "quickslot-bind", slot: 31, skillId: 0 } );
	} finally {
		f.dispose();
	}
});

test("GPU inventory uses one native gold confirmation with numeric clamp and modal cancellation", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const item = {
			slot: 13,
			refObjId: 1,
			typeFlags: 0x6c,
			quantity: 3,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: []
		};
		const game = {
			...f.state.gameplay,
			inventory: [ item ],
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventoryPending: false,
			progression: { gold: "5000", masteries: [] }
		};
		const state = { ...f.state, gameplay: game };
		let now = 0;
		const click = id => {
			f.ui.event( { kind: "activate", id } );
			f.ui.step( state, ++now );
		};
		f.ui.step( state, now );
		click( "open-window:Inventory" );
		click( "slot:13" );
		click( "drop-item" );
		assert.equal( sent.length, 0 );
		click( "ground-drop-confirm" );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "item-drop", slot: 13 } } );
		click( "inventory-gold" );
		const edit = value => {
			f.ui.event( {
				kind: "edit",
				id: "gold-amount",
				value,
				start: value.length,
				end: value.length,
				composing: false
			} );
			f.ui.step( state, ++now );
		};
		edit( "100" );
		click( "drop-gold" );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "gold-drop", amount: 100 } } );
		click( "inventory-gold" );
		edit( "200" );
		f.ui.event( { kind: "key", code: "Escape" } );
		f.ui.step( state, ++now );
		assert.equal( sent.length, 0 );
		assert.equal( f.ui.stats().panel, "Inventory" );
		click( "inventory-gold" );
		edit( "6000" );
		f.ui.event( { kind: "key", code: "Enter" } );
		f.ui.step( state, ++now );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "gold-drop", amount: 5000 } } );
	} finally {
		f.dispose();
	}
});

test("GPU shop gates merchant capability, affordability and exact sale confirmation", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		const game = f.state.gameplay;
		game.target = 17;
		game.targetCapabilities = 1;
		game.inventorySlotCount = 109;
		game.equipmentSlotCount = 13;
		game.progression = { gold: "5000", masteries: [] };
		game.inventory = [ { slot: 13, refObjId: 3630, name: "Potion", quantity: 4, typeFlags: 0x8ec } ];
		f.state.entities.push( { ...f.state.entities[0], gid: 17, kind: "npc", name: "Merchant" } );
		let now = 0;
		const click = id => {
			f.ui.event( { kind: "activate", id } );
			return f.ui.step( f.state, now += 100 );
		};
		f.ui.step( f.state, now );
		click( "shop-open" );
		assert.deepEqual( sent.pop(), { kind: "shop-open", gid: 17 } );
		game.shop = {
			npc: 17,
			name: "Merchant",
			offers: [ { tab: 0, slot: 2, refObjId: 3630, name: "Potion", price: "60", maxStack: 50 } ]
		};
		game.shopCompletionRevision = 1;
		f.ui.step( f.state, now += 100 );
		click( "shop-offer:0" );
		click( "shop-trade" );
		assert.deepEqual( sent.pop(), { kind: "shop-buy", tab: 0, slot: 2, quantity: 1 } );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		f.ui.step( f.state, now += 100 );
		assert.deepEqual( sent.pop(), { kind: "shop-open", gid: 17 } );
		click( "shop-trade" );
		assert.equal( sent.length, 0, "unquoted sale must wait" );
		game.shop = { ...game.shop, saleQuotes: [ { slot: 13, refObjId: 3630, quantity: 4, price: "20" } ] };
		f.ui.step( f.state, now += 100 );
		click( "shop-trade" );
		assert.deepEqual( sent.pop(), { kind: "shop-sell", slot: 13, quantity: 4 } );
		click( "shop-offer:0" );
		game.target = 18;
		const output = f.ui.step( f.state, now += 100 );
		assert.equal( output.controls.find( c => c.id === "shop-trade" ).disabled, true );
		click( "shop-trade" );
		assert.equal( sent.length, 0 );
	} finally {
		f.dispose();
	}
});

/*
================
ctrlShopTransaction

570120 / 567290: a CTRL buy takes one package of several items, else the
item's MaxStack (one staff, never the editor's purchase limit of five). A
CTRL sell sells at once but refuses rare items.
================
*/
test("CTRL shop click buys MaxStack and refuses a rare quick sell", () => {
	const sent = [], f = uiFixture( command => sent.push( command.command ) );
	try {
		const game = f.state.gameplay;
		game.target = 17;
		game.targetCapabilities = 1;
		game.inventorySlotCount = 45;
		game.equipmentSlotCount = 13;
		game.progression = { gold: "100000", masteries: [] };
		game.inventory = [
			{ slot: 13, refObjId: 900, name: "Staff", quantity: 1, typeFlags: 0x0c },
			{
				slot: 14,
				refObjId: 901,
				name: "Rare Staff",
				quantity: 1,
				typeFlags: 0x0c,
				tooltip: { fields: { rarity: 2 } }
			}
		];
		f.state.entities.push( { ...f.state.entities[0], gid: 17, kind: "npc", name: "Merchant" } );
		let now = 0;
		const draw = () => f.ui.step( f.state, now += 100 );
		draw();
		f.ui.event( { kind: "activate", id: "shop-open" } );
		draw();
		sent.length = 0;
		game.shop = {
			npc: 17,
			name: "Merchant",
			offers: [
				{ tab: 0, slot: 0, refObjId: 900, name: "Staff", price: "100", maxStack: 1, purchaseLimit: 5 },
				{ tab: 0, slot: 1, refObjId: 3630, name: "Potion", price: "60", maxStack: 50, purchaseLimit: 50 },
				{
					tab: 0,
					slot: 2,
					refObjId: 100,
					name: "Set",
					price: "100",
					maxStack: 1,
					purchaseLimit: 5,
					contents: [ { refObjId: 900, quantity: 1 }, { refObjId: 3630, quantity: 1 } ]
				}
			],
			saleQuotes: [
				{ slot: 13, refObjId: 900, quantity: 1, price: "20" },
				{ slot: 14, refObjId: 901, quantity: 1, price: "20" }
			]
		};
		game.shopCompletionRevision = 1;
		draw();
		for ( const [index, quantity] of [ [ 0, 1 ], [ 1, 50 ], [ 2, 1 ] ] ) {
			f.ui.event( { kind: "activate", id: "shop-offer:" + index, ctrl: true } );
			draw();
			assert.deepEqual( sent.pop(), { kind: "shop-buy", tab: 0, slot: index, quantity } );
		}
		f.ui.event( { kind: "activate", id: "slot:14", ctrl: true } );
		draw();
		assert.equal( sent.length, 0, "rare items refuse a quick sell" );
		f.ui.event( { kind: "activate", id: "slot:13", ctrl: true } );
		draw();
		assert.deepEqual( sent.pop(), { kind: "shop-sell", slot: 13, quantity: 1 } );
	} finally {
		f.dispose();
	}
});

/*
================
merchantQuantityLimit

Exercise the real editor and confirmation path. An oversized draft must be
corrected visibly before the command is composed, using the offer's limit.
================
*/
test("merchant amount editor clamps to the authored limit before purchase", () => {
	const sent = [], f = uiFixture( command => sent.push( command.command ) );
	try {
		const game = f.state.gameplay;
		game.target = 17;
		game.targetCapabilities = 1;
		game.inventorySlotCount = 45;
		game.equipmentSlotCount = 13;
		game.progression = { gold: "10000", masteries: [] };
		f.state.entities.push( { ...f.state.entities[0], gid: 17, kind: "npc", name: "Merchant" } );
		let now = 0;
		/*
		================
		draw
		================
		*/
		function draw() {
			return f.ui.step( f.state, now += 100 );
		}
		draw();
		f.ui.event( { kind: "activate", id: "shop-open" } );
		draw();
		sent.length = 0;
		game.shop = {
			npc: 17,
			name: "Merchant",
			offers: [
				{ tab: 0, slot: 0, refObjId: 62, name: "Arrow", price: "2", maxStack: 250, purchaseLimit: 250 },
				{ tab: 0, slot: 1, refObjId: 3630, name: "Potion", price: "60", maxStack: 50, purchaseLimit: 50 },
				{ tab: 0, slot: 2, refObjId: 100, name: "Package", price: "100", maxStack: 250, purchaseLimit: 5 }
			]
		};
		game.shopCompletionRevision = 1;
		draw();
		for ( const [index, maximum] of [ [ 0, 250 ], [ 1, 50 ], [ 2, 5 ] ] ) {
			f.ui.event( { kind: "activate", id: "shop-offer:" + index } );
			draw();
			f.ui.event( { kind: "edit", id: "shop-quantity", value: "1000" } );
			const output = draw();
			assert.equal( output.controls.find( control => control.id === "shop-quantity" ).value, String( maximum ) );
			assert.equal( output.controls.find( control => control.id === "shop-trade" ).disabled, false );
			f.ui.event( { kind: "activate", id: "shop-trade" } );
			draw();
			assert.deepEqual( sent.pop(), { kind: "shop-buy", tab: 0, slot: index, quantity: maximum } );
		}
		f.ui.event( { kind: "activate", id: "shop-offer:0" } );
		draw();
		for ( const value of [ "", "0", "99", "250" ] ) {
			f.ui.event( { kind: "edit", id: "shop-quantity", value } );
			const output = draw();
			assert.equal( output.controls.find( control => control.id === "shop-quantity" ).value, value );
			assert.equal( output.controls.find( control => control.id === "shop-trade" ).disabled, !Number( value ) );
		}
		game.progression.gold = "499";
		f.ui.event( { kind: "edit", id: "shop-quantity", value: "1000" } );
		const unaffordable = draw();
		assert.equal( unaffordable.controls.find( control => control.id === "shop-quantity" ).value, "250" );
		assert.equal( unaffordable.controls.find( control => control.id === "shop-trade" ).disabled, true );
		f.ui.event( { kind: "activate", id: "shop-trade" } );
		draw();
		assert.equal( sent.length, 0, "normalization must not bypass the gold check" );
	} finally {
		f.dispose();
	}
});

test("merchant wheel scrolling follows the dragged window and ignores its old location", () => {
	const f = uiFixture();
	try {
		const game = f.state.gameplay;
		game.target = 17;
		game.targetCapabilities = 1;
		game.inventorySlotCount = 109;
		game.shop = {
			npc: 17,
			name: "Merchant",
			offers: [ 0, 30, 60 ].map( slot => ({
				tab: 0,
				slot,
				refObjId: 3630,
				name: "Page " + slot,
				price: "60",
				maxStack: 50
			}) )
		};
		f.state.entities.push( { ...f.state.entities[0], gid: 17, kind: "npc", name: "Merchant" } );
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "shop-open" } );
		game.shopCompletionRevision = 1;
		let output = f.ui.step( f.state, 100 );
		for ( let i = 0; i < 20; i++ ) output = f.ui.step( f.state, 101 + i ) ?? output;
		const old = output.controls.find( c => c.id === "shop-offer:0" ).rect;
		f.ui.event( { kind: "drag", id: "window-drag:Shop", dx: -500, dy: -200 } );
		output = f.ui.step( f.state, 200 );
		const moved = output.controls.find( c => c.id === "shop-offer:0" ).rect;
		assert.notDeepEqual( moved, old );
		f.ui.event( { kind: "scroll", x: moved[0] + 5, y: moved[1] + 5, delta: 1 } );
		output = f.ui.step( f.state, 300 ) ?? output;
		assert.ok( output.controls.some( c => c.id === "shop-offer:1" ), "moved offer area advances the page" );
		f.ui.event( { kind: "scroll", x: old[0] + 5, y: old[1] + 5, delta: 1 } );
		output = f.ui.step( f.state, 400 ) ?? output;
		assert.ok(
			output.controls.some( c => c.id === "shop-offer:1" ),
			"old offer area cannot advance the moved shop"
		);
	} finally {
		f.dispose();
	}
});

test("GPU COS bag leaves a quick transfer's slot to the worker in either direction", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		const game = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, refObjId: 8, quantity: 30 } ],
			cosRecords: [ {
				gid: 7,
				refObjId: 102,
				name: "Pet",
				hp: 100,
				dead: false,
				status: 4,
				inventory: [ { slot: 0, refObjId: 8, quantity: 40 } ]
			} ]
		};
		f.ui.step( { ...f.state, gameplay: game }, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		f.ui.step( { ...f.state, gameplay: game }, 1 );
		f.ui.event( { kind: "activate", id: "cos-bag" } );
		f.ui.step( { ...f.state, gameplay: game }, 2 );
		f.ui.event( { kind: "activate", id: "cos-player:13" } );
		f.ui.step( { ...f.state, gameplay: game }, 3 );
		f.ui.event( { kind: "activate", id: "to-cos" } );
		// A quick transfer leaves the slot to the worker (cosQuickDestination).
		assert.deepEqual( sent.at( -1 ), { kind: "cos-transfer", gid: 7, toCos: true, source: 13 } );
		f.ui.event( { kind: "activate", id: "cos-slot:0" } );
		f.ui.step( { ...f.state, gameplay: game }, 4 );
		f.ui.event( { kind: "activate", id: "from-cos" } );
		assert.deepEqual( sent.at( -1 ), { kind: "cos-transfer", gid: 7, toCos: false, source: 0 } );
	} finally {
		f.dispose();
	}
});

test("world camera snapshots retain idle HUD layout while gameplay, resize and tip deadlines invalidate it", () => {
	const f = uiFixture();
	let reads = 0;
	const vitals = [];
	f.state.frontend = { phase: "world", generation: 1, error: null, selectedCharacter: "Player" };
	f.state.gameplay = {
		...f.state.gameplay,
		get vitals() {
			reads++;
			return vitals;
		}
	};
	try {
		for ( let now = 0; now <= 1000; now += 100 ) f.ui.step( f.state, now );
		reads = 0;
		for ( let now = 1004; now < 1100; now += 4 ) {
			f.ui.step( { ...f.state, frontend: { ...f.state.frontend, elapsed: now } }, now );
		}
		assert.equal( reads, 0, "camera-only frames do not reconstruct the world HUD" );
		f.ui.step( { ...f.state, width: 1200 }, 1100 );
		assert.ok( reads > 0 );
		assert.equal( f.scenes.at( -1 ).width, 1200 );
		reads = 0;
		f.ui.step( { ...f.state, width: 1200 }, 61000 );
		assert.ok( reads > 0, "native tip deadline is not swallowed by idle retention" );
		const changed = { ...f.state, gameplay: { ...f.state.gameplay, vitals: [ { gid: 1, hp: 40, mp: 20 } ] } };
		const semantics = f.ui.step( changed, 61004 );
		assert.match( semantics.message, /Health 40; mana 20/ );
	} finally {
		f.dispose();
	}
});

test("inventory drag submits one current-slot move and cancels across modal, close and session boundaries", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, refObjId: 1, typeFlags: 0x6c, quantity: 3, name: "Potion" } ]
		};
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyI" } );
		let scene = f.ui.step( f.state, 1300 );
		const dest = scene.controls.find( c => c.id === "slot:14" ).rect;
		const drag = () => f.ui.event( { kind: "drag", id: "slot:13", dx: 36, dy: 0 } );
		const drop = () => f.ui.event( { kind: "drag-end", id: "slot:13", x: dest[0] + 16, y: dest[1] + 16 } );
		drag();
		drop();
		drop();
		assert.deepEqual( sent, [ {
			kind: "gameplay",
			command: { kind: "inventory-move", source: 13, destination: 14, quantity: 3 }
		} ] );
		sent.length = 0;
		// A prior click selection must not survive a pointer carry/cancel and move
		// that old source when the user next clicks another inventory slot.
		f.ui.event( { kind: "activate", id: "slot:13" } );
		drag();
		// The bridge reports an abandoned carry as drag-cancel; press null is
		// only the visual release and must not drop a carried item.
		f.ui.event( { kind: "press", id: null } );
		f.ui.event( { kind: "drag-cancel", id: "slot:13" } );
		drop();
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		assert.deepEqual( sent, [], "cancelled pointer carry retires the click source too" );
		f.ui.event( { kind: "activate", id: "slot:14" } );

		drag();
		f.ui.event( { kind: "key", code: "Escape" } );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.step( f.state, 1400 );
		drop();
		assert.deepEqual( sent, [] );
		drag();
		const invited = { ...f.state, gameplay: { ...f.state.gameplay, social: { invitation: { type: 1, gid: 2 } } } };
		f.ui.step( invited, 1500 );
		drop();
		assert.deepEqual( sent, [] );
		f.ui.step( f.state, 1600 );
		drag();
		f.ui.step( { ...f.state, session: { ...f.state.session, phase: "disconnected" } }, 1700 );
		drop();
		assert.deepEqual( sent, [] );
	} finally {
		f.dispose();
	}
});

test("disconnect retains world annotations and Inventory presentation but only the modal accepts input", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyI" } );
		for ( let t = 1300; t < 1900; t += 100 ) f.ui.step( f.state, t );
		const before = f.scenes.at( -1 ).quads.filter( q => q.characterAnchor || q.doll );
		assert.ok( before.some( q => q.characterAnchor ) );
		assert.ok( before.some( q => q.doll ) );
		const disconnected = { ...f.state, session: { ...f.state.session, phase: "disconnected" } };
		const semantic = f.ui.step( disconnected, 1900 );
		assert.deepEqual( f.scenes.at( -1 ).quads.filter( q => q.characterAnchor || q.doll ), before );
		assert.deepEqual( semantic.controls.map( c => c.id ), [ "disconnect-drag", "disconnect-confirm" ] );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.event( { kind: "activate", id: "slot:13" } );
		assert.deepEqual( sent, [] );
	} finally {
		f.dispose();
	}
});

test("title notices expire on timer or title-process retirement, never on dock camera completion", () => {
	const f = uiFixture();
	try {
		const front = { phase: "login", generation: 1, elapsed: 0, alpha: 1, logoAlpha: 0, error: null };
		const state = {
			...f.state,
			frontend: front,
			session: { phase: "authenticating", revision: 1 },
			gameplay: null,
			entities: []
		};
		f.ui.step( state, 1000 );
		f.ui.step( state, 2000 );
		assert.ok( f.hasText( "Requesting user confirmation..." ) );
		f.ui.step( state, 15999 );
		assert.ok( f.hasText( "Requesting user confirmation..." ) );
		f.ui.step( state, 16000 );
		assert.ok( !f.hasText( "Requesting user confirmation..." ) );
		f.ui.step( { ...state, session: { phase: "signed-out", revision: 2 } }, 17000 );
		f.ui.step( { ...state, session: { phase: "authenticating", revision: 3 } }, 18000 );
		assert.ok( f.hasText( "Requesting user confirmation..." ) );
		const accepted = {
			...state,
			frontend: { ...front, phase: "login-accepted" },
			session: { phase: "character-select", revision: 4, characters: [] }
		};
		f.ui.step( accepted, 18100 );
		assert.ok( f.hasText( "Requesting user confirmation..." ) );
		const dock = { ...accepted, frontend: { ...front, phase: "dock-arrival", cameraMoving: true } };
		f.ui.step( dock, 18600 );
		assert.ok( !f.hasText( "Requesting user confirmation..." ) );
		f.ui.step( { ...dock, frontend: { ...dock.frontend, phase: "dock", cameraMoving: false } }, 21000 );
		assert.ok( !f.hasText( "Requesting user confirmation..." ) );
	} finally {
		f.dispose();
	}
});

test("Character and Party use native main-popup art and share drag placement with Inventory", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			progression: { level: 7, statPoints: 1, experience: "12", masteries: [] }
		};
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyC" } );
		let preDrag = null;
		for ( let i = 1; i < 15; i++ ) preDrag = f.ui.step( f.state, i * 100 ) ?? preDrag;
		assert.ok( preDrag );
		assert.ok( f.hasText( "Lv 7" ) );
		assert.ok( !f.hasText( "Mounted: No" ) );
		assert.ok( f.scenes.at( -1 ).quads.some( q => q.texture.endsWith( "/character/chr_job_window.png" ) ) );
		f.ui.event( { kind: "activate", id: "stat-str" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "stat-increase", stat: "str" } } );
		assert.deepEqual(
			preDrag.controls.find( c => c.id === "main-popup-drag" ).rect,
			[ 1222, 422, 367, 34 ],
			"default frame [1212,422]=[1600-388,900-478] with native +10 x inset and 367x34=(388-21)x34 strip"
		);
		f.ui.event( { kind: "drag", id: "main-popup-drag", dx: -200, dy: -100 } );
		const postDrag = f.ui.step( f.state, 1500 );
		assert.ok( postDrag );
		assert.deepEqual(
			postDrag.controls.find( c => c.id === "main-popup-drag" ).rect,
			[ 1022, 322, 367, 34 ],
			"drag delta [-200,-100] moves the frame origin; strip keeps the +10 inset"
		);
		f.ui.event( { kind: "key", code: "KeyI" } );
		let result;
		for ( let i = 16; i < 22; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.deepEqual(
			result.controls.find( c => c.id === "main-popup-drag" ).rect,
			[ 1022, 322, 367, 34 ],
			"Inventory shares Character popup placement across tabs"
		);
		const tabIds = result.controls.map( c => c.id );
		assert.equal(
			new Set( tabIds ).size,
			tabIds.length,
			"main popup publishes unique control ids for DOM hit-testing"
		);
		f.ui.event( { kind: "key", code: "KeyP" } );
		for ( let i = 22; i < 28; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.equal( f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "/party/pt_slot.png" ) ).length, 7 );
		f.ui.event( { kind: "activate", id: "party-settings" } );
		for ( let i = 28; i < 35; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.ok( result.controls.some( c => c.id === "party-settings-ok" ) );
		f.ui.event( { kind: "activate", id: "party-setting:1:1" } );
		f.ui.event( { kind: "activate", id: "party-settings-cancel" } );
		f.ui.step( f.state, 3600 );
		assert.ok( !f.scenes.at( -1 ).quads.some( q => q.texture.endsWith( "/messagebox/msgbox_blackbox_03.png" ) ) );
	} finally {
		f.dispose();
	}
});

test("Options resizes all five pages without moving the window origin", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		let result;
		for ( let i = 1; i < 20; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.deepEqual( result.controls.filter( c => c.id.startsWith( "option-tab:" ) ).map( c => c.label ), [
			"Video",
			"Audio",
			"Set time",
			"Set input",
			"Set game"
		] );
		for (
			const [tab, height, row] of [ [ 0, 413, 379 ], [ 1, 319, 285 ], [ 2, 312, 278 ], [ 3, 413, 379 ], [
				4,
				415,
				380
			] ]
		) {
			f.ui.event( { kind: "activate", id: "option-tab:" + tab } );
			for ( let i = 0; i < 12; i++ ) result = f.ui.step( f.state, 2100 + tab * 1500 + i * 100 ) ?? result;
			assert.equal(
				result.controls.find( c => c.id === "option-ok" ).rect[1],
				Math.floor( (900 - 413) / 2 ) + row
			);
			assert.equal( result.controls.some( c => c.id === "option-apply" ), tab === 0 );
			assert.deepEqual( result.controls.find( c => c.id === "option-tab:0" ).rect.slice( 0, 2 ), [ 647, 283 ] );
			// Native 5404BB centers the font board inside the nine-pixel tab client height.
			for ( const control of result.controls.filter( c => c.id.startsWith( "option-tab:" ) ) ) {
				const face = control.selected ? fontAtlas.fonts["0"].styles["2"] : fontAtlas.fonts["0"];
				const glyph = face.glyphs[control.label.codePointAt( 0 )];
				const uv = [
					glyph.x / fontAtlas.atlasWidth,
					glyph.y / fontAtlas.atlasHeight,
					glyph.width / fontAtlas.atlasWidth,
					glyph.height / fontAtlas.atlasHeight
				];
				const ink = f.scenes.at( -1 ).quads.find( q =>
					q.texture === fontAtlas.image &&
					q.rect[0] >= control.rect[0] && q.rect[0] < control.rect[0] + control.rect[2] &&
					q.rect[1] >= control.rect[1] && q.rect[1] < control.rect[1] + control.rect[3] &&
					q.uv.every( ( value, i ) => value === uv[i] )
				);
				assert.ok( ink, control.label + " renders its native glyph" );
				const baseline = control.rect[1] + 9 + Math.floor( (9 - (face.recordHeight + 5)) / 2 ) + face.ascent;
				assert.equal( ink.rect[1], baseline - glyph.originY, control.label + " stays vertically centered" );
			}
			if ( tab === 0 ) {
				assert.deepEqual( result.controls.find( c => c.id === "option-video-up" ).rect, [ 945, 415, 16, 16 ] );
				assert.deepEqual( result.controls.find( c => c.id === "option-video-down" ).rect, [
					945,
					576,
					16,
					16
				] );
			}
			if ( tab === 3 ) {
				assert.deepEqual( result.controls.find( c => c.id === "option-input-up" ).rect, [ 947, 444, 16, 16 ] );
				assert.deepEqual( result.controls.find( c => c.id === "option-input-down" ).rect, [
					947,
					577,
					16,
					16
				] );
			}
		}
	} finally {
		f.dispose();
	}
});

test("Audio sliders preview, Cancel restores saved mix, and Default is scoped to Audio", () => {
	const changes = [], f = uiFixture( () => {}, () => false, ( value, commit ) => changes.push( { value, commit } ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:1" } );
		let state;
		for ( let i = 1; i < 20; i++ ) state = f.ui.step( f.state, i * 100 ) ?? state;
		assert.equal( state.controls.filter( c => c.id.startsWith( "option-audio:" ) ).length, 3 );
		// Slider position 132 is level 83: the first 50 positions are the quiet range.
		f.ui.event( { kind: "edit", id: "option-audio:bgm", value: "132", start: 0, end: 0, composing: false } );
		assert.equal( changes.at( -1 ).value.bgm, 83 );
		assert.equal( changes.at( -1 ).commit, false );
		f.ui.event( { kind: "activate", id: "option-mute:muteEnvironment" } );
		assert.equal( changes.at( -1 ).value.muteEnvironment, true );
		f.ui.event( { kind: "activate", id: "option-cancel" } );
		assert.equal( changes.at( -1 ).value.bgm, 30 );
		assert.equal( changes.at( -1 ).value.muteEnvironment, false );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:1" } );
		f.ui.event( { kind: "activate", id: "option-default" } );
		assert.equal( changes.at( -1 ).value.bgm, 50 );
		f.ui.event( { kind: "activate", id: "option-ok" } );
		assert.equal( changes.filter( c => c.commit ).length, 1 );
		assert.equal( changes.at( -1 ).value.bgm, 50 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:1" } );
		f.ui.event( { kind: "edit", id: "option-audio:bgm", value: "99", start: 0, end: 0, composing: false } );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.equal( changes.at( -1 ).value.bgm, 50 );
	} finally {
		f.dispose();
	}
});

test("password survives failed login and successful title fade, and is released with the title process", () => {
	const f = uiFixture();
	try {
		const front = { phase: "login", loginOpacity: 1, loginEnabled: true, loading: false, cameraMoving: false };
		let state = {
			...f.state,
			gameplay: null,
			entities: [],
			worldReady: false,
			frontend: front,
			session: { phase: "signed-out", revision: 1, servers: [] }
		};
		f.ui.step( state, 0 );
		f.ui.event( { kind: "edit", id: "password", value: "scratch-only", start: 12, end: 12, composing: false } );
		state = { ...state, session: { phase: "failed", revision: 2 } };
		let result = f.ui.step( state, 100 );
		assert.equal( result.controls.find( c => c.id === "password" ).value, "scratch-only" );
		state = {
			...state,
			frontend: { ...front, phase: "login-accepted" },
			session: { phase: "character-select", revision: 3, characters: [] }
		};
		result = f.ui.step( state, 200 );
		assert.ok( f.hasText( "************" ), "password mask stays painted during accepted fade" );
		f.ui.step( { ...state, frontend: { ...front, phase: "dock-arrival" } }, 700 );
		result = f.ui.step( { ...state, frontend: front, session: { phase: "signed-out", revision: 4 } }, 900 );
		assert.equal( result.controls.find( c => c.id === "password" ).value, "" );
	} finally {
		f.dispose();
	}
});

test("camera choices commit on OK, discard on Cancel and F9 cycles live modes", () => {
	const modes = [], f = uiFixture( () => {}, () => false, () => {}, value => modes.push( value ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:2" } );
		let state;
		for ( let i = 1; i < 20; i++ ) state = f.ui.step( f.state, i * 100 ) ?? state;
		assert.equal( state.controls.filter( c => c.id.startsWith( "option-sight:" ) ).length, 3 );
		f.ui.event( { kind: "activate", id: "option-sight:1" } );
		assert.deepEqual( modes, [] );
		f.ui.event( { kind: "activate", id: "option-cancel" } );
		assert.deepEqual( modes, [] );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-sight:2" } );
		f.ui.event( { kind: "activate", id: "option-ok" } );
		assert.deepEqual( modes, [ 2 ] );
		f.ui.event( { kind: "key", code: "F9" } );
		assert.deepEqual( modes, [ 2, 0 ] );
	} finally {
		f.dispose();
	}
});

test("Input binding capture removes conflicts, rejects reserved keys and commits only on OK", () => {
	const saved = [], f = uiFixture( () => {}, () => false, () => {}, () => {}, v => saved.push( v ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:3" } );
		let result;
		for ( let i = 1; i < 15; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.equal( result.controls.filter( c => c.id.startsWith( "option-bind:" ) ).length, 10 );
		f.ui.event( { kind: "activate", id: "option-bind:0" } );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.event( { kind: "key", code: "Digit1" } );
		f.ui.event( { kind: "activate", id: "option-ok" } );
		assert.equal( saved[0].keys[0], 73 );
		assert.equal( saved[0].keys[1], 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		result = f.ui.step( f.state, 1800 );
		assert.ok( result.controls.some( c => c.id === "main-popup-drag" ), "new key opens Character" );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:3" } );
		f.ui.event( { kind: "activate", id: "option-default" } );
		f.ui.event( { kind: "activate", id: "option-cancel" } );
		assert.equal( saved.length, 1 );
	} finally {
		f.dispose();
	}
});

test("frame limit uses the video draft and survives Apply, Cancel and defaults", () => {
	const saved = [], f = uiFixture( () => {}, () => false, () => {}, () => {}, () => {}, v => saved.push( v ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		let result;
		for ( let i = 1; i < 15; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		for ( let i = 0; i < 10; i++ ) f.ui.event( { kind: "activate", id: "option-video-down" } );
		result = f.ui.step( f.state, 1500 ) ?? result;
		assert.equal( result.controls.find( c => c.id === "option-video-combo:-3" ).label, "Frame rate" );
		f.ui.event( { kind: "activate", id: "option-video-combo:-3" } );
		result = f.ui.step( f.state, 1600 ) ?? result;
		assert.equal( result.controls.filter( c => c.id.startsWith( "option-video-choice:-3:" ) ).length, 4 );
		assert.equal( result.controls.find( c => c.id === "option-video-choice:-3:3" ).label, "Display refresh rate" );
		f.ui.event( { kind: "activate", id: "option-video-choice:-3:1" } );
		assert.equal( saved.length, 0 );
		f.ui.event( { kind: "activate", id: "option-apply" } );
		assert.equal( saved[0].frameLimit, 120 );
		f.ui.event( { kind: "activate", id: "option-video-choice:-3:3" } );
		f.ui.event( { kind: "activate", id: "option-cancel" } );
		assert.equal( saved.length, 1 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-default" } );
		f.ui.event( { kind: "activate", id: "option-ok" } );
		assert.equal( saved[1].frameLimit, 0 );
	} finally {
		f.dispose();
	}
});

test("the screen size combo opens and its choice is applied", () => {
	const saved = [],
		f = uiFixture( () => {}, () => false, () => {}, () => {}, () => {}, v => saved.push( v ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		let result;
		// A retained frame returns null; keep the last published controls.
		for ( let i = 1; i < 15; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		const combo = result.controls.find( c => c.id === "option-video-combo:-1" );
		assert.ok( combo, "screen size combo is rendered" );
		assert.ok( !combo.disabled, "screen size combo accepts the pointer" );
		f.ui.event( { kind: "activate", id: combo.id } );
		result = f.ui.step( f.state, 1600 ) ?? result;
		const choice = result.controls.find( c => c.id === "option-video-choice:-1:1" );
		assert.ok( choice && !choice.disabled, "the open list offers enabled sizes" );
		f.ui.event( { kind: "activate", id: choice.id } );
		f.ui.event( { kind: "activate", id: "option-apply" } );
		assert.equal( saved.length, 1 );
		assert.ok( saved[0].displaySize?.[0] > 0 && saved[0].displaySize[1] > 0, "the chosen mode is saved" );
	} finally {
		f.dispose();
	}
});

test("Apply commits every tab, as OK does, and keeps the Options window open", () => {
	const saved = [],
		bindings = [],
		f = uiFixture( () => {}, () => false, () => {}, () => {}, v => bindings.push( v ), v => saved.push( v ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		for ( let i = 1; i < 15; i++ ) f.ui.step( f.state, i * 100 );
		f.ui.event( { kind: "activate", id: "option-video-choice:8:0" } );
		f.ui.event( { kind: "activate", id: "option-video-choice:9:0" } );
		f.ui.event( { kind: "activate", id: "option-apply" } );
		assert.equal( saved.length, 1 );
		assert.equal( saved[0].records[0][8], 0 );
		assert.equal( saved[0].records[0][9], 0 );
		// The input tab is committed with the rest.
		assert.equal( bindings.length, 1 );
		const result = f.ui.step( f.state, 1700 );
		assert.ok( result.controls.some( c => c.id === "option-apply" ) );
		f.ui.event( { kind: "activate", id: "option-video-record:1" } );
		f.ui.event( { kind: "activate", id: "option-video-choice:10:0" } );
		f.ui.event( { kind: "activate", id: "option-cancel" } );
		assert.equal( saved.length, 1 );
	} finally {
		f.dispose();
	}
});

test("player and lower-bar retention invalidate independently for vitals, progression and viewport", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.vitals = [ { gid: 1, hp: 90, mp: 80, maxHp: 100, maxMp: 100 } ];
		// The gauge converges per frame; 30 steps with nonzero EXP is not a settled bar.
		// Keep the native animated SP gauge settled while isolating HP invalidation.
		f.state.gameplay.progression = { level: 5, experience: "123", skillExperience: 0, skillPoints: 11 };
		for ( let i = 0; i < 250; i++ ) f.ui.step( { ...f.state }, 1000 );
		const first = f.ui.stats().layoutRetention;
		f.state.gameplay = { ...f.state.gameplay, vitals: [ { ...f.state.gameplay.vitals[0], hp: 70 } ] };
		f.ui.step( { ...f.state }, 1001 );
		const hp = f.ui.stats().layoutRetention;
		assert.equal( hp.player.rebuilds, first.player.rebuilds + 1 );
		assert.equal( hp.bar.rebuilds, first.bar.rebuilds );
		assert.ok( Object.keys( fontAtlas.fonts ).some( font => f.hasText( "70 / 100", font ) ) );
		for ( let i = 0; i < 250; i++ ) f.rawStep( { ...f.state }, 1001 );
		const settled = f.ui.stats().layoutRetention;
		f.state.gameplay = { ...f.state.gameplay, progression: { ...f.state.gameplay.progression, skillPoints: 12 } };
		f.ui.step( { ...f.state }, 1002 );
		const sp = f.ui.stats().layoutRetention;
		assert.equal( sp.player.rebuilds, settled.player.rebuilds );
		assert.equal( sp.bar.rebuilds, hp.bar.rebuilds + 1 );
		assert.ok( f.hasText( "12" ) );
		f.state.width = 1400;
		f.ui.step( { ...f.state }, 1003 );
		const resized = f.ui.stats().layoutRetention;
		assert.equal( resized.player.rebuilds, sp.player.rebuilds + 1 );
		assert.equal( resized.bar.rebuilds, sp.bar.rebuilds + 1 );
		f.ui.event( { kind: "activate", id: "ability-details" } );
		const semantics = f.ui.step( { ...f.state }, 1004 );
		assert.ok( semantics.controls.some( c => c.id === "ability-details" ) );
		assert.equal( f.ui.stats().layoutRetention.player.rebuilds, resized.player.rebuilds + 1 );
	} finally {
		f.dispose();
	}
});

test("SP gauge animation invalidates only the lower bar without another progression packet", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.progression = { level: 5, experience: "123", skillExperience: 0, skillPoints: 11 };
		for ( let i = 0; i < 30; i++ ) f.ui.step( { ...f.state }, 1000 );
		const before = f.ui.stats().layoutRetention;
		f.state.gameplay = {
			...f.state.gameplay,
			progression: { ...f.state.gameplay.progression, skillExperience: 40 }
		};
		f.ui.step( { ...f.state }, 1001 );
		const first = f.ui.stats().layoutRetention;
		f.ui.step( { ...f.state }, 1002 );
		const second = f.ui.stats().layoutRetention;
		assert.equal( first.player.rebuilds, before.player.rebuilds );
		assert.equal( second.player.rebuilds, first.player.rebuilds );
		assert.equal( first.bar.rebuilds, before.bar.rebuilds + 1 );
		assert.equal( second.bar.rebuilds, first.bar.rebuilds + 1 );
	} finally {
		f.dispose();
	}
});

test("inventory rotation updates the admitted doll while preserving world commands", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		f.ui.step( f.state, 1 );
		const yaw = () => f.scenes.at( -1 ).quads.find( q => q.doll )?.doll.yaw;
		const initial = yaw();
		assert.equal( initial, .100000001 );
		f.ui.event( { kind: "activate", id: "doll-right" } );
		f.ui.step( f.state, 2 );
		assert.equal( yaw(), initial + .1 );
		f.ui.event( { kind: "activate", id: "doll-reset" } );
		f.ui.step( f.state, 3 );
		assert.equal( yaw(), initial );
		assert.deepEqual( sent, [] );
	} finally {
		f.dispose();
	}
});

test("the party delete question is the native simple message box with its tiled body", () => {
	// 635300 opens it through 6888C0: 360x151, the client tiled at (16,40),
	// Yes/No 37 above the bottom around the centre (52E720). It drew only
	// its frame ring, so the party window showed through it.
	const f = uiFixture();
	try {
		f.state.gameplay.partyMatching = {
			page: 0,
			pages: 1,
			rows: [],
			own: {
				id: 7,
				party: 0,
				name: "Player",
				race: 0,
				members: 1,
				type: 3,
				purpose: 0,
				min: 1,
				max: 90,
				title: "Mine"
			},
			request: null,
			pending: null,
			result: null,
			auto: []
		};
		f.state.gameplay.social = { localName: "Player", self: 1, leader: 0, options: 3, members: [] };
		f.state.gameplay.progression = { level: 10, masteries: [] };
		for ( let t = 0; t < 1600; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyE" } );
		f.ui.step( f.state, 1700 );
		f.ui.event( { kind: "activate", id: "party-match:20" } );
		let scene;
		for ( let t = 1800; t < 2400; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		const quads = f.scenes.at( -1 ).quads,
			ring = quads.filter( q => q.texture.includes( "/msgbox2_window_" ) );
		const bounds = list => [
			Math.min( ...list.map( q => q.rect[0] ) ),
			Math.min( ...list.map( q => q.rect[1] ) ),
			Math.max( ...list.map( q => q.rect[0] + q.rect[2] ) ),
			Math.max( ...list.map( q => q.rect[1] + q.rect[3] ) )
		];
		const [x, y, x2, y2] = bounds( ring );
		assert.deepEqual( [ x2 - x, y2 - y ], [ 360, 151 ] );
		const tiles = quads.filter( q =>
			q.texture.endsWith( "/com_bg_tile_b.png" ) && q.rect[0] >= x && q.rect[1] >= y && q.rect[0] < x2 &&
			q.rect[1] < y2
		);
		assert.deepEqual( bounds( tiles ), [ x + 16, y + 40, x + 360 - 16, y + 151 - 16 ], "the client is tiled" );
		const yes = scene.controls.find( c => c.id === "party-form-confirm" ),
			no = scene.controls.find( c => c.id === "party-form-cancel" );
		assert.deepEqual( [ yes.label, yes.rect ], [ "Yes", [ x + 99, y + 114, 76, 24 ] ] );
		assert.deepEqual( [ no.label, no.rect ], [ "No", [ x + 185, y + 114, 76, 24 ] ] );
	} finally {
		f.dispose();
	}
});

test("party matching native form, local filters and owner approval are actionable without background leakage", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay.partyMatching = {
			page: 0,
			pages: 1,
			rows: [ {
				id: 42,
				party: 0,
				name: "Peer",
				race: 0,
				members: 1,
				type: 3,
				purpose: 0,
				min: 1,
				max: 90,
				title: "Hunting"
			} ],
			own: null,
			request: null,
			pending: null,
			result: null,
			auto: []
		};
		f.state.gameplay.social = { localName: "Player", self: 1, leader: 0, options: 3, members: [] };
		f.state.gameplay.progression = { level: 10, masteries: [] };
		for ( let t = 0; t < 1600; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyE" } );
		let scene = f.ui.step( f.state, 1700 );
		assert.ok( scene.controls.find( c => c.id === "party-match:18" && !c.disabled ) );
		assert.ok( scene.controls.find( c => c.id === "party-match:17" && !c.disabled ) );
		f.ui.event( { kind: "activate", id: "party-match:18" } );
		scene = f.ui.step( f.state, 1800 );
		assert.ok( scene.controls.some( c => c.id === "party-form-title" ) );
		assert.ok( !scene.controls.some( c => c.id === "party-match:18" ) );
		const edit = ( id, value ) =>
			f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
		edit( "party-form-title", "Hunting" );
		f.ui.event( { kind: "activate", id: "party-form-confirm" } );
		assert.equal( sent.at( -1 ).command.kind, "party-match-register" );
		assert.equal( sent.at( -1 ).command.registration.title, "Hunting" );
		f.ui.step( f.state, 1900 );
		edit( "party-search-name", "Nobody" );
		f.ui.event( { kind: "activate", id: "party-match:55" } );
		scene = f.ui.step( f.state, 2000 );
		assert.equal( scene.controls.filter( c => c.id.startsWith( "party-match-row:" ) ).length, 0 );
		f.state.gameplay = {
			...f.state.gameplay,
			partyMatching: {
				...f.state.gameplay.partyMatching,
				request: {
					a: 7,
					b: 42,
					member: { id: 77, name: "Peer", model: 1907, level: 20, region: 1 },
					expires: 20000
				}
			}
		};
		scene = f.ui.step( f.state, 2100 );
		assert.ok( scene.controls.some( c => c.id === "party-answer:1" ) );
		assert.ok( !scene.controls.some( c => c.id === "party-match:18" ) );
		const count = sent.length;
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		assert.equal( sent.length, count );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "party-match-answer", a: 7, b: 42, answer: 2 }
		} );
	} finally {
		f.dispose();
	}
});

test("native party join progress owns input for ten seconds and uses the 200ms gauge clock", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		for ( let t = 0; t < 1600; t += 100 ) f.ui.step( f.state, t );
		f.state.gameplay = {
			...f.state.gameplay,
			partyMatching: {
				auto: [],
				page: 1,
				pages: 1,
				rows: [],
				own: null,
				request: null,
				pending: "join",
				joining: { name: "Peer", since: 2000 }
			}
		};
		let scene = f.ui.step( f.state, 2200 );
		assert.equal( scene.controls.length, 0 );
		assert.ok( f.scenes.at( -1 ).quads.some( q => q.texture.endsWith( "pt_progress.png" ) && q.rect[2] > 0 ) );
		f.ui.event( { kind: "key", code: "KeyI" } );
		assert.equal( sent.length, 0 );
		scene = f.ui.step( f.state, 12000 );
		assert.ok( scene.controls.length > 0 );
		assert.ok( !f.scenes.at( -1 ).quads.some( q => q.texture.endsWith( "pt_progress.png" ) ) );
	} finally {
		f.dispose();
	}
});

test("a pending item move keeps inventory slots enabled and drops further moves", () => {
	// 69B5A0 sets the native move flag for 3 s; 699359 drops a request while it
	// is set, but the slots stay enabled (hover and tooltips keep working).
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		const game = {
			...f.state.gameplay,
			inventory: [ { slot: 14, refObjId: 1, typeFlags: 0x6c, quantity: 10, name: "Stack", magic: [] } ],
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventoryPending: true
		};
		const state = { ...f.state, gameplay: game };
		let now = 0;
		f.ui.step( state, ++now );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		const semantics = f.ui.step( state, ++now );
		const slot = semantics?.controls.find( c => c.id === "slot:14" );
		assert.equal( slot?.disabled, false, "a pending move does not disable the slot" );
		f.ui.event( { kind: "double-activate", id: "slot:14" } );
		f.ui.step( state, ++now );
		assert.deepEqual( sent.filter( c => c?.kind === "inventory-move" ), [], "the second move is dropped" );
	} finally {
		f.dispose();
	}
});

test("an edit normalized back to the published value still republishes the field", () => {
	// 521A85: the edit replaces an oversized draft with its limit. When that
	// limit is already the field's value the semantics compare equal; the UI
	// must publish them anyway, or the field keeps the raw keystrokes.
	const f = uiFixture();
	try {
		const game = {
			...f.state.gameplay,
			inventory: [ { slot: 14, refObjId: 1, typeFlags: 0x6c, quantity: 10, name: "Stack", magic: [] } ],
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventoryPending: false
		};
		const state = { ...f.state, gameplay: game };
		let now = 0;
		f.ui.step( state, ++now );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		f.ui.step( state, ++now );
		f.ui.event( { kind: "activate", id: "slot:14", shift: true } );
		f.ui.step( state, ++now );
		const field = semantics => semantics?.controls.find( c => c.id === "split-amount" )?.value;
		f.ui.event( { kind: "edit", id: "split-amount", value: "99", start: 2, end: 2, composing: false } );
		assert.equal( field( f.ui.step( state, ++now ) ), "9" );
		assert.equal( f.ui.step( state, ++now ), null, "nothing changed, nothing published" );
		f.ui.event( { kind: "edit", id: "split-amount", value: "999", start: 3, end: 3, composing: false } );
		assert.equal( field( f.ui.step( state, ++now ) ), "9", "the clamped field is published again" );
	} finally {
		f.dispose();
	}
});

test("native Shift split clamps to stack minus one, chooses first free bag slot and cancels without mutation", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		const game = {
			...f.state.gameplay,
			inventory: [ { slot: 14, refObjId: 1, typeFlags: 0x6c, quantity: 10, name: "Stack", magic: [] } ],
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventoryPending: false
		};
		const state = { ...f.state, gameplay: game };
		let now = 0;
		const step = () => f.ui.step( state, ++now ),
			event = e => {
				f.ui.event( e );
				step();
			},
			click = id => event( { kind: "activate", id } );
		step();
		click( "open-window:Inventory" );
		const split = () => event( { kind: "activate", id: "slot:14", shift: true } );
		split();
		event( { kind: "activate", id: "slot:15" } );
		assert.equal( sent.length, 0 );
		event( { kind: "edit", id: "split-amount", value: "99", start: 2, end: 2, composing: false } );
		event( { kind: "key", code: "Enter" } );
		assert.deepEqual( sent.pop(), { kind: "inventory-move", source: 14, destination: 13, quantity: 9 } );
		split();
		event( { kind: "key", code: "Escape" } );
		assert.equal( sent.length, 0 );
		assert.equal( f.ui.stats().panel, "Inventory" );
		split();
		event( { kind: "edit", id: "split-amount", value: "0", start: 1, end: 1, composing: false } );
		click( "split-confirm" );
		assert.equal( sent.pop().quantity, 1 );
		split();
		game.inventory = Array.from(
			{ length: 32 },
			( _, i ) => ({ slot: i + 13, refObjId: 1, typeFlags: 0x6c, quantity: 10, magic: [] })
		);
		step();
		click( "split-confirm" );
		assert.equal( sent.length, 0, "full bag closes silently" );
	} finally {
		f.dispose();
	}
});

test("avatar drag resolves storage identity rather than the visible subtype control", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [],
			avatarInventory: [ { slot: 3, refObjId: 1, typeFlags: 0x16ac, quantity: 1, name: "Dress" } ]
		};
		let now = 0;
		for ( ; now < 1200; now += 100 ) f.ui.step( f.state, now );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.step( f.state, ++now );
		f.ui.event( { kind: "activate", id: "equipment-view" } );
		const scene = f.ui.step( f.state, ++now ), dest = scene.controls.find( c => c.id === "slot:20" ).rect;
		f.ui.event( { kind: "drag", id: "avatar:2", dx: 36, dy: 0 } );
		f.ui.event( { kind: "drag-end", id: "avatar:2", x: dest[0] + 16, y: dest[1] + 16 } );
		assert.deepEqual( sent, [ { kind: "avatar-move", equip: false, source: 3, destination: 20 } ] );
	} finally {
		f.dispose();
	}
});

test("avatar attachment admission requires a dress and split modal retires on disconnect", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		const game = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, refObjId: 1, typeFlags: 0x1eac, quantity: 1 }, {
				slot: 14,
				refObjId: 2,
				typeFlags: 0x6c,
				quantity: 10
			} ],
			avatarInventory: []
		};
		const state = { ...f.state, gameplay: game };
		let now = 0;
		for ( ; now < 1200; now += 100 ) f.ui.step( state, now );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.step( state, ++now );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.equal( sent.length, 0 );
		game.avatarInventory = [ { slot: 3, refObjId: 3, typeFlags: 0x16ac, quantity: 1 } ];
		f.ui.step( state, ++now );
		f.ui.event( { kind: "double-activate", id: "slot:13" } );
		assert.deepEqual( sent.pop(), { kind: "avatar-move", equip: true, source: 13, destination: 0 } );
		f.ui.event( { kind: "activate", id: "slot:14", shift: true } );
		f.ui.step( state, ++now );
		const disconnected = { ...state, session: { ...state.session, phase: "disconnected" } };
		const scene = f.ui.step( disconnected, ++now );
		assert.ok( scene.controls.some( c => c.id === "disconnect-confirm" ) );
		assert.ok( !scene.controls.some( c => c.id === "split-confirm" ) );
		f.ui.event( { kind: "key", code: "Enter" } );
		assert.notEqual( sent.at( -1 )?.kind, "inventory-move" );
	} finally {
		f.dispose();
	}
});

test("ground names are hover or hold-only, use gold amount, and invalidate retained UI on release", () => {
	const f = uiFixture();
	try {
		const drop = {
			gid: 123,
			kind: "ground-item",
			regionId: 1,
			x: 20,
			y: 0,
			z: 0,
			heading: 0,
			name: "Gold",
			groundItem: { typeFlags: 0x2ec, goldAmount: 8800, tint: 0 }
		};
		f.state.entities = [ ...f.state.entities, drop ];
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( { ...f.state }, t );
		const labels = () => f.scenes.at( -1 ).quads.filter( q => q.characterAnchor === 123 );
		assert.equal( labels().length, 0 );
		f.ui.step( { ...f.state, dropNamesHeld: true }, 1300 );
		assert.ok( labels().length );
		assert.ok( f.hasText( "8800 Gold" ) );
		assert.equal( labels().filter( q => !q.texture ).length, 1 );
		assert.deepEqual( labels()[0].color, [ 0, 0, 0, 64 / 255 ] );
		assert.ok( labels().slice( 1 ).every( q => q.texture ) );
		assert.ok(
			labels().every( q => q.occlusion === "none" ),
			"ground label backing and every glyph bypass scene depth"
		);
		f.ui.step( { ...f.state, dropNamesHeld: false }, 1301 );
		assert.equal( labels().length, 0 );
		f.ui.step( { ...f.state, hoveredEntity: 123 }, 1302 );
		assert.ok( labels().length );
		f.state.entities = [ f.state.entities[0], { ...drop, x: 300 } ];
		f.ui.step( { ...f.state, dropNamesHeld: true }, 1400 );
		assert.equal( labels().length, 0 );
		f.ui.step( { ...f.state, hoveredEntity: 123 }, 1401 );
		assert.ok( labels().length );
		f.state.entities = [ f.state.entities[0] ];
		f.ui.step( { ...f.state, hoveredEntity: 123, dropNamesHeld: true }, 1402 );
		assert.equal( labels().length, 0 );
	} finally {
		f.dispose();
	}
});

test("matching form derives job from live equipment and refuses a stale job choice", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			partyMatching: {
				page: 1,
				pages: 1,
				rows: [],
				own: null,
				request: null,
				pending: null,
				result: null,
				auto: []
			},
			social: { localName: "Player", self: 1, leader: 0, options: 3, members: [] },
			progression: { level: 10, masteries: [] }
		};
		let now = 0;
		const step = () => f.ui.step( { ...f.state }, ++now );
		for ( let t = 0; t < 1600; t += 100 ) f.ui.step( f.state, t );
		now = 1600;
		f.ui.event( { kind: "key", code: "KeyE" } );
		step();
		for ( let job = 1; job <= 4; job++ ) {
			f.state.gameplay = {
				...f.state.gameplay,
				inventory: job === 4 ?
					[] :
					[ { slot: 8, refObjId: 100, typeFlags: 0x3ac | (job << 11), quantity: 1, magic: [] } ]
			};
			step();
			f.ui.event( { kind: "activate", id: "party-match:18" } );
			const scene = step();
			for ( let purpose = 0; purpose < 4; purpose++ ) {
				assert.equal(
					scene.controls.find( c => c.id === "party-form-purpose:" + purpose ).disabled,
					!(job === 4 ? purpose < 2 : job === 2 ? purpose === 3 : purpose === 2)
				);
			}
			f.ui.event( { kind: "edit", id: "party-form-title", value: "Party", start: 5, end: 5, composing: false } );
			if ( job === 1 ) {
				f.state.gameplay = { ...f.state.gameplay, inventory: [] };
				step();
				const before = sent.length;
				f.ui.event( { kind: "activate", id: "party-form-confirm" } );
				assert.equal( sent.length, before, "removed suit invalidates selected trade purpose" );
			}
			f.ui.event( { kind: "activate", id: "party-form-cancel" } );
			step();
		}
	} finally {
		f.dispose();
	}
});

test("a shortcut carried across an F1-F4 bar switch lands on the shown bar", async () => {
	const { hotbarSlot } = await load( "src/engine/foundation/gameplay/quickslots.ts" );
	const commands = [], f = uiFixture( c => commands.push( c.command ) );
	try {
		f.state.gameplay.skills = [ 7 ];
		f.state.gameplay.quickSlots = [ { slot: 1, kind: 0x49, payload: 7 } ];
		f.state.gameplay.skillCatalog = [ { id: 7, name: "Fixture", cooldownGroup: 0, cooldownMs: 2000 } ];
		let now = 1000, output;
		const draw = () => output = f.ui.step( { ...f.state }, now += 20 ) ?? output;
		const control = id => defined( output ).controls.find( c => c.id === id );
		draw();
		f.ui.event( { kind: "drag", id: "hotbar:1", dx: 4, dy: -20 } );
		draw();
		f.ui.event( { kind: "key", code: "F2" } );
		draw();
		const destination = "hotbar:" + hotbarSlot( 1, 3 );
		assert.equal( control( "hotbar:1" ), undefined, "F2 took the dragged slot's bar off screen" );
		assert.ok( control( destination ), "F2 shows the second bar" );
		f.ui.event( { kind: "drag", id: "hotbar:1", dx: 10, dy: 0 } );
		const target = control( destination );
		f.ui.event( { kind: "drag-end", id: "hotbar:1", x: target.rect[0] + 2, y: target.rect[1] + 2 } );
		draw();
		assert.deepEqual( commands.filter( c => c.kind === "quickslot-set" ), [
			{ kind: "quickslot-set", binding: { slot: 1, kind: 0, payload: 0 } },
			{ kind: "quickslot-set", binding: { slot: hotbarSlot( 1, 3 ), kind: 0x49, payload: 7 } }
		] );
	} finally {
		f.dispose();
	}
});

test("retail extended quickslot layouts, fixed bindings, locks and bottom-bar activation share one path", () => {
	const commands = [], f = uiFixture( c => commands.push( c.command ) );
	try {
		f.state.gameplay.skills = [ 7 ];
		f.state.gameplay.quickSlots = [ { slot: 1, kind: 0x49, payload: 7 }, { slot: 41, kind: 0x49, payload: 7 } ];
		f.state.gameplay.skillCatalog = [ { id: 7, name: "Fixture", cooldownGroup: 0, cooldownMs: 2000 } ];
		let now = 1000, output;
		const draw = () => output = f.ui.step( { ...f.state }, now += 20 ) ?? output,
			click = id => {
				f.ui.event( { kind: "activate", id } );
				return draw();
			};
		draw();
		assert.deepEqual(
			defined( output ).controls.filter( c => c.id.startsWith( "hotbar:" ) ).map( c => Number( c.id.slice( 7 ) ) )
				.sort( (
					a,
					b
				) => a - b ),
			[ 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50 ]
		);
		const first = defined( output ).controls.find( c => c.id === "hotbar:41" ),
			last = defined( output ).controls.find( c => c.id === "hotbar:50" );
		assert.deepEqual( first.rect, [ 1499, 212, 32, 32 ] );
		assert.deepEqual( [ last.rect[0] - first.rect[0], last.rect[1] - first.rect[1] ], [ 36, 144 ] );
		assert.ok( f.ui.blocks( first.rect[0] + 1, first.rect[1] + 1 ) );
		click( "hotbar:41" );
		assert.deepEqual( commands.pop(), { kind: "skill", skillId: 7 } );
		click( "ext-horizontal" );
		const horizontal = defined( output ).controls.find( c => c.id === "hotbar:50" );
		const hfirst = defined( output ).controls.find( c => c.id === "hotbar:41" );
		assert.deepEqual( [ horizontal.rect[0] - hfirst.rect[0], horizontal.rect[1] - hfirst.rect[1] ], [ 144, 36 ] );
		click( "ext-options" );
		click( "ext-double" );
		click( "ext-options-apply" );
		assert.equal(
			defined( output ).controls.find( c => c.id === "hotbar:50" ).rect[0] -
				defined( output ).controls.find( c => c.id === "hotbar:41" ).rect[0],
			324
		);
		click( "ext-options-close" );
		const target = defined( output ).controls.find( c => c.id === "hotbar:50" );
		f.ui.event( { kind: "drag-end", id: "hotbar:1", x: target.rect[0] + 2, y: target.rect[1] + 2 } );
		draw();
		assert.deepEqual( commands.splice( 0 ), [
			{ kind: "quickslot-set", binding: { slot: 1, kind: 0, payload: 0 } },
			{ kind: "quickslot-set", binding: { slot: 50, kind: 0x49, payload: 7 } }
		], "moving to the extended bar clears the source before publishing the destination" );
		click( "ext-options" );
		click( "ext-slot-lock" );
		click( "ext-options-ok" );
		f.ui.event( { kind: "drag-end", id: "hotbar:1", x: target.rect[0] + 2, y: target.rect[1] + 2 } );
		draw();
		assert.equal( commands.length, 0 );
		click( "ext-open" );
		assert.equal(
			defined( output ).controls.filter( c => c.id.startsWith( "hotbar:" ) && Number( c.id.slice( 7 ) ) >= 41 )
				.length,
			0
		);
		click( "ext-open" );
		for ( const path of f.requested.filter( path => path.includes( "/quick_slot/" ) ) ) {
			assert.doesNotThrow(
				() => readFileSync( CLIENT_PUBLIC_ROOT + path ),
				"Quickslot requests only published authored textures: " + path
			);
		}
		for ( let page = 0; page < 4; page++ ) {
			f.ui.event( { kind: "key", code: "F" + (page + 1) } );
			draw();
			assert.ok( defined( output ).controls.some( c => c.id === "hotbar:" + (page * 10 + 1) ) );
			assert.ok( defined( output ).controls.some( c => c.id === "hotbar:41" ) );
		}
		f.ui.event( { kind: "key", code: "F1" } );
		draw();
		click( "open-window:Inventory" );
		f.ui.event( { kind: "key", code: "Digit1" } );
		assert.deepEqual( commands.pop(), { kind: "skill", skillId: 7 } );
		f.state.gameplay.skillCooldowns = [ { skill: 7, group: 0, startedAtMs: now, durationMs: 2000 } ];
		draw();
		// A cooling-down press is forwarded: the worker holds or denies it
		// (skill-queue.ts) and the slot keeps drawing the cooldown.
		click( "hotbar:41" );
		assert.deepEqual( commands.pop(), { kind: "skill", skillId: 7 } );
		assert.ok( f.scenes.at( -1 ).quads.some( q => q.texture.endsWith( "/skill_delay.png" ) ) );
	} finally {
		f.dispose();
	}
});

test("dragging shortcuts into the world clears either bar and respects locked slots and occupied destinations", () => {
	const commands = [], f = uiFixture( c => commands.push( c.command ) );
	try {
		const game = f.state.gameplay;
		game.skills = [ 7 ];
		game.quickSlots = [ { slot: 1, kind: 0x49, payload: 7 }, { slot: 41, kind: 0x49, payload: 7 } ];
		game.skillCatalog = [ { id: 7, name: "Fixture", cooldownGroup: 0, cooldownMs: 2000 } ];
		let now = 1000, output;
		const draw = () => output = f.ui.step( { ...f.state }, now += 20 ) ?? output;
		draw();
		const world = [ 800, 400 ];
		assert.equal( f.ui.blocks( ...world ), false );
		const drop = ( id, x = world[0], y = world[1] ) => {
			f.ui.event( { kind: "drag", id, dx: 100, dy: 100 } );
			f.ui.event( { kind: "drag-end", id, x, y } );
			draw();
		};
		for ( const slot of [ 1, 41 ] ) {
			drop( "hotbar:" + slot );
			assert.deepEqual( commands.splice( 0 ), [ {
				kind: "quickslot-set",
				binding: { slot, kind: 0, payload: 0 }
			} ] );
		}
		assert.deepEqual( game.skills, [ 7 ], "removing a shortcut does not unlearn the skill" );
		const original = defined( output ).controls.find( c => c.id === "hotbar:1" );
		drop( "hotbar:1", original.rect[0] + 2, original.rect[1] + 2 );
		assert.deepEqual( commands, [] );
		const destination = defined( output ).controls.find( c => c.id === "hotbar:41" );
		drop( "hotbar:1", destination.rect[0] + 2, destination.rect[1] + 2 );
		assert.equal( commands.splice( 0 ).length, 2, "slot swap owns both writes without a third clear" );
		for ( const id of [ "ext-options", "ext-slot-lock", "ext-options-ok" ] ) {
			f.ui.event( { kind: "activate", id } );
			draw();
		}
		drop( "hotbar:41" );
		assert.deepEqual( commands, [], "locked extended shortcut cannot be removed" );
		drop( "hotbar:1" );
		assert.deepEqual( commands.splice( 0 ), [ {
			kind: "quickslot-set",
			binding: { slot: 1, kind: 0, payload: 0 }
		} ], "extended lock does not lock the bottom bar" );
		drop( "skill:7" );
		drop( "hotbar:2" );
		drop( "hotbar:1", -10, -10 );
		assert.deepEqual(
			commands,
			[],
			"catalog, empty slots and releases outside the viewport cannot clear a shortcut"
		);
	} finally {
		f.dispose();
	}
});

test("skill training UI rechecks SP at confirmation, waits for authority, and exposes free first mastery training", () => {
	const commands = [], f = uiFixture( c => commands.push( c.command ) );
	try {
		const base = {
			id: 3,
			group: 174,
			level: 1,
			name: "SKILL_CH_SWORD_SMASH_A",
			nameSymbol: "SN_SKILL_CH_SWORD_SMASH_A",
			icon: "skill/china/sword_smash_a.ddj",
			spCost: 1,
			trainable: true,
			targetRequired: true,
			cooldownMs: 3000,
			masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		};
		const game = f.state.gameplay;
		game.skills = [ 3 ];
		game.skillCatalog = [ base, {
			...base,
			id: 291,
			level: 2,
			spCost: 5,
			masteries: [ { ID: 257, Level: 7 }, { ID: 0, Level: 0 } ]
		} ];
		game.progression = { level: 10, skillPoints: 4, masteries: [ { id: 257, level: 7 } ] };
		let now = 1000, output;
		const draw = () => output = f.ui.step( { ...f.state, gameplay: { ...game } }, now += 20 ) ?? output,
			click = id => {
				f.ui.event( { kind: "activate", id } );
				return draw();
			};
		draw();
		click( "open-window:Skills" );
		assert.ok( !defined( output ).controls.some( c => c.id === "skill-learn:291" ) );
		game.progression = { ...game.progression, skillPoints: 5 };
		draw();
		assert.ok(
			defined( output ).controls.some( c => c.id === "skill-learn:291" ),
			JSON.stringify( {
				stats: f.ui.stats(),
				controls: defined( output ).controls.filter( c => c.id.startsWith( "skill" ) ).map( c => c.id )
			} )
		);
		click( "skill-learn:291" );
		assert.ok( defined( output ).controls.some( c => c.id === "skill-confirm-ok" ) );
		// 5DE4E0 / 5DDF00: the slot frame (control 11) belongs to the skill face only.
		const slotFrame = () => f.scenes.at( -1 ).quads.some( q => q.texture.includes( "msgbox_itemwindow" ) );
		assert.ok( slotFrame(), "the skill face draws its slot frame" );
		game.progression = { ...game.progression, skillPoints: 4 };
		draw();
		click( "skill-confirm-ok" );
		assert.equal( commands.length, 0, "SP changed while confirmation was open" );
		assert.deepEqual( game.skills, [ 3 ] );
		game.progression = { ...game.progression, skillPoints: 5 };
		draw();
		click( "skill-learn:291" );
		click( "skill-confirm-ok" );
		assert.deepEqual( commands.pop(), { kind: "skill-train", id: 291 } );
		assert.deepEqual( game.skills, [ 3 ], "sending never invents the learned successor" );
		// 588AF0 has no pending gate: the button stays while a request is in
		// flight, and confirming it sends nothing until the first is answered.
		game.trainingPending = true;
		draw();
		assert.ok( defined( output ).controls.some( c => c.id === "skill-learn:291" ), "the button does not blink" );
		click( "skill-learn:291" );
		click( "skill-confirm-ok" );
		assert.equal( commands.length, 0, "a pending request blocks a second one" );
		game.trainingPending = false;
		game.progression = { level: 10, skillPoints: 0, masteries: [ { id: 257, level: 0 } ] };
		draw();
		assert.equal( defined( output ).controls.find( c => c.id === "mastery:257" ).disabled, false );
		// 5DE040's mastery face confirms first: the board's level-up sends nothing.
		click( "mastery:257" );
		assert.equal( commands.length, 0, "a mastery level-up waits for the practice box" );
		assert.ok( defined( output ).controls.some( c => c.id === "skill-confirm-ok" ) );
		assert.ok( !slotFrame(), "the mastery face hides the empty slot frame" );
		click( "skill-confirm-cancel" );
		assert.equal( commands.length, 0, "cancel sends nothing" );
		click( "mastery:257" );
		click( "skill-confirm-ok" );
		assert.deepEqual( commands.pop(), { kind: "mastery-train", id: 257 } );
		game.progression = { ...game.progression, masteries: [ { id: 257, level: 4 } ], skillPoints: 1 };
		draw();
		assert.ok( !defined( output ).controls.some( c => c.id === "mastery:257" ) );
		game.progression = { ...game.progression, skillPoints: 2 };
		draw();
		assert.equal( defined( output ).controls.find( c => c.id === "mastery:257" ).disabled, false );
		f.ui.event( { kind: "hover", id: "mastery-info:257" } );
		draw();
		assert.ok( f.hasText( "Required skillpoint : 2" ) ); // 55AB40 is bound to slot kind 0x50, the mastery icon.
	} finally {
		f.dispose();
	}
});

test("NPC portal destination menu uses native talk controls and sends the selected source GID", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			target: 17,
			targetCapabilities: 0x80,
			npcConversation: { phase: "menu", gid: 17 }
		};
		f.state.entities.push( { ...f.state.entities[0], gid: 17, refObjId: 2011, kind: "npc", name: "Ferry" } );
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
		let now = 0;
		const settle = () => {
			for ( let i = 0; i < 40; i++ ) semantics = f.ui.step( f.state, now++ ) ?? semantics;
		};
		settle();
		assert.ok( defined( semantics ).controls.some( c => c.id === "npc-portal-open" ) );
		assert.ok(
			!defined( semantics ).controls.some( c => c.id === "npc-talk" ),
			"portal-only capability cannot invent conversation"
		);
		f.ui.event( { kind: "activate", id: "npc-portal-open" } );
		settle();
		assert.ok( defined( semantics ).controls.some( c => c.id === "npc-portal:9" ) );
		assert.equal(
			new Set( defined( semantics ).controls.map( c => c.id ) ).size,
			defined( semantics ).controls.length
		);
		f.ui.event( { kind: "activate", id: "npc-portal:9" } );
		assert.deepEqual( sent.at( -1 ), { kind: "travel-gate", gid: 17, type: 2, target: 9 } );
		f.state.gameplay = { ...f.state.gameplay, target: 0, npcConversation: { phase: "closed" } };
		settle();
		const count = sent.length;
		f.ui.event( { kind: "activate", id: "npc-portal:9" } );
		assert.equal( sent.length, count, "retired NPC destination cannot send travel" );
	} finally {
		f.dispose();
	}
});

test("GPU merchant menu branches retain all tabs, sparse pages and native purchase identities", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		const game = f.state.gameplay;
		game.target = 17;
		game.targetCapabilities = 3;
		game.inventorySlotCount = 109;
		game.equipmentSlotCount = 13;
		game.progression = { gold: "5000", masteries: [] };
		game.npcConversation = { phase: "menu", gid: 17 };
		f.state.entities.push( {
			...f.state.entities[0],
			gid: 17,
			kind: "npc",
			name: "Armor merchant",
			merchantBranches: [ { id: 850, labelSymbol: "SN_STORE_ARMOR_GROUP1", tabs: [ 0, 1, 2 ] }, {
				id: 851,
				labelSymbol: "SN_STORE_ARMOR_GROUP2",
				tabs: [ 3, 4, 5 ]
			} ]
		} );
		game.shop = {
			npc: 17,
			name: "Armor merchant",
			tabs: Array.from( { length: 6 }, ( _, i ) => ({ index: i, labelSymbol: "SN_TAB_HEAVYARMOR" }) ),
			offers: Array.from(
				{ length: 6 },
				( _, i ) => ({
					tab: i,
					slot: i === 5 ? 31 : 0,
					refObjId: 3630,
					name: "Item",
					price: "60",
					maxStack: 50
				})
			)
		};
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
		let now = 100;
		const settle = () => {
			for ( let i = 0; i < 40; i++ ) semantics = f.ui.step( f.state, now++ ) ?? semantics;
		};
		const click = id => {
			f.ui.event( { kind: "activate", id } );
			if ( id.startsWith( "shop-group:" ) ) game.shopCompletionRevision = (game.shopCompletionRevision ?? 0) + 1;
			settle();
		};
		settle();
		assert.ok( defined( semantics ).controls.some( c => c.id === "shop-group:850" ) );
		assert.ok( defined( semantics ).controls.some( c => c.id === "shop-group:851" ) );
		assert.ok( !defined( semantics ).controls.some( c => c.id === "shop-open" ) );
		click( "shop-group:851" );
		assert.deepEqual(
			defined( semantics ).controls.filter( c => c.id.startsWith( "shop-tab:" ) ).map( c => c.id ),
			[
				"shop-tab:3",
				"shop-tab:4",
				"shop-tab:5"
			]
		);
		click( "shop-tab:5" );
		assert.ok( !defined( semantics ).controls.some( c => c.id === "shop-offer:5" ) );
		click( "shop-next" );
		assert.ok( defined( semantics ).controls.some( c => c.id === "shop-offer:5" ) );
		// 5B28F0: the open tab's button keeps the page; another tab resets it.
		click( "shop-tab:5" );
		assert.ok( defined( semantics ).controls.some( c => c.id === "shop-offer:5" ) );
		click( "shop-tab:4" );
		click( "shop-tab:5" );
		assert.ok( !defined( semantics ).controls.some( c => c.id === "shop-offer:5" ) );
		click( "shop-next" );
		click( "shop-offer:5" );
		click( "shop-trade" );
		assert.deepEqual( sent.at( -1 ), { kind: "shop-buy", tab: 5, slot: 31, quantity: 1 } );
		click( "close" );
		click( "shop-group:850" );
		assert.deepEqual(
			defined( semantics ).controls.filter( c => c.id.startsWith( "shop-tab:" ) ).map( c => c.id ),
			[
				"shop-tab:0",
				"shop-tab:1",
				"shop-tab:2"
			]
		);
	} finally {
		f.dispose();
	}
});

test("native window sisters retain drag placement, close on ESC and reject retired captures", () => {
	const f = uiFixture();
	f.state.gameplay.academy = { rows: [], member: null, request: null };
	// The pet window follows native admission: a living owned COS must exist.
	f.state.gameplay.cosRecords = [ { gid: 7, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false } ];
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
	/*
	================
	settle

	Drain dependent window resources before asserting placement or capture state.
	================
	*/
	const settle = () => {
		for ( let i = 0; i < 50; i++ ) semantics = f.ui.step( f.state, 1000 + i ) ?? semantics;
	};
	try {
		settle();
		for (
			const panel of [
				"Option",
				"Auto Potion",
				"Blocking",
				"Party Matching",
				"Academy Matching",
				"Guild",
				"Shop",
				"COS inventory",
				"Alchemy",
				"Actions"
			]
		) {
			f.ui.event( { kind: "activate", id: "open-window:" + panel } );
			settle();
			const panelIds = defined( semantics ).controls.map( c => c.id );
			assert.equal(
				new Set( panelIds ).size,
				panelIds.length,
				panel + " publishes unique control ids for DOM hit-testing"
			);
			const id = panel === "Actions" ? "main-popup-drag" : "window-drag:" + panel;
			const before = defined( semantics ).controls.find( c => c.id === id );
			assert.ok( before?.draggable, panel + " publishes native drag control: " + JSON.stringify( f.ui.stats() ) );
			// Left/up remains within every fixture window's viewport.
			f.ui.event( { kind: "drag", id, dx: -25, dy: -20 } );
			settle();
			const moved = defined( semantics ).controls.find( c => c.id === id );
			assert.deepEqual( moved.rect.slice( 0, 2 ), [ before.rect[0] - 25, before.rect[1] - 20 ], panel );
			f.ui.event( { kind: "key", code: "Escape" } );
			settle();
			assert.equal( f.ui.stats().panel, "", panel + " closes, not opens System" );
			f.ui.event( { kind: "drag", id, dx: -100, dy: -100 } );
			f.ui.event( { kind: "activate", id: "open-window:" + panel } );
			settle();
			assert.deepEqual(
				defined( semantics ).controls.find( c => c.id === id ).rect.slice( 0, 2 ),
				moved.rect.slice( 0, 2 ),
				panel + " ignores stale capture and retains placement"
			);
			f.ui.event( { kind: "activate", id: "close" } );
			settle();
			assert.equal( f.ui.stats().panel, "" );
		}
	} finally {
		f.dispose();
	}
});

test("shop-side inventory publishes companion close on shared popup placement", () => {
	const f = uiFixture();
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Shop" } );
		let result;
		for ( let i = 1; i < 15; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.ok( result );
		assert.deepEqual( result.controls.find( c => c.id === "companion-close" )?.rect, [ 1574, 432, 16, 16 ] );
		const before = result.controls.find( c => c.id === "main-popup-drag" ).rect;
		f.ui.event( { kind: "drag", id: "main-popup-drag", dx: -100, dy: -50 } );
		for ( let i = 15; i < 20; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.deepEqual( result.controls.find( c => c.id === "main-popup-drag" ).rect.slice( 0, 2 ), [
			before[0] - 100,
			before[1] - 50
		] );
		assert.deepEqual( result.controls.find( c => c.id === "companion-close" ).rect.slice( 0, 2 ), [
			1574 - 100,
			432 - 50
		] );
		f.ui.event( { kind: "activate", id: "companion-close" } );
		for ( let i = 20; i < 25; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.equal( f.ui.stats().panel, "" );
	} finally {
		f.dispose();
	}
});

test("NPC ESC uses target release; modal input cannot move or scroll the underlying window", () => {
	const commands = [], f = uiFixture( c => commands.push( c ) );
	f.state.entities.push( { gid: 7, kind: "npc", name: "NPC", regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	f.state.gameplay = {
		...f.state.gameplay,
		target: 7,
		targetCapabilities: 2,
		npcConversation: { phase: "menu", gid: 7, dialogueRevision: 0 }
	};
	/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
	const settle = () => {
		for ( let i = 0; i < 50; i++ ) semantics = f.ui.step( f.state, 1000 + i ) ?? semantics;
	};
	try {
		settle();
		const initial = defined( semantics ).controls.find( c => c.id === "npc-drag" );
		assert.deepEqual( initial.rect, [ 210, 200, 365, 34 ] );
		const npcIds = defined( semantics ).controls.map( c => c.id );
		assert.equal( new Set( npcIds ).size, npcIds.length, "NPC controls publish unique ids for DOM hit-testing" );
		f.ui.event( { kind: "drag", id: "npc-drag", dx: 60, dy: 40 } );
		settle();
		assert.deepEqual( defined( semantics ).controls.find( c => c.id === "npc-talk" ).rect.slice( 0, 2 ), [
			294,
			311
		] );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.deepEqual( commands.at( -1 ), { kind: "gameplay", command: { kind: "npc-close" } } );
		assert.equal( f.ui.stats().panel, "" );
		f.state.gameplay.npcConversation = { phase: "closed" };
		settle();
		f.ui.event( { kind: "drag", id: "npc-drag", dx: 100, dy: 100 } );
		f.state.gameplay.npcConversation = { phase: "menu", gid: 7, dialogueRevision: 0 };
		settle();
		assert.deepEqual( defined( semantics ).controls.find( c => c.id === "npc-drag" ).rect.slice( 0, 2 ), [
			270,
			240
		] );
		f.state.session = { phase: "disconnected", revision: 2, character: "Player", error: "lost" };
		settle();
		const count = commands.length;
		f.ui.event( { kind: "drag", id: "npc-drag", dx: 100, dy: 100 } );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.equal( commands.length, count );
		f.state.session = { phase: "world", revision: 3, character: "Player" };
		settle();
		assert.deepEqual( defined( semantics ).controls.find( c => c.id === "npc-drag" ).rect.slice( 0, 2 ), [
			270,
			240
		] );
	} finally {
		f.dispose();
	}
});

test("cold service windows cannot republish the companion inventory as their retained frame", () => {
	let held = true;
	const f = uiFixture( () => {}, path => held && path.endsWith( "/alcm_window_2.png" ) );
	try {
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Alchemy" } );
		/** @type {import("../../src/engine/contracts/ui.ts").UiSemantics | null | undefined} */ let semantics;
		for ( let i = 1; i < 35; i++ ) {
			semantics = f.ui.step( f.state, i * 100 ) ?? semantics;
			if ( semantics ) {
				const ids = semantics.controls.map( c => c.id );
				assert.equal(
					new Set( ids ).size,
					ids.length,
					"cold admission must preserve each control instance exactly once"
				);
			}
		}
		assert.ok( !semantics.controls.some( c => c.id === "window-drag:Alchemy" ) );
		assert.ok( semantics.controls.some( c => c.id === "companion-close" ) );
		held = false;
		for ( let i = 35; i < 50; i++ ) semantics = f.ui.step( f.state, i * 100 ) ?? semantics;
		assert.ok( semantics.controls.some( c => c.id === "window-drag:Alchemy" ) );
	} finally {
		f.dispose();
	}
});

test("low-HP caution atlas keeps its 100 ms cadence on retained world frames", async () => {
	const { defaultGameOptions } = await load( "src/engine/foundation/gameplay/game-options.ts" );
	const f = uiFixture();
	try {
		f.ui.event( { kind: "preferences", value: defaultGameOptions() } );
		f.state.frontend = { phase: "world", generation: 1, error: null, selectedCharacter: "Player" };
		f.state.gameplay = { ...f.state.gameplay, vitals: [ { gid: 1, hp: 10, mp: 100, maxHp: 100, maxMp: 100 } ] };
		for ( let now = 0; now <= 1000; now += 100 ) f.ui.step( f.state, now );
		for ( let i = 0; i < 250; i++ ) f.ui.step( f.state, 1000 );
		const cell = () =>
			f.scenes.at( -1 ).quads.find( q => q.uv[2] === .25 && q.uv[3] === .5 )?.uv.slice( 0, 2 ).join( "," );
		const published = f.scenes.length, cells = new Set( [ cell() ] );
		assert.ok( cells.has( "0.5,0" ), "frame 10 % 8 = cell 2" );
		for ( let now = 1004; now < 1800; now += 4 ) {
			f.ui.step( { ...f.state, frontend: { ...f.state.frontend, elapsed: now } }, now );
			cells.add( cell() );
		}
		assert.equal( cells.size, 8, "all eight caution cells play without new packets" );
		assert.equal(
			f.scenes.length - published,
			7,
			"camera-only frames rebuild once per caution cell, not every frame"
		);
	} finally {
		f.dispose();
	}
});

test("mini HP/MP and target HP animate without new packets, retain through resize and reset on selection", () => {
	const f = uiFixture();
	try {
		f.state.frontend = { phase: "world" };
		f.state.gameplay.vitals = [ { gid: 1, hp: 50, mp: 30, maxHp: 100, maxMp: 100 } ];
		const bars = suffix => f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( suffix ) );
		for ( let i = 0; i < 250; i++ ) f.ui.step( { ...f.state }, 1000 );
		const before = bars( "pmi_hp.png" ).at( -1 ).rect[2];
		assert.ok( before > 0 );
		f.state.gameplay = { ...f.state.gameplay, vitals: [ { gid: 1, hp: 60, mp: 20, maxHp: 100, maxMp: 100 } ] };
		f.rawStep( { ...f.state }, 3000 );
		let hp = bars( "pmi_hp.png" ), mp = bars( "pmi_mp.png" );
		assert.equal( hp.length, 2 );
		assert.equal( mp.length, 2 );
		assert.equal( hp[0].color[3], 96 / 255 );
		assert.equal( hp[1].rect[2], before );
		assert.ok(
			Object.keys( fontAtlas.fonts ).some( font => f.hasText( "60 / 100", font ) ),
			"numbers reflect the packet immediately"
		);
		f.rawStep( { ...f.state }, 3016 );
		assert.ok( bars( "pmi_hp.png" )[1].rect[2] > before, "retained-world earlyout must not stop interpolation" );
		const advancing = bars( "pmi_hp.png" )[1].rect[2];
		f.state.width += 100;
		f.rawStep( { ...f.state }, 3032 );
		assert.ok( bars( "pmi_hp.png" )[1].rect[2] > advancing, "resize must not reset current" );
		const monster = {
			gid: 17,
			kind: "monster",
			name: "Manyang",
			level: 1,
			maxHp: 100,
			rarity: 0,
			regionId: 1,
			x: 0,
			y: 0,
			z: 0,
			heading: 0
		};
		f.state.entities = [ ...f.state.entities, monster ];
		f.state.gameplay = {
			...f.state.gameplay,
			target: 17,
			vitals: [ ...f.state.gameplay.vitals, { gid: 17, hp: 80 } ]
		};
		f.ui.step( { ...f.state }, 4000 );
		const targetBars = () =>
			f.scenes.at( -1 ).quads.filter( q => q.texture.includes( "targetwindow" ) && q.rect[3] === 4 );
		const bound = targetBars();
		assert.equal( bound.length, 1 );
		const width = bound[0].rect[2];
		f.state.gameplay = { ...f.state.gameplay, vitals: [ f.state.gameplay.vitals[0], { gid: 17, hp: 40 } ] };
		f.rawStep( { ...f.state }, 4016 );
		assert.equal( targetBars().length, 2 );
		assert.equal( targetBars()[0].rect[2], width );
		f.rawStep( { ...f.state }, 4032 );
		assert.ok( targetBars()[0].rect[2] < width );
		f.state.entities = [ f.state.entities[0], { ...monster, gid: 18 } ];
		f.state.gameplay = {
			...f.state.gameplay,
			target: 18,
			vitals: [ f.state.gameplay.vitals[0], { gid: 18, hp: 20 } ]
		};
		f.rawStep( { ...f.state }, 4048 );
		assert.equal( targetBars().length, 1, "new target cannot inherit previous target health" );
	} finally {
		f.dispose();
	}
});

test("quick-party vitals interpolate independently and retire on member removal", () => {
	const f = uiFixture();
	try {
		f.state.frontend = { phase: "world" };
		const member = { id: 2, name: "Peer", status: 0x85, level: 5 };
		f.state.gameplay.social = {
			self: 1,
			localName: "Player",
			leader: 1,
			members: [ { id: 1, name: "Player", status: 0xaa, level: 5 }, member ]
		};
		for ( let i = 0; i < 35; i++ ) f.ui.step( { ...f.state }, 1000 );
		const bars = name => f.scenes.at( -1 ).quads.filter( q => q.texture.endsWith( "qpt_" + name + ".png" ) );
		assert.equal( bars( "hp" ).length, 1 );
		const width = bars( "hp" )[0].rect[2];
		f.state.gameplay = {
			...f.state.gameplay,
			social: {
				...f.state.gameplay.social,
				members: [ f.state.gameplay.social.members[0], { ...member, status: 0x43 } ]
			}
		};
		f.rawStep( { ...f.state }, 1016 );
		assert.equal( bars( "hp" ).length, 2 );
		assert.equal( bars( "hp" )[0].rect[2], width );
		assert.equal( bars( "mp" ).length, 2 );
		f.rawStep( { ...f.state }, 1032 );
		assert.ok( bars( "hp" )[0].rect[2] < width );
		f.state.gameplay = {
			...f.state.gameplay,
			social: { ...f.state.gameplay.social, members: [ f.state.gameplay.social.members[0] ] }
		};
		f.rawStep( { ...f.state }, 1048 );
		assert.equal( bars( "hp" ).length, 0 );
		f.state.gameplay = {
			...f.state.gameplay,
			social: {
				...f.state.gameplay.social,
				members: [ ...f.state.gameplay.social.members, { ...member, status: 0xaa } ]
			}
		};
		f.rawStep( { ...f.state }, 1064 );
		assert.equal( bars( "hp" ).length, 1, "rejoined member must not inherit its retired gauge" );
	} finally {
		f.dispose();
	}
});

test("disconnect confirmation refreshes discovery after cookie restoration skipped the server list", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const login = {
			...f.state,
			session: { phase: "signed-out", revision: 1 },
			gameplay: null,
			entities: [],
			frontend: { phase: "login", generation: 1, elapsed: 1, alpha: 1, logoAlpha: 0, error: null }
		};
		f.ui.step( login, 0 );
		assert.equal( sent.filter( c => c.kind === "servers" ).length, 1 );
		f.ui.step( { ...login, session: { phase: "listing-servers", revision: 2 } }, 100 );
		// The restore response proceeds to world without a servers property.
		const world = {
			...f.state,
			frontend: { phase: "world" },
			session: { phase: "world", revision: 3, character: "Player" }
		};
		f.ui.step( world, 200 );
		f.ui.step( { ...world, session: { ...world.session, phase: "disconnected", revision: 4 } }, 300 );
		f.ui.event( { kind: "activate", id: "disconnect-confirm" } );
		assert.equal( sent.at( -1 ).kind, "logout" );
		f.ui.step( { ...login, session: { phase: "authenticating", revision: 5 } }, 400 );
		f.ui.step( { ...login, session: { phase: "signed-out", revision: 6 } }, 500 );
		assert.equal(
			sent.filter( c => c.kind === "servers" ).length,
			2,
			"return to title must discover servers again"
		);
		f.ui.step( { ...login, session: { phase: "listing-servers", revision: 7 } }, 600 );
		const ready = {
			...login,
			session: {
				phase: "signed-out",
				revision: 8,
				servers: [ { id: "offline", name: "Test", operating: false }, {
					id: "first",
					name: "Global",
					operating: true
				} ]
			}
		};
		f.ui.step( ready, 700 );
		f.ui.step( ready, 800 );
		assert.equal(
			sent.filter( c => c.kind === "servers" ).length,
			2,
			"list completion must not start a request loop"
		);
		for ( const [id, value] of [ [ "account", "fixture" ], [ "password", "fixture-only" ] ] ) {
			f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
		}
		f.ui.event( { kind: "activate", id: "submit" } );
		assert.equal( sent.at( -1 ).kind, "login" );
		assert.equal( sent.at( -1 ).serverId, "first" );
	} finally {
		f.dispose();
	}
});

/*
================
server list stays usable while a refresh is in flight

The 2026-09-29 release probe clicked LIST, a row and accept during the refresh
the login reveal starts. Every server control was disabled for that round trip,
so the overlay dropped the clicks and the list never opened.
================
*/
test("server list stays usable while a refresh is in flight", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const state = {
			...f.state,
			gameplay: null,
			entities: [],
			frontend: { phase: "login", generation: 1, elapsed: 1, alpha: 1, logoAlpha: 0, error: null }
		};
		const listed = [ { id: "first", name: "First", operating: true }, {
			id: "second",
			name: "Second",
			operating: true
		} ];
		let semantics = f.ui.step( { ...state, session: { phase: "signed-out", revision: 1, servers: listed } }, 0 );
		f.ui.step( { ...state, session: { phase: "listing-servers", revision: 2 } }, 100 );
		const refreshing = { ...state, session: { phase: "listing-servers", revision: 2 } };
		const control = id => defined( semantics ).controls.find( c => c.id === id );
		semantics = f.ui.step( refreshing, 200 ) ?? semantics;
		assert.equal( control( "native:servers" )?.disabled, false, "LIST must accept a click during a refresh" );
		const requests = sent.filter( c => c.kind === "servers" ).length;
		f.ui.event( { kind: "activate", id: "native:servers" } );
		assert.equal(
			sent.filter( c => c.kind === "servers" ).length,
			requests,
			"an in-flight refresh is not restarted"
		);
		semantics = f.ui.step( refreshing, 300 ) ?? semantics;
		semantics = f.ui.step( refreshing, 900 ) ?? semantics;
		assert.equal( control( "server:second" )?.disabled, false, "rows stay selectable during a refresh" );
		f.ui.event( { kind: "activate", id: "server:second" } );
		semantics = f.ui.step( refreshing, 1000 ) ?? semantics;
		assert.equal( control( "native:server-accept" )?.disabled, false );
		f.ui.event( { kind: "activate", id: "native:server-accept" } );
		for ( const [id, value] of [ [ "account", "fixture" ], [ "password", "fixture-only" ] ] ) {
			f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
		}
		f.ui.step( { ...state, session: { phase: "signed-out", revision: 3, servers: listed } }, 1100 );
		f.ui.event( { kind: "activate", id: "submit" } );
		assert.equal( sent.at( -1 ).kind, "login" );
		assert.equal( sent.at( -1 ).serverId, "second" );
	} finally {
		f.dispose();
	}
});

test("refreshed server list preserves a usable selection and replaces an unavailable one", () => {
	for ( const operating of [ true, false ] ) {
		const sent = [], f = uiFixture( command => sent.push( command ) );
		try {
			const state = {
				...f.state,
				gameplay: null,
				entities: [],
				frontend: { phase: "login", generation: 1, elapsed: 1, alpha: 1, logoAlpha: 0, error: null }
			};
			f.ui.step( { ...state, session: { phase: "signed-out", revision: 1 } }, 0 );
			f.ui.step( {
				...state,
				session: {
					phase: "signed-out",
					revision: 2,
					servers: [ { id: "chosen", name: "Chosen", operating: true } ]
				}
			}, 100 );
			f.ui.event( { kind: "activate", id: "native:servers" } );
			f.ui.step( { ...state, session: { phase: "listing-servers", revision: 3 } }, 200 );
			f.ui.step( {
				...state,
				session: {
					phase: "signed-out",
					revision: 4,
					servers: [ { id: "first", name: "First", operating: true }, {
						id: "chosen",
						name: "Chosen",
						operating
					} ]
				}
			}, 300 );
			f.ui.event( { kind: "key", code: "Escape" } );
			for ( const [id, value] of [ [ "account", "fixture" ], [ "password", "fixture-only" ] ] ) {
				f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
			}
			f.ui.event( { kind: "activate", id: "submit" } );
			assert.equal( sent.at( -1 ).kind, "login" );
			assert.equal( sent.at( -1 ).serverId, operating ? "chosen" : "first" );
		} finally {
			f.dispose();
		}
	}
});

test("native popup menu, selected tab weight, localized EU mastery and empty-party branches", () => {
	const f = uiFixture();
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			guide: { country: 1 },
			progression: { level: 1, masteries: [ { id: 513, level: 0 }, { id: 515, level: 0 } ] },
			academy: { member: false, rows: [], request: null },
			social: { leader: 0, members: [], options: 0 }
		};
		let now = 0;
		const settle = () => {
			for ( let i = 0; i < 50; i++ ) f.ui.step( f.state, ++now );
		};
		settle();
		f.ui.event( { kind: "activate", id: "hud-menu" } );
		settle();
		assert.equal( f.ui.stats().panel, "Character" );
		assert.ok( f.hasText( "<Nothing>" ) );
		assert.ok( f.hasText( "Not..." ), "unregistered status fits its 28px honor-value field" );
		assert.equal( f.state.gameplay.academy.member, false );
		f.ui.event( { kind: "key", code: "KeyI" } );
		settle();
		const glyph = fontAtlas.fonts["0"].styles["2"].glyphs["69"],
			uv = [
				glyph.x / fontAtlas.atlasWidth,
				glyph.y / fontAtlas.atlasHeight,
				glyph.width / fontAtlas.atlasWidth,
				glyph.height / fontAtlas.atlasHeight
			];
		assert.ok(
			f.scenes.at( -1 ).quads.some( q => q.texture === fontAtlas.image && q.uv.every( ( v, i ) => v === uv[i] ) ),
			"selected Equipment uses the published style-2 E glyph"
		);
		f.ui.event( { kind: "key", code: "KeyS" } );
		settle();
		assert.ok( f.hasText( "Warrior" ) );
		assert.ok( f.hasText( "Rogue" ) );
		f.ui.event( { kind: "key", code: "KeyP" } );
		settle();
		assert.ok( f.hasText( "<No party area>" ) );
		assert.ok( f.hasText( "<No guild>" ) );
		f.ui.event( { kind: "activate", id: "hud-menu" } );
		settle();
		assert.equal( f.ui.stats().panel, "" );
		f.ui.event( { kind: "activate", id: "hud-menu" } );
		settle();
		assert.equal( f.ui.stats().panel, "Party" );
	} finally {
		f.dispose();
	}
});

test("open Skills and hotbar do not rescan the catalog during camera/hover UI rebuilds", () => {
	const f = uiFixture();
	try {
		const base = {
			id: 3,
			group: 174,
			level: 1,
			name: "SKILL_CH_SWORD_SMASH_A",
			nameSymbol: "SN_SKILL_CH_SWORD_SMASH_A",
			icon: "skill/china/sword_smash_a.ddj",
			spCost: 1,
			trainable: true,
			targetRequired: true,
			cooldownMs: 3000,
			masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		};
		const rows = [
			base,
			{ ...base, id: 291, level: 2, spCost: 5 },
			...Array.from( { length: 10000 }, ( _, i ) => ({ ...base, id: 100000 + i, group: 100000 + i }) )
		];
		let reads = 0;
		const catalog = new Proxy( rows, {
			/*
			================
			get
			================
			*/
			get( target, key, receiver ) {
				if ( typeof key === "string" && /^\d+$/.test( key ) ) reads++;
				return Reflect.get( target, key, receiver );
			}
		} );
		f.state.gameplay = {
			...f.state.gameplay,
			skills: [ 3 ],
			skillCatalog: catalog,
			quickSlots: [ { slot: 1, kind: 0x49, payload: 3 } ],
			progression: { level: 10, skillPoints: 10, masteries: [ { id: 257, level: 7 } ] }
		};
		f.ui.event( { kind: "key", code: "KeyS" } ); // World input is admitted after the first step.
		for ( let i = 0; i < 30; i++ ) f.ui.step( { ...f.state }, 1000 );
		f.ui.event( { kind: "key", code: "KeyS" } );
		f.ui.step( { ...f.state }, 1016 );
		const before = reads;
		assert.ok( before >= rows.length, "catalog was admitted" );
		for ( let i = 0; i < 20; i++ ) {
			f.ui.event( { kind: "hover", id: i % 2 ? "skill:3" : null } );
			f.ui.step(
				{ ...f.state, frontend: { phase: "world" }, hoveredEntity: i % 2 ? 1 : undefined },
				1032 + i * 16
			);
		}
		assert.equal( reads, before, "render-rate consumers must use the retained index" );
		f.state.gameplay = { ...f.state.gameplay, skills: [ 291 ] };
		f.ui.step( { ...f.state }, 2000 );
		assert.ok( reads > before, "learned changes rebuild prerequisite state" );
	} finally {
		f.dispose();
	}
});

test("open game options rebase acknowledged settings without reverting untouched flags", async () => {
	const { initialGameOptions } = await load( "src/engine/foundation/gameplay/game-options.ts" );
	const saved = [],
		f = uiFixture( undefined, undefined, undefined, undefined, undefined, undefined, value => saved.push( value ) );
	const initial = initialGameOptions();
	try {
		f.ui.event( { kind: "preferences", value: initial } );
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:4" } );
		f.ui.event( { kind: "activate", id: "option-toggle:ownName" } );
		const updated = { ...initial, ownStatus: true, monsterStatus: true, warningSound: true, windowMode: true };
		f.ui.event( { kind: "preferences", value: updated } );
		f.ui.event( { kind: "activate", id: "option-ok" } );
		assert.deepEqual( saved.at( -1 ), { ...updated, ownName: false } );
		f.ui.event( { kind: "activate", id: "open-window:Option" } );
		f.ui.event( { kind: "activate", id: "option-tab:4" } );
		f.ui.event( { kind: "activate", id: "option-default" } );
		f.ui.event( { kind: "activate", id: "option-cancel" } );
		assert.equal( saved.length, 1, "cancel never saves Reset values" );
	} finally {
		f.dispose();
	}
});

test("both Enter keys focus chat; unknown keys never match unassigned bindings", () => {
	for ( const code of [ "Enter", "NumpadEnter" ] ) {
		const f = uiFixture();
		try {
			f.ui.step( f.state, 0 );
			f.ui.event( { kind: "key", code: "Unidentified" } );
			f.ui.event( { kind: "key", code } );
			const result = f.ui.step( f.state, 16 );
			assert.equal( result.focusRequest?.id, "chat-text", code );
		} finally {
			f.dispose();
		}
	}
});

test("minimap zoom advances while world inputs are unchanged and resets on exit", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.pose = { regionId: 0x6262, x: 900, y: 0, z: 900, angle: 0 };
		for ( let i = 0; i < 40; i++ ) f.ui.step( f.state, i * 100 );
		// The tile is what the build published: a native .texture for a DXT1
		// source, else a .png (copyMissionMinimapTileImages.mjs).
		const tile = /\/minimap\/98x98\.(png|texture)$/;
		const width = () => f.scenes.at( -1 ).quads.find( q => tile.test( q.texture ) )?.rect[2];
		assert.equal( width(), 160 );
		f.ui.event( { kind: "activate", id: "minimap-in" } );
		f.ui.step( f.state, 3910 );
		assert.ok( width() > 160 && width() < 179.2, "first frame is intermediate" );
		const first = width();
		f.ui.step( f.state, 3920 );
		assert.ok( width() > first );
		f.ui.step( f.state, 4400 );
		assert.equal( width(), Math.fround( 179.20000076293945 ) );
		f.ui.event( { kind: "activate", id: "minimap-out" } );
		f.ui.step( f.state, 4410 );
		assert.ok( width() < 179.2 && width() > 160 );
		f.ui.step( { ...f.state, session: { phase: "signed-out", revision: 2 } }, 4420 );
		f.ui.step( f.state, 4500 );
		// Returning from title loading presents its completed frame before the HUD.
		f.ui.step( f.state, 4601 );
		assert.equal( width(), 160 );
	} finally {
		f.dispose();
	}
});

test("native minimap zoom clamps accumulated targets, reverses and converges without overshoot", async () => {
	const { advanceMinimapZoom: step, minimapZoomTarget: target } = await load(
		"src/engine/foundation/ui/minimap-zoom.ts"
	);
	const { virtualKey } = await load( "src/engine/foundation/ui/input-options.ts" );
	assert.equal( virtualKey( "NumpadEnter" ), 13 );
	assert.equal( virtualKey( "Enter" ), 13 );
	let goal = 160;
	for ( let i = 0; i < 20; i++ ) goal = target( goal, 1 );
	assert.equal( goal, 256 );
	for ( let i = 0; i < 20; i++ ) goal = target( goal, -1 );
	assert.equal( goal, 64 );
	assert.equal( step( 160, 180, 100 ), 165 );
	assert.equal( step( 165, 140, 100 ), 160 );
	assert.equal( step( 179, 180, 100 ), 180 );
	assert.equal( step( 141, 140, 100 ), 140 );
	assert.equal( step( 160, 180, 0 ), 160 );
	assert.equal( step( 160, 180, -1 ), 160 );
	for ( const hz of [ 30, 60, 144, 240 ] ) {
		let value = 160;
		for ( let i = 0; i < hz; i++ ) value = step( value, target( 160, 1 ), 1000 / hz );
		assert.equal( value, target( 160, 1 ) );
	}
});

test("world admission failures publish a visible native retry dialog and clear after recovery", () => {
	const f = uiFixture();
	try {
		for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, i * 100 );
		const failed = {
			...f.state,
			worldError: "World scene residency budget exceeded: 204550758 bytes > 201326592 bytes",
			worldReady: false
		};
		let result;
		for ( let i = 20; i < 40; i++ ) result = f.ui.step( failed, i * 100 ) ?? result;
		assert.match( result.loadingError, /residency/ );
		assert.match( result.message, /Unable to finish loading/ );
		assert.ok( result.controls.some( c => c.id === "world-load-retry" ) );
		assert.ok( f.hasText( "Unable to finish loading" ) );
		for ( let i = 40; i < 45; i++ ) result = f.ui.step( f.state, i * 100 ) ?? result;
		assert.equal( result.loadingError, undefined );
		assert.ok( !result.controls.some( c => c.id === "world-load-retry" ) );
	} finally {
		f.dispose();
	}
});

test("inventory ground drop confirms once, cancels and rejects a replaced source", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, refObjId: 1, typeFlags: 0x6c, quantity: 3, name: "Potion" } ]
		};
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.step( f.state, 1300 );
		const drop = () => {
			f.ui.event( { kind: "drag", id: "slot:13", dx: 100, dy: 0 } );
			f.ui.event( { kind: "drag-end", id: "slot:13", x: 700, y: 300 } );
		};
		drop();
		let scene = f.ui.step( f.state, 1400 );
		assert.deepEqual( sent, [] );
		assert.ok( scene.controls.some( c => c.id === "ground-drop-confirm" ) );
		f.ui.event( { kind: "key", code: "Escape" } );
		f.ui.step( f.state, 1500 );
		assert.deepEqual( sent, [] );
		drop();
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "key", code: "Enter" } );
		f.ui.event( { kind: "activate", id: "ground-drop-confirm" } );
		assert.deepEqual( sent, [ { kind: "gameplay", command: { kind: "item-drop", slot: 13 } } ] );
		sent.length = 0;
		f.ui.step( f.state, 1700 );
		drop();
		f.ui.step( f.state, 1800 );
		f.state.gameplay = { ...f.state.gameplay, inventory: [ { ...f.state.gameplay.inventory[0], refObjId: 2 } ] };
		f.ui.step( f.state, 1900 );
		f.ui.event( { kind: "activate", id: "ground-drop-confirm" } );
		assert.deepEqual( sent, [] );
	} finally {
		f.dispose();
	}
});

test("inventory equipment drop automatically matches a wrong socket and the paperdoll", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, refObjId: 1, typeFlags: 0x8ac, quantity: 1, name: "Helm" } ]
		};
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyI" } );
		const scene = f.ui.step( f.state, 1300 );
		for ( const id of [ "slot:6", "equipment-drop-zone" ] ) {
			const r = scene.controls.find( c => c.id === id ).rect;
			f.ui.event( { kind: "drag", id: "slot:13", dx: 36, dy: 0 } );
			f.ui.event( { kind: "drag-end", id: "slot:13", x: r[0] + r[2] / 2, y: r[1] + r[3] / 2 } );
		}
		assert.equal( sent.length, 2 );
		for ( const c of sent ) {
			assert.deepEqual( c, {
				kind: "gameplay",
				command: { kind: "inventory-move", source: 13, destination: 0, quantity: 1 }
			} );
		}
	} finally {
		f.dispose();
	}
});

test("equipment auto-match uses a free ring hand but honors a compatible explicit hand", async () => {
	const { equipmentDropSlot } = await load( "src/engine/foundation/gameplay/equipment-drop.ts" );
	const ring = 0x1aac;
	assert.equal( equipmentDropSlot( ring, [], 0 ), 11 );
	assert.equal( equipmentDropSlot( ring, [ { slot: 11 } ], 0 ), 12 );
	assert.equal( equipmentDropSlot( ring, [ { slot: 11 }, { slot: 12 } ], 0 ), 11 );
	assert.equal( equipmentDropSlot( ring, [], 12 ), 12 );
	assert.equal( equipmentDropSlot( 0x26c, [] ), 7, "ammunition" );
	assert.equal( equipmentDropSlot( 0x6c, [] ), undefined, "consumable is not equipment" );
});

test("buff dismissal uses right release, authored eligibility and server-owned teardown", () => {
	/** @type {any[]} */ const sent = [];
	const f = uiFixture( c => sent.push( c ) );
	let semantic;
	const settle = () => {
		for ( let i = 0; i < 25; i++ ) semantic = f.ui.step( f.state, i * 10 ) ?? semantic;
	};
	try {
		const game = f.state.gameplay;
		game.skillCatalog = [
			{ id: 7, name: "Buff", icon: "item/etc/hp_potion_01.ddj", buffCancel: "direct" },
			{ id: 8, name: "Scroll", icon: "item/etc/hp_potion_01.ddj", buffCancel: "confirm" },
			{ id: 9, name: "Protected", icon: "item/etc/hp_potion_01.ddj", buffCancel: "blocked" },
			{ id: 10, name: "Link", icon: "item/etc/hp_potion_01.ddj", buffCancel: "direct", buffCancelInstance: true }
		];
		game.buffSlots = game.skillCatalog.map( ( skill, i ) => ({
			state: "active",
			serial: i + 1,
			secondary: false,
			effect: { gid: 1, skill: skill.id, token: 100 + i, phase: 1 }
		}) );
		settle();
		assert.equal( defined( semantic ).controls.find( c => c.id === "buff:100:7" ).rightActivate, true );
		f.ui.event( { kind: "activate", id: "buff:100:7" } );
		f.ui.event( { kind: "double-activate", id: "buff:100:7" } );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "right-activate", id: "buff:100:7" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "effect-cancel", skillId: 7, token: 0 } } );
		assert.equal( game.buffSlots.length, 4 );
		f.ui.event( { kind: "right-activate", id: "buff:102:9" } );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "right-activate", id: "buff:103:10" } );
		assert.equal( defined( sent.pop() ).command.token, 103 );
		f.ui.event( { kind: "right-activate", id: "buff:101:8" } );
		settle();
		assert.ok( defined( semantic ).controls.some( c => c.id === "buff-dismiss-confirm" ) );
		assert.deepEqual( sent, [] );
		f.ui.event( { kind: "key", code: "Escape" } );
		settle();
		assert.ok( !defined( semantic ).controls.some( c => c.id === "buff-dismiss-confirm" ) );
		f.ui.event( { kind: "right-activate", id: "buff:101:8" } );
		settle();
		f.ui.event( { kind: "activate", id: "buff-dismiss-confirm" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "effect-cancel", skillId: 8, token: 0 } } );
		settle();
		f.ui.event( { kind: "right-activate", id: "buff:101:8" } );
		settle();
		game.buffSlots = game.buffSlots.filter( s => s.effect.skill !== 8 );
		settle();
		f.ui.event( { kind: "activate", id: "buff-dismiss-confirm" } );
		assert.deepEqual( sent, [], "expired confirmation cannot cancel a replacement" );
		game.buffSlots[0].secondary = true;
		settle();
		assert.equal( defined( semantic ).controls.find( c => c.id === "buff:100:7" ).rightActivate, false );
	} finally {
		f.dispose();
	}
});

test("academy master can submit the shared notice editor without guild membership", () => {
	const sent = [], f = uiFixture( c => sent.push( c ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			academy: {
				member: true,
				localMemberId: 7,
				members: [ { id: 7, kind: 0, name: "Master", level: 80, entryLevel: 80 } ],
				subject: "Old",
				contents: "Old body",
				request: null,
				rows: [],
				page: 0
			}
		};
		for ( let t = 0; t < 1200; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "key", code: "KeyL" } );
		let out = f.ui.step( f.state, 1300 );
		assert.equal( out.controls.find( c => c.id === "academy-notice" ).disabled, false );
		f.ui.event( { kind: "activate", id: "academy-notice" } );
		out = f.ui.step( f.state, 1400 );
		assert.equal( out.controls.find( c => c.id === "social-subject" ).value, "" );
		assert.equal( out.controls.find( c => c.id === "social-subject" ).maxLength, 127 );
		assert.equal( out.controls.find( c => c.id === "social-contents" ).maxLength, 1023 );
		assert.equal( out.controls.find( c => c.id === "academy-notice-submit" ).disabled, false );
		f.ui.event( { kind: "activate", id: "academy-notice-submit" } );
		out = f.ui.step( f.state, 1500 ) ?? out;
		assert.ok(
			out.controls.some( c => c.id === "academy-notice-submit" ),
			"empty local validation keeps editor open"
		);
		assert.deepEqual( sent.at( -1 ).command, { kind: "academy-notice", subject: "", contents: "" } );
		for ( const [id, value] of [ [ "social-subject", "New" ], [ "social-contents", "Body" ] ] ) {
			f.ui.event( { kind: "edit", id, value, start: value.length, end: value.length, composing: false } );
		}
		f.ui.event( { kind: "activate", id: "academy-notice-submit" } );
		out = f.ui.step( f.state, 1600 );
		assert.deepEqual( sent.at( -1 ).command, { kind: "academy-notice", subject: "New", contents: "Body" } );
		assert.equal( out.controls.some( c => c.id === "academy-notice-submit" ), false );
		f.state.gameplay = { ...f.state.gameplay, academy: { ...f.state.gameplay.academy, localMemberId: 8 } };
		out = f.ui.step( f.state, 1700 );
		assert.equal( out.controls.find( c => c.id === "academy-notice" ).disabled, true );
		f.ui.event( { kind: "activate", id: "academy-notice" } );
		out = f.ui.step( f.state, 1800 ) ?? out;
		assert.equal( out.controls.some( c => c.id === "academy-notice-submit" ), false );
	} finally {
		f.dispose();
	}
});

test("recall menu has independent capability, modal cancel and stale-target protection", () => {
	const sent = [], f = uiFixture( c => sent.push( c.command ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			pose: { ...f.state.gameplay.pose, regionId: 25000 },
			target: 17,
			targetCapabilities: 0x40,
			npcConversation: { phase: "menu", gid: 17 }
		};
		f.state.entities.push( { ...f.state.entities[0], gid: 17, refObjId: 2094, kind: "teleport", name: "Jangan" } );
		let output, now = 0;
		const draw = () => {
			for ( let i = 0; i < 40; i++ ) output = f.ui.step( { ...f.state }, now++ ) ?? output;
		};
		draw();
		assert.ok( defined( output ).controls.some( c => c.id === "npc-recall-designate" ) );
		assert.ok( !defined( output ).controls.some( c => c.id === "npc-portal-open" ) );
		const click = id => {
			f.ui.event( { kind: "activate", id } );
			draw();
		};
		click( "npc-recall-designate" );
		assert.ok( defined( output ).controls.some( c => c.id === "recall-confirm" ) );
		assert.equal( sent.length, 0 );
		if ( process.env.RECALL_UI_CAPTURE ) {
			mkdirSync( "temp/artifacts/recall-appointment", { recursive: true } );
			writeFileSync(
				"temp/artifacts/recall-appointment/ui.json",
				JSON.stringify( { scene: f.scenes.at( -1 ), semantics: output }, null, 2 )
			);
		}
		click( "recall-cancel" );
		assert.equal( sent.length, 0 );
		click( "npc-recall-designate" );
		click( "recall-confirm" );
		assert.deepEqual( sent.splice( 0 ), [ { kind: "recall-appoint", gid: 17 } ] );
		click( "npc-recall-designate" );
		f.state.gameplay = { ...f.state.gameplay, target: 0, npcConversation: { phase: "closed" } };
		draw();
		click( "recall-confirm" );
		assert.equal( sent.length, 0 );
	} finally {
		f.dispose();
	}
});

test("target window carries the native CIFBuffViewer row and diffs it once a second", () => {
	const f = uiFixture();
	let semantic;
	const settle = ( from, to ) => {
		for ( let now = from; now <= to; now += 50 ) semantic = f.ui.step( f.state, now ) ?? semantic;
	};
	try {
		const monster = {
			gid: 9,
			regionId: 1,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			kind: "monster",
			name: "Mangyang",
			level: 1,
			rarity: 0,
			maxHp: 100
		};
		const game = f.state.gameplay;
		f.state.entities.push( monster );
		game.target = 9;
		game.vitals = [ { gid: 9, hp: 50, abnormal: 0x8 } ];
		game.skillCatalog = [ { id: 7, name: "Buff", icon: "item/etc/hp_potion_01.ddj" }, {
			id: 8,
			name: "Debuff",
			icon: "item/etc/hp_potion_01.ddj",
			buffSecondary: true
		} ];
		game.attachedEffects = [ { gid: 9, skill: 8, token: 21, phase: 2 }, { gid: 9, skill: 7, token: 20, phase: 1 } ];
		settle( 0, 400 );
		const cells = () => semantic.controls.filter( c => c.id.startsWith( "target-buff:" ) );
		const frame = defined( semantic ).controls.find( c => c.id === "clear-target" ).rect,
			left = frame[0] + 20 - 196,
			top = 7 + 78 + 1;
		assert.deepEqual( cells().map( c => [ c.id.split( ":" ).slice( 2 ).join( ":" ), c.rect ] ), [
			[ "buff:7", [ left, top, 20, 20 ] ],
			[ "buff:8", [ left, top + 25, 20, 20 ] ],
			[ "abnormal:3", [ left + 23, top + 25, 20, 20 ] ]
		] );
		assert.deepEqual( cells()[2].helpSource, { kind: "abnormal", gid: 9, bit: 3, viewer: true } );
		game.attachedEffects = [ { gid: 9, skill: 7, token: 20, phase: 1 } ];
		game.vitals = [ { gid: 9, hp: 50, abnormal: 0x8 } ];
		settle( 450, 950 );
		assert.equal( cells().length, 3, "waits for the one-second timer" );
		settle( 1000, 1100 );
		assert.deepEqual( cells().map( c => c.id.split( ":" ).slice( 2 ).join( ":" ) ), [ "buff:7", "abnormal:3" ] );
		f.state.entities[1] = { ...monster, kind: "npc" };
		settle( 1150, 1200 );
		assert.equal( cells().length, 0, "NPC targets carry no viewer" );
	} finally {
		f.dispose();
	}
});

test("party panel keeps leader and member actions distinct for every local role", () => {
	const sent = [], f = uiFixture( command => sent.push( command.command ) );
	try {
		f.state.gameplay.social = {
			self: 11,
			leader: 22,
			options: 3,
			members: [ { id: 11, name: "Player", level: 1 }, { id: 22, name: "Leader", level: 1 } ]
		};
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "activate", id: "open-window:Party" } );
		const memberPanel = f.ui.step( f.state, 100 );
		const memberIds = memberPanel.controls.map( control => control.id );
		assert.equal( new Set( memberIds ).size, memberIds.length, "Every visible control has one identity" );
		assert.equal( memberPanel.controls.find( control => control.id === "party-disband" ).disabled, true );
		assert.equal( memberPanel.controls.find( control => control.id === "party-leave" ).disabled, false );
		f.ui.event( { kind: "activate", id: "party-leave" } );
		assert.deepEqual( sent.pop(), { kind: "party-leave" } );
		f.state.gameplay.social.leader = 11;
		const leaderPanel = f.ui.step( f.state, 200 );
		const leaderIds = leaderPanel.controls.map( control => control.id );
		assert.equal( new Set( leaderIds ).size, leaderIds.length );
		assert.equal( leaderPanel.controls.find( control => control.id === "party-disband" ).disabled, false );
		f.ui.event( { kind: "activate", id: "party-disband" } );
		assert.deepEqual( sent.pop(), { kind: "party-leave" } );
	} finally {
		f.dispose();
	}
});

test("quick party portrait stays requested after the peer leaves world visibility", async () => {
	const { partyPortraitGid } = await load( "src/engine/foundation/ui/party-overlay.ts" );
	const f = uiFixture();
	try {
		f.state.gameplay.social = {
			localName: "Player",
			self: 11,
			leader: 11,
			options: 3,
			members: [ { id: 11, name: "Player", model: 1 }, { id: 22, name: "Peer", model: 2, status: 255 } ]
		};
		const peer = { ...f.state.entities[0], gid: 22, kind: "player", name: "Peer" };
		f.state.entities.push( peer );
		for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, i * 100 );
		const gid = partyPortraitGid( 22 );
		assert.ok( f.scenes.at( -1 ).quads.some( quad => quad.portraitGid === gid ) );
		f.state.entities = f.state.entities.filter( entity => entity.gid !== 22 );
		const semantic = f.ui.step( f.state, 2100 );
		assert.ok( f.scenes.at( -1 ).quads.some( quad => quad.portraitGid === gid ) );
		assert.equal( semantic.controls.some( control => control.id === "party-target:22" ), false );
	} finally {
		f.dispose();
	}
});

test("the party window shows the leader's and every member's portrait", async () => {
	// GDR_PTY_PICTURE and GDR_PTYSLOT_PICTURE are picture clips: rendered head
	// shots, reported missing when the window drew only the empty frame.
	const { partyPortraitGid } = await load( "src/engine/foundation/ui/party-overlay.ts" );
	const f = uiFixture();
	try {
		f.state.gameplay.social = {
			localName: "Player",
			self: 11,
			leader: 11,
			options: 3,
			members: [ { id: 11, name: "Player", model: 1 }, { id: 22, name: "Peer", model: 2, status: 255 } ]
		};
		const count = gid => f.scenes.at( -1 ).quads.filter( quad => quad.portraitGid === gid ).length;
		f.ui.step( f.state, 100 );
		const local = f.state.gameplay.localGid, peer = partyPortraitGid( 22 );
		const closed = [ count( local ), count( peer ) ];
		f.ui.event( { kind: "activate", id: "open-window:Party" } );
		f.ui.step( f.state, 200 );
		assert.deepEqual( [ count( local ), count( peer ) ], [ closed[0] + 1, closed[1] + 1 ] );
	} finally {
		f.dispose();
	}
});

test("selecting a quick party member keeps its name and gauges above the opaque selection backing", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.social = {
			localName: "Player",
			self: 11,
			leader: 11,
			options: 3,
			members: [ { id: 11, name: "Player", model: 1 }, { id: 22, name: "Peer", model: 2, status: 255 } ]
		};
		f.state.entities.push( { ...f.state.entities[0], gid: 22, kind: "player", name: "Peer" } );
		f.state.gameplay.target = 22;
		for ( let i = 0; i < 30; i++ ) f.ui.step( f.state, i * 100 );
		const quads = f.scenes.at( -1 ).quads;
		const selection = quads.findIndex( quad => quad.texture.endsWith( "/qpt_grope_select.png" ) );
		assert.ok( selection >= 0, "selected row draws the shipped selection texture" );
		const backing = quads[selection].rect;
		const content = quads.map( ( quad, index ) => ({ quad, index }) ).filter( ( { quad } ) =>
			quad.rect[0] >= backing[0] && quad.rect[0] < backing[0] + backing[2] &&
			quad.rect[1] >= backing[1] && quad.rect[1] < backing[1] + backing[3] &&
			(quad.texture === fontAtlas.image || /\/qpt_(hp|mp)\.png$/.test( quad.texture ))
		);
		assert.ok( f.hasText( "Peer" ), "selection preserves the member name" );
		assert.ok( content.some( ( { quad } ) => quad.texture === fontAtlas.image ) );
		assert.ok( content.some( ( { quad } ) => quad.texture.endsWith( "/qpt_hp.png" ) ) );
		assert.ok( content.every( ( { index } ) => index > selection ), "opaque backing cannot occlude row content" );
	} finally {
		f.dispose();
	}
});

test("right-click action and inventory icons use shared commands without changing single-click selection", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			inventorySlotCount: 58,
			equipmentSlotCount: 13,
			inventory: [ { slot: 13, refObjId: 1, typeFlags: 0x6c, quantity: 3, name: "Potion" } ],
			quickSlots: [ { slot: 1, kind: 0x4a, payload: 1000 } ]
		};
		for ( let time = 0; time < 1200; time += 100 ) f.ui.step( f.state, time );
		f.ui.event( { kind: "key", code: "KeyA" } );
		let scene = f.ui.step( f.state, 1300 );
		assert.ok( scene.controls.find( c => c.id === "action:1000" )?.rightActivate );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: "action:1000" } );
		assert.equal( sent.length, 0 );
		f.ui.event( { kind: "right-activate", id: "action:1000" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "action-command", id: 1000 } } );
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "action-command", id: 1000 } } );
		f.ui.event( { kind: "key", code: "KeyI" } );
		scene = f.ui.step( f.state, 1400 );
		assert.ok( scene.controls.find( c => c.id === "slot:13" )?.rightActivate );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: "slot:13" } );
		assert.equal( sent.length, 0 );
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "item-use", slot: 13 } } );
		f.ui.event( { kind: "activate", id: "slot:13", shift: true } );
		f.ui.step( f.state, 1500 );
		sent.length = 0;
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.equal( sent.length, 0, "split-stack modal blocks underlying right clicks" );
		f.ui.event( { kind: "key", code: "Escape" } );
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "drag", id: "slot:13", dx: 5, dy: 5 } );
		sent.length = 0;
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.equal( sent.length, 0, "right click cancels a carried item before using an icon" );
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "action-command", id: 1000 } } );
		f.state.gameplay = { ...f.state.gameplay, inventoryPending: true };
		f.ui.step( f.state, 1700 );
		sent.length = 0;
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		assert.equal( sent.length, 0, "pending inventory requests cannot be duplicated" );
	} finally {
		f.dispose();
	}
});

test("chat rows and world whisper selection prepare the same draft without sending it", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		f.state.entities.push( { ...f.state.entities[0], gid: 2, name: "Peer" } );
		f.state.gameplay = {
			...f.state.gameplay,
			chat: {
				lines: [
					{ channel: 1, name: "Peer", text: "OtherName: " + "a long wrapped message ".repeat( 5 ) },
					{ channel: 1, name: "Player", text: "my own message" },
					{ channel: 7, name: "Notice", text: "system message" }
				]
			}
		};
		let scene;
		for ( let time = 0; time < 1200; time += 100 ) scene = f.ui.step( f.state, time ) ?? scene;
		const peerRows = scene.controls.filter( c => c.whisperTarget === "Peer" );
		assert.ok( peerRows.length > 1, "wrapped rows preserve their actual sender" );
		assert.ok( peerRows.every( c => c.kind === "button" ) );
		assert.ok( !scene.controls.some( c => c.whisperTarget === "Notice" ) );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: peerRows.at( -1 ).id } );
		scene = f.ui.step( f.state, 1300 );
		assert.equal( scene.controls.find( c => c.id === "chat-text" ).value, "$Peer " );
		assert.equal( scene.focusRequest.id, "chat-text" );
		assert.equal( scene.focusRequest.caret, 6 );
		assert.equal( sent.length, 0 );
		const self = scene.controls.find( c => c.whisperTarget === "Player" );
		f.ui.event( { kind: "activate", id: self.id } );
		scene = f.ui.step( f.state, 1400 ) ?? scene;
		assert.equal(
			scene.controls.find( c => c.id === "chat-text" ).value,
			"$Peer ",
			"self click preserves the existing draft"
		);
		f.ui.event( { kind: "edit", id: "chat-text", value: "unsent text", start: 11, end: 11, composing: false } );
		f.ui.event( { kind: "whisper-target", gid: 2 } );
		scene = f.ui.step( f.state, 1500 );
		assert.equal( scene.controls.find( c => c.id === "chat-text" ).value, "$Peer " );
		f.ui.event( { kind: "whisper-target", gid: 1 } );
		f.ui.event( { kind: "whisper-target", gid: 999 } );
		assert.equal( sent.length, 0, "prefill never sends a chat packet" );
	} finally {
		f.dispose();
	}
});

test("learned skill icons and shortcut bars share casting and cooldown admission", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const skill = {
			id: 3,
			group: 3,
			level: 1,
			skillPoint: 1,
			spCost: 1,
			trainable: true,
			targetRequired: true,
			cooldownMs: 3000,
			masteries: [ { ID: 257, Level: 1 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		};
		f.state.gameplay = {
			...f.state.gameplay,
			skills: [ 3 ],
			skillCatalog: [ skill ],
			target: 2,
			quickSlots: [ { slot: 1, kind: 0x49, payload: 3 }, { slot: 41, kind: 0x49, payload: 3 } ],
			progression: { level: 10, skillPoints: 10, masteries: [ { id: 257, level: 7 } ] }
		};
		for ( let time = 0; time < 1200; time += 100 ) f.ui.step( f.state, time );
		f.ui.event( { kind: "key", code: "KeyS" } );
		let scene = f.ui.step( f.state, 1300 );
		f.ui.event( { kind: "activate", id: "skill-tab:0" } );
		f.ui.event( { kind: "activate", id: "skill-mastery:257" } );
		scene = f.ui.step( f.state, 1400 ) ?? scene;
		assert.ok( scene.controls.find( c => c.id === "skill:3" )?.rightActivate );
		sent.length = 0;
		f.ui.event( { kind: "activate", id: "skill:3" } );
		assert.equal( sent.length, 0 );
		f.ui.event( { kind: "right-activate", id: "skill:3" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "skill", skillId: 3, gid: 2 } } );
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "skill", skillId: 3, gid: 2 } } );
		if ( !scene.controls.some( c => c.id === "hotbar:41" ) ) {
			f.ui.event( { kind: "activate", id: "ext-open" } );
			scene = f.ui.step( f.state, 1450 ) ?? scene;
		}
		assert.ok( scene.controls.find( c => c.id === "hotbar:41" )?.rightActivate );
		f.ui.event( { kind: "right-activate", id: "hotbar:41" } );
		assert.deepEqual( sent.pop(), { kind: "gameplay", command: { kind: "skill", skillId: 3, gid: 2 } } );
		f.state.gameplay = {
			...f.state.gameplay,
			skillCooldowns: [ { skill: 3, group: 0, startedAtMs: 1400, durationMs: 3000 } ]
		};
		f.ui.step( f.state, 1500 );
		sent.length = 0;
		f.ui.event( { kind: "right-activate", id: "skill:3" } );
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		f.ui.event( { kind: "activate", id: "hotbar:1" } );
		// The board and both bars forward a cooling-down press alike; the
		// worker decides it (skill-queue.ts).
		assert.equal( sent.length, 3, "board and shortcuts forward the same cooling-down press" );
		for ( const command of sent ) {
			assert.deepEqual( command, { kind: "gameplay", command: { kind: "skill", skillId: 3, gid: 2 } } );
		}
		sent.length = 0;
		f.state.gameplay = { ...f.state.gameplay, skills: [], skillCooldowns: [] };
		f.ui.step( f.state, 1600 );
		f.ui.event( { kind: "right-activate", id: "skill:3" } );
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.equal( sent.length, 0, "unlearned skills remain ineligible even with stale bindings" );
		f.state.gameplay = {
			...f.state.gameplay,
			skills: [ 69 ],
			quickSlots: [ { slot: 1, kind: 0x49, payload: 69 } ]
		};
		f.ui.step( f.state, 1700 );
		f.ui.event( { kind: "activate", id: "hotbar:1" } );
		f.ui.event( { kind: "right-activate", id: "hotbar:1" } );
		assert.equal( sent.length, 0, "passive bindings cannot be cast with either mouse button" );
	} finally {
		f.dispose();
	}
});

test("an attack pet shows its mini window under the player mini window", () => {
	const f = uiFixture();
	try {
		f.state.gameplay.cosRecords = [ {
			gid: 7,
			refObjId: 100,
			band: 3,
			hp: 100,
			mp: 0,
			status: 0,
			dead: false,
			level: 12,
			satiety: 10000,
			name: "Fang"
		} ];
		for ( let i = 0; i < 50; i++ ) f.ui.step( f.state, 1000 + i );
		assert.ok( f.hasText( "Fang" ), "the pet's own name" );
		assert.ok( f.hasText( "12" ), "the pet's level" );
		f.state.gameplay.cosRecords = [ { ...f.state.gameplay.cosRecords[0], band: 4 } ];
		for ( let i = 0; i < 5; i++ ) f.ui.step( f.state, 1100 + i );
		assert.ok( !f.hasText( "Fang" ), "a pickup pet has no mini window" );
		f.state.entities = [ ...f.state.entities, {
			...f.state.entities[0],
			gid: 7,
			kind: "cos",
			refObjId: 100,
			name: "Fang",
			maxHp: 100
		} ];
		f.state.gameplay.target = 7;
		f.state.gameplay.vitals = [ ...f.state.gameplay.vitals, { gid: 7, hp: 50, mp: 0 } ];
		for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, 1200 + i );
		assert.ok( f.hasText( "Fang" ), "the selected grab pet still has its name" );
		// 5823B0: a non-combat COS target keeps the NPC window's 168x4 gauge.
		assert.ok(
			f.scenes.at( -1 ).quads.some( q => q.rect[1] === 44 && q.rect[3] === 4 ),
			"a selected grab pet shows the native target gauge"
		);
	} finally {
		f.dispose();
	}
});

test("the player panel draws native siege rank and guild status and removes them on war end", () => {
	const f = uiFixture( () => {} );
	try {
		f.state.gameplay = {
			...f.state.gameplay,
			social: { ...f.state.gameplay.social, guild: { id: 1, name: "Owner", members: [] } },
			fortress: {
				...f.state.gameplay.fortress,
				worldId: 7,
				listId: 1,
				worlds: [ { id: 7, code: "FORTRESS_JANGAN" } ],
				fortresses: [ { id: 1, code: "FORTRESS_JANGAN", nameStrId: "FORTRESS_NAME" } ],
				wars: [ { id: 1, name: "Owner", flags: 1, stoneWait: 29 } ],
				localKills: 150,
				localDeaths: 3
			}
		};
		let semantics;
		for ( let t = 0; t < 1200; t += 100 ) semantics = f.ui.step( { ...f.state }, t ) ?? semantics;
		assert.ok( semantics.controls.some( c => c.id === "GDR_PMI_BATTLE_GRADE" ) );
		assert.ok( semantics.controls.some( c => c.id === "GDR_PMI_FORTRESS_INFO" && c.helpText.includes( "Owner" ) ) );
		assert.ok( f.scenes.at( -1 ).quads.some( q => q.texture?.endsWith( "/rank_combat_commander.png" ) ) );
		f.state.gameplay = {
			...f.state.gameplay,
			fortress: { ...f.state.gameplay.fortress, wars: [ { id: 1, name: "Owner", flags: 0 } ] }
		};
		for ( let t = 1200; t < 2400; t += 100 ) semantics = f.ui.step( { ...f.state }, t ) ?? semantics;
		assert.ok(
			!semantics.controls.some( c => [ "GDR_PMI_BATTLE_GRADE", "GDR_PMI_FORTRESS_INFO" ].includes( c.id ) ),
			JSON.stringify( semantics.controls.filter( c => c.id.includes( "PMI" ) ) )
		);
	} finally {
		f.dispose();
	}
});

/*
================
Clock of Reincarnation targets a pet through the retail yellow cursor
================
*/
test("right-clicking a rental clock arms the yellow cursor and confirms the clicked grab pet", () => {
	const sent = [], f = uiFixture( command => sent.push( command ) );
	try {
		const clock = {
			slot: 13,
			refObjId: 8985,
			typeFlags: 0x66ec,
			quantity: 1,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: []
		};
		const pet = { ...clock, slot: 14, refObjId: 901, typeFlags: 0x10cc, summon: { state: 4, rentals: [] } };
		f.state.gameplay.inventory = [ clock, pet, { ...pet, slot: 15, typeFlags: 0x08cc }, {
			...clock,
			slot: 6,
			typeFlags: 0x032c
		} ];
		f.state.gameplay.inventorySlotCount = 45;
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		let scene;
		for ( let t = 100; t <= 2000; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		assert.equal( f.ui.cursor(), 0xa6 );
		assert.equal( sent.length, 0, "arming never picks a pet or spends the clock" );
		scene = f.ui.step( f.state, 2100 ) ?? scene;
		const targets = defined( scene ).controls.filter( row => [ "slot:6", "slot:14" ].includes( row.id ) );
		assert.equal( targets.length, 2 );
		assert.ok( targets.every( row => !row.draggable && !row.carry ), "clock targets cannot capture a drag" );
		// 567290: any occupied slot opens the confirmation and clears the
		// cursor; the worker checks the target when the user confirms.
		f.ui.event( { kind: "activate", id: "slot:15" } );
		assert.equal( f.ui.cursor(), null );
		for ( let t = 2200; t <= 2600; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		assert.ok( defined( scene ).controls.some( row => row.id === "cos-renew-confirm" ) );
		f.ui.event( { kind: "activate", id: "cos-renew-confirm" } );
		assert.deepEqual( sent.splice( 0 ).at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, summonerSlot: 15 }
		} );
		f.ui.step( f.state, 2700 );
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		for ( let t = 2800; t <= 3000; t += 100 ) scene = f.ui.step( f.state, t ) ?? scene;
		assert.ok( defined( scene ).controls.some( row => row.id === "cos-renew-confirm" ) );
		assert.equal( sent.length, 0, "choosing a pet waits for confirmation" );
		f.ui.event( { kind: "activate", id: "cos-renew-cancel" } );
		f.ui.step( f.state, 3100 );
		assert.equal( sent.length, 0 );
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		f.ui.step( f.state, 3200 );
		f.ui.event( { kind: "activate", id: "cos-renew-confirm" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, summonerSlot: 14 }
		} );
		assert.equal( f.ui.cursor(), null );
		f.ui.step( f.state, 3300 );
		f.ui.event( { kind: "right-activate", id: "slot:13" } );
		f.ui.event( { kind: "key", code: "Escape" } );
		assert.equal( f.ui.cursor(), null );
		assert.equal( sent.length, 1, "Escape never sends a renewal" );
		f.ui.event( { kind: "activate", id: "close" } );
		f.state.gameplay.quickSlots = [ { slot: 1, kind: 0x46, payload: 0 } ];
		f.ui.step( f.state, 3400 );
		f.ui.event( { kind: "key", code: "Digit1" } );
		f.ui.step( f.state, 3500 );
		assert.equal( f.ui.cursor(), 0xa6, "hotbar use retains targeting with the inventory closed" );
		assert.equal( sent.length, 1, "hotbar activation cannot spend a clock without its target" );
		f.ui.event( { kind: "key", code: "KeyI" } );
		for ( let t = 3600; t <= 3900; t += 100 ) f.ui.step( f.state, t );
		f.ui.event( { kind: "activate", id: "slot:14" } );
		f.ui.step( f.state, 4000 );
		f.ui.event( { kind: "activate", id: "cos-renew-confirm" } );
		assert.deepEqual( sent.at( -1 ), {
			kind: "gameplay",
			command: { kind: "item-use", slot: 13, summonerSlot: 14 }
		} );
		assert.equal( sent.length, 2 );
	} finally {
		f.dispose();
	}
});
