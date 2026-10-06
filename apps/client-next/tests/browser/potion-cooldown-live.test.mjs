import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "MP potion acknowledgement owns quickslot cooldown and repeat-use suppression", {
	skip: process.env.SRO_POTION_LIVE !== "1",
	timeout: 150000
}, async () => {
	const inventoryProbe = process.env.SRO_POTION_INVENTORY === "1",
		mode = process.env.SRO_POTION_CAPTURE_ONLY === "1" ? "before" : "after",
		directory = "temp/artifacts/" + (inventoryProbe ? "potion-inventory-cooldown" : "potion-cooldown") + "/" + mode;
	await mkdir( directory, { recursive: true } );
	const { browser, page } = await launchProbeBrowser(), report = { wire: [], errors: [], samples: [] };
	page.on( "pageerror", e => report.errors.push( String( e ) ) );
	page.on( "console", m => {
		if ( m.text().startsWith( "[potion-wire]" ) ) report.wire.push( JSON.parse( m.text().slice( 13 ) ) );
	} );
	try {
		await holdProbeRuntime( page );
		const position = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/npcpos.json", "utf8" ) ).rows
			.map( r => r.split( "\t" ).map( Number ) ).find( r => r[0] === 2005 );
		assert.ok( position );
		await resetMissionMovementFixture( {
			characterName: process.env.SRO_PROBE_CHARACTER ?? "asd3",
			fixture: {
				id: "mp-potion-cooldown",
				movementMode: 3,
				start: { regionId: position[1], x: position[2] + 15, y: position[3], z: position[4] },
				startYawRadians: 0
			}
		} );
		await page.route( "**/runtime/simulation/worker/network/network.ts*", async route => {
			const response = await route.fetch();
			let body = await response.text();
			const inbound = "const frame = codec.decode(new Uint8Array(event.data));",
				outbound = "socket.send(encoded);";
			assert.ok( body.includes( inbound ) && body.includes( outbound ) );
			const observe = direction =>
				`if([0x75bd,0xb5bd,0xb245,0x303f].includes(frame.opcode))console.log('[potion-wire]'+JSON.stringify({direction:'${direction}',at:performance.now(),opcode:frame.opcode,payload:Array.from(frame.payload)}));`;
			await route.fulfill( {
				response,
				body: body.replace( inbound, inbound + observe( "in" ) ).replace(
					outbound,
					outbound + observe( "out" )
				)
			} );
		} );
		await page.route( "**/runtime/ui/ui.ts*", async route => {
			const response = await route.fetch(), body = await response.text();
			assert.ok( body.includes( "export function createUi(" ) );
			await route.fulfill( {
				response,
				body: body.replace( "export function createUi(", "function observedUi(" ) +
					`\nexport function createUi(...args){globalThis.__potionUi={textures:[]};const texture=args[3];args[3]=(id,image)=>{if(image)globalThis.__potionUi.textures.push(id);texture(id,image)};const publish=args[2];args[2]=scene=>{globalThis.__potionUi.scene=scene;publish(scene)};const owner=observedUi(...args);return {...owner,step(view,now){globalThis.__potionUi.view=view;return owner.step(view,now)}};}`
			} );
		} );
		console.log( "[potion] authenticated boot" );
		await bootPlayableSession( page, process.env.SRO_PROBE_CHARACTER ?? "asd3" );
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		report.originalAutoPotion = await page.evaluate( () => __playableRuntime.gameplay().autoPotion );
		await page.evaluate(
			settings =>
				__playableRuntime.session( {
					kind: "gameplay",
					command: {
						kind: "auto-potion-save",
						settings: {
							...settings,
							hp: settings.hp & 0x7fff,
							mp: settings.mp & 0x7fff,
							cure: settings.cure & 0x7fff
						}
					}
				} ),
			report.originalAutoPotion
		);
		await page.waitForFunction( () => __potionUi.view?.entities.some( e => e.refObjId === 2005 ) );
		if (
			!await page.evaluate( () =>
				__playableRuntime.gameplay().inventory.some( i => i.refObjId === 12 && i.quantity >= 3 )
			)
		) {
			await page.evaluate( () =>
				__playableRuntime.session( {
					kind: "gameplay",
					command: { kind: "select", gid: __potionUi.view.entities.find( e => e.refObjId === 2005 ).gid }
				} )
			);
			await page.locator( '[data-ui-id="shop-open"]' ).click();
			await page.waitForFunction( () => __playableRuntime.gameplay().shop?.offers.length > 0 );
			const offer = await page.evaluate( () =>
				__playableRuntime.gameplay().shop.offers.find( o => o.refObjId === 12 )
			);
			assert.ok( offer, "merchant MP small" );
			await page.evaluate(
				o => __playableRuntime.session( {
					kind: "gameplay",
					command: { kind: "shop-buy", slot: o.slot, tab: o.tab, quantity: 5 }
				} ),
				offer
			);
			await page.waitForFunction( () =>
				!__playableRuntime.gameplay().inventoryPending &&
				__playableRuntime.gameplay().inventory.some( i => i.refObjId === 12 && i.quantity >= 3 )
			);
			await page.evaluate( () =>
				__playableRuntime.session( { kind: "gameplay", command: { kind: "npc-close" } } )
			);
		}
		report.initial = await page.evaluate( () => {
			const g = __playableRuntime.gameplay();
			return {
				character: __playableRuntime.sessionState().character,
				gm: g.eligibility?.gm,
				inventory: g.inventory.map( ( { slot, name, quantity, refObjId, typeFlags } ) => ({
					slot,
					name,
					quantity,
					refObjId,
					typeFlags
				}) ),
				skills: g.skillCatalog.filter( s => g.skills.includes( s.id ) ),
				vitals: g.vitals,
				progression: g.progression
			};
		} );
		report.originalBinding = await page.evaluate( () =>
			__playableRuntime.gameplay().quickSlots?.find( r => r.slot === 1 ) ?? { slot: 1, kind: 0, payload: 0 }
		);
		const potion = report.initial.inventory.find( r => r.refObjId === 12 );
		assert.ok( potion && potion.quantity >= 2, "scratch MP small potion stack required" );
		await page.evaluate(
			slot =>
				__playableRuntime.session( {
					kind: "gameplay",
					command: { kind: "quickslot-set", binding: { slot: 1, kind: 0x46, payload: slot - 13 } }
				} ),
			potion.slot
		);
		await page.waitForFunction( () =>
			__playableRuntime.gameplay().quickSlots?.some( r => r.slot === 1 && r.kind === 0x46 )
		);
		await page.waitForFunction(
			() =>
				__potionUi.textures.some( p => p.endsWith( "/skill_delay.png" ) ) &&
				__potionUi.textures.some( p => p.endsWith( "/cool_time_0.png" ) ),
			null,
			{ timeout: 20000 }
		);
		if ( inventoryProbe ) {
			await page.keyboard.press( "KeyI" );
			const cell = page.locator( '[data-ui-id="slot:' + potion.slot + '"]' );
			await cell.waitFor( { state: "visible" } );
			report.inventoryRect = await cell.boundingBox();
			await page.evaluate( r => globalThis.__potionInventoryRect = r, report.inventoryRect );
		}
		const snapshot = () =>
			page.evaluate( () => ({
				at: performance.now(),
				quantity: __playableRuntime.gameplay().inventory.find( r => r.refObjId === 12 )?.quantity ?? 0,
				timers: __playableRuntime.gameplay().itemCooldowns ?? [],
				quads: (__potionUi.scene?.quads ?? []).filter( q => q.texture?.endsWith( "/skill_delay.png" ) ),
				inventoryQuads: (__potionUi.scene?.quads ?? []).filter( q => {
					const r = globalThis.__potionInventoryRect;
					return r && q.texture?.includes( "skill_delay" ) && Math.abs( q.rect[0] - r.x ) < 1 &&
						Math.abs( q.rect[1] - r.y ) < 1;
				} ),
				pending: __playableRuntime.gameplay().inventoryPending,
				simulationTimeMs: __potionUi.view?.simulationTimeMs,
				textures: __potionUi.textures.filter( p => p.includes( "skill_delay" ) )
			}) );
		const fullMP = await page.evaluate( () => {
			const g = __playableRuntime.gameplay(), v = g.vitals.find( v => v.gid === g.localGid );
			return v && v.mp >= v.maxMp;
		} );
		if ( fullMP ) {
			assert.ok(
				report.initial.skills.some( s => s.id === 124 ),
				"scratch learned fire imbue required to spend MP"
			);
			await page.evaluate( () =>
				__playableRuntime.session( { kind: "gameplay", command: { kind: "skill", skillId: 124 } } )
			);
			await page.waitForFunction(
				() => {
					const g = __playableRuntime.gameplay(), v = g.vitals.find( v => v.gid === g.localGid );
					return v && v.mp < v.maxMp;
				},
				null,
				{ timeout: 10000 }
			);
		}
		report.samples.push( await snapshot() );
		await page.keyboard.press( "Digit1" );
		await page.waitForFunction(
			quantity =>
				(__playableRuntime.gameplay().inventory.find( r => r.refObjId === 12 )?.quantity ?? 0) === quantity - 1,
			potion.quantity,
			{ timeout: 10000 }
		);
		report.samples.push( await snapshot() );
		for ( let i = 0; i < 3; i++ ) {
			await page.keyboard.press( "Digit1" );
			await new Promise( r => setTimeout( r, 45 ) );
		}
		report.samples.push( await snapshot() );
		await page.screenshot( { path: directory + "/active.png" } );
		if ( mode === "after" ) {
			assert.ok( report.samples[1].timers.some( t => t.category === 2 ) );
			assert.ok( report.samples[2].quads.length > 0, "rendered potion cooldown atlas" );
			if ( inventoryProbe ) {
				assert.equal( report.samples[2].inventoryQuads.length, 1, "inventory cooldown sweep" );
				assert.deepEqual(
					report.samples[2].inventoryQuads[0].uv,
					report.samples[2].quads[0].uv,
					"inventory and quickslot synchronized"
				);
			}
			assert.equal(
				report.wire.filter( r => r.direction === "out" && r.opcode === 0x75bd ).length,
				1,
				"repeat use suppressed"
			);
		}
		await new Promise( r => setTimeout( r, 1200 ) );
		report.samples.push( await snapshot() );
		assert.equal( report.samples.at( -1 ).quantity, potion.quantity - 1, "only one potion consumed" );
		if ( inventoryProbe ) {
			assert.equal( report.samples.at( -1 ).inventoryQuads.length, 0, "inventory sweep expires" );
			if ( mode === "before" ) {
				assert.equal( report.samples[2].inventoryQuads.length, 0, "reproduce missing inventory sweep" );
			}
		}
		report.verdict = mode === "before" ? "BASELINE CAPTURED" : "PASS";
		console.log(
			"[potion]",
			report.verdict,
			JSON.stringify( {
				inventory: inventoryProbe,
				requests: report.wire.length,
				sweeps: report.samples.map( s => ({ all: s.quads.length, bag: s.inventoryQuads.length }) )
			} )
		);
	} catch ( e ) {
		report.error = String( e );
		throw e;
	} finally {
		await page.context().tracing.stop( { path: directory + "/trace.zip" } ).catch( () => {} );
		if ( report.originalAutoPotion ) {
			await page.evaluate(
				settings =>
					__playableRuntime.session( { kind: "gameplay", command: { kind: "auto-potion-save", settings } } ),
				report.originalAutoPotion
			).catch( () => {} );
		}
		if ( report.originalBinding ) {
			await page.evaluate(
				binding =>
					__playableRuntime.session( { kind: "gameplay", command: { kind: "quickslot-set", binding } } ),
				report.originalBinding
			).catch( () => {} );
		}
		await page.screenshot( { path: directory + "/screen.png" } ).catch( () => {} );
		await writeFile( directory + "/incident.json", JSON.stringify( report, null, 2 ) );
		await browser.close();
	}
} );
