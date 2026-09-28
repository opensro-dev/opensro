/*
===========================================================================

merchant-quantity-live.test.mjs - authenticated merchant amount regression

Uses the scratch character and the server's actual arrow offer. The browser
edits the visible quantity and confirms the resulting purchase; packet and
inventory observations prove the amount was not merely corrected visually.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import {
	openProbeAgentSession,
	readProbeCharacterSpawnFromSession,
	resolveProbeCredentials,
	resolveProbeDivisionId
} from "../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";

const TEST_TIMEOUT_MS = 180000;
const WORLD_TIMEOUT_MS = 60000;
const RESPONSE_TIMEOUT_MS = 15000;
const BLACKSMITH_REF = 2003;
const ARROW_REF = 62;
const ARROW_LIMIT = 250;
const ARTIFACT_DIR = process.env.SRO_PROBE_ARTIFACT_DIR ?? "temp/artifacts/merchant-quantity-live";

/*
================
merchantQuantityLive

The original position is restored even when the assertion fails. The only
inventory mutation is an ordinary purchase on the designated scratch user.
================
*/
test(
	"live merchant corrects oversized arrow quantities before charging gold",
	{ timeout: TEST_TIMEOUT_MS },
	async () => {
		const character = assertCharacterAllowed( "asd2", { context: "merchant quantity regression" } );
		const authority = await openProbeAgentSession();
		const original = await readProbeCharacterSpawnFromSession( authority, character );
		assert.ok( original );
		await mkdir( ARTIFACT_DIR, { recursive: true } );
		const { browser, page } = await launchProbeBrowser( {
			viewport: { width: 1024, height: 768 },
			executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE
		} );
		/** @type {{character:string, errors:string[], verdict:string, offer:unknown, purchase:unknown, failure:string|null}} */
		const evidence = { character, errors: [], verdict: "PENDING", offer: null, purchase: null, failure: null };
		try {
			await resetMissionMovementFixture( {
				session: authority,
				characterName: character,
				timeoutMs: RESPONSE_TIMEOUT_MS,
				fixture: {
					id: "merchant-quantity-jangan",
					movementMode: 3,
					start: { regionId: 25000, x: 390, y: 0, z: 1407 },
					startYawRadians: Math.PI
				}
			} );
			page.on( "pageerror", error => evidence.errors.push( String( error ) ) );
			await page.addInitScript( () => {
				const NativeWorker = window.Worker;
				/** @type {{entities:Record<number, any>, outbound:any[], failure:string|null}} */
				const record = { entities: {}, outbound: [], failure: null };
				globalThis["__merchantWire"] = record;
				window.Worker = class extends NativeWorker {
					/*
================
constructor
================
				*/
					constructor( url, options ) {
						super( url, options );
						this.addEventListener( "message", ( { data } ) => {
							if ( data.kind === "failure" ) record.failure = data.message;
							if ( data.kind !== "world" || !data.batch ) return;
							for ( const event of data.batch.events ) {
								if ( event.kind === "reset" ) record.entities = {};
								if ( event.kind === "spawn" || event.kind === "state" ) {
									record.entities[event.entity.gid] = event.entity;
								}
								if ( event.kind === "despawn" ) delete record.entities[event.gid];
							}
						} );
					}
					/*
================
postMessage
================
				*/
					postMessage( data, ...args ) {
						if ( data?.kind === "session" && data.command?.kind === "gameplay" ) {
							record.outbound.push( data.command.command );
						}
						return super.postMessage( data, ...args );
					}
				};
			} );
			console.log( "[merchant-quantity] authenticated scratch boot" );
			await page.goto( CLIENT_NEXT_BASE_URL );
			await page.evaluate( async () => {
				globalThis["__merchantRuntime"] = (await import( "/src/bootstrap.ts" )).runtime;
			} );
			await page.waitForFunction( () => globalThis["__merchantRuntime"].sessionState()?.phase === "signed-out" );
			const credentials = resolveProbeCredentials();
			await page.evaluate( ( { credentials, serverId } ) => {
				globalThis["__merchantRuntime"].session( {
					kind: "login",
					apiBase: location.origin + "/api",
					id: credentials.loginId,
					password: credentials.loginPassword,
					serverId
				} );
			}, { credentials, serverId: resolveProbeDivisionId() } );
			await page.waitForFunction( () => {
				const session = globalThis["__merchantRuntime"].sessionState();
				return session?.phase === "character-select" && Array.isArray( session.characters );
			} );
			const roster = await page.evaluate( () => globalThis["__merchantRuntime"].sessionState().characters );
			assert.equal( roster[1]?.name, character, "this dock fixture expects the scratch user in the second slot" );
			await page.waitForFunction( () =>
				/Frontend: dock\n/.test( document.querySelector( "output" )?.textContent ?? "" )
			);
			await page.mouse.click( 390, 430 );
			await page.waitForFunction( id => {
				const picked = document.querySelector( "output" )?.textContent?.match( /Dock pick: [^\n]*: (\d+)/ )
					?.[1];
				return picked === String( id );
			}, roster[1].id );
			await page.waitForFunction( () =>
				document.querySelector( '[data-ui-id="enter"]' )?.getAttribute( "disabled" ) === null
			);
			await page.locator( '[data-ui-id="enter"]' ).click();
			await page.waitForFunction(
				() => {
					const runtime = globalThis["__merchantRuntime"];
					if ( globalThis["__merchantWire"].failure ) throw Error( globalThis["__merchantWire"].failure );
					return runtime.sessionState()?.phase === "world" &&
						/Frontend: world\n/.test( document.querySelector( "output" )?.textContent ?? "" );
				},
				null,
				{ timeout: WORLD_TIMEOUT_MS }
			);
			console.log( "[merchant-quantity] select authored merchant" );
			await page.evaluate( ref => {
				const npc = Object.values( globalThis["__merchantWire"].entities ).find( entity =>
					entity.refObjId === ref
				);
				if ( !npc ) throw Error( "Blacksmith absent from admitted world" );
				globalThis["__merchantRuntime"].session( {
					kind: "gameplay",
					command: { kind: "select", gid: npc.gid }
				} );
			}, BLACKSMITH_REF );
			await page.locator( '[data-ui-id="shop-open"]' ).click( { timeout: RESPONSE_TIMEOUT_MS } );
			await page.waitForFunction( () => !!globalThis["__merchantRuntime"].gameplay()?.shop?.offers.length );
			const offer = await page.evaluate( ref => {
				const game = globalThis["__merchantRuntime"].gameplay();
				const index = game.shop.offers.findIndex( item => item.refObjId === ref );
				return index < 0 ? null : { ...game.shop.offers[index], index, gold: game.progression.gold };
			}, ARROW_REF );
			assert.ok( offer, "authored merchant must sell arrows" );
			assert.equal( offer.purchaseLimit, ARROW_LIMIT );
			assert.ok(
				BigInt( offer.gold ) >= BigInt( offer.price ) * BigInt( ARROW_LIMIT ),
				"scratch gold must cover arrows"
			);
			evidence.offer = offer;
			await page.locator( `[data-ui-id="shop-tab:${offer.tab}"]` ).click();
			await page.locator( `[data-ui-id="shop-offer:${offer.index}"]` ).click();
			const quantity = page.locator( '[data-ui-id="shop-quantity"]' );
			await quantity.fill( "1000" );
			await page.waitForTimeout( 200 );
			assert.equal( await quantity.inputValue(), String( ARROW_LIMIT ) );
			assert.equal( await page.locator( '[data-ui-id="shop-trade"]' ).isDisabled(), false );
			await page.screenshot( { path: ARTIFACT_DIR + "/corrected-amount.png" } );
			console.log( "[merchant-quantity] confirm corrected amount" );
			const before = await page.evaluate( ref => {
				const game = globalThis["__merchantRuntime"].gameplay();
				return {
					gold: game.progression.gold,
					count: game.inventory.filter( item => item.refObjId === ref ).reduce(
						( sum, item ) => sum + item.quantity,
						0
					)
				};
			}, ARROW_REF );
			await page.locator( '[data-ui-id="shop-trade"]' ).click();
			await page.waitForFunction(
				( { ref, expected } ) => {
					const game = globalThis["__merchantRuntime"].gameplay();
					return !game.inventoryPending &&
						game.inventory.filter( item => item.refObjId === ref ).reduce(
								( sum, item ) => sum + item.quantity,
								0
							) === expected;
				},
				{ ref: ARROW_REF, expected: before.count + ARROW_LIMIT },
				{ timeout: RESPONSE_TIMEOUT_MS }
			);
			const after = await page.evaluate( () => ({
				gold: globalThis["__merchantRuntime"].gameplay().progression.gold,
				commands: globalThis["__merchantWire"].outbound.filter( command => command.kind === "shop-buy" )
			}) );
			assert.equal( BigInt( after.gold ), BigInt( before.gold ) - BigInt( offer.price ) * BigInt( ARROW_LIMIT ) );
			assert.deepEqual( after.commands, [ {
				kind: "shop-buy",
				tab: offer.tab,
				slot: offer.slot,
				quantity: ARROW_LIMIT
			} ] );
			assert.deepEqual( evidence.errors, [] );
			evidence.purchase = { before, after };
			evidence.verdict = "PASS SUCCESS";
		} catch ( error ) {
			evidence.failure = String( error );
			throw error;
		} finally {
			await page.screenshot( { path: ARTIFACT_DIR + "/final.png" } ).catch( () => {} );
			await writeFile( ARTIFACT_DIR + "/session.json", JSON.stringify( evidence, null, 2 ) );
			await page.evaluate( () => globalThis["__merchantRuntime"]?.session( { kind: "logout" } ) ).catch(
				() => {}
			);
			await browser.close();
			await resetMissionMovementFixture( {
				session: authority,
				characterName: character,
				timeoutMs: RESPONSE_TIMEOUT_MS,
				fixture: {
					id: "merchant-quantity-restore",
					movementMode: 3,
					start: original,
					startYawRadians: original.angle / 65535 * Math.PI * 2
				}
			} );
		}
	}
);
