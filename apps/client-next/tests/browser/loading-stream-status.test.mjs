/*
===========================================================================

loading-stream-status.test.mjs - loading feedback independent of UI snapshots

Exercises the real platform DOM owner. A retained native loading screen must
show fresh transfer activity without requiring another UI publication.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test( "native loading status advances while the semantic scene is retained", { timeout: 30000 }, async t => {
	const { browser, page } = await launchProbeBrowser();
	t.after( () => browser.close() );
	await page.route(
		url => url.origin === new URL( CLIENT_NEXT_BASE_URL ).origin && url.pathname === "/",
		route =>
			route.fulfill( {
				contentType: "text/html",
				body:
					'<canvas></canvas><output></output><div id="startup-loading"><div class="sro-boot-loading__label"></div><div class="sro-boot-loading__detail"></div></div>'
			} )
	);
	await page.goto( CLIENT_NEXT_BASE_URL );
	const result = await page.evaluate( async () => {
		const { createPlatform } = await import(
			new URL( "/src/engine/runtime/platform/platform.ts", location.origin ).href
		);
		const platform = createPlatform(
			document.querySelector( "canvas" ),
			document.querySelector( "output" ),
			() => {},
			() => {}
		);
		const loading = document.getElementById( "startup-loading" );
		if ( !loading ) throw Error( "Missing loading fixture" );
		const progress = {
			bytesReceived: 0,
			bytesRead: 1,
			bytesPerSecond: 0,
			filesReady: 0,
			filesActive: 1,
			cacheHits: 0,
			currentFile: "/assets/audio/hit.wav"
		};
		try {
			platform.presentLoading( { visible: true, title: "Loading world", progress } );
			platform.presentUi( {
				title: "World",
				message: "",
				controls: [],
				loading: false,
				loadingVisible: true,
				loadingStatus: "Loading world",
				loadingProgress: .25
			} );
			const initial = loading.textContent;
			await new Promise( resolve => setTimeout( resolve, 300 ) );
			platform.presentLoading( {
				visible: true,
				title: "Loading world",
				progress: { ...progress, bytesRead: 4096, filesReady: 1, currentFile: "/assets/char/hero.glb" }
			} );
			const advanced = loading.textContent, active = loading.dataset.active, native = loading.dataset.native;
			platform.presentUi( { title: "World", message: "", controls: [], loading: false, loadingVisible: false } );
			return { initial, advanced, active, native, hidden: loading.dataset.active };
		} finally {
			platform.dispose();
		}
	} );
	assert.match( result.initial, /sounds/ );
	assert.match( result.advanced, /characters/ );
	assert.equal( result.active, "true" );
	assert.equal( result.native, "true" );
	assert.equal( result.hidden, "false" );
} );
