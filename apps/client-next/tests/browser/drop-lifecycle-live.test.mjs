/*
===========================================================================

drop-lifecycle-live.test.mjs - a removed monster's fading actor beside scenery and drops, live

Kills a monster on a local stack and checks that its fading death copy is
published with the scenery and the drop that replace it.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { mkdir, writeFile } from "node:fs/promises";
test(
	"live monster removal preserves its fading actor alongside scenery and drop presentation",
	{ timeout: 90000 },
	async () => {
		const dir = "temp/artifacts/drop-lifecycle-live";
		await mkdir( dir, { recursive: true } );
		const { browser, page } = await launchProbeBrowser();
		const errors = [], evidence = {};
		let capture;
		page.on( "pageerror", e => errors.push( String( e ) ) );
		try {
			await page.route( "**/src/engine/runtime/renderer/renderer.ts", async route => {
				const response = await route.fetch(), source = await response.text();
				await route.fulfill( {
					response,
					body: source.replace( "export function createRenderer(", "function observedRenderer(" ) +
						`\nexport function createRenderer(...args){const owner=observedRenderer(...args);globalThis.__dropRenderer=owner;return {...owner,setCharacterActors(actors,portraits,clocks){globalThis.__dropActors=actors;owner.setCharacterActors(actors,portraits,clocks)}}}`,
					contentType: "application/javascript"
				} );
			} );
			await bootPlayableSession( page, "asd3" );
			console.log( "world" );
			await page.waitForFunction( () => globalThis.__dropActors?.length );
			evidence.initial = await page.evaluate( () => ({
				game: __playableRuntime.gameplay(),
				actors: __dropActors
			}) );
			let target;
			for ( let attempt = 0; attempt < 6 && !target; attempt++ ) {
				target = await page.evaluate( () => {
					const local = __playableRuntime.gameplay().localGid;
					for ( let y = .15; y < .7; y += .03 ) {
						for ( let x = .02; x < .98; x += .03 ) {
							const gid = __dropRenderer.pickEntity( x, y, local ),
								e = gid && __playableRuntime.entity( gid );
							if (
								e?.kind === "monster" && e.name === "Mangyang" && e.appearanceState?.[0] !== 2
							) return { gid, entity: e };
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
			if ( !target ) throw Error( "No visible Mangyang" );
			evidence.target = target;
			console.log( "target", target.gid );
			await page.evaluate( gid => {
				__playableRuntime.session( { kind: "gameplay", command: { kind: "select", gid } } );
			}, target.gid );
			await page.waitForFunction(
				gid => __playableRuntime.gameplay().target === gid && !__playableRuntime.gameplay().targetPending,
				target.gid
			);
			capture = page.evaluate( async gid => {
				const rows = [];
				for ( let i = 0; i < 240; i++ ) {
					const g = __playableRuntime.gameplay();
					rows.push( {
						at: performance.now(),
						pose: g.pose,
						casts: g.casts,
						target: __playableRuntime.entity( gid ),
						actors: __dropActors.filter( a => a.gid === gid || a.gid < 0 || a.groundItem ),
						stats: __dropRenderer.characterStats(),
						feedback: g.feedback
					} );
					await new Promise( r => setTimeout( r, 100 ) );
				}
				return rows;
			}, target.gid );
			void capture.catch( () => {} );
			await page.evaluate(
				gid => __playableRuntime.session( { kind: "gameplay", command: { kind: "attack", gid } } ),
				target.gid
			);
			await page.waitForFunction(
				gid => __playableRuntime.entity( gid )?.appearanceState?.[0] === 2,
				target.gid,
				{ timeout: 15000 }
			);
			await page.waitForFunction(
				() => __dropActors.some( a => a.gid < 0 && /death/i.test( a.clip ) && (a.opacity ?? 1) < 1 ),
				null,
				{ timeout: 8000 }
			);
			await page.screenshot( { path: dir + "/fade-start.png" } );
			await page.waitForTimeout( 700 );
			await page.screenshot( { path: dir + "/fade-middle.png" } );
			evidence.rows = await capture;
			await page.keyboard.down( "KeyZ" );
			await page.waitForTimeout( 100 );
			await page.screenshot( { path: dir + "/end.png" } );
			await page.keyboard.up( "KeyZ" );
			evidence.end = await page.evaluate( () => __playableRuntime.gameplay() );
			console.log( "captured", evidence.rows.length );
			const dead = evidence.rows.find( r => r.target?.appearanceState?.[0] === 2 );
			assert.ok( dead, "server must confirm death" );
			const final = evidence.rows.filter( r => r.actors.some( a => a.gid === target.gid ) ).at( -1 ).actors.find(
				a => a.gid === target.gid
			);
			const corpse = evidence.rows.flatMap( r => r.actors ).find( a =>
				a.gid < 0 && a.model === final.model && a.pose.x === final.pose.x && a.pose.z === final.pose.z &&
				/death/i.test( a.clip )
			);
			assert.ok( corpse, "retirement must preserve the corpse" );
			const fade = evidence.rows.flatMap( r => r.actors.filter( a => a.gid === corpse.gid ) );
			assert.ok(
				fade.some( a => a.opacity > .8 ) && fade.some( a => a.opacity < .2 ),
				"fade must progress, not disappear at first update"
			);
			assert.ok(
				evidence.rows.some( r =>
					r.actors.some( a => a.gid === corpse.gid ) && r.actors.some( a => a.clip === "effect" && a.loop )
				),
				"corpse and looping scenery coexist"
			);
			assert.ok( !evidence.rows.at( -1 ).actors.some( a => a.gid === corpse.gid ), "retired corpse expires" );
			assert.deepEqual( errors, [] );
		} catch ( e ) {
			if ( capture ) evidence.rows = await capture.catch( () => [] );
			evidence.failure = String( e );
			throw e;
		} finally {
			await writeFile( dir + "/incident.json", JSON.stringify( { ...evidence, errors }, null, 2 ) );
			await browser.close();
		}
	}
);
