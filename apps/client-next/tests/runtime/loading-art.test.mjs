/*
===========================================================================

loading-art.test.mjs - only the loading art that can show next stays decoded

All nine loading images were in every frame's demand: 18 MiB decoded for
screens that could not appear. loadingArtDemand keeps the chrome, the
europe variant the next load draws, the rebirth art while dead and the
character-select art outside the world or after a leave request. Art no
lead time foresaw is demanded on use; its screen draws the loading form
and gauge at once and the background once decoded, as region art always
has.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { loadingArtDemand, travelLoadingBackground } = await import(
	"../../src/engine/foundation/ui/mission-loading.ts"
);
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

const ROOT = "/assets/images/Media_extracted/interface/loading/";
const CHROME = [ "loading_form", "gauge_loading", "nowloading" ];
const names = paths => paths.map( path => path.slice( ROOT.length, -".png".length ) ).sort();
const ALIVE = { inWorld: true, dead: false, leaving: false, nextVariant: 1 };

/*
================
fixture

The production UI with an asset owner that records every request and
never delivers, so loading art stays pending.
================
*/
function fixture( loadingVariant = () => 1 ) {
	let scene;
	const requested = new Set();
	const ui = createUi(
		{
			available: () => 1000,
			take: () => null,
			request: url => {
				requested.add( new URL( url ).pathname );
				return requested.size;
			},
			cancel: () => {}
		},
		() => {},
		value => {
			scene = value;
		},
		() => {},
		"https://fixture.invalid",
		"https://fixture.invalid",
		() => {},
		() => {},
		loadingVariant
	);
	const loadingRequested = () =>
		[ ...requested ].filter( path => path.startsWith( ROOT ) ).map( path =>
			path.slice( ROOT.length, -".png".length )
		)
			.sort();
	return { ui, scene: () => scene, loadingRequested };
}

for ( const safeLoadingScreen of [ false, true ] ) {
	test(`safe loading ${safeLoadingScreen} controls repeated entry artwork and preloading`, () => {
		let variant = 0;
		const f = fixture( () => variant++ % 2 + 1 );
		const seen = new Set();
		try {
			if ( safeLoadingScreen ) {
				f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { safeLoadingScreen } ) } );
			}
			for ( let generation = 1; generation <= 8; generation++ ) {
				const frontend = {
					phase: /** @type {const} */ ("loading-world"),
					generation,
					elapsed: 0,
					alpha: 0,
					logoAlpha: 0,
					error: null
				};
				const state = {
					frontend,
					session: { phase: /** @type {const} */ ("entering-world"), revision: generation },
					gameplay: null,
					entities: [],
					width: 1024,
					height: 768,
					worldReady: false,
					loadingProgress: .4
				};
				const now = generation * 1000;
				f.ui.step( state, now );
				const background = f.scene().quads.find( q => q.texture.startsWith( ROOT + "loading_europe_" ) );
				assert.ok( background );
				seen.add( background.texture );
				const ready = {
					...state,
					frontend: { ...frontend, phase: /** @type {const} */ ("world") },
					session: { phase: /** @type {const} */ ("world"), revision: generation },
					worldReady: true
				};
				f.ui.step( ready, now + 1 );
				f.ui.step( ready, now + 102 );
			}
			assert.deepEqual(
				[ ...seen ].sort(),
				safeLoadingScreen ?
					[ ROOT + "loading_europe_1.png" ] :
					[ ROOT + "loading_europe_1.png", ROOT + "loading_europe_2.png" ]
			);
			assert.equal( f.loadingRequested().includes( "loading_europe_2" ), !safeLoadingScreen );
		} finally {
			f.ui.dispose();
		}
	});
}

test("changing the saved safe-loading preference affects the next load without a reload", () => {
	const f = fixture( () => 2 );
	const world = {
		session: { phase: /** @type {const} */ ("world"), revision: 1 },
		gameplay: null,
		entities: [],
		width: 1024,
		height: 768,
		worldReady: true
	};
	try {
		f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { safeLoadingScreen: true } ) } );
		f.ui.step( world, 0 );
		assert.ok( f.loadingRequested().includes( "loading_europe_1" ) );
		assert.ok( !f.loadingRequested().includes( "loading_europe_2" ) );
		f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { safeLoadingScreen: false } ) } );
		f.ui.step( world, 1000 );
		assert.ok( f.loadingRequested().includes( "loading_europe_2" ) );
		f.ui.step( { ...world, worldReady: false }, 1010 );
		assert.ok( f.scene().quads.some( q => q.texture === ROOT + "loading_europe_2.png" ) );
	} finally {
		f.ui.dispose();
	}
});

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

test("the UI asks for the next screen's art, not all nine", () => {
	const f = fixture();
	try {
		f.ui.step( {
			session: { phase: "world", revision: 1, character: "Fixture" },
			gameplay: null,
			entities: [],
			width: 1024,
			height: 768,
			worldReady: true
		}, 0 );
		const requested = f.loadingRequested();
		assert.ok( CHROME.every( name => requested.includes( name ) ), requested.join() );
		assert.equal( requested.filter( name => name.startsWith( "loading_europe_" ) ).length, 1 );
		for ( const absent of [ "loading_rebirth", "loading_thief2", "loading_charactercustom_europe" ] ) {
			assert.ok( !requested.includes( absent ), `${absent} stays out of a live world's demand` );
		}
	} finally {
		f.ui.dispose();
	}
});

test("art demanded on use draws the loading form and gauge at once", () => {
	const f = fixture();
	try {
		const region = travelLoadingBackground( { mode: 2, region: 0x694f }, 1 );
		assert.equal( region, ROOT + "loading_constantinople.png" );
		const semantics = f.ui.step( {
			session: { phase: "world", revision: 1, character: "Fixture" },
			gameplay: null,
			entities: [],
			width: 1024,
			height: 768,
			worldReady: false,
			travel: { mode: 2, region: 0x694f, revision: 1 },
			loadingProgress: .3
		}, 0 );
		assert.equal( semantics?.loadingVisible, true );
		const quads = f.scene().quads;
		assert.ok( quads.some( q => q.texture.endsWith( "gauge_loading.png" ) ), "the gauge is drawn" );
		assert.ok( quads.some( q => q.texture === region ), "the region art is drawn once decoded" );
		assert.ok( f.loadingRequested().includes( "loading_constantinople" ), "and demanded on use" );
		// The screen's own black backdrop is expected; a bare black screen is
		// one without the loading form, gauge and status.
		assert.ok( quads.some( q => q.texture.endsWith( "loading_form.png" ) ), "the loading form is drawn" );
	} finally {
		f.ui.dispose();
	}
});
