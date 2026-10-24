import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials } from "../../../../scripts/lib/probeSession.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
import { bindPlayableRuntime } from "./helpers/playable-session.mjs";

test( "movement cadence captures live click reversal and authoritative receipts", { timeout: 150000 }, async () => {
	const character = assertCharacterAllowed( "asd2", { context: "client-next mission entry" } );
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		control = id => page.locator( `[data-ui-id="${id}"]` );
	await mkdir( "temp/artifacts/motion-cadence", { recursive: true } );
	try {
		await page.addInitScript( () => {
			const WorkerBase = window.Worker;
			window.__entry = {
				commands: [],
				phases: [],
				game: null,
				menus: [],
				samples: [],
				frames: [],
				timings: [],
				visual: [],
				receipts: [],
				cycles: []
			};
			const tick = at => {
				if ( window.__entry.samples.length ) window.__entry.frames.push( at );
				requestAnimationFrame( tick );
			};
			requestAnimationFrame( tick );
			window.addEventListener( "contextmenu", e => window.__entry.menus.push( e.defaultPrevented ) );
			window.Worker = class extends WorkerBase {
				postMessage( data, ...args ) {
					if ( data.kind === "session" && [ "gameplay", "world-ready" ].includes( data.command?.kind ) ) {
						window.__entry.commands.push(
							data.command.command?.kind === "navigation" ?
								{
									kind: "gameplay",
									command: { kind: "navigation", regionId: data.command.command.regionId }
								} :
								data.command
						);
					}
					return super.postMessage( data, ...args );
				}
				constructor( ...args ) {
					super( ...args );
					this.addEventListener( "message", ( { data } ) => {
						const r = window.__entry;
						if ( data.kind === "probe-timing" ) r.timings.push( data );
						if ( data.kind === "probe-receipt" ) r.receipts.push( data );
						if ( data.kind === "session" ) {
							r.session = data.state;
							if ( r.phases.at( -1 ) !== data.state.phase ) r.phases.push( data.state.phase );
						}
						if ( data.kind === "world" ) {
							for ( const e of data.batch.events ) {
								if ( e.kind === "gameplay" ) {
									r.game = { ...r.game, ...e.state };
									if ( e.state.pose ) {
										r.samples.push( {
											at: performance.now(),
											pose: e.state.pose,
											ack: e.state.acknowledgedMove
										} );
									}
								}
							}
						}
						if ( data.kind === "failure" ) r.failure = data.message;
					} );
				}
			};
		} );
		await page.route( "**/character/list", async route => {
			const response = await route.fetch(), body = await response.json();
			assert.ok(
				body.characters.some( row => row.name === character ),
				"scratch character must exist on the selected shard"
			);
			await route.fulfill( {
				response,
				json: { ...body, characters: body.characters.filter( row => row.name === character ) }
			} );
		} );
		await page.route( "**/simulation/worker/simulation.ts*", async route => {
			const response = await route.fetch(), body = await response.text();
			assert.ok( body.includes( "const sessionState = session.step(timeMs);" ) );
			await route.fulfill( {
				response,
				body: body.replace(
					"const sessionState = session.step(timeMs);",
					'const probeAt=performance.now(); const sessionState = session.step(timeMs); send({kind:"probe-timing",at:probeAt,elapsed:performance.now()-probeAt,timeMs},[]);'
				)
			} );
		} );
		await page.route( "**/characters/pose-presentation.ts*", async route => {
			const response = await route.fetch(), body = await response.text();
			// Record the local player's drawn pose where pose() hands the sampled
			// result back, once per frame. Two presenter generations spell that
			// return differently (sampledPose on main, ownWalks.pose with #1444's
			// interpolation), so the hook accepts either spelling and fails loudly
			// if both drift. The needles match the served (esbuild) text, which
			// has no inner-paren padding.
			const sampled = /return (sampledPose\(gid, input, target, now\));/,
				ownWalk = /return (ownWalks\.pose\(gid, input, target, \{[^}]+\}\));/;
			assert.ok( sampled.test( body ) || ownWalk.test( body ), "pose-presentation sampled return drifted" );
			const record = ( match, call ) =>
				`const __recorded=${call}; if(globalThis.__entry?.game?.localGid===gid)globalThis.__entry.visual.push({at:now,pose:__recorded,moving:input.moving}); return __recorded;`;
			await route.fulfill( { response, body: body.replace( sampled, record ).replace( ownWalk, record ) } );
		} );
		// The movement-receipt probe that used to hook "if (confirmedPrediction)
		// owner = predictedOwner;" is gone: the prediction rewrite removed that
		// line, and nothing in this file asserts on receipts. Re-add it against
		// the new receipt seam when the epic's budget part needs the stream.
		await page.route( "**/runtime/characters/actor-motion.ts*", async route => {
			const response = await route.fetch(),
				body = await response.text(),
				needle = "displayedDependencies.set(entity.gid, dependencies);";
			assert.ok( body.includes( needle ), "actor-motion dependency seam drifted" );
			await route.fulfill( {
				response,
				body: body.replace(
					needle,
					needle +
						" if(globalThis.__entry?.game?.localGid===entity.gid)globalThis.__entry.cycles.push({at:seconds,clip,time:seconds-state.started,moving:gameplay?.moving});"
				)
			} );
		} );
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL + "?diagnostics" );
		await control( "frontend:reveal" ).click( { timeout: 30000 } );
		await page.waitForFunction( () =>
			!document.querySelector( '[data-ui-id="login"]' )?.disabled &&
			document.querySelector( '[data-ui-id="login"]' )
		);
		await control( "native:servers" ).click();
		await control( "server:global-official" ).click();
		await control( "native:server-accept" ).click();
		const { loginId, loginPassword } = resolveProbeCredentials();
		await control( "account" ).fill( loginId );
		await control( "password" ).fill( loginPassword );
		await control( "password" ).press( "Enter" );
		await control( "frontend:create" ).waitFor( { timeout: 30000 } );
		await page.mouse.click( 505, 430 );
		await control( "enter" ).waitFor();
		await page.waitForFunction( () => !document.querySelector( '[data-ui-id="enter"]' )?.disabled );
		await page.screenshot( { path: "temp/artifacts/motion-cadence/dock.png" } );
		await control( "enter" ).click();
		await page.waitForFunction(
			() => /Frontend: loading-world/.test( document.querySelector( "output" )?.textContent ),
			null,
			{ timeout: 15000 }
		);
		const loading = await page.screenshot( { path: "temp/artifacts/motion-cadence/loading.png" } );
		const loadingMatch = await page.evaluate( async png => {
			const canvas = document.createElement( "canvas" );
			canvas.width = 1024;
			canvas.height = 768;
			const ctx = canvas.getContext( "2d" ), points = [ [ 800, 200 ], [ 400, 350 ], [ 900, 400 ], [ 600, 200 ] ];
			async function samples( url ) {
				const image = await createImageBitmap( await (await fetch( url )).blob() );
				ctx.clearRect( 0, 0, 1024, 768 );
				ctx.drawImage( image, 0, 0, 1024, 768 );
				image.close();
				return points.flatMap( ( [x, y] ) => [ ...ctx.getImageData( x, y, 1, 1 ).data ].slice( 0, 3 ) );
			}
			const actual = await samples( "data:image/png;base64," + png ), errors = [];
			for ( const variant of [ 1, 2 ] ) {
				const expected = await samples(
					"/assets/images/Media_extracted/interface/loading/loading_europe_" + variant + ".png"
				);
				errors.push( Math.max( ...actual.map( ( v, i ) => Math.abs( v - expected[i] ) ) ) );
			}
			return Math.min( ...errors );
		}, loading.toString( "base64" ) );
		assert.ok( loadingMatch <= 3, "Loading pixels must match a retail entry background: " + loadingMatch );
		await page.waitForFunction(
			() => /Frontend: world\n/.test( document.querySelector( "output" )?.textContent ),
			null,
			{ timeout: 60000 }
		);
		const before = await page.evaluate( () => window.__entry.game );
		await page.screenshot( { path: "temp/artifacts/motion-cadence/world-before.png" } );
		await page.mouse.move( 550, 400 );
		await page.mouse.down( { button: "right" } );
		await page.mouse.move( 600, 410, { steps: 5 } );
		await page.mouse.up( { button: "right" } );
		await page.mouse.click( 650, 520 );
		await page.waitForFunction(
			() => window.__entry.commands.some( c => c.command?.kind === "ground-move" ),
			null,
			{ timeout: 5000 }
		);
		await page.waitForFunction( ack => window.__entry.game?.acknowledgedMove > ack, before.acknowledgedMove ?? 0, {
			timeout: 10000
		} );
		await page.waitForTimeout( 1200 );
		await page.mouse.click( 450, 520 );
		await page.waitForTimeout( 2200 );
		const result = await page.evaluate( () => window.__entry );
		assert.ok( result.menus.length > 0 );
		assert.ok( result.menus.every( Boolean ) );
		assert.equal( result.failure, undefined );
		assert.equal( result.commands.filter( c => c.kind === "world-ready" ).length, 1 );
		const moving = result.visual.filter( row => row.moving );
		assert.ok( moving.length > 20, "record real visible-body samples, not only worker FPS" );
		const cycles = result.cycles.filter( row => row.moving );
		assert.ok( cycles.length > 10 );
		// The gait clips are 'run'/'walk', or the weapon set's native ids once it
		// loads (actor-motion: stateId 7 is run, 1 is walk, 0 is stand).
		for ( const row of cycles ) {
			assert.ok(
				[ "run", "walk" ].includes( row.clip ) || /:7$|:1$/.test( row.clip ),
				"an active path cannot select idle between packets"
			);
		}
		for ( let i = 1; i < result.cycles.length; i++ ) {
			const a = result.cycles[i - 1], b = result.cycles[i];
			if ( a.moving && b.moving && a.clip === b.clip ) {
				assert.ok( b.time >= a.time, "run cycle cannot restart while movement stays active" );
			}
		}
		for ( let i = 1; i < result.visual.length; i++ ) {
			const a = result.visual[i - 1], b = result.visual[i], dt = b.at - a.at;
			if ( dt <= 0 || dt > .25 ) continue;
			const turn = Math.abs( ((b.pose.angle - a.pose.angle + 98304) % 65536) - 32768 );
			// Angles are whole units (65536 per turn) and the presenter integrates
			// in float before rounding, so the cap needs two units of slop, not a
			// sub-quantum 0.001 - at exactly the native rate a frame can land a
			// quarter unit over.
			assert.ok( turn <= dt * 98304 + 2, "visible heading must obey the native turn rate" );
		}
		assert.notDeepEqual( result.game.pose, before.pose );
		await page.screenshot( { path: "temp/artifacts/motion-cadence/world-after.png" } );
		// #1443 (epic #1437 acceptance): through a start, a hard redirect and a
		// stop, the drawn pose must never outrun the walk itself - the start and
		// redirect surges of the old presenter ran well above walking speed.
		const startMark = await page.evaluate( () => window.__entry.visual.length );
		// Drive the manoeuvre with session move commands to fixed destinations
		// (the speed-compare probe's seam): screen clicks raycast into whatever
		// wall stands nearby - the character may be parked inside a town, where
		// no click travels - while a destination paths there from anywhere. The
		// drawn presenter under test is the same one a click feeds.
		await bindPlayableRuntime( page );
		const poseNow = () => page.evaluate( () => __playableRuntime.gameplay().pose );
		const moveTo = destination =>
			page.evaluate(
				d => __playableRuntime.session( { kind: "gameplay", command: { kind: "move", destination: d } } ),
				destination
			);
		const start = await poseNow();
		await moveTo( { ...start, x: start.x + 150 } );
		await page.waitForFunction( () => window.__entry.visual.at( -1 )?.moving, null, { timeout: 10000 } );
		const legDeadline = Date.now() + 25000;
		for ( ;; ) {
			const pose = await poseNow();
			if ( Math.hypot( pose.x - start.x, pose.z - start.z ) > 50 ) break;
			assert.ok( Date.now() < legDeadline, "the first leg travels" );
			await page.waitForTimeout( 200 );
		}
		const turnAt = await page.evaluate( () => window.__entry.visual.at( -1 )?.at ), mid = await poseNow();
		await moveTo( { ...mid, z: mid.z + 150 } );
		const stopDeadline = Date.now() + 30000;
		for ( ;; ) {
			const tail = await page.evaluate( () => window.__entry.visual.slice( -30 ) ),
				lastMove = [ ...tail ].reverse().find( r => r.moving );
			if ( tail.length && !tail.at( -1 ).moving && (!lastMove || tail.at( -1 ).at - lastMove.at > 0.8) ) break;
			assert.ok( Date.now() < stopDeadline, "the redirect leg stops" );
			await page.waitForTimeout( 250 );
		}
		await page.waitForTimeout( 600 );
		const trace = await page.evaluate( mark => window.__entry.visual.slice( mark ), startMark ), frames = [];
		for ( let i = 1; i < trace.length; i++ ) {
			const a = trace[i - 1], b = trace[i], dt = b.at - a.at;
			if ( dt <= 0 || dt > .25 ) continue;
			frames.push( {
				at: b.at,
				moving: b.moving,
				angle: b.pose.angle,
				region: b.pose.regionId,
				x: b.pose.x,
				z: b.pose.z,
				speed: Math.hypot( b.pose.x - a.pose.x, b.pose.z - a.pose.z ) / dt
			} );
		}
		assert.ok( frames.length > 60, "record a full start/redirect/stop trace: " + frames.length );
		assert.ok( frames.every( f => f.region === frames[0].region ), "the manoeuvre stays in one region" );
		const movingSpeeds = frames.filter( f => f.moving ).map( f => f.speed ).sort( ( a, b ) => a - b ),
			walk = movingSpeeds[Math.floor( movingSpeeds.length / 2 )];
		assert.ok( walk > 10, "the trace really walks (median speed " + walk + ")" );
		const worst = frames.reduce( ( m, f ) => f.speed > m.speed ? f : m, frames[0] );
		assert.ok(
			worst.speed <= walk * 1.05,
			`no drawn frame above 1.05x walking speed (worst ${worst.speed.toFixed( 1 )} u/s vs walk ${
				walk.toFixed( 1 )
			} u/s)`
		);
		const legHeading = ( a, b ) => {
			const w = frames.filter( f => f.moving && f.at >= a && f.at <= b );
			assert.ok( w.length >= 6, "a leg of the redirect is recorded (" + w.length + ")" );
			return Math.atan2( -(w.at( -1 ).z - w[0].z), w.at( -1 ).x - w[0].x );
		};
		const preLeg = legHeading( turnAt - 1, turnAt - .15 ), postLeg = legHeading( turnAt + .45, turnAt + 1.3 );
		const turn = Math.abs( Math.atan2( Math.sin( postLeg - preLeg ), Math.cos( postLeg - preLeg ) ) );
		assert.ok(
			turn >= 1,
			"the redirect really turns (measured " + (turn * 180 / Math.PI).toFixed( 0 ) + " degrees)"
		);
		assert.ok(
			frames.slice( -10 ).every( f => !f.moving || f.speed < walk * .2 ),
			"the stop settles instead of surging past it"
		);
	} finally {
		await page.screenshot( { path: "temp/artifacts/motion-cadence/final.png" } );
		await writeFile( "temp/artifacts/motion-cadence/diagnostic.txt", await page.locator( "output" ).textContent() );
		await writeFile(
			"temp/artifacts/motion-cadence/result.json",
			JSON.stringify( await page.evaluate( () => window.__entry ), null, 2 )
		);
		await browser.close();
	}
} );
