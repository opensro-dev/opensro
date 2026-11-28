/*
===========================================================================

skill-combat-live.test.mjs - a hotbar skill's motion, hit effects and fatal presentation, live

Casts from the hotbar on a local stack and checks the native motion, the hit
effects and the deferred fatal presentation from the published actors.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";

test(
	"live hotbar skill drives native motion, hit effects and deferred fatal presentation",
	{ timeout: 90000 },
	async () => {
		const { browser, page } = await launchProbeBrowser(),
			directory = "temp/artifacts/skill-combat-live",
			errors = [];
		await mkdir( directory, { recursive: true } );
		page.on( "pageerror", e => errors.push( String( e ) ) );
		let evidence = {}, capture;
		try {
			await page.route( "**/src/engine/runtime/renderer/renderer.ts", async route => {
				const response = await route.fetch(), source = await response.text();
				assert.ok( source.includes( "export function createRenderer(" ) );
				const body = source.replace( "export function createRenderer(", "function createObservedRenderer(" ) +
					`\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);globalThis.__castRenderer=owner;return {...owner,setCharacterActors(rows,portraits,clocks){globalThis.__castActors=rows.map(a=>({...a,...clocks?.get(a)}));owner.setCharacterActors(rows,portraits,clocks)}};}`;
				await route.fulfill( { response, body, contentType: "application/javascript" } );
			} );
			// Existing combat scratch character; protected user characters are rejected by boot.
			await bootPlayableSession( page, process.env.SRO_PROBE_CHARACTER ?? "asd3" );
			await page.waitForFunction( () =>
				__playableRuntime.gameplay()?.skillCatalog?.length && __castActors?.length
			);
			await page.waitForFunction(
				() => /UI: 0 pending images; 0 failed images/.test( document.querySelector( "output" )?.textContent ),
				null,
				{ timeout: 30000 }
			);
			const initial = await page.evaluate( () => {
				const g = __playableRuntime.gameplay();
				return {
					gid: g.localGid,
					skills: g.skills,
					bindings: g.quickSlots,
					weapon: g.inventory.find( i => i.slot === 6 )
				};
			} );
			evidence.initial = initial;
			assert.ok( initial.skills.includes( 3 ), "combat scratch character must legitimately know Strike n smash" );
			assert.ok( initial.weapon, "combat scratch character must have a weapon" );
			let target;
			for ( let attempt = 0; attempt < 6 && !target; attempt++ ) {
				target = await page.evaluate( () => {
					const local = __playableRuntime.gameplay().localGid;
					for ( let y = .15; y < .7; y += .02 ) {
						for ( let x = .02; x < .98; x += .02 ) {
							if (
								document.elementFromPoint( x * innerWidth, y * innerHeight )?.closest( "[data-ui-id]" )
							) continue;
							const gid = __castRenderer.pickEntity( x, y, local ),
								entity = gid && __playableRuntime.entity( gid );
							if (
								entity?.kind === "monster" && entity.name === "Mangyang" &&
								entity.appearanceState?.[0] !== 2 && __castActors.some( a => a.gid === gid )
							) return { gid, x, y };
						}
					}
					return null;
				} );
				if ( !target ) {
					await page.mouse.move( 500, 350 );
					await page.mouse.down( { button: "right" } );
					await page.mouse.move( 660, 350, { steps: 8 } );
					await page.mouse.up( { button: "right" } );
					await page.waitForTimeout( 200 );
				}
			}
			assert.ok( target, "in-viewport, unoccluded, model-loaded Mangyang is required" );
			evidence.target = target;
			await page.evaluate( () =>
				__playableRuntime.session( {
					kind: "gameplay",
					command: { kind: "quickslot-set", binding: { slot: 1, kind: 0x49, payload: 3 } }
				} )
			);
			const viewport = page.viewportSize();
			await page.mouse.click( target.x * viewport.width, target.y * viewport.height );
			await page.waitForFunction( gid => {
				const g = __playableRuntime.gameplay();
				return g.target === gid && !g.targetPending &&
					g.quickSlots.some( s => s.slot === 1 && s.payload === 3 );
			}, target.gid );
			await page.screenshot( { path: directory + "/selected.png" } );
			capture = page.evaluate( async gid => {
				const rows = [];
				for ( let i = 0; i < 140; i++ ) {
					const g = __playableRuntime.gameplay();
					rows.push( {
						at: performance.now(),
						casts: g.casts,
						actor: __castActors.find( a => a.gid === g.localGid ),
						victim: __castActors.find( a => a.gid === gid ),
						effects: __castActors.filter( a => a.gid < 0 ).map( a => ({
							gid: a.gid,
							model: a.model,
							opacity: a.opacity,
							time: a.time,
							clip: a.clip
						}) ),
						status: document.querySelector( "output" ).textContent
					} );
					await new Promise( r => setTimeout( r, 100 ) );
				}
				return rows;
			}, target.gid );
			void capture.catch( () => {} ); // A failed assertion must not leak a page-close rejection.
			await page.keyboard.press( "Digit1" );
			await page.waitForFunction(
				() => {
					const gid = __playableRuntime.gameplay().localGid;
					return __castActors.find( a => a.gid === gid )?.layers?.some( l => l.clip === "native:sword:26" );
				},
				null,
				{ timeout: 12000 }
			);
			await page.screenshot( { path: directory + "/swing.png" } );
			const rows = await capture;
			evidence.rows = rows;
			const accepted = rows.flatMap( r => r.casts ).find( c =>
				c.skill === 3 && c.caster === initial.gid && c.target === target.gid
			);
			assert.ok( accepted, "real server accepts the selected skill and target" );
			assert.ok(
				rows.some( r => r.effects.some( e => e.model.includes( "hit_1_cut_smash.efp" ) ) ),
				"authored animation callback emits slash geometry"
			);
			assert.ok(
				rows.some( r => r.effects.some( e => e.model.includes( "hit_1_cut_critical.efp" ) ) ),
				"authored impact callback emits target VFX"
			);
			if ( accepted.fatal ) {
				assert.ok(
					rows.some( r =>
						r.casts.some( c => c.token === accepted.token ) && r.victim && !r.victim.pickable &&
						!r.victim.layers?.some( l => /death/.test( l.clip ) ) && !/death/.test( r.victim.clip )
					),
					"authority retires picking before the authored fatal hit"
				);
				assert.ok(
					rows.some( r =>
						r.victim &&
						(/death/.test( r.victim.clip ) || r.victim.layers?.some( l => /death/.test( l.clip ) ))
					),
					"fatal hit eventually plays death"
				);
			}
			assert.ok(
				rows.some( r => r.casts.some( c => c.token === accepted.token && c.cancelledAtMs !== undefined ) ),
				"server closes the same cast token"
			);
			assert.deepEqual( errors, [] );
		} catch ( error ) {
			if ( capture ) evidence.rows = await capture.catch( () => [] );
			evidence.failure = String( error );
			evidence.state = await page.evaluate( () => ({
				game: globalThis.__playableRuntime?.gameplay(),
				status: document.querySelector( "output" )?.textContent
			}) ).catch( () => null );
			throw error;
		} finally {
			await writeFile( directory + "/incident.json", JSON.stringify( { ...evidence, errors }, null, 2 ) );
			await browser.close();
			await capture?.catch( () => {} );
		}
	}
);
