/*
===========================================================================

compact-hud.test.mjs - responsive HUD admission through real touch input

Use the real assets, UI, platform and renderer without a live character.
One touch browser rotates and returns to desktop without replacing its owners.
Screenshots and semantic rectangles record what was actually admitted.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ARTIFACT_DIRECTORY = "temp/artifacts/compact-hud";
const ADMISSION_TIMEOUT_MS = 30000;
const FRAME_WAIT_MS = 50;
const DESKTOP = { width: 1512, height: 982 };
const HOTBAR_IDS = Array.from( { length: 11 }, ( _, index ) => `hotbar:${index}` );
const UTILITY_IDS = [
	"hud-menu",
	"toggle-window:System",
	"toggle-window:Guild",
	"item-mall",
	"compact-chat",
	"compact-status",
	"compact-extra",
	"hotbar-prev",
	"hotbar-next",
	"compact-map"
];
const RECT_EPSILON = 0.1;
const SLOT_BACKGROUND_SIZE = 40;
const SLOT_ART_SIZE = 32;
const RESIZE_FRAME_COUNT = 3;
const RESIZE_VIEWPORTS = [
	{ width: 375, height: 667 },
	{ width: 667, height: 375 },
	DESKTOP,
	{ width: 320, height: 568 },
	{ width: 200, height: 200 },
	{ width: 667, height: 375 },
	{ width: 375, height: 667 },
	DESKTOP
];

/*
================
contained
================
*/
function contained( controls, viewport ) {
	assert.ok( controls.length > 0, "expected admitted controls" );
	for ( const { id, rect: [x, y, width, height] } of controls ) {
		assert.ok( [ x, y, width, height ].every( Number.isFinite ), `${id}: finite rectangle` );
		assert.ok( width > 0 && height > 0, `${id}: nonempty rectangle` );
		assert.ok( x >= -RECT_EPSILON && y >= -RECT_EPSILON, `${id}: nonnegative position` );
		assert.ok( x + width <= viewport.width + RECT_EPSILON, `${id}: right edge` );
		assert.ok( y + height <= viewport.height + RECT_EPSILON, `${id}: bottom edge` );
	}
}

/*
================
admit

Drive the fixture's normal frames while production asset jobs finish.
================
*/
async function admit( page, fixture, required ) {
	const deadline = Date.now() + ADMISSION_TIMEOUT_MS;
	const viewport = page.viewportSize();
	let report;
	do {
		report = await fixture.evaluate( owner => owner.draw() );
		if ( report.error ) throw Error( report.error );
		if (
			report.pending === 0 && report.phase === "running" &&
			report.logical?.[0] === viewport?.width && report.logical?.[1] === viewport?.height &&
			required.every( id => report.controls.some( control => control.id === id ) )
		) return report;
		await page.waitForTimeout( FRAME_WAIT_MS );
	} while ( Date.now() < deadline );
	assert.fail( `HUD admission timed out: ${JSON.stringify( { required, report } )}` );
}

/*
================
tap

Use browser hit testing and touch events, never call ui.event for actions.
================
*/
async function tap( page, id ) {
	await page.locator( `[data-ui-id="${id}"]` ).tap();
}

/*
================
nativeGeometry
================
*/
function nativeGeometry( report ) {
	return report.controls.filter( row =>
		HOTBAR_IDS.includes( row.id ) || [ "chat-text", "ext-drag", "hud-menu" ].includes( row.id )
	).map( ( { id, rect } ) => ({ id, rect }) ).sort( ( a, b ) => a.id.localeCompare( b.id ) );
}

/*
================
slotBackgrounds

Assert published paint, not just transparent browser hit targets. Each native
slot keeps its own opaque crop, even when no binding supplies an item icon.
================
*/
function slotBackgrounds( report ) {
	const crops = [];
	for ( const id of HOTBAR_IDS ) {
		const control = report.controls.find( row => row.id === id );
		assert.ok( control, id );
		assert.deepEqual( control.rect.slice( 2 ), [ SLOT_ART_SIZE, SLOT_ART_SIZE ], `${id}: native art extent` );
		const background = report.backgrounds.find( quad =>
			Math.abs( quad.rect[0] - (control.rect[0] - 4) ) < RECT_EPSILON &&
			Math.abs( quad.rect[1] - (control.rect[1] - 4) ) < RECT_EPSILON
		);
		assert.ok( background, `${id}: drawn underbar background` );
		assert.deepEqual( background.rect.slice( 2 ), [ SLOT_BACKGROUND_SIZE, SLOT_BACKGROUND_SIZE ] );
		assert.deepEqual( background.color, [ 1, 1, 1, 1 ], `${id}: crop is not passed as tint` );
		assert.ok( background.size, `${id}: resident underbar texture` );
		const [u, v, width, height] = background.uv;
		assert.ok( u >= 0 && v >= 0 && u + width <= 1 && v + height <= 1, `${id}: valid crop` );
		assert.ok( Math.abs( width * background.size[0] - SLOT_BACKGROUND_SIZE ) < RECT_EPSILON );
		assert.ok( Math.abs( height * background.size[1] - SLOT_BACKGROUND_SIZE ) < RECT_EPSILON );
		crops.push( background.uv.join( "," ) );
	}
	assert.equal( new Set( crops ).size, HOTBAR_IDS.length, "each slot retains its authored underbar crop" );
}

for ( const deviceScaleFactor of [ 1, 2 ] ) {
	test(
		`compact HUD admits touch, rotation and desktop at DPR ${deviceScaleFactor}`,
		{ timeout: 180000 },
		async () => {
			const launched = await launchProbeBrowser( { viewport: DESKTOP } );
			const browser = launched.browser;
			const reports = {}, errors = [];
			const artifactDirectory = deviceScaleFactor === 1 ? ARTIFACT_DIRECTORY : ARTIFACT_DIRECTORY + "/dpr2";
			try {
				await launched.page.close();
				const context = await browser.newContext( { viewport: DESKTOP, hasTouch: true, deviceScaleFactor } );
				const page = await context.newPage();
				page.on( "pageerror", error => errors.push( String( error ) ) );
				await mkdir( artifactDirectory, { recursive: true } );
				await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
				const fixture = await page.evaluateHandle( async () => {
					const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
					const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
					const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
					const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
					const { emptySocial } = await import( "/src/engine/foundation/gameplay/social.ts" );
					const canvas = document.querySelector( "canvas" );
					if ( !canvas ) throw Error( "Missing fixture canvas" );
					const style = document.createElement( "style" );
					style.textContent = "body{overflow:hidden}canvas{display:block;width:100vw;height:100vh}";
					document.head.append( style );
					const renderer = createRenderer( canvas ), assets = createAssets();
					const telemetryStyle = document.createElement( "link" );
					telemetryStyle.rel = "stylesheet";
					telemetryStyle.href = "/src/engine/runtime/platform/loading.css";
					document.head.append( telemetryStyle );
					const telemetry = document.createElement( "div" );
					telemetry.id = "fps-chip";
					telemetry.className = "sro-fps-chip";
					telemetry.innerHTML =
						'<button class="sro-fps-chip__toggle">&lt;/&gt;</button><button class="sro-fps-chip__toggle">F</button>';
					document.body.append( telemetry );
					let scene, semantics;
					const commands = [], worldClicks = [], textureSizes = new Map();
					const ui = createUi(
						assets,
						command => commands.push( command ),
						value => {
							scene = value;
							renderer.setUi( value );
						},
						( id, image ) => {
							if ( image ) textureSizes.set( id, [ image.width, image.height ] );
							else textureSizes.delete( id );
							renderer.setUiTexture( id, image );
						},
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
					// Same pose/entity shape as helpers/ui-fixture.mjs. Vitals use the
					// retained gameplay contract; progression level matches the report.
					const entity = {
						gid: 7,
						regionId: 1,
						x: 0,
						y: 0,
						z: 0,
						heading: 0,
						kind: "player",
						name: "Compact fixture",
						mountedOn: 0
					};
					const bindings = Array.from( { length: 11 }, ( _, slot ) => ({ slot, kind: 0x46, payload: slot }) );
					// Match the populated Guild runtime fixture, with this character as leader.
					const members = Array.from( { length: 18 }, ( _, i ) => ({
						id: i + 1,
						name: i === 0 ? entity.name : "Member" + String( i + 1 ).padStart( 2, "0" ),
						grade: i === 0 ? 0 : 1,
						level: 40 + i,
						permissions: 31,
						donated: i * 100,
						grant: "Officer",
						model: 1907,
						role: 1,
						offline: i % 2,
						warScore: i * 10
					}) );
					const state = {
						frontend: { phase: "world" },
						session: { phase: "world", revision: 1, character: "Compact fixture" },
						gameplay: {
							social: {
								...emptySocial( entity.name ),
								self: 1,
								allianceMaster: 7,
								guild: {
									id: 7,
									name: "Native Guild",
									level: 5,
									gp: 12345,
									subject: "Guild notice",
									contents: "Notice body",
									crest: 0,
									members
								}
							},
							localGid: 7,
							notices: [ { sequence: 0, key: "", value: 0, text: "", notificationBanner: true } ],
							target: 8,
							cosRecords: [ {
								gid: 9,
								refObjId: 9,
								band: 3,
								hp: 500,
								mp: 0,
								status: 0,
								dead: false,
								level: 12,
								satiety: 5000,
								name: "Companion"
							} ],
							pose: { ...entity, angle: 0 },
							progression: {
								level: 32,
								experience: "1200",
								gold: "5000",
								skillPoints: 12,
								masteries: []
							},
							inventory: Array.from( { length: 11 }, ( _, index ) => ({
								slot: 13 + index,
								refObjId: 3630,
								typeFlags: 0x08ec,
								quantity: 50,
								plus: 0,
								durability: 0,
								variance: "0",
								magic: []
							}) ),
							quickSlots: bindings,
							inventorySlotCount: 45,
							equipmentSlotCount: 13,
							vitals: [ { gid: 7, hp: 900, maxHp: 1200, mp: 600, maxMp: 800 } ],
							casts: [],
							skillCatalog: []
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
						/*
						================
						flashNotice
						================
						*/
						flashNotice() {
							state.gameplay.notices = [ {
								sequence: state.gameplay.notices[0].sequence + 1,
								key: "",
								value: 0,
								notificationBanner: true,
								text:
									"Cannot use due to insufficient MP. A longer message must wrap without covering the combat header."
							} ];
						},
						/*
				================
				emptySlots
				================
				*/
						emptySlots( empty ) {
							state.gameplay = { ...state.gameplay, quickSlots: empty ? [] : bindings };
						},
						/*
				================
				draw
				================
				*/
						draw() {
							semantics = ui.step( { ...state, ...platform.readUiViewport() }, performance.now() ) ??
								semantics;
							if ( semantics ) platform.presentUi( semantics );
							renderer.frame( platform.readViewport() );
							return {
								pending: ui.stats().pending,
								loadingError: semantics?.loadingError,
								message: semantics?.message,
								error: renderer.error(),
								phase: renderer.phase(),
								logical: scene && [ scene.width, scene.height ],
								noticeRects: (scene?.quads ?? []).filter( quad =>
									quad.texture.includes( "/com_notice_" )
								)
									.map( quad => quad.rect ),
								petQuads: (scene?.quads ?? []).filter( quad => quad.texture.includes( "/pmi_pet" ) )
									.map( quad => quad.rect ),
								glyphScales: (scene?.quads ?? []).flatMap( quad => {
									const size = textureSizes.get( quad.texture );
									return quad.run && size ?
										quad.run.glyphs.filter( glyph => glyph.uv[2] > 0 && glyph.uv[3] > 0 )
											.map(
												glyph => [
													glyph.width / (glyph.uv[2] * size[0]),
													glyph.height / (glyph.uv[3] * size[1])
												]
											) :
										[];
								} ),
								backgrounds: (scene?.quads ?? []).filter( quad =>
									typeof quad.texture === "string" && quad.texture.endsWith( "/ub_window_01.png" )
								).map( quad => ({ ...quad, size: textureSizes.get( quad.texture ) }) ),
								controls:
									semantics?.controls.map( ( { id, rect, disabled } ) => ({ id, rect, disabled }) ) ??
										[],
								bindings: state.gameplay.quickSlots,
								commands: commands.slice(),
								clicks: worldClicks.length
							};
						},
						/*
				================
				dispose
				================
				*/
						dispose() {
							platform.dispose();
							ui.dispose();
							assets.dispose();
							renderer.dispose();
						}
					};
				} );
				try {
					const desktop = await admit( page, fixture, [ ...HOTBAR_IDS, "chat-text", "ext-drag" ] );
					reports["desktop-before"] = desktop;
					// Run crash coverage first so an unrelated interaction assertion cannot
					// prevent reproduction of invalid geometry during retained-window fitting.
					for ( const window of [ "closed", "inventory" ] ) {
						if ( window === "inventory" ) {
							await tap( page, "hud-menu" );
							await admit( page, fixture, [ "select-window:Inventory" ] );
							await tap( page, "select-window:Inventory" );
							await admit( page, fixture, [ "slot:13", "close" ] );
						}
						for ( const [index, viewport] of RESIZE_VIEWPORTS.entries() ) {
							const key = `resize-${window}-${index}-${viewport.width}x${viewport.height}`;
							reports[key] = { viewport, window, phase: "resizing" };
							try {
								await page.setViewportSize( viewport );
								await page.evaluate( async () => {
									await new Promise( requestAnimationFrame );
									await new Promise( requestAnimationFrame );
								} );
								for ( let frame = 0; frame < RESIZE_FRAME_COUNT; frame++ ) {
									const report = await fixture.evaluate( owner => owner.draw() );
									reports[key] = { viewport, window, frame, ...report };
									assert.ok( !report.error, `${key}: ${report.error}` );
									assert.deepEqual( errors, [], `${key}: page errors` );
									assert.deepEqual( report.logical, [ viewport.width, viewport.height ] );
									await page.waitForTimeout( FRAME_WAIT_MS );
								}
							} catch ( error ) {
								reports[key] = { ...reports[key], failure: String( error ) };
								throw error;
							}
						}
						await admit( page, fixture, HOTBAR_IDS );
						if ( window === "inventory" ) {
							await tap( page, "close" );
							await admit( page, fixture, HOTBAR_IDS );
						}
					}
					for ( const viewport of [ { width: 375, height: 667 }, { width: 667, height: 375 } ] ) {
						const name = viewport.width === 375 ? "portrait" : "landscape";
						await page.setViewportSize( viewport );
						await fixture.evaluate( owner => owner.flashNotice() );
						let report = await admit( page, fixture, [ ...HOTBAR_IDS, ...UTILITY_IDS ] );
						assert.deepEqual( report.logical, [ viewport.width, viewport.height ] );
						contained( report.controls, viewport );
						slotBackgrounds( report );
						assert.ok(
							!report.controls.some( row => [ "chat-text", "ext-drag", "minimap-in" ].includes( row.id ) )
						);
						assert.deepEqual( report.bindings, desktop.bindings );
						reports[name] = report;
						await page.screenshot( { path: `${artifactDirectory}/${name}.png` } );
						const player = report.controls.find( row => row.id === "self-target" );
						const target = report.controls.find( row => row.id === "target-info" );
						assert.ok( player && target );
						assert.ok(
							target.rect[0] >= player.rect[0] + player.rect[2] &&
								target.rect[1] < player.rect[1] + player.rect[3],
							"target shares the first row beside player vitals"
						);
						assert.ok( report.petQuads.length > 0, "active companion panel is painted" );
						for ( const pet of report.petQuads ) {
							assert.ok(
								target.rect[1] >= pet[1] + pet[3] || target.rect[0] >= pet[0] + pet[2],
								"target frame stays beside or below active companion vitals"
							);
						}
						const mapBox = await page.locator( '[data-ui-id="compact-map"]' ).boundingBox();
						const telemetryBox = await page.locator( "#fps-chip" ).boundingBox();
						assert.ok( mapBox && telemetryBox );
						assert.ok(
							telemetryBox.y + telemetryBox.height <= mapBox.y,
							"Map lives in the toolbar, outside the combat header"
						);
						assert.ok( report.noticeRects.length > 0, "the transient notice is visibly painted" );
						const combatBottom = Math.max(
							player.rect[1] + player.rect[3],
							target.rect[1] + target.rect[3],
							...report.petQuads.map( r => r[1] + r[3] ),
							telemetryBox.y + telemetryBox.height
						);
						for ( const r of report.noticeRects ) {
							assert.ok( r[1] >= combatBottom, "notice chrome remains below the complete combat header" );
							assert.ok(
								r[0] >= 0 && r[0] + r[2] <= viewport.width,
								"wrapped notice chrome fits the viewport"
							);
						}
						await fixture.evaluate( owner => owner.emptySlots( true ) );
						report = await admit( page, fixture, HOTBAR_IDS );
						assert.deepEqual( report.bindings, [] );
						slotBackgrounds( report );
						reports[`${name}-empty-slots`] = report;
						await page.screenshot( { path: `${artifactDirectory}/${name}-empty-slots.png` } );
						await fixture.evaluate( owner => owner.emptySlots( false ) );
						report = await admit( page, fixture, HOTBAR_IDS );
						const before = report.commands.length;
						for ( const id of HOTBAR_IDS ) {
							await tap( page, id );
							await admit( page, fixture, HOTBAR_IDS );
						}
						report = await fixture.evaluate( owner => owner.draw() );
						assert.deepEqual(
							report.commands.slice( before ).filter( row => row.command?.kind !== "minimap-floors" ),
							HOTBAR_IDS.map( ( _, index ) => ({
								kind: "gameplay",
								command: { kind: "item-use", slot: 13 + index }
							}) ),
							"rotation preserves each native slot's binding and touch activation"
						);
						await tap( page, "compact-chat" );
						report = await admit( page, fixture, [ "chat-text", ...HOTBAR_IDS ] );
						contained( report.controls, viewport );
						const editor = page.locator( '[data-ui-id="chat-text"]' );
						await editor.tap();
						assert.equal( await editor.evaluate( element => document.activeElement === element ), true );
						await page.keyboard.insertText( `${name} touch draft` );
						await admit( page, fixture, [ "chat-text" ] );
						assert.equal( await editor.inputValue(), `${name} touch draft` );
						const chat = report.controls.find( row => row.id === "chat-text" );
						assert.ok( chat );
						const barTop = Math.min(
							...report.controls.filter( row => HOTBAR_IDS.includes( row.id ) ).map( row => row.rect[1] )
						);
						assert.ok( chat.rect[1] + chat.rect[3] <= barTop, "chat stays above hotbar" );
						reports[`${name}-chat`] = await fixture.evaluate( owner => owner.draw() );
						await page.screenshot( { path: `${artifactDirectory}/${name}-chat.png` } );
						await editor.fill( "" );
						await tap( page, "compact-chat" );
						await admit( page, fixture, UTILITY_IDS );
						await tap( page, "compact-extra" );
						report = await admit( page, fixture, [
							"ext-drag",
							...Array.from( { length: 10 }, ( _, i ) => `hotbar:${41 + i}` )
						] );
						contained( report.controls, viewport );
						reports[`${name}-extra`] = report;
						await tap( page, "compact-extra" );
						await admit( page, fixture, UTILITY_IDS );
						await tap( page, "compact-map" );
						report = await admit( page, fixture, [ "minimap-in", "minimap-out" ] );
						contained( report.controls, viewport );
						reports[`${name}-map`] = report;
						await tap( page, "compact-map" );
						await admit( page, fixture, UTILITY_IDS );
						await tap( page, "compact-status" );
						report = await admit( page, fixture, [ "status-size", "status-filter" ] );
						contained( report.controls, viewport );
						reports[`${name}-status`] = report;
						await tap( page, "compact-status" );
						await admit( page, fixture, UTILITY_IDS );
						await tap( page, "hud-menu" );
						await admit( page, fixture, [ "select-window:Inventory" ] );
						await tap( page, "select-window:Inventory" );
						report = await admit( page, fixture, [ "slot:13", "slot:44", "close" ] );
						contained( report.controls, viewport );
						reports[`${name}-inventory`] = report;
						await page.screenshot( { path: `${artifactDirectory}/${name}-inventory.png` } );
						await tap( page, "close" );
						report = await admit( page, fixture, UTILITY_IDS );
						assert.ok(
							!report.controls.some( row => row.id === "slot:13" ),
							"touch Close dismisses inventory"
						);
						for (
							const [open, close, required] of [
								[ "toggle-window:System", "close", "open-window:Option" ],
								[ "toggle-window:Guild", "close", "social-member:1" ],
								[ "item-mall", "item-mall-close", "item-mall-close" ]
							]
						) {
							await tap( page, open );
							report = await admit( page, fixture, [ required ] );
							contained( report.controls, viewport );
							if ( open === "toggle-window:Guild" ) {
								assert.ok(
									report.controls.filter( row => row.id.startsWith( "social-member:" ) ).length >= 3
								);
								reports[`${name}-populated-guild`] = report;
							}
							if ( open === "toggle-window:Guild" || open === "item-mall" ) {
								assert.ok( report.glyphScales.length > 0 );
								assert.ok(
									report.glyphScales.every( scale => scale.every( value => value >= 0.99 ) ),
									`${open}: mobile text must retain its native bitmap size`
								);
							}
							await page.screenshot( {
								path: `${artifactDirectory}/${name}-${open.replaceAll( ":", "-" )}.png`
							} );
							await tap( page, close );
							await admit( page, fixture, UTILITY_IDS );
						}
						assert.equal( report.clicks, 0, "HUD touch gestures must not reach world input" );
					}
					await page.setViewportSize( DESKTOP );
					const restored = await admit( page, fixture, [ ...HOTBAR_IDS, "chat-text", "ext-drag" ] );
					assert.ok( !restored.controls.some( row => row.id.startsWith( "compact-" ) ) );
					assert.deepEqual(
						nativeGeometry( restored ),
						nativeGeometry( desktop ),
						"native desktop geometry is restored"
					);
					assert.deepEqual( restored.bindings, desktop.bindings );
					assert.equal( restored.clicks, 0 );
					reports["desktop-after"] = restored;
					await page.screenshot( { path: `${artifactDirectory}/desktop-restored.png` } );
					assert.deepEqual( errors, [] );
				} finally {
					await writeFile(
						`${artifactDirectory}/control-stats.json`,
						JSON.stringify( { reports, errors }, null, 2 )
					);
					await fixture.evaluate( owner => owner.dispose() );
					await fixture.dispose();
				}
			} finally {
				await browser.close();
			}
		}
	);
}
