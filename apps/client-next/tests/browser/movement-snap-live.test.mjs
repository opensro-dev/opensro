/*
===========================================================================

movement-snap-live.test.mjs - double-click approach and retaliation movement, live

Captures the local player's approach to a double-clicked monster and the
monster's retaliation movement from the published actors.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
test( "capture double-click approach and monster retaliation movement", { timeout: 150000 }, async () => {
	const dir = "apps/client-next/temp/artifacts/movement-snap";
	await mkdir( dir, { recursive: true } );
	const { browser, page } = await launchProbeBrowser();
	const errors = [];
	let tracing = false, report = { verdict: "COLLECTING" };
	page.on( "pageerror", e => errors.push( e.message ) );
	try {
		await page.addInitScript( () => {
			window.__snap = { packets: [], journal: [], samples: [] };
			const Base = Worker;
			window.Worker = class extends Base {
				constructor( ...a ) {
					super( ...a );
					this.addEventListener( "message", ( { data: d } ) => {
						if ( d.kind === "snap-packet" && __snap.packets.length < 5000 ) __snap.packets.push( d );
						if ( d.kind === "world" && d.batch && __snap.journal.length < 8000 ) {
							for ( const e of d.batch.events ) {
								if ( e.kind === "state" || e.kind === "gameplay" ) {
									__snap.journal.push( {
										at: performance.now(),
										kind: e.kind,
										entity: e.entity,
										game: e.kind === "gameplay" ?
											{
												pose: e.state.pose,
												moving: e.state.moving,
												localGid: e.state.localGid,
												casts: e.state.casts
											} :
											undefined
									} );
								}
							}
						}
					} );
				}
			};
		} );
		await page.route( "**/session/world/core.ts*", async route => {
			const response = await route.fetch(),
				body = await response.text(),
				needle = "function receive(frame, now) {";
			assert.ok( body.includes( needle ) );
			await route.fulfill( {
				response,
				body: body.replace(
					needle,
					needle +
						' if([0xb738,0xb2f5,0x3122,0xb245,0x30e3].includes(frame.opcode)) globalThis.postMessage({kind:"snap-packet",now,opcode:frame.opcode,payload:Array.from(frame.payload),localGid:gameplay.localIdentity(),before:entities.read(new DataView(frame.payload.buffer,frame.payload.byteOffset,frame.payload.byteLength).getUint32(0,true))});'
				)
			} );
		} );
		await page.route( "**/runtime/renderer/renderer.ts*", async route => {
			const response = await route.fetch(), body = await response.text();
			assert.ok( body.includes( "export function createRenderer(" ) );
			await route.fulfill( {
				response,
				body: body.replace( "export function createRenderer(", "function createObservedRenderer(" ) +
					"\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);globalThis.__snapRenderer=owner;return {...owner,setCharacterActors(actors,portraits,clocks){globalThis.__snapActors=actors;owner.setCharacterActors(actors,portraits,clocks)}};}"
			} );
		} );
		await bootPlayableSession( page, "asd3" );
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		tracing = true;
		await page.waitForFunction( () => __snapActors?.length > 0 );
		let target;
		for ( let attempt = 0; attempt < 8 && !target; attempt++ ) {
			target = await page.evaluate( () => {
				const local = __playableRuntime.gameplay().localGid;
				for ( let y = .15; y < .7; y += .02 ) {
					for ( let x = .02; x < .98; x += .02 ) {
						if (
							document.elementFromPoint( x * innerWidth, y * innerHeight )?.closest( "[data-ui-id]" )
						) continue;
						const gid = __snapRenderer.pickEntity( x, y, local ),
							e = gid && __playableRuntime.entity( gid );
						if (
							e?.kind === "monster" && e.name === "Mangyang" && e.appearanceState?.[0] !== 2 &&
							__snapActors.some( a => a.gid === gid )
						) return { gid, x, y, entity: e };
					}
				}
				return null;
			} );
			if ( !target ) {
				await page.mouse.move( 500, 350 );
				await page.mouse.down( { button: "right" } );
				await page.mouse.move( 650, 350, { steps: 8 } );
				await page.mouse.up( { button: "right" } );
				await page.waitForTimeout( 200 );
			}
		}
		assert.ok( target, "visible, model-loaded Mangyang required" );
		report.target = target;
		report.initial = await page.evaluate( () => __playableRuntime.gameplay() );
		await page.evaluate( gid => {
			__snap.samples = [];
			__snap.packets = [];
			__snap.journal = [];
			const end = performance.now() + 18000;
			const tick = () => {
				const g = __playableRuntime.gameplay();
				__snap.samples.push( {
					at: performance.now(),
					local: g.pose,
					target: __playableRuntime.entity( gid ),
					moving: g.moving,
					actors: __snapActors.filter( a => a.gid === gid || a.gid === g.localGid ).map( a => ({
						gid: a.gid,
						position: a.position,
						transform: a.transform,
						clip: a.clip
					}) )
				} );
				if ( performance.now() < end ) requestAnimationFrame( tick );
			};
			tick();
		}, target.gid );
		const viewport = page.viewportSize();
		await page.mouse.dblclick( target.x * viewport.width, target.y * viewport.height, { delay: 100 } );
		await page.waitForTimeout( 7000 );
		await page.screenshot( { path: dir + "/approach.png" } );
		await page.mouse.click( 220, 550 );
		await page.waitForTimeout( 10000 );
		await page.screenshot( { path: dir + "/chase.png" } );
		report = { ...report, verdict: "CAPTURED", errors, ...await page.evaluate( () => __snap ) };
	} catch ( error ) {
		report = {
			...report,
			verdict: "FAILED",
			error: String( error ),
			errors,
			state: await page.evaluate( () => ({
				session: window.__playableRuntime?.sessionState(),
				output: document.querySelector( "output" )?.textContent
			}) )
		};
		throw error;
	} finally {
		await writeFile( dir + "/incident.json", JSON.stringify( report, null, 2 ) );
		try {
			if ( tracing ) await page.context().tracing.stop( { path: dir + "/trace.zip" } );
		} finally {
			await browser.close();
		}
	}
} );
