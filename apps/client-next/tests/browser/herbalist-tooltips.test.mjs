/*
===========================================================================

herbalist-tooltips.test.mjs - live herbalist shop titles

Opens the herbalist through an authenticated probe session and checks that
every offer publishes a readable title and tooltip.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { openProbeAgentSession, readProbeCharacterSpawnFromSession } from "../../../../scripts/lib/probeSession.mjs";
import { resolveProbeCredentials } from "../../../../scripts/lib/probeSession.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

// Authenticated product session, original server replies, real worker owners.
// The recorder observes published state; it never fabricates inbound packets.
test( "herbalist shop publishes readable titles for every offer", { timeout: 180000 }, async () => {
	const phase = name => console.log( "[commerce] " + name );
	phase( "scratch-checkpoint" );
	const character = assertCharacterAllowed( "asd2", { context: "client-next live commerce" } );
	const authority = await openProbeAgentSession(),
		original = await readProbeCharacterSpawnFromSession( authority, character );
	assert.ok( original );
	await resetMissionMovementFixture( {
		session: authority,
		characterName: character,
		timeoutMs: 30000,
		fixture: {
			id: "jangan-herbalist-tooltips-v1",
			movementMode: 3,
			start: { regionId: 25000, x: 1484, y: 0, z: 1407 },
			startYawRadians: Math.PI
		}
	} );
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		errors = [],
		control = id => page.locator( `[data-ui-id="${id}"]` );
	const capture = async ( name, id ) => {
		await control( id ).waitFor();
		await page.evaluate( () =>
			new Promise( resolve => requestAnimationFrame( () => requestAnimationFrame( resolve ) ) )
		);
		await page.screenshot( { path: "temp/artifacts/herbalist-tooltips/" + name + ".png" } );
	};
	await mkdir( "temp/artifacts/herbalist-tooltips", { recursive: true } );
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.route( "**/src/engine/runtime/ui/ui.ts", async route => {
			const response = await route.fetch(), source = await response.text();
			const body = source.replace( "export function createUi(", "function createObservedCommerceUi(" ) +
				`\nexport function createUi(...args){const publish=args[2];args[2]=scene=>{globalThis.__commerceHelpScene=scene;publish(scene)};const owner=createObservedCommerceUi(...args);return {...owner,event(event){if(event.kind==='hover')globalThis.__commerceHelpHover=event.id;return owner.event(event)}};}`;
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		const help = async ( id, name ) => {
			await control( id ).hover();
			await page.waitForFunction(
				id =>
					__commerceHelpHover === id &&
					__commerceHelpScene?.quads.some( q => q.texture.endsWith( "com_tooltip_corner.png" ) ),
				id
			);
			await page.screenshot( { path: "temp/artifacts/herbalist-tooltips/" + name + ".png" } );
		};
		await page.route( "**/src/engine/foundation/ui/item-tooltip.ts", async route => {
			const response = await route.fetch(), source = await response.text();
			const body = source.replace( "export function itemTooltip(", "function observedItemTooltip(" ) +
				"\nexport function itemTooltip(...args){const rows=observedItemTooltip(...args);globalThis.__herbalistTooltip={refObjId:args[0].refObjId,name:args[0].name,rows};return rows;}";
			await route.fulfill( { response, body, contentType: "application/javascript" } );
		} );
		await page.addInitScript( () => {
			const Original = window.Worker;
			window.__commerce = { entities: {}, phases: [], native: [], game: null, readyCount: 0 };
			window.Worker = class extends Original {
				postMessage( data, ...args ) {
					if ( data?.kind === "session" && data.command?.kind === "world-ready" ) {
						window.__commerce.readyCount++;
					}
					return super.postMessage( data, ...args );
				}
				constructor( ...args ) {
					super( ...args );
					this.addEventListener( "message", ( { data } ) => {
						const r = window.__commerce;
						if ( data.kind === "failure" ) r.failure = data.message;
						if ( data.kind === "session" ) {
							r.session = data.state;
							if ( r.phases.at( -1 ) !== data.state.phase ) r.phases.push( data.state.phase );
							window.__commerceWorker = this;
						}
						if ( data.kind === "world" && data.batch ) {
							for ( const e of data.batch.events ) {
								if ( e.kind === "spawn" || e.kind === "state" ) r.entities[e.entity.gid] = e.entity;
								else if ( e.kind === "despawn" ) delete r.entities[e.gid];
								else if ( e.kind === "gameplay" ) r.game = { ...r.game, ...e.state };
								else if ( e.kind === "native" && [ 0xb06d, 0xb4b5, 0x30b3 ].includes( e.opcode ) ) {
									r.native.push( { opcode: e.opcode, payload: [ ...e.payload ] } );
									if ( r.native.length > 100 ) r.native.shift();
								}
							}
						}
					} );
				}
			};
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await control( "login" ).waitFor( { timeout: 15000 } );
		await page.waitForFunction( () => !document.querySelector( '[data-ui-id="login"]' )?.disabled );
		const { loginId, loginPassword } = resolveProbeCredentials();
		await control( "account" ).fill( loginId );
		await control( "password" ).fill( loginPassword );
		await control( "password" ).press( "Enter" );
		await control( "frontend:create" ).waitFor( { timeout: 30000 } );
		await page.waitForFunction( () => /Frontend: dock\n/.test( document.querySelector( "output" )?.textContent ) );
		const roster = await page.evaluate( () => window.__commerce.session.characters );
		assert.equal( roster[1]?.name, character );
		await page.mouse.click( 505, 430 );
		await page.waitForFunction( () => !document.querySelector( '[data-ui-id="enter"]' )?.disabled );
		await control( "enter" ).click();
		await page.waitForFunction(
			() =>
				window.__commerce.failure || window.__commerce.session?.error ||
				[ "world", "failed", "disconnected" ].includes( window.__commerce.session?.phase ),
			null,
			{ timeout: 45000 }
		);
		assert.equal( await page.evaluate( () => window.__commerce.session.phase ), "world" );
		await page.waitForFunction(
			() => /Frontend: world\n/.test( document.querySelector( "output" )?.textContent ),
			null,
			{ timeout: 45000 }
		);
		const selectNpc = async () => {
			await page.evaluate( () => {
				const npc = Object.values( window.__commerce.entities ).find( e =>
					e.kind === "npc" && e.refObjId === 2005
				);
				if ( !npc ) throw Error( "Authored herbalist absent" );
				window.__commerceWorker.postMessage( {
					kind: "session",
					command: { kind: "gameplay", command: { kind: "select", gid: npc.gid } }
				} );
			} );
			await page.waitForFunction( () => window.__commerce.game?.target && !window.__commerce.game.targetPending );
		};
		const selectMerchant = async () => {
			await selectNpc();
			await capture( "menu", "shop-open" );
			await control( "shop-open" ).click();
			await page.waitForFunction( () =>
				window.__commerce.game?.shop && !window.__commerce.game.inventoryPending
			);
		};
		await page.waitForFunction( () => window.__commerce.readyCount === 1 );
		phase( "merchant" );
		await selectMerchant();
		const shop = await page.evaluate( () => window.__commerce.game.shop );
		assert.equal( shop.error, undefined );
		assert.ok( shop.offers.length > 0 );
		for ( const offer of shop.offers ) {
			assert.ok( offer.items?.length, "merchant publishes actual item previews" );
			for ( const item of offer.items ) assert.ok( item.tooltip, "preview has authoritative item metadata" );
		}
		const captured = [];
		for ( let index = 0; index < shop.offers.length; index++ ) {
			const offer = shop.offers[index];
			phase( "hover-" + offer.refObjId );
			await control( "shop-tab:" + offer.tab ).click();
			await help( "shop-offer:" + index, "offer-" + offer.refObjId );
			await page.waitForFunction( id => globalThis.__herbalistTooltip?.refObjId === id, offer.refObjId, {
				timeout: 10000
			} );
			captured.push( { offer, tooltip: await page.evaluate( () => globalThis.__herbalistTooltip ) } );
		}
		await writeFile( "temp/artifacts/herbalist-tooltips/offers.json", JSON.stringify( captured, null, 2 ) );
		for ( const { offer, tooltip } of captured ) {
			assert.ok( tooltip.rows[0]?.heading, "Missing heading for " + offer.refObjId );
			assert.ok(
				tooltip.rows[0].value.trim() && tooltip.rows[0].value.trim() !== "-",
				"Placeholder heading for " + offer.refObjId
			);
			assert.equal( tooltip.rows[0].ornament, "item" );
		}
		assert.deepEqual( errors, [] );
		phase( "PASS SUCCESS" );
	} finally {
		await writeFile(
			"temp/artifacts/herbalist-tooltips/session.json",
			JSON.stringify(
				{
					errors,
					status: await page.locator( "output" ).textContent().catch( () => null ),
					record: await page.evaluate( () => window.__commerce ).catch( () => null )
				},
				null,
				2
			)
		);
		await page.screenshot( { path: "temp/artifacts/herbalist-tooltips/mission.png" } ).catch( () => {} );
		await page.evaluate( () =>
			window.__commerceWorker?.postMessage( { kind: "session", command: { kind: "logout" } } )
		).catch( () => {} );
		await page.waitForTimeout( 500 );
		await browser.close();
		await resetMissionMovementFixture( {
			session: authority,
			characterName: character,
			timeoutMs: 30000,
			fixture: {
				id: "restore-herbalist-origin",
				movementMode: 3,
				start: original,
				startYawRadians: original.angle / 65535 * Math.PI * 2
			}
		} );
	}
} );
