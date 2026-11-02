/*
===========================================================================

responsive-hud.test.mjs - a small viewport degrades the native HUD, never replaces it

A fine pointer below the original's smallest screen mode keeps the native
underbar (scaled to the window), the chat line and the party board in a
bounded left band; a touch screen keeps the compact layout with the same
party band and a visible chat line. Uses the real assets, UI, platform and
renderer without a live character, like compact-hud.test.mjs.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";

const ARTIFACT_DIRECTORY = "temp/artifacts/responsive-hud";
const ADMISSION_TIMEOUT_MS = 30000;
const FRAME_WAIT_MS = 50;
const DESKTOP = { width: 1512, height: 982 };
const PARTY_BAND_WIDTH = 480;
const RECT_EPSILON = 0.1;
const HOTBAR_IDS = Array.from( { length: 11 }, ( _, index ) => `hotbar:${index}` );
// The fine-pointer windows: every one is below the original's smallest mode,
// so each exercises the scaled underbar, the always-drawn chat and the
// banded party board on a real renderer.
const FINE_VIEWPORTS = [
	{ width: 1024, height: 600 },
	{ width: 1000, height: 550 },
	{ width: 854, height: 393 },
	{ width: 667, height: 375 },
	{ width: 640, height: 480 },
	{ width: 393, height: 854 }
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
partyRows

The board's rows and their right edges: every row must stay inside the left
band, which is what stops the roster from marching across the screen.
================
*/
function partyRows( report, viewport ) {
	const rows = report.controls.filter( row => row.id.startsWith( "party-target:" ) );
	assert.ok( rows.length >= 7, `expected the party roster, saw ${rows.length} rows` );
	for ( const row of rows ) {
		assert.ok(
			row.rect[0] + row.rect[2] <= Math.min( viewport.width, PARTY_BAND_WIDTH ) + RECT_EPSILON,
			`${row.id}: stays in the left band`
		);
	}
	return rows;
}

/*
================
openFixture

The compact-hud fixture's state with a party roster instead of a guild: a
leader, seven members with live entities, and the local player.
================
*/
async function openFixture( page ) {
	return await page.evaluateHandle( async () => {
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
		let scene, semantics;
		const commands = [];
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
			() => {}
		);
		const self = {
			gid: 7,
			regionId: 1,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			kind: "player",
			name: "Responsive fixture",
			mountedOn: 0
		};
		const members = [
			{
				id: 7,
				name: self.name,
				status: 0x87
			},
			...Array.from(
				{ length: 7 },
				( _, i ) => ({ id: 11 + i, name: "Member" + String( i + 1 ).padStart( 2, "0" ), status: 0x85 })
			)
		];
		const entities = [
			self,
			...members.slice( 1 ).map( ( member, i ) => ({
				...self,
				gid: 100 + i,
				name: member.name
			}) )
		];
		const bindings = Array.from( { length: 11 }, ( _, slot ) => ({ slot, kind: 0x46, payload: slot }) );
		const state = {
			frontend: { phase: "world" },
			session: { phase: "world", revision: 1, character: self.name },
			gameplay: {
				social: { ...emptySocial( self.name ), self: 7, leader: 11, members },
				localGid: 7,
				notices: [],
				target: 0,
				pose: { ...self, angle: 0 },
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
			entities,
			worldReady: true
		};
		return {
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
					error: renderer.error(),
					phase: renderer.phase(),
					logical: scene && [ scene.width, scene.height ],
					controls: semantics?.controls.map( ( { id, rect } ) => ({ id, rect }) ) ?? []
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
}

for ( const touch of [ false, true ] ) {
	test(
		`${touch ? "touch" : "fine pointer"}: a small viewport keeps the chat, the party band and the native underbar`,
		{ timeout: 240000 },
		async () => {
			const launched = await launchProbeBrowser( { viewport: DESKTOP } );
			const errors = [];
			const artifactDirectory = ARTIFACT_DIRECTORY + (touch ? "/touch" : "/fine");
			try {
				await launched.page.close();
				const context = await launched.browser.newContext( {
					viewport: DESKTOP,
					hasTouch: touch,
					deviceScaleFactor: 1
				} );
				const page = await context.newPage();
				page.on( "pageerror", error => errors.push( String( error ) ) );
				await mkdir( artifactDirectory, { recursive: true } );
				await page.goto( new URL( "/tests/browser/fixtures/ui-bridge.html", CLIENT_NEXT_BASE_URL ).href );
				const fixture = await openFixture( page );
				const partyRow = "party-target:100";
				try {
					await admit( page, fixture, [ ...HOTBAR_IDS, "hud-menu", "chat-text", partyRow ] );
					for ( const viewport of FINE_VIEWPORTS ) {
						await page.setViewportSize( viewport );
						await page.evaluate( async () => {
							await new Promise( requestAnimationFrame );
							await new Promise( requestAnimationFrame );
						} );
						const report = await admit(
							page,
							fixture,
							[ "hud-menu", "chat-text", partyRow, ...HOTBAR_IDS ]
						);
						const name = `${viewport.width}x${viewport.height}`;
						assert.deepEqual( errors, [], `${name}: page errors` );
						assert.deepEqual( report.logical, [ viewport.width, viewport.height ], name );
						contained( report.controls, viewport );
						partyRows( report, viewport );
						// The chat line never leaves the screen on a small viewport.
						const chat = report.controls.find( row => row.id === "chat-text" );
						assert.ok( chat, `${name}: chat line admitted` );
						assert.ok( chat.rect[1] + chat.rect[3] <= viewport.height, `${name}: chat inside` );
						// The compact tool row belongs to touch alone; a fine pointer
						// keeps the native underbar scaled to the window.
						assert.equal(
							report.controls.some( row => row.id === "compact-chat" ),
							touch,
							`${name}: compact tool row is the touch layout`
						);
						assert.equal(
							report.controls.some( row => row.id === "toggle-window:Guild" ),
							!touch || viewport.width >= 800,
							`${name}: native underbar buttons stay reachable`
						);
						await page.screenshot( { path: `${artifactDirectory}/${name}.png` } );
					}
				} finally {
					await fixture.evaluate( owner => owner.dispose() );
					await context.close();
				}
			} finally {
				await launched.browser.close();
			}
		}
	);
}
