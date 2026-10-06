/*
===========================================================================

recovery-bench.mjs - real-session scheduling and transport recovery evidence

Uses the shared authenticated launcher and asd2 fixture. Faults delay real
WebSocket bytes and scheduling; they never manufacture gameplay replies or
rewrite a production source file. The pre-login hook runs in the existing simulation worker before its first
world connection; the production entry and message delivery stay untouched.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { MISSION_MOVEMENT_FIXTURES } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { openClient, closeClient, createCaptures, measure, revive } from "../core/client.mjs";
import { parseOptions } from "../core/report.mjs";
import { walk } from "./scenarios.mjs";

import { installFaults, holdStream, stallServer, faultLog } from "../core/transport-faults.mjs";

import { captureVisual } from "../core/visual-capture.mjs";

// The stall lanes: a late downlink (the server applied the command on time)
// and a bidirectional delay (the command reaches the server late and its
// reply comes late). The latter is a delayed-command surrogate for a late
// tick or blocked handler, NOT proof of a paused tick: server AI and
// existing movement keep advancing while the bytes are held.
const STALL_LANES = [ "network-delay", "bidirectional-delay", "edge-delay" ];
// Legs per lane, relative to the pose at each leg. Open lanes walk free
// ground; the edge lane walks into the anchor's +z boundary (z~620 in
// 0x6E4B) and turns along it, where clipping must agree on a partial move.
const STALL_LEGS = {
	open: { approach: { x: 120, z: 0 }, turn: { x: -120, z: 60 } },
	edge: { approach: { x: 0, z: 120 }, turn: { x: 120, z: 120 } }
};
const STALL_MS = [ 0, 150, 300, 600 ];
const CHARACTER = process.env.SRO_PROBE_CHARACTER ?? "asd2";
// The GameWorld's local operator snapshot (two-second capture cache).
const OBSERVATORY_URL = process.env.SRO_BENCH_OBSERVATORY ??
	"http://127.0.0.1:8791/internal/diagnostics/observatory";
const OBSERVATORY_CACHE_MS = 2000;
const SERVER_POSE_TOLERANCE = 1;
// The client's movement command (movement.ts OP_PREDICTED_MOVE).
const OP_PREDICTED_MOVE = 0x0009;
const METRICS_URL = process.env.SRO_BENCH_TRANSPORT_METRICS ?? "http://127.0.0.1:8788/transport/metrics";
const ANCHOR_TIMEOUT_MS = 15000;

/*
================
moveTo
================
*/
function moveTo( page, offset ) {
	return page.evaluate( offset => {
		const root = globalThis.__benchRuntime, pose = root.gameplay().pose;
		const destination = { ...pose, x: pose.x + offset.x, z: pose.z + offset.z };
		root.session( { kind: "gameplay", command: { kind: "move", destination } } );
		return destination;
	}, offset );
}

/*
================
movementState

The worker's movement owner as the stall left it: the fields a receipt is
supposed to reconcile.
================
*/
function movementState( page ) {
	return page.evaluate( () => {
		const game = globalThis.__benchRuntime.gameplay();
		return {
			atMs: Date.now(),
			pose: game.pose,
			authoritativePose: game.authoritativePose,
			poseAtMs: game.poseAtMs,
			pendingMoves: game.pendingMoves,
			moving: game.moving,
			movementPath: game.movementPath,
			movementRevision: game.movementRevision
		};
	} );
}

/*
================
returnToAnchor

Walks back to the lane's start point and waits until the move is settled, so
every step starts on the same open ground instead of drifting into an edge.
================
*/
async function returnToAnchor( page, anchor ) {
	await page.evaluate( destination => {
		globalThis.__benchRuntime.session( { kind: "gameplay", command: { kind: "move", destination } } );
	}, anchor );
	const until = Date.now() + ANCHOR_TIMEOUT_MS;
	while ( Date.now() < until ) {
		await page.waitForTimeout( 100 );
		const state = await movementState( page );
		if ( !state.moving && state.pendingMoves === 0 ) return;
	}
	throw Error( "the character did not return to the stall anchor" );
}

/*
================
transportMetrics

The GameWorld's lifetime timing counters; the lane artifact keeps a
snapshot from before and after so its deltas are attributable.
================
*/
async function transportMetrics() {
	const response = await fetch( METRICS_URL );
	assert.ok( response.ok, `transport metrics ${response.status}` );
	return { fetchedAtMs: Date.now(), metrics: await response.json() };
}

/*
================
stallTurns

For each stall length: start a walk, open the stall, and send a sharp turn
or a stop a third of the way into it, then let it release and settle. The
turn or stop is the command the coordinator saw answered wrong. A stop is an
explicit move to the current pose, not a key release. Length 0 is the
no-hold control. Every step records its exact request and, after the
observatory cache has turned over, the client's movement state beside the
server's pose.
================
*/
async function stallTurns( page, lane, name ) {
	const stall = lane === "bidirectional-delay" ? ms => stallServer( page, ms ) : ms => holdStream( page, "rx", ms );
	const legs = lane === "edge-delay" ? STALL_LEGS.edge : STALL_LEGS.open;
	const steps = [];
	const anchor = (await movementState( page )).pose;
	for ( const ms of STALL_MS ) {
		for ( const action of [ "turn", "stop" ] ) {
			await returnToAnchor( page, anchor );
			await moveTo( page, legs.approach );
			await page.waitForTimeout( 400 );
			if ( ms > 0 ) await stall( ms );
			await page.waitForTimeout( Math.round( ms / 3 ) );
			const requested = await moveTo( page, action === "turn" ? legs.turn : { x: 0, z: 0 } );
			await page.waitForTimeout( ms + 900 + OBSERVATORY_CACHE_MS );
			const client = await movementState( page );
			const server = await serverPose( name );
			steps.push( {
				ms,
				action,
				requested,
				// movement.ts truncates the 0x7738 destination to integer region units.
				wire: { x: Math.trunc( requested.x ), y: Math.trunc( requested.y ), z: Math.trunc( requested.z ) },
				client,
				server,
				xz: Math.hypot( server.x - client.pose.x, server.z - client.pose.z ),
				y: server.y - client.pose.y
			} );
		}
	}
	return steps;
}

/*
================
serverPose

The named character's live pose as the GameWorld itself reports it, read
after the snapshot cache has turned over so it postdates the settle.
================
*/
async function serverPose( name ) {
	const response = await fetch( OBSERVATORY_URL, { headers: { "X-SRO-Local-Diagnostics": "1" } } );
	assert.ok( response.ok, `observatory ${response.status}` );
	const player = (await response.json()).players.find( row => row.name.toLowerCase() === name.toLowerCase() );
	assert.ok( player, `the GameWorld reports no online ${name}` );
	// The body carries no capture time; the snapshot is at most the cache age old.
	const fetchedAtMs = Date.now();
	return {
		regionId: player.region,
		x: player.x,
		y: player.y,
		z: player.z,
		capturedAfterMs: fetchedAtMs - OBSERVATORY_CACHE_MS,
		fetchedAtMs
	};
}

/*
================
run
================
*/
async function run( options ) {
	const client = await openClient( MISSION_MOVEMENT_FIXTURES.region_cross, {
		spans: true,
		uncapped: false,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	const { page } = client;
	try {
		let worker;
		for ( const candidate of page.workers() ) {
			if ( await candidate.evaluate( () => !!globalThis.__recoveryLink ) ) worker = candidate;
		}
		assert.ok( worker, "instrumented simulation worker must exist" );
		await mkdir( options.out, { recursive: true } );
		const results = [];
		for ( const lane of [ "main", "worker", "transport", ...STALL_LANES ] ) {
			if ( !options.only.includes( lane ) ) continue;
			await revive( page );
			await worker.evaluate( delay => {
				globalThis.__recoveryLink.delay = delay;
				globalThis.__recoveryLink.jitter = 25;
			}, lane === "transport" ? 100 : 0 );
			const captures = await createCaptures( page, {
				dir: options.out,
				cpu: true,
				trace: options.trace ? `${options.out}/${lane}.json` : null
			} );
			/*
			================
			drive
			================
			*/
			const drive = async more => {
				if ( STALL_LANES.includes( lane ) ) {
					result.timing = { before: await transportMetrics() };
					faultMark = (await faultLog( page )).logged;
					result.stalls = await stallTurns( page, lane, CHARACTER );
					while ( more() ) await page.waitForTimeout( 50 );
					return;
				}
				const moving = walk( page, more );
				if ( lane !== "transport" ) {
					for ( const gap of [ 50, 100, 150, 300, 1000 ] ) {
						await page.waitForTimeout( 650 );
						await (lane === "main" ? page : worker).evaluate( ms => {
							const until = performance.now() + ms;
							while ( performance.now() < until ) { /* Deliberate scheduling fault. */ }
						}, gap );
					}
				}
				await moving;
			};
			await captures.start();
			const result = {};
			let faultMark = 0;
			Object.assign( result, await measure( page, lane, STALL_LANES.includes( lane ) ? 90000 : 7000, drive ) );
			await captures.stop( lane );
			await captures.finish();
			if ( options.video && lane === "main" ) {
				result.visual = await captureVisual( page, `${options.out}/video`, async () => {
					const start = Date.now();
					await drive( () => Date.now() - start < 7000 );
				} );
			}
			result.transport = await worker.evaluate( () => ({ ...globalThis.__recoveryLink, log: undefined }) );
			if ( STALL_LANES.includes( lane ) ) {
				result.faults = await faultLog( page, faultMark );
				result.timing.after = await transportMetrics();
				// Settle past the snapshot cache, then compare the server's own pose.
				await page.waitForTimeout( OBSERVATORY_CACHE_MS + 1000 );
				const local = await page.evaluate( () => {
					const root = globalThis.__benchRuntime, game = root.gameplay();
					const body = root.characterActors().find( actor => actor.gid === game.localGid );
					return { logical: game.pose, drawn: body?.pose ?? null };
				} );
				const server = await serverPose( CHARACTER );
				result.serverPose = {
					...local,
					server,
					xz: Math.hypot( server.x - local.logical.x, server.z - local.logical.z ),
					y: server.y - local.logical.y
				};
				// Evidence first: a failed comparison must still leave its record.
				await writeFile(
					`${options.out}/stall-${lane}.json`,
					JSON.stringify(
						{
							lane,
							stalls: result.stalls,
							faults: result.faults,
							serverPose: result.serverPose,
							timing: result.timing
						},
						null,
						2
					)
				);
				assert.equal( result.faults.transport, "websocket", "the stall ran on the client's real WebSocket" );
				// Only this lane's holds count; the log runs across lanes.
				const held = direction => result.faults.held.filter( entry => entry.direction === direction );
				assert.ok( held( "rx" ).length > 0, "the stall held real downlink frames" );
				if ( lane === "bidirectional-delay" ) {
					assert.ok(
						held( "tx" ).some( entry => entry.opcode === OP_PREDICTED_MOVE ),
						"the bidirectional stall held a movement command"
					);
				}
				assert.equal( server.regionId, local.logical.regionId, JSON.stringify( result.serverPose ) );
				// y is recorded, not asserted: whether the server's y is the wire Y
				// or a surface height is still open.
				if ( lane === "edge-delay" ) {
					// An edge lane whose walks all arrived never touched the edge.
					assert.ok(
						result.stalls.some( step =>
							Math.hypot( step.client.pose.x - step.wire.x, step.client.pose.z - step.wire.z ) > 1
						),
						"the edge lane was never blocked"
					);
				}
				for ( const step of [ ...result.stalls, result.serverPose ] ) {
					assert.ok(
						step.xz <= SERVER_POSE_TOLERANCE,
						`settled client and server poses differ: ${JSON.stringify( step )}`
					);
				}
			}
			assert.ok(
				result.transport.rx > 0 && result.transport.tx > 0,
				"real WebSocket traffic must cross the fault injector"
			);
			assert.ok( result.movement.length > 50, "record worker and displayed poses" );
			assert.ok(
				result.movement.every( row => !row.displayed || Number.isFinite( row.displayed.x ) ),
				"all displayed positions are finite"
			);
			let bodyFrames = 0;
			for ( const sample of result.movement ) {
				if ( !sample.body || !sample.displayed || sample.body.mountedOn ) continue;
				assert.ok( sample.body.clip, "local body must have an authored animation" );
				const a = sample.body.pose, b = sample.displayed;
				assert.equal( a.regionId, b.regionId );
				assert.ok(
					Math.hypot( a.x - b.x, a.y - b.y, a.z - b.z ) < .001,
					"body and camera must consume the same presentation pose"
				);
				bodyFrames++;
			}
			assert.ok( bodyFrames > 50, "actual body/camera comparisons are required" );
			result.bodyFrames = bodyFrames;
			result.session = await page.evaluate( () => globalThis.__benchRuntime.sessionState().phase );
			result.sessionError = await page.evaluate( () => globalThis.__benchRuntime.sessionState().error );
			results.push( result );
			await writeFile( `${options.out}/results.json`, JSON.stringify( results, null, 2 ) );
			assert.equal(
				result.session,
				"world",
				JSON.stringify( { error: result.sessionError, closes: result.transport.closes } )
			);
			console.log(
				JSON.stringify( {
					lane,
					cpuRate: options.cpuRate,
					frames: result.frames,
					p95: result.p95,
					p99: result.p99,
					transport: result.transport
				} )
			);
			await page.waitForTimeout( 1000 );
		}
	} finally {
		await closeClient( client );
	}
}

await run( parseOptions(
	process.argv.slice( 2 ),
	{
		cpuRate: 1,
		out: "temp/artifacts/recovery",
		trace: false,
		video: false,
		only: [ "main", "worker", "transport", ...STALL_LANES ]
	},
	"recovery-bench.mjs [--cpu-rate 4] [--out DIR] [--only main,worker,transport,network-delay,bidirectional-delay] " +
		"[--trace] [--video]"
) );
