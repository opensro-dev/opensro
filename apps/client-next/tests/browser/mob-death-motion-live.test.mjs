/*
===========================================================================

mob-death-motion-live.test.mjs - a moving monster's death retires its travel first, live

Samples the dying monster's published actor each frame: the corpse
presentation starts only after its travel has stopped.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { resetMissionMovementFixture } from "../../../../scripts/lib/missionMovementFixture.mjs";
import { bootPlayableSession } from "./helpers/playable-session.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
import { installPursuitRecorder } from "./helpers/pursuit-recorder.mjs";

test( "moving monster death retires travel before corpse presentation", {
	skip: process.env.SRO_MOB_DEATH_LIVE !== "1",
	timeout: 180000
}, async () => {
	const mode = process.env.SRO_MOB_DEATH_CAPTURE ?? "after", out = "temp/artifacts/mob-death-motion/" + mode;
	await mkdir( out, { recursive: true } );
	const character = process.env.SRO_PROBE_CHARACTER ?? "asd3", report = { fights: [], errors: [] };
	await resetMissionMovementFixture( {
		characterName: character,
		fixture: {
			id: "mob-death-motion-field",
			movementMode: 3,
			start: { regionId: 0x62a6, x: 863, y: 20, z: 1746 },
			startYawRadians: 0
		}
	} );
	const { browser, page } = await launchProbeBrowser();
	page.on( "pageerror", e => report.errors.push( String( e ) ) );
	try {
		await holdProbeRuntime( page );
		await installPursuitRecorder( page, [ 0x33a6 ] );
		await page.route( "**/runtime/renderer/renderer.ts*", async route => {
			const response = await route.fetch(), body = await response.text();
			assert.ok( body.includes( "export function createRenderer(" ) );
			await route.fulfill( {
				response,
				body: body.replace( "export function createRenderer(", "function observedRenderer(" ) +
					`\nexport function createRenderer(...args){const r=observedRenderer(...args);globalThis.__deathRenderer=r;globalThis.__deathSamples=[];return {...r,setCharacterActors(actors,portraits,clocks){globalThis.__deathActors=actors;const gid=globalThis.__deathTarget,shown=actors.find(a=>a.gid===gid),actor=shown&&{...shown,...clocks?.get(shown)};if(gid&&__deathSamples.length<3000)__deathSamples.push({at:performance.now(),entity:globalThis.__playableRuntime?.entity(gid),actor:actor?{pose:actor.pose,clip:actor.clip,time:actor.time}:null});return r.setCharacterActors(actors,portraits,clocks)}}}`
			} );
		} );
		console.log( "[mob-death] authenticated boot" );
		await bootPlayableSession( page, character );
		await page.context().tracing.start( { screenshots: true, snapshots: true } );
		await page.waitForFunction( () => __deathActors?.length > 0 );
		await new Promise( r => setTimeout( r, 1500 ) );
		for ( let fight = 0; fight < 5; fight++ ) {
			let target = null;
			for ( let turn = 0; turn < 10 && !target; turn++ ) {
				target = await page.evaluate( () => {
					const g = __playableRuntime.gameplay();
					let candidate;
					for ( let y = .2; y < .76; y += .025 ) {
						for ( let x = .03; x < .97; x += .025 ) {
							if (
								document.elementFromPoint( x * innerWidth, y * innerHeight )?.closest( "[data-ui-id]" )
							) continue;
							const gid = __deathRenderer.pickEntity( x, y, g.localGid ),
								e = gid && __playableRuntime.entity( gid );
							if (
								e?.kind === "monster" && e.name === "Mangyang" && e.appearanceState?.[0] !== 2 &&
								__deathActors.some( a => a.gid === gid )
							) {
								candidate = { gid, x, y, moving: e.moving };
								if ( e.moving ) return candidate;
							}
						}
					}
					return candidate;
				} );
				if ( !target ) {
					await page.mouse.move( 500, 350 );
					await page.mouse.down( { button: "right" } );
					await page.mouse.move( 640, 350, { steps: 5 } );
					await page.mouse.up( { button: "right" } );
					await new Promise( r => setTimeout( r, 150 ) );
				}
			}
			assert.ok( target, "visible model-loaded Mangyang required" );
			console.log( "[mob-death] target", target.gid, "moving", target.moving );
			await page.evaluate( gid => {
				globalThis.__deathTarget = gid;
				globalThis.__deathSamples = [];
				__playableRuntime.session( { kind: "gameplay", command: { kind: "attack", gid } } );
			}, target.gid );
			await page.waitForFunction(
				() => __deathSamples.some( s => s.actor && /die|death/i.test( s.actor.clip ) ),
				null,
				{ timeout: 20000 }
			);
			await page.screenshot( { path: out + "/death-" + fight + ".png" } );
			await new Promise( r => setTimeout( r, 1000 ) );
			const capture = await page.evaluate( () => ({
				samples: __deathSamples,
				events: __pursuit.events.filter( e => e.kind === "wire" ),
				segments: __pursuit.segments
			}) );
			capture.target = target;
			const death = capture.samples.filter( s => s.actor && /die|death/i.test( s.actor.clip ) );
			assert.ok( death.length > 1 );
			const p = death[0].actor.pose;
			capture.deathDrift = Math.max(
				...death.map( s => Math.hypot( s.actor.pose.x - p.x, s.actor.pose.y - p.y, s.actor.pose.z - p.z ) )
			);
			capture.movingAtDeath = death.some( s => s.entity?.moving );
			capture.wasMoving = capture.samples.some( s => s.entity?.moving );
			report.fights.push( capture );
			if ( mode === "before" && capture.movingAtDeath && capture.deathDrift > 1 ) {
				report.verdict = "REPRODUCED";
				break;
			}
			if ( mode !== "before" ) {
				assert.equal( capture.movingAtDeath, false, "worker still publishes moving corpse" );
				assert.ok( capture.deathDrift < .001, "rendered death travelled " + capture.deathDrift );
				if ( capture.wasMoving ) {
					report.verdict = "PASS SUCCESS";
					break;
				}
			}
		}
		assert.equal( report.verdict, mode === "before" ? "REPRODUCED" : "PASS SUCCESS" );
		assert.deepEqual( report.errors, [] );
		console.log(
			"[mob-death]",
			report.verdict,
			report.fights.map( f => ({ gid: f.target.gid, drift: f.deathDrift, moving: f.movingAtDeath }) )
		);
	} catch ( e ) {
		report.error = String( e );
		throw e;
	} finally {
		await page.context().tracing.stop( { path: out + "/trace.zip" } ).catch( () => {} );
		await page.screenshot( { path: out + "/final.png" } ).catch( () => {} );
		await writeFile( out + "/incident.json", JSON.stringify( report, null, 2 ) );
		await browser.close();
	}
} );
