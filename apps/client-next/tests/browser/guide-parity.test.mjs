/*
===========================================================================

guide-parity.test.mjs - the game guide's dictionary and articles in a real page

Opens the guide through the browser platform and checks article
visibility and that an authored article's painted glyphs equal the
guide layout's own.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

test(
	"guide uses retail tabs and retains its admitted window across cold asset transitions",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.setViewportSize( { width: 1200, height: 900 } );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.waitForFunction( () =>
				document.querySelector( "output" )?.textContent?.includes( "runtime: running" )
			);
			await page.evaluate( async () => {
				const entry = Array.from( document.scripts ).find( s =>
					s.src && new URL( s.src ).pathname === "/src/bootstrap.ts"
				);
				const { runtime } = await import( entry.src );
				runtime.dispose();
				const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" ),
					{ createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" ),
					{ createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
				const jobs = new Map(),
					requests = [],
					commands = [],
					renderer = createRenderer( document.querySelector( "canvas" ) );
				let serial = 0, scene = null;
				const assets = {
					available: () => Math.max( 0, 32 - jobs.size ),
					/*
					================
					request
					================
					*/
					request( url, limit, kind ) {
						const id = ++serial;
						jobs.set( id, null );
						requests.push( url );
						fetch( url ).then( async r => {
							if ( !r.ok ) throw Error( r.status + " " + url );
							const result = kind === "png" ?
								{ kind: "image", image: await createImageBitmap( await r.blob() ) } :
								{ kind: "bytes", buffer: await r.arrayBuffer() };
							if ( jobs.has( id ) ) jobs.set( id, result );
							else if ( result.kind === "image" ) result.image.close();
						} ).catch( e => {
							if ( jobs.has( id ) ) jobs.set( id, { kind: "error", error: String( e ) } );
						} );
						return id;
					},
					/*
					================
					take
					================
					*/
					take( id ) {
						if ( window.chromeFixture?.holdAssets ) return null;
						const result = jobs.get( id );
						if ( result ) jobs.delete( id );
						return result;
					},
					/*
					================
					cancel
					================
					*/
					cancel( id ) {
						const result = jobs.get( id );
						if ( result?.kind === "image" ) result.image.close();
						jobs.delete( id );
					}
				};
				const ui = createUi(
					assets,
					c => commands.push( c ),
					s => {
						scene = s;
						renderer.setUi( s );
					},
					( id, image ) => renderer.setUiTexture( id, image ),
					location.origin,
					"http://fixture.invalid"
				);
				const platform = createPlatform(
					document.querySelector( "canvas" ),
					document.querySelector( "output" ),
					() => {},
					() => {},
					() => {},
					ui.event,
					ui.blocks
				);
				const state = {
					session: {
						phase: "world",
						revision: 1,
						character: "Fixture",
						characters: [ { id: 1, name: "Fixture", level: 1, maxHp: 200, maxMp: 200 } ]
					},
					gameplay: {
						localGid: 1,
						inventory: [],
						inventorySlotCount: 45,
						equipmentSlotCount: 13,
						vitals: [ { gid: 1, hp: 200, mp: 200 } ],
						casts: []
					},
					entities: [],
					width: 1200,
					height: 900,
					worldReady: true
				};
				window.chromeFixture = {
					ui,
					renderer,
					platform,
					state,
					requests,
					commands,
					get scene() {
						return scene;
					},
					/*
					================
					draw
					================
					*/
					draw() {
						const semantic = ui.step( { ...state }, performance.now() );
						if ( semantic ) platform.presentUi( semantic );
						renderer.frame( { width: state.width, height: state.height } );
					}
				};
				chromeFixture.draw();
				ui.event( { kind: "activate", id: "open-window:Game Guide" } );
			} );
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0 && document.querySelector( '[data-ui-id="guide-drag"]' );
			} );
			const box = await page.locator( '[data-ui-id="guide-drag"]' ).boundingBox();
			assert.deepEqual( box, { x: 400, y: 229, width: 380, height: 25 } );
			const retained = await page.evaluate( () => {
				chromeFixture.holdAssets = true;
				const before = chromeFixture.scene.quads.filter( q => q.texture.endsWith( "gd_paper.png" ) );
				chromeFixture.ui.event( { kind: "activate", id: "guide-sidebar" } );
				for ( let i = 0; i < 10; i++ ) chromeFixture.draw();
				return {
					before,
					after: chromeFixture.scene.quads.filter( q => q.texture.endsWith( "gd_paper.png" ) ),
					present: !!document.querySelector( '[data-ui-id="guide-drag"]' )
				};
			} );
			assert.ok( retained.present );
			assert.deepEqual( retained.after, retained.before );
			await page.evaluate( () => {
				chromeFixture.holdAssets = false;
			} );
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0 &&
					document.querySelector( '[data-ui-id="guide-tab:general"]' );
			} );
			assert.deepEqual( await page.locator( '[data-ui-id^="guide-tab:"]' ).allTextContents(), [
				"Help",
				"Alarm",
				"Quest"
			] );
			await mkdir( "temp/artifacts/retail-ui-parity", { recursive: true } );
			for ( const tab of [ "general", "quests", "events" ] ) {
				await page.locator( '[data-ui-id="guide-tab:' + tab + '"]' ).click();
				await page.waitForFunction( () => {
					chromeFixture.draw();
					if ( !document.querySelector( '[data-ui-id="guide-drag"]' ) ) throw Error( "Guide disappeared" );
					return chromeFixture.ui.stats().pending === 0;
				} );
				await page.screenshot( { path: "temp/artifacts/retail-ui-parity/guide-" + tab + ".png" } );
			}
			const control = id => page.locator( '[data-ui-id="' + id + '"]' );
			await control( "guide-tab:quests" ).click();
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0 &&
					document.querySelector( '[data-ui-id="guide-group:100000"]' );
			} );
			assert.equal( await page.locator( '[data-ui-id^="guide-group:"]' ).count(), 10 );
			await control( "guide-group:100000" ).click();
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0 &&
					document.querySelector( '[data-ui-id="guide-article:100001"]' );
			} );
			assert.equal( await control( "guide-article:100001" ).count(), 1, "unaccepted tutorial is in dictionary" );
			assert.equal( await control( "guide-article:100002" ).count(), 0, "successor requires completion" );
			await control( "guide-article:100003" ).click();
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0;
			} );
			const content = await page.evaluate( async () => {
				const { guideTokens, guideContent } = await import( "/src/engine/foundation/ui/guide-content.ts" );
				const { decodeUiFont } = await import( "/src/engine/foundation/rendering/ui-glyphs.ts" );
				const { expandTextRuns } = await import( "/src/engine/foundation/rendering/text-run.ts" );
				// Compare painted glyphs: publication drops layout sidecars, guideContent keeps them.
				const painted = quads => expandTextRuns( quads ).map( ( { textLayout, ...q } ) => q );
				const data = await (await fetch( "/assets/data/questData.json" )).json(),
					raw = await (await fetch( "/assets/fonts/native-ui-font-atlas.json" )).json();
				const tokens = guideTokens( data.textEntries.SN_PAYCON_QNO_CH_CHEF_1 ),
					font = decodeUiFont( raw ),
					clip = [ 443, 340, 290, 273 ];
				const expected = painted(
					guideContent( font, tokens, clip, clip, [ 1, 1, 1, 1 ], () => undefined ).quads
				);
				const actual = painted(
					chromeFixture.scene.quads.filter( q => q.clip.every( ( n, i ) => n === clip[i] ) )
				);
				return { expected, actual, english: data.textEntries.SN_PAYCON_QNO_CH_CHEF_1 };
			} );
			assert.match( content.english, /Find the shoes/ );
			assert.deepEqual(
				content.actual,
				content.expected,
				"full English authored article, color and emphasis reach GPU quads"
			);
			await page.screenshot( { path: "temp/artifacts/retail-ui-parity/quest-article.png" } );
			await page.evaluate( () => {
				chromeFixture.state.gameplay = { ...chromeFixture.state.gameplay, completedQuests: [ 2 ] };
				chromeFixture.draw();
			} );
			assert.equal(
				await control( "guide-article:100002" ).count(),
				1,
				"completion invalidates eligibility while guide stays open"
			);
			await control( "guide-article:100001" ).click();
			await page.evaluate( () => chromeFixture.draw() );
			const completed = await page.evaluate( () =>
				chromeFixture.scene.quads.filter( q => q.clip[2] === 164 && q.color[0] === .6 )
			);
			assert.ok( completed.length, "completed tutorial retains native gray" );
			await control( "guide-tab:general" ).click();
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0 &&
					!document.querySelector( '[data-ui-id="guide-tab:general"]' )?.disabled;
			} );
			await control( "guide-tab:quests" ).click();
			await page.waitForFunction( () => {
				chromeFixture.draw();
				return chromeFixture.ui.stats().pending === 0 &&
					document.querySelector( '[data-ui-id="guide-group:100000"]' );
			} );
			assert.equal(
				await control( "guide-article:100001" ).count(),
				0,
				"changing tabs resets category expansion like native row rebuild"
			);
		} finally {
			await page.evaluate( () => {
				chromeFixture.platform.dispose();
				chromeFixture.ui.dispose();
				chromeFixture.renderer.dispose();
			} ).catch( () => {} );
			await browser.close();
		}
	}
);
