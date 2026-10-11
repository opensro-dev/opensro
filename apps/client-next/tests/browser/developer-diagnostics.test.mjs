/*
===========================================================================

developer-diagnostics.test.mjs - developer opt-in through the shipped HUD

Exercises browser storage, real pointer input, reload, viewport placement
and disposal. The build endpoint is controlled to test its displayed state.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

/*
================
ready
================
*/
async function ready( page ) {
	await page.waitForFunction( () => typeof window.sroDebug?.setDiagnostics === "function" );
	await page.waitForFunction( () => document.querySelector( "output" )?.textContent?.includes( "runtime: running" ) );
}

test(
	"tester HUD stays minimal; developer icon persists but its panel stays closed after reload",
	{ timeout: 90000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		const errors = [];
		page.on( "pageerror", error => errors.push( error.message ) );
		let requests = 0;
		await page.route( "**/title/build", route => {
			requests++;
			return route.fulfill( {
				json: { build: { revision: "abcdef0123456789", subject: "Fixture Agent build", uptimeSeconds: 42 } }
			} );
		} );
		try {
			await page.goto( CLIENT_NEXT_BASE_URL );
			await ready( page );
			const movement = await page.evaluate( () => window.sroDebug?.dumpMovement() );
			assert.ok( movement && typeof movement === "object" && "version" in movement && "events" in movement );
			assert.equal( movement.version, 1 );
			assert.ok( Array.isArray( movement.events ) );
			const dump = await page.evaluate( () => window.sroDebug?.dumpAssets() );
			assert.ok( dump && typeof dump === "object" && "assets" in dump );
			const assets = dump.assets;
			assert.ok( assets && typeof assets === "object" && "jobs" in assets && "available" in assets );
			assert.ok( Array.isArray( assets.jobs ) );
			assert.equal( assets.available, 4 - assets.jobs.length );

			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
			await page.locator( "#fps-toggle" ).click();
			await page.waitForFunction( () =>
				/FPS · .* ms$/.test( document.getElementById( "fps-readout" )?.textContent ?? "" )
			);
			assert.doesNotMatch( await page.locator( "#fps-readout" ).innerText(), /Client|Agent|cpu|draws/i );
			assert.equal( requests, 0 );
			await page.evaluate( () => window.sroDebug?.setDiagnostics( true ) );
			const fps = await page.locator( "#fps-toggle" ).boundingBox();
			const developer = await page.locator( "#developer-toggle" ).boundingBox();
			assert.ok( developer && fps && developer.x + developer.width <= fps.x && developer.y === fps.y );
			await page.locator( "#developer-toggle" ).click();
			await page.waitForFunction( () =>
				document.getElementById( "developer-readout" )?.textContent?.includes( "Agent abcdef0" )
			);
			assert.equal( await page.locator( "#fps-readout" ).isVisible(), false );
			assert.match( await page.locator( "#developer-readout" ).innerText(), /Fixture Agent build/ );
			assert.equal( requests, 1 );
			await mkdir( "temp/artifacts/developer-diagnostics", { recursive: true } );
			await page.screenshot( { path: "temp/artifacts/developer-diagnostics/developer.png" } );
			await page.reload();
			await ready( page );
			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), true );
			assert.equal( await page.locator( "#developer-readout" ).isVisible(), false );
			assert.equal( requests, 1 );
			await page.setViewportSize( { width: 640, height: 480 } );
			await page.locator( "#developer-toggle" ).click();
			await page.waitForFunction( () =>
				document.getElementById( "developer-readout" )?.textContent?.includes( "Agent abcdef0" )
			);
			const panel = await page.locator( "#developer-readout" ).boundingBox();
			assert.ok(
				panel && panel.x >= 0 && panel.y >= 0 && panel.x + panel.width <= 640 && panel.y + panel.height <= 480
			);
			await page.screenshot( { path: "temp/artifacts/developer-diagnostics/compact.png" } );
			await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
			assert.equal( await page.locator( "#developer-readout" ).isVisible(), false );
			await page.reload();
			await ready( page );
			assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
			await page.locator( "#fps-toggle" ).click();
			await page.screenshot( { path: "temp/artifacts/developer-diagnostics/player.png" } );
			await page.evaluate( async () => {
				const { runtime } = await import( "/src/bootstrap.ts" );
				runtime.dispose();
			} );
			assert.equal( await page.evaluate( () => window.sroDebug ), undefined );
			assert.equal( await page.locator( "#developer-toggle" ).count(), 0 );
			assert.deepEqual( errors, [] );
		} finally {
			await browser.close();
		}
	}
);

test( "console opt-in works when local storage is blocked", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.addInitScript( () => {
			Storage.prototype.getItem = () => {
				throw new DOMException( "Blocked", "SecurityError" );
			};
			Storage.prototype.setItem = () => {
				throw new DOMException( "Blocked", "SecurityError" );
			};
		} );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await ready( page );
		await page.evaluate( () => window.sroDebug?.setDiagnostics( true ) );
		assert.equal( await page.locator( "#developer-toggle" ).isVisible(), true );
		await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
		assert.equal( await page.locator( "#developer-toggle" ).isVisible(), false );
	} finally {
		await browser.close();
	}
} );

/*
================
hudToolsFixture

The actual HUD and DOM tools share one isolated canvas. Only the report's
outgoing callbacks are captured; no character or reporting server is used.
================
*/
async function hudToolsFixture( page ) {
	await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
	return page.evaluateHandle( async () => {
		const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
		const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
		const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
		const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
		const { createBugReportDialog } = await import( "/src/engine/runtime/bug-report/dialog.ts" );
		for (
			const href of [
				"/src/engine/runtime/platform/loading.css",
				"/src/engine/runtime/bug-report/bug-report.css"
			]
		) {
			const link = document.createElement( "link" );
			link.rel = "stylesheet";
			link.href = href;
			document.head.append( link );
		}
		const style = document.createElement( "style" );
		style.textContent = "body{overflow:hidden}canvas{display:block;width:100vw;height:100vh}";
		document.head.append( style );
		const chip = document.createElement( "div" );
		chip.id = "fps-chip";
		chip.className = "sro-fps-chip";
		chip.innerHTML = '<button id="fps-toggle" class="sro-fps-chip__toggle" type="button">F</button>' +
			'<div id="fps-readout" class="sro-fps-chip__readout" hidden></div>';
		document.body.append( chip );
		const canvas = document.querySelector( "canvas" );
		if ( !canvas ) throw Error( "Missing fixture canvas" );
		const assets = createAssets(), renderer = createRenderer( canvas );
		let scene, semantics;
		const commands = [], worldClicks = [], actions = [];
		const ui = createUi(
			assets,
			command => commands.push( command ),
			value => {
				scene = value;
				renderer.setUi( value );
			},
			( id, image ) => renderer.setUiTexture( id, image ),
			location.origin,
			"https://fixture.invalid"
		);
		const platform = createPlatform(
			canvas,
			document.createElement( "output" ),
			() => {},
			() => {},
			() => {},
			ui.event,
			ui.blocks,
			( x, y ) => worldClicks.push( [ x, y ] )
		);
		const dialog = createBugReportDialog( {
			launch: () => actions.push( "report" ),
			record: () => actions.push( "record" ),
			stopRecording: () => actions.push( "stop" ),
			deliver: async () => ({ ok: true, message: "Fixture only" }),
			still: async () => null,
			saved: async () => [],
			exportZip: async () => null,
			forget: async () => {}
		} );
		dialog.showLauncher( true );
		dialog.showRecording( "idle", 0, 30 );
		window.sroDebug?.setDiagnostics( true );
		platform.presentTelemetry( {
			fps: 60,
			pingMs: 25,
			frameMs: 16,
			p95FrameMs: 20,
			cpuMs: 5,
			p95CpuMs: 7,
			actors: 2,
			draws: 10,
			visibleGroups: 1,
			build: { lines: [ "Client fixture", "Agent fixture" ], detail: "HUD tools placement" }
		} );
		const entity = {
			gid: 7,
			regionId: 0x6a48,
			x: 1000,
			y: 0,
			z: 1000,
			heading: 0,
			kind: "player",
			name: "HUD fixture",
			mountedOn: 0
		};
		const pet = {
			gid: 9,
			refObjId: 7493,
			band: 2,
			hp: 500,
			mp: 0,
			status: 0,
			dead: false,
			level: 12,
			satiety: 5000,
			name: "Companion"
		};
		const state = {
			frontend: { phase: "world" },
			session: { phase: "world", revision: 1, character: "HUD fixture" },
			gameplay: {
				localGid: 7,
				pose: { ...entity, angle: 0 },
				inventory: [],
				vitals: [],
				cosRecords: [ pet ],
				target: 8
			},
			entities: [ entity, {
				...entity,
				gid: 8,
				kind: "monster",
				name: "Hyungno Ghost",
				level: 24,
				maxHp: 1200
			} ],
			worldReady: true
		};
		return {
			state,
			pet,
			actions,
			worldClicks,
			event: ui.event,
			recording: dialog.showRecording,
			reporting: dialog.showLauncher,
			/*
			================
			draw
			================
			*/
			draw() {
				semantics = ui.step( { ...state, ...platform.readUiViewport() }, performance.now() ) ?? semantics;
				if ( semantics ) platform.presentUi( semantics );
				renderer.frame( platform.readViewport() );
				return {
					pending: ui.stats().pending,
					failed: ui.stats().failed,
					error: renderer.error(),
					controls: semantics?.controls ?? [],
					logical: scene && [ scene.width, scene.height ],
					petRects: (scene?.quads ?? []).filter( quad =>
						quad.texture.includes( "/cos_outline_" ) ||
						/\/am_(cos_)?window\.png$/.test( quad.texture ) ||
						quad.texture.includes( "/pmi_pet_face" )
					).map( quad => quad.rect )
				};
			},
			/*
			================
			dispose
			================
			*/
			dispose() {
				dialog.dispose();
				platform.dispose();
				ui.dispose();
				renderer.dispose();
				assets.dispose();
			}
		};
	} );
}

/*
================
settleHudTools
================
*/
async function settleHudTools( fixture ) {
	const deadline = Date.now() + 30000;
	let stable = 0;
	while ( Date.now() < deadline ) {
		const report = await fixture.evaluate( owner => owner.draw() );
		assert.equal( report.error, null );
		if ( report.pending === 0 && report.controls.some( row => row.id === "hotbar:1" ) ) {
			if ( ++stable === 3 ) return report;
		} else stable = 0;
		await new Promise( resolve => setTimeout( resolve, 50 ) );
	}
	throw Error( "HUD tools fixture did not settle" );
}

/*
================
overlaps
================
*/
function overlaps( a, b ) {
	return a.x < b.x + b.width && a.x + a.width > b.x &&
		a.y < b.y + b.height && a.y + a.height > b.y;
}

/*
================
checkHudTools
================
*/
async function checkHudTools( { page, report, viewport, label, evidence, directory } ) {
	const scale = viewport.width / report.logical[0];
	const obstacles = [
		...report.petRects,
		...report.controls.filter( row =>
			/^(cos-status:|minimap-|clear-target|target-info|player-info|hotbar:|status-|compact-)/.test( row.id )
		).map( row => row.rect )
	].map( ( [x, y, width, height] ) => ({
		x: x * scale,
		y: y * scale,
		width: width * scale,
		height: height * scale
	}) );
	const boxes = await page.locator(
		"#fps-chip button:visible, .sro-bug-record__time:visible"
	).evaluateAll( elements =>
		elements.map( element => {
			const { x, y, width, height } = element.getBoundingClientRect();
			return {
				id: element.id || element.className,
				x,
				y,
				width,
				height,
				opacity: getComputedStyle( element ).opacity,
				isButton: element.tagName === "BUTTON"
			};
		} )
	);
	evidence.push( { label, boxes, obstacles } );
	await page.screenshot( { path: `${directory}/${label}.png` } );
	for ( const box of boxes ) {
		assert.ok(
			!obstacles.some( obstacle => overlaps( box, obstacle ) ),
			`${label}: ${box.id} overlaps the native HUD`
		);
		assert.ok(
			box.x >= 0 && box.y >= 0 && box.x + box.width <= viewport.width &&
				box.y + box.height <= viewport.height,
			`${label}: ${box.id} remains in the viewport`
		);
	}
	assert.deepEqual( report.failed, [] );
}

test(
	"report and developer tools stay visible and clickable outside the companion HUD",
	{ timeout: 180000 },
	async () => {
		const directory = "temp/artifacts/developer-diagnostics/hud-tools";
		await mkdir( directory, { recursive: true } );
		for ( const deviceScaleFactor of [ 1, 2 ] ) {
			const { browser, page } = await launchProbeBrowser( { deviceScaleFactor } );
			const errors = [], evidence = [];
			page.on( "pageerror", error => errors.push( error.message ) );
			let fixture;
			try {
				fixture = await hudToolsFixture( page );
				for (
					const viewport of [
						{ width: 1600, height: 900 },
						{ width: 800, height: 600 },
						{ width: 640, height: 480 },
						{ width: 375, height: 667 },
						{ width: 667, height: 375 },
						{ width: 320, height: 568 }
					]
				) {
					await page.setViewportSize( viewport );
					for ( const mode of [ "pickup", "attack", "multiple", "hidden" ] ) {
						await fixture.evaluate( ( owner, mode ) => {
							owner.state.gameplay.cosRecords = mode === "hidden" ?
								[] :
								mode === "multiple" ?
								[ owner.pet, { ...owner.pet, gid: 10, refObjId: 6117, band: 3 } ] :
								[ {
									...owner.pet,
									refObjId: mode === "attack" ? 6117 : 7493,
									band: mode === "attack" ? 3 : 2
								} ];
						}, mode );
						const report = await settleHudTools( fixture );
						const label = `${viewport.width}x${viewport.height}-${mode}-dpr${deviceScaleFactor}`;
						await checkHudTools( { page, report, viewport, label, evidence, directory } );
						await page.locator( "#fps-toggle" ).click();
						assert.equal( await page.locator( "#fps-readout" ).isVisible(), true );
						await page.locator( "#developer-toggle" ).click();
						assert.equal( await page.locator( "#developer-readout" ).isVisible(), true );
						assert.equal( await page.locator( "#fps-readout" ).isVisible(), false );
						const panel = await page.locator( "#developer-readout" ).boundingBox();
						assert.ok(
							panel && panel.x >= 0 && panel.y >= 0 &&
								panel.x + panel.width <= viewport.width && panel.y + panel.height <= viewport.height
						);
						await page.locator( "#developer-toggle" ).click();
						await page.locator( ".sro-bug-launcher" ).click();
						await page.locator( ".sro-bug-record__button" ).click();
						await fixture.evaluate( owner => owner.recording( "recording", 12, 30 ) );
						await checkHudTools( {
							page,
							report,
							viewport,
							label: label + "-recording",
							evidence,
							directory
						} );
						await page.locator( ".sro-bug-record__button" ).click();
						await fixture.evaluate( owner => owner.recording( "idle", 0, 30 ) );
						if ( mode === "multiple" && report.controls.some( row => row.id === "compact-map" ) ) {
							for ( const overlay of [ "compact-map", "compact-pets" ] ) {
								await fixture.evaluate(
									( owner, id ) => owner.event( { kind: "activate", id } ),
									overlay
								);
								const expanded = await settleHudTools( fixture );
								await checkHudTools( {
									page,
									report: expanded,
									viewport,
									label: label + "-" + overlay,
									evidence,
									directory
								} );
								await fixture.evaluate(
									( owner, id ) => owner.event( { kind: "activate", id } ),
									overlay
								);
								await settleHudTools( fixture );
							}
						}
						if ( mode === "hidden" ) {
							await fixture.evaluate( owner => {
								owner.state.gameplay.target = 0;
							} );
							const untargeted = await settleHudTools( fixture );
							await checkHudTools( {
								page,
								report: untargeted,
								viewport,
								label: label + "-no-target",
								evidence,
								directory
							} );
							await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
							await fixture.evaluate( owner => owner.reporting( false ) );
							await checkHudTools( {
								page,
								report: untargeted,
								viewport,
								label: label + "-tools-off",
								evidence,
								directory
							} );
							await page.evaluate( () => window.sroDebug?.setDiagnostics( true ) );
							await fixture.evaluate( owner => {
								owner.reporting( true );
								owner.state.gameplay.target = 8;
							} );
						}
					}
				}
				assert.equal( await fixture.evaluate( owner => owner.worldClicks.length ), 0 );
				const actions = await fixture.evaluate( owner => owner.actions );
				assert.deepEqual( actions, Array.from( { length: 24 }, () => [ "report", "record", "stop" ] ).flat() );
				await page.setViewportSize( { width: 1600, height: 900 } );
				await settleHudTools( fixture );
				const developer = await page.locator( "#developer-toggle" ).boundingBox();
				const fps = await page.locator( "#fps-toggle" ).boundingBox();
				assert.ok( developer && fps );
				await page.mouse.click( (developer.x + developer.width + fps.x) / 2, fps.y + fps.height / 2 );
				assert.equal(
					await fixture.evaluate( owner => owner.worldClicks.length ),
					1,
					"the gap between tools keeps world pointer input"
				);
				assert.deepEqual( errors, [] );
			} finally {
				await writeFile(
					`${directory}/dpr${deviceScaleFactor}.json`,
					JSON.stringify(
						{
							browser: browser.version(),
							evidence,
							errors
						},
						null,
						2
					)
				);
				if ( fixture ) {
					await fixture.evaluate( owner => owner.dispose() );
					await fixture.dispose();
				}
				await browser.close();
			}
		}
	}
);

/*
================
Legacy console preference migration
================
*/
test( "legacy opt-in migrates without overriding an explicit Experimental choice", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.addInitScript( () => localStorage.setItem( "sro.developerDiagnostics", "true" ) );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await ready( page );
		assert.equal( await page.locator( "#developer-toggle" ).isVisible(), true );
		assert.equal( await page.locator( "#developer-readout" ).isVisible(), false );
		await page.evaluate( () => window.sroDebug?.setDiagnostics( false ) );
		assert.equal( await page.evaluate( () => localStorage.getItem( "sro.developerDiagnostics" ) ), null );
		const stored = await page.evaluate( () =>
			JSON.parse( localStorage.getItem( "sro:v1150:experimental-options:1" ) ?? "null" )
		);
		assert.equal( stored.developerDiagnostics, false );
		assert.equal( stored.chatTimestamps, false );
		await page.reload();
		await ready( page );
		assert.equal(
			await page.locator( "#developer-toggle" ).isVisible(),
			false,
			"canonical off wins even when a legacy key remains"
		);
	} finally {
		await browser.close();
	}
} );
