/*
===========================================================================

live-commerce.test.mjs - live NPC commerce against the product server

An authenticated probe session trades at the Jangan blacksmith through the
real worker owners; the recorder keeps the item-move, shop and gold (0x30B3)
frames for the failure report.

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
test( "authenticated mission admits the live commerce session", { timeout: 180000 }, async () => {
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
			id: "jangan-blacksmith-commerce-v1",
			movementMode: 3,
			start: { regionId: 25000, x: 390, y: 0, z: 1407 },
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
		await page.screenshot( { path: "temp/artifacts/live-commerce/" + name + ".png" } );
	};
	await mkdir( "temp/artifacts/live-commerce", { recursive: true } );
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
			await page.screenshot( { path: "temp/artifacts/live-commerce/" + name + ".png" } );
		};
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
					e.kind === "npc" && e.refObjId === 2003
				);
				if ( !npc ) throw Error( "Authored blacksmith absent" );
				window.__commerceWorker.postMessage( {
					kind: "session",
					command: { kind: "gameplay", command: { kind: "select", gid: npc.gid } }
				} );
			} );
			await page.waitForFunction( () => window.__commerce.game?.target && !window.__commerce.game.targetPending );
		};
		let panelAudit = process.env.SRO_PANEL_AUDIT === "1";
		const selectMerchant = async () => {
			await selectNpc();
			if ( panelAudit ) {
				panelAudit = false;
				phase( "npc-panel-audit" );
				await capture( "npc-menu", "npc-talk" );
				await control( "npc-talk" ).click();
				await page.waitForFunction( () => window.__commerce.game?.npcConversation?.phase === "ready", null, {
					timeout: 15000
				} );
				await capture( "npc-dialogue", "npc-close" );
				const dialogue = await page.evaluate( () => window.__commerce.game.npcConversation.dialogue );
				if ( dialogue.kind === 4 && dialogue.options.length ) {
					const id = "npc-choice:" + dialogue.options[0].choice;
					for ( let i = 0; i < 80 && !await control( id ).count(); i++ ) {
						await control( "npc-scroll-down" ).click();
						await page.evaluate( () => new Promise( requestAnimationFrame ) );
					}
					await control( id ).click();
					await page.waitForFunction(
						() => window.__commerce.game?.npcConversation?.phase === "ready",
						null,
						{ timeout: 15000 }
					);
					await capture( "npc-quest-branch", "npc-close" );
				}
				await page.evaluate( () => {
					window.__commerce.panelAudit = {
						dialogue: window.__commerce.game.npcConversation,
						rectangles: [ ...document.querySelectorAll( "[data-ui-id]" ) ].filter( e =>
							e.getAttribute( "data-ui-id" ).startsWith( "npc-" )
						).map( e => ({
							id: e.getAttribute( "data-ui-id" ),
							label: e.textContent,
							rect: e.getBoundingClientRect().toJSON()
						}) )
					};
				} );
				await control( "npc-close" ).click();
				await page.waitForFunction(
					() =>
						!window.__commerce.game?.targetPending &&
						window.__commerce.game?.npcConversation?.phase === "closed",
					null,
					{ timeout: 10000 }
				);
				await selectNpc();
			}
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
		const budget = BigInt( await page.evaluate( () => window.__commerce.game.progression.gold ) );
		const bag = await page.evaluate( () => window.__commerce.game.inventory );
		const candidates = shop.offers.map( ( offer, index ) => ({ offer, index }) ).filter( ( { offer } ) =>
			BigInt( offer.price ) <= budget &&
			((offer.items[0].typeFlags & 0x60) !== 0x60 || !bag.some( item => item.refObjId === offer.refObjId ))
		).sort( ( a, b ) => BigInt( a.offer.price ) < BigInt( b.offer.price ) ? -1 : 1 );
		const index = candidates[0]?.index;
		assert.notEqual( index, undefined, "Scratch balance must cover an offer that creates a new slot" );
		phase( "purchase" );
		await control( "shop-tab:" + shop.offers[index].tab ).click();
		for ( let n = 0; n < Math.floor( shop.offers[index].slot / 30 ); n++ ) await control( "shop-next" ).click();
		await capture( "grid", "shop-tab:" + shop.offers[index].tab );
		await help( "shop-offer:" + index, "offer-tooltip" );
		await control( "shop-offer:" + index ).click();
		await capture( "purchase", "shop-trade" );
		const before = await page.evaluate( () => window.__commerce.game.inventory );
		await control( "shop-trade" ).click();
		await page.waitForFunction(
			count => !window.__commerce.game.inventoryPending && window.__commerce.game.inventory.length > count,
			before.length
		);
		const bought = await page.evaluate(
			slots => window.__commerce.game.inventory.find( i => !slots.includes( i.slot ) ),
			before.map( i => i.slot )
		);
		assert.ok( bought );
		assert.equal( bought.refObjId, shop.offers[index].refObjId );
		phase( "sale-quote" );
		await control( "slot:" + bought.slot ).dblclick();
		await page.waitForFunction(
			() => !window.__commerce.game.inventoryPending && window.__commerce.game.shop.saleQuotes?.length > 0,
			null,
			{ timeout: 10000 }
		);
		await capture( "sale", "shop-trade" );
		await control( "shop-trade" ).click();
		await page.waitForFunction(
			id =>
				!window.__commerce.game.inventoryPending &&
				window.__commerce.game.shop?.buyback?.some( e => e.refObjId === id ),
			bought.refObjId
		);
		const sold = await page.evaluate(
			ids => window.__commerce.game.shop.buyback.find( e => !ids.includes( e.id ) ),
			(shop.buyback ?? []).map( e => e.id )
		);
		await page.evaluate( () =>
			window.__commerceWorker.postMessage( { kind: "session", command: { kind: "disconnect" } } )
		);
		await page.waitForFunction( () => window.__commerce.session.phase === "disconnected" );
		await page.evaluate( () =>
			window.__commerceWorker.postMessage( { kind: "session", command: { kind: "reconnect" } } )
		);
		await page.waitForFunction( () => window.__commerce.session.phase === "world", null, { timeout: 30000 } );
		await page.waitForFunction( () => window.__commerce.readyCount === 2, null, { timeout: 30000 } );
		phase( "reopened-merchant" );
		await selectMerchant();
		const buybackIndex = await page.evaluate(
			id => window.__commerce.game.shop.buyback.findIndex( e => e.id === id ),
			sold.id
		);
		assert.ok( buybackIndex >= 0, "Ledger must survive authenticated reconnect" );
		phase( "buyback" );
		const retained = await page.evaluate( index => window.__commerce.game.shop.buyback[index].item, buybackIndex );
		assert.equal( retained.refObjId, bought.refObjId );
		assert.equal( retained.plus, bought.plus );
		assert.deepEqual( retained.magic, bought.magic );
		await help( "shop-buyback:" + buybackIndex, "buyback-tooltip" );
		await control( "shop-buyback:" + buybackIndex ).click();
		await capture( "buyback", "shop-trade" );
		await control( "shop-trade" ).click();
		await page.waitForFunction(
			id =>
				!window.__commerce.game.inventoryPending &&
				!window.__commerce.game.shop.buyback.some( e => e.id === id ),
			sold.id
		);
		const restored = await page.evaluate(
			slots => window.__commerce.game.inventory.find( i => !slots.includes( i.slot ) ),
			before.map( i => i.slot )
		);
		assert.deepEqual( { ...restored, slot: 0 }, { ...bought, slot: 0 } );
		await page.evaluate( ( { sold, bought } ) => {
			window.__commerce.acceptance = { purchase: bought, sale: sold, buybackRestored: true, reconnected: true };
		}, { sold, bought } );
		assert.deepEqual( errors, [] );
	} finally {
		await writeFile(
			"temp/artifacts/live-commerce/session.json",
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
		await page.screenshot( { path: "temp/artifacts/live-commerce/mission.png" } ).catch( () => {} );
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
				id: "restore-live-commerce-origin",
				movementMode: 3,
				start: original,
				startYawRadians: original.angle / 65535 * Math.PI * 2
			}
		} );
	}
} );
