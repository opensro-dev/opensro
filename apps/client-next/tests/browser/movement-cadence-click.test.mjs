/*
===========================================================================

movement-cadence-click.test.mjs - A5 by real ground clicks, mover on and off

The #1530 acceptance (movement-cadence.test.mjs) driven by real mouse
ground clicks, so the measured walk starts where a player's click starts
it (#1443, epic #1437). It runs once with the Experimental "Instant own
movement" row on (the main-thread mover, #1541) and once with it off (the
worker-driven presenter). Browser test: needs the local stack.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { resolveProbeCredentials } from "../../../../scripts/lib/probeSession.mjs";
import { assertCharacterAllowed } from "../../../../scripts/lib/probeCharacter.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
import { bindPlayableRuntime } from "./helpers/playable-session.mjs";

// #1541 (epic #1437, C1): the A5 acceptance of movement-cadence.test.mjs
// driven by real ground clicks instead of session move commands, so the
// measured walk starts where a player's click starts it. One run walks
// with the Experimental "Instant own movement" row (mainThreadOwnWalk)
// on - the main-thread mover presents the walk - and one with it off, the
// worker-driven presenter. Both runs owe the #1530 bars: no drawn frame
// above 1.05x the entity's own gait speed (median anchored to the pose's
// own walk/run speed within 5%), a real turn of at least a radian across
// the redirect, and a stop that settles.

/*
================
clickAcceptance

One acceptance run with the row on or off: a warp to flat ground, a leg,
a reversing click and a settle, every drawn frame read at the final
presented pose and held to the bars above.
================
*/
async function clickAcceptance( rowOn ) {
	const character = assertCharacterAllowed( "asd2", { context: "client-next mission entry" } );
	const { browser, page } = await launchProbeBrowser( { viewport: { width: 1024, height: 768 } } ),
		control = id => page.locator( `[data-ui-id="${id}"]` );
	const artifactDir = "temp/artifacts/motion-cadence-click-" + (rowOn ? "on" : "off");
	await mkdir( artifactDir, { recursive: true } );
	try {
		await page.addInitScript( rowOn => {
			if ( rowOn ) {
				localStorage.setItem(
					"sro:v1150:experimental-options:2",
					JSON.stringify( { mainThreadOwnWalk: true } )
				);
			} else {
				localStorage.removeItem( "sro:v1150:experimental-options:2" );
			}
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
					super( .../** @type {[any, any]} */ (args) );
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
		}, rowOn );
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
		await page.route( "**/runtime/characters/pose-presentation.ts*", async route => {
			const response = await route.fetch(), body = await response.text();
			// Record the local player's drawn pose once per frame. With
			// the main-thread mover (#1541) every generation exits
			// through pose()'s single result return, mover override
			// included, so that return is the seam; on the older
			// presenter the sampled returns (sampledPose, or
			// ownWalks.pose with #1444's interpolation) are. Accept
			// whichever the served build has and fail loudly if neither
			// survives. The needles match the served (esbuild) text,
			// which has no inner-paren padding.
			const moverReturn = /return \{\s*\.\.\.result\s*\};/,
				sampled = /return (sampledPose\(gid, input, target, now\));/,
				ownWalk = /return (ownWalks\.pose\(gid, input, target, \{[^}]+\}\));/;
			if ( moverReturn.test( body ) ) {
				await route.fulfill( {
					response,
					body: body.replace(
						moverReturn,
						"const __drawnPose = { ...result }; " +
							"if ( gid === globalThis.__entry?.game?.localGid ) " +
							"globalThis.__entry.visual.push( { at: now, pose: __drawnPose, moving: inputs.get( gid )?.moving ?? false } ); " +
							"return __drawnPose;"
					)
				} );
				return;
			}
			assert.ok( sampled.test( body ) || ownWalk.test( body ), "pose-presentation sampled return drifted" );
			const record = ( match, call ) =>
				`const __recorded=${call}; if(globalThis.__entry?.game?.localGid===gid)globalThis.__entry.visual.push({at:now,pose:__recorded,moving:input.moving}); return __recorded;`;
			await route.fulfill( { response, body: body.replace( sampled, record ).replace( ownWalk, record ) } );
		} );
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
			!(/** @type {HTMLButtonElement | null} */ (document.querySelector( '[data-ui-id="login"]' )))?.disabled &&
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
		await page.waitForFunction( () =>
			!(/** @type {HTMLButtonElement | null} */ (document.querySelector( '[data-ui-id="enter"]' )))?.disabled
		);
		await control( "enter" ).click();
		await page.waitForFunction(
			() => /Frontend: world\n/.test( document.querySelector( "output" )?.textContent ?? "" ),
			null,
			{ timeout: 60000 }
		);
		assert.equal( await page.evaluate( () => window.__entry.failure ), undefined );
		// The row's seeded state survived boot: the run measures the
		// presenter it claims to measure.
		const storedRow = await page.evaluate( () => {
			const raw = localStorage.getItem( "sro:v1150:experimental-options:2" );
			return raw ? JSON.parse( raw ).mainThreadOwnWalk === true : false;
		} );
		assert.equal( storedRow, rowOn, "the Experimental row seeding did not survive boot" );
		await bindPlayableRuntime( page );
		const poseNow = () => page.evaluate( () => __playableRuntime.gameplay().pose );
		const settled = async () => {
			const tail = await page.evaluate( () => window.__entry.visual.slice( -30 ) ),
				lastMove = [ ...tail ].reverse().find( r => r.moving );
			return tail.length > 0 && !tail.at( -1 ).moving && (!lastMove || tail.at( -1 ).at - lastMove.at > 0.8);
		};
		const waitSettled = async ( deadlineMs, what ) => {
			const deadline = Date.now() + deadlineMs;
			for ( ;; ) {
				if ( await settled() ) return;
				assert.ok( Date.now() < deadline, what );
				await page.waitForTimeout( 250 );
			}
		};
		// Place the character on open ground with the dev account's GM
		// warp: a deterministic start the earlier acceptance runs walked
		// (Jangan's south field). Session-move placement depends on
		// wherever the character last logged out, and a town click
		// raycasts into whatever wall stands nearby. Only the measured
		// legs below are clicks.
		await page.keyboard.press( "Shift+Backquote" );
		const gmInput = page.locator( '[data-ui-id="gm-input"]' );
		await gmInput.waitFor( { timeout: 10000 } );
		await gmInput.fill( "/warp 25000 1200 0 1500" );
		await gmInput.press( "Enter" );
		await page.keyboard.press( "Escape" );
		await page.waitForFunction(
			() => {
				const pose = __playableRuntime.gameplay()?.pose;
				return !!pose && Math.abs( pose.x - 1200 ) < 40 && Math.abs( pose.z - 1500 ) < 40;
			},
			null,
			{ timeout: 15000 }
		);
		await waitSettled( 15000, "the warp landing settles" );
		// The warp re-enters the world; clicks land only once the drawn
		// stream is live again, so wait for it to advance.
		await page.waitForFunction(
			() => window.__entry.visual.length > 0,
			null,
			{ timeout: 15000 }
		);
		const liveAt = await page.evaluate( () => window.__entry.visual.at( -1 )?.at );
		await page.waitForFunction(
			at => window.__entry.visual.at( -1 )?.at > at,
			liveAt,
			{ timeout: 15000 }
		);
		await page.waitForTimeout( 800 );
		const startMark = await page.evaluate( () => window.__entry.visual.length );
		const seqBefore = await page.evaluate( () => window.__entry.game?.movementInputSeq ?? 0 ),
			groundMoves = () =>
				page.evaluate( () => window.__entry.commands.filter( c => c.command?.kind === "ground-move" ).length );
		// A click the still-settling world swallows sends nothing, so
		// retrying the same point is honest: only a click the worker
		// answered counts. Returns whether one did.
		const clickGround = async ( x, y ) => {
			for ( let attempt = 0; attempt < 3; attempt++ ) {
				const before = await groundMoves();
				await page.mouse.click( x, y );
				try {
					await page.waitForFunction(
						n => window.__entry.commands.filter( c => c.command?.kind === "ground-move" ).length > n,
						before,
						{ timeout: 4000 }
					);
					return true;
				} catch {
					await page.waitForTimeout( 1000 );
				}
			}
			return false;
		};
		// Leg one: a real ground click ahead of the character. A click at
		// its feet travels nowhere and a click near the horizon can path
		// across the map, so the point sits between: measured legs from
		// here run a few tens of units.
		const legStart = await poseNow();
		// Whichever point the terrain in front of the character answers:
		// the warp's facing varies, and a point can pick a wall or a roof
		// and send nothing.
		let answered = false;
		for ( const [x, y] of [ [ 512, 280 ], [ 512, 340 ], [ 420, 300 ], [ 600, 300 ] ] ) {
			if ( await clickGround( x, y ) ) {
				answered = true;
				break;
			}
		}
		assert.ok( answered, "no ground-move answered the first leg's clicks" );
		await page.waitForFunction( () => window.__entry.visual.at( -1 )?.moving, null, { timeout: 10000 } );
		// One click only reaches the ground the camera shows (a few tens
		// of units), so a second click ahead while the body walks the
		// first lengthens the leg before the turn.
		const travelDeadline = Date.now() + 25000;
		let extended = false;
		for ( ;; ) {
			const pose = await poseNow();
			const travelled = Math.hypot( pose.x - legStart.x, pose.z - legStart.z );
			if ( travelled > 30 ) break;
			if ( !extended && travelled > 10 ) {
				// A swallowed extension click only shortens the leg.
				await clickGround( 512, 280 );
				extended = true;
			}
			assert.ok( Date.now() < travelDeadline, "the first click leg travels" );
			await page.waitForTimeout( 200 );
		}
		// The redirect: a real ground click back toward the bottom of
		// the screen, reversing the leg. The visible ground behind the
		// character is a thin strip, so a candidate can land at its feet
		// and travel nothing; walk the bounded candidate list until one
		// travels and turns. The turn is read off the worker poses at
		// each click, so extra candidates cost the presenter more
		// redirects, never a weaker bar.
		const turnFrom = await poseNow(),
			legHeading = Math.atan2( -(turnFrom.z - legStart.z), turnFrom.x - legStart.x );
		let turned = 0, turnTravel = 0;
		for ( const [x, y] of [ [ 512, 640 ], [ 110, 640 ], [ 914, 640 ], [ 512, 600 ] ] ) {
			const atClick = await poseNow();
			if ( !(await clickGround( x, y )) ) continue;
			await waitSettled( 25000, "the redirect leg stops" );
			const end = await poseNow();
			turnTravel = Math.hypot( end.x - atClick.x, end.z - atClick.z );
			if ( turnTravel < 5 ) continue;
			const heading = Math.atan2( -(end.z - atClick.z), end.x - atClick.x );
			turned = Math.abs( Math.atan2( Math.sin( heading - legHeading ), Math.cos( heading - legHeading ) ) );
			if ( turned >= 1 ) break;
		}
		await page.waitForTimeout( 600 );
		assert.ok(
			turned >= 1,
			"the redirect really turns (measured " + (turned * 180 / Math.PI).toFixed( 0 ) +
				" degrees over " + turnTravel.toFixed( 1 ) + " units)"
		);
		if ( rowOn ) {
			// The input sequence only exists in C1's worker: its advance
			// proves the on run measured the build that has the mover,
			// not a silently inert seed on an older presenter.
			const seqAfter = await page.evaluate( () => window.__entry.game?.movementInputSeq ?? 0 );
			assert.ok( seqAfter > seqBefore, "the click never reached the sequenced worker input" );
		}
		const trace = await page.evaluate( mark => window.__entry.visual.slice( mark ), startMark ), frames = [];
		for ( let i = 1; i < trace.length; i++ ) {
			const a = trace[i - 1], b = trace[i], dt = b.at - a.at;
			if ( dt <= 0 || dt > .25 ) continue;
			frames.push( {
				at: b.at,
				dt,
				dist: Math.hypot( b.pose.x - a.pose.x, b.pose.z - a.pose.z ),
				moving: b.moving,
				angle: b.pose.angle,
				region: b.pose.regionId,
				x: b.pose.x,
				z: b.pose.z,
				speed: Math.hypot( b.pose.x - a.pose.x, b.pose.z - a.pose.z ) / dt
			} );
		}
		assert.ok( frames.length > 40, "record a full start/redirect/stop trace: " + frames.length );
		assert.ok( frames.every( f => f.region === frames[0].region ), "the manoeuvre stays in one region" );
		const moving = frames.filter( f => f.moving );
		assert.ok( moving.length > 20, "the click legs really walk (" + moving.length + " moving frames)" );
		const movingSpeeds = moving.map( f => f.speed ).sort( ( a, b ) => a - b ),
			walk = movingSpeeds[Math.floor( movingSpeeds.length / 2 )];
		assert.ok( walk > 10, "the trace really walks (median speed " + walk + ")" );
		// Name the gait the trace actually drew (the cycles rows: run is
		// state 7, walk is state 1) so the bars below read on the local
		// entity's own speed for that gait.
		const gait = await page.evaluate(
			( [from, to] ) => {
				let run = 0, stroll = 0;
				for ( const row of window.__entry.cycles ) {
					if ( !row.moving || row.at < from || row.at > to ) continue;
					if ( row.clip === "run" || /:7$/.test( row.clip ) ) run++;
					else if ( row.clip === "walk" || /:1$/.test( row.clip ) ) stroll++;
				}
				return { run, stroll };
			},
			[ frames[0].at, frames[frames.length - 1].at ]
		);
		assert.ok( gait.run + gait.stroll > 0, "the manoeuvre's gait is recorded" );
		const runs = gait.run >= gait.stroll,
			speedRow = trace.find( row => typeof (runs ? row.pose.runSpeed : row.pose.walkSpeed) === "number" );
		assert.ok( speedRow, "the trace pose carries its own speeds" );
		const expectedSpeed = runs ? speedRow.pose.runSpeed : speedRow.pose.walkSpeed;
		// Anchor on the end-to-end average, not the per-frame median:
		// the worker-driven presenter lags its ticks and recovers at the
		// capped rate (#1527), so the median sits low by design and
		// swings with the lag, while the distance covered over the
		// moving time cannot drift far from the walk itself. The upper
		// bound is what catches a presenter that runs uniformly fast;
		// the lower one proves the trace really walked the gait.
		const pathLength = moving.reduce( ( sum, f ) => sum + f.dist, 0 ),
			movingTime = moving.reduce( ( sum, f ) => sum + f.dt, 0 ),
			average = pathLength / movingTime;
		assert.ok(
			average <= expectedSpeed * 1.05,
			`the average moving speed ${average.toFixed( 1 )} u/s outruns the entity's own ${
				runs ? "run" : "walk"
			} speed ${expectedSpeed} u/s`
		);
		assert.ok(
			average >= expectedSpeed * .85,
			`the trace really walks at the gait (average ${average.toFixed( 1 )} u/s vs ${
				runs ? "run" : "walk"
			} speed ${expectedSpeed} u/s)`
		);
		const worst = frames.reduce( ( m, f ) => f.speed > m.speed ? f : m, frames[0] );
		// The bar reads on the entity's own speed, not on the drawn
		// median: the worker-driven presenter lags a few percent behind
		// its ticks and recovers at the capped correction rate (#1527),
		// so a median-relative bar would charge the lag twice. The
		// anchor assert above already catches a presenter that runs
		// uniformly fast, which is what the median cannot be trusted
		// to show on its own here.
		assert.ok(
			worst.speed <= expectedSpeed * 1.05,
			`no drawn frame above 1.05x the entity's own speed (worst ${worst.speed.toFixed( 1 )} u/s vs ${
				runs ? "run" : "walk"
			} speed ${expectedSpeed} u/s; drawn median ${walk.toFixed( 1 )})`
		);
		assert.ok(
			frames.slice( -10 ).every( f => !f.moving || f.speed < expectedSpeed * .2 ),
			"the stop settles instead of surging past it"
		);
		console.log(
			`cadence-click row=${rowOn ? "on" : "off"} frames=${frames.length} moving=${moving.length} ` +
				`median=${walk.toFixed( 2 )} average=${average.toFixed( 2 )} expected=${expectedSpeed} ` +
				`worst=${worst.speed.toFixed( 2 )} ratio=${(worst.speed / expectedSpeed).toFixed( 4 )} ` +
				`turn=${(turned * 180 / Math.PI).toFixed( 0 )}deg`
		);
		await page.screenshot( { path: artifactDir + "/world-after.png" } );
	} finally {
		await page.screenshot( { path: artifactDir + "/final.png" } ).catch( () => {} );
		await writeFile( artifactDir + "/diagnostic.txt", (await page.locator( "output" ).textContent()) ?? "" ).catch(
			() => {}
		);
		await writeFile(
			artifactDir + "/result.json",
			JSON.stringify( await page.evaluate( () => window.__entry ), null, 2 )
		).catch( () => {} );
		await browser.close();
	}
}

test(
	"movement cadence (ground clicks): the main-thread mover never outruns the walk",
	{ timeout: 300000 },
	async () => clickAcceptance( true )
);

test(
	"movement cadence (ground clicks): the worker-driven walk never outruns the walk",
	{ timeout: 300000 },
	async () => clickAcceptance( false )
);
