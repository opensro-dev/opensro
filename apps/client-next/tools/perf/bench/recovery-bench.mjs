/*
===========================================================================

recovery-bench.mjs - real-session scheduling and transport recovery evidence

Uses the shared authenticated launcher and scratch-character fixture. Optional
crowd peers use real accounts and sockets, with the existing crowd owner handling
provisioning, rate limits and cleanup. Faults delay real
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
import { createCrowd } from "../core/crowd.mjs";
import { waitForMovementSettlement } from "../core/movement-settlement.mjs";

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
// The existing merchant fixture stays inside Jangan's safe town. Field
// recovery remains available, but level-one peers cannot survive its mobs.
const TOWN_FIXTURE = {
	id: "jangan-accessory-merchant-recovery",
	movementMode: 3,
	start: { regionId: 25000, x: 1600, y: 0, z: 1078 },
	startYawRadians: 0
};
const CHARACTER = process.env.SRO_PROBE_CHARACTER ?? "asd2";
// The GameWorld's local operator snapshot (two-second capture cache).
const OBSERVATORY_URL = process.env.SRO_BENCH_OBSERVATORY ??
	"http://127.0.0.1:8791/internal/diagnostics/observatory";
const OBSERVATORY_CACHE_MS = 2000;
// Client and server consume the same truncated command and each samples its
// own ground, so a settled pair agrees to float precision (0.000 on every
// step measured so far). Any visible gap is a desync.
const SETTLED_POSE_TOLERANCE = 0.001;
// The anchor return is admission, not a comparison: the walk ends on the
// truncated anchor, at most a unit from the fractional start.
const ANCHOR_TOLERANCE = 1;
// The client's movement command (movement.ts OP_PREDICTED_MOVE).
const OP_PREDICTED_MOVE = 0x0009;
const METRICS_URL = process.env.SRO_BENCH_TRANSPORT_METRICS ?? "http://127.0.0.1:8788/transport/metrics";
const SETTLE_TIMEOUT_MS = 15000;
const SETTLE_MARGIN_MS = 250;
// The transport's default resume grace is 30 seconds; allow a fresh operator
// snapshot after it before declaring a closed crowd session left behind.
const CROWD_CLEANUP_TIMEOUT_MS = 45000;
// The server's movement answer (movement.ts receipt).
const OP_PREDICTED_MOVE_RESULT = 0x000a;

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
			movementRevision: game.movementRevision,
			worldClock: game.worldClock,
			hp: game.vitals?.find( row => row.gid === game.localGid )?.hp
		};
	} );
}

/*
================
settle

Polls until the worker has no move in flight and no receipt pending, then
returns that state. A fixed wait cannot tell a slow settle from a settled
one, and the observatory comparison is only meaningful after it.
================
*/
function settle( page, afterRevision ) {
	return waitForMovementSettlement( {
		read: () => movementState( page ),
		pause: ms => page.waitForTimeout( ms ),
		timeoutMs: SETTLE_TIMEOUT_MS,
		afterRevision
	} );
}

/*
================
returnToAnchor

Walks back to the lane's start point and requires arrival on its wire
(truncated) position in the same region, so every step starts on the same
open ground instead of drifting into an edge.
================
*/
async function returnToAnchor( page, anchor ) {
	const beforeRevision = await page.evaluate( destination => {
		const root = globalThis.__benchRuntime, revision = root.gameplay().movementRevision;
		root.session( { kind: "gameplay", command: { kind: "move", destination } } );
		return revision;
	}, anchor );
	const { pose } = await settle( page, beforeRevision );
	assert.equal( pose.regionId, anchor.regionId, "the anchor return left the lane's region" );
	assert.ok(
		Math.hypot( pose.x - Math.trunc( anchor.x ), pose.z - Math.trunc( anchor.z ) ) <= ANCHOR_TOLERANCE,
		`the character did not reach the stall anchor: ${JSON.stringify( { pose, anchor } )}`
	);
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
async function stallTurns( page, { lane, name, record, crowdNames } ) {
	const stall = lane === "bidirectional-delay" ? ms => stallServer( page, ms ) : ms => holdStream( page, "rx", ms );
	const legs = lane === "edge-delay" ? STALL_LEGS.edge : STALL_LEGS.open;
	const steps = [];
	const anchor = (await movementState( page )).pose;
	try {
		await stallSteps( page, { lane, name, stall, legs, anchor, steps, record, crowdNames } );
	} catch ( error ) {
		// The incident is the evidence: keep what led up to it and the live
		// state it left, as far as the page still answers.
		const incident = {
			error: String( error?.stack ?? error ),
			client: await movementState( page ).catch( failure => String( failure ) ),
			server: await serverPose( name, crowdNames ).catch( failure => String( failure ) ),
			faults: await faultLog( page ).catch( failure => String( failure ) )
		};
		await record( steps, incident );
		throw error;
	}
	return steps;
}

/*
================
stallSteps

The step loop of stallTurns; each completed step is recorded at once.
================
*/
async function stallSteps( page, { lane, name, stall, legs, anchor, steps, record, crowdNames } ) {
	for ( const ms of STALL_MS ) {
		for ( const action of [ "turn", "stop" ] ) {
			await returnToAnchor( page, anchor );
			await moveTo( page, legs.approach );
			await page.waitForTimeout( 400 );
			const mark = (await faultLog( page )).logged;
			if ( ms > 0 ) await stall( ms );
			await page.waitForTimeout( Math.round( ms / 3 ) );
			const requested = await moveTo( page, action === "turn" ? legs.turn : { x: 0, z: 0 } );
			// Past every hold (bidirectional holds the downlink for 2 ms), then settled,
			// then past the observatory cache so its snapshot postdates the settle.
			await page.waitForTimeout( 2 * ms );
			const settledAtMs = (await settle( page )).atMs;
			await page.waitForTimeout( OBSERVATORY_CACHE_MS + SETTLE_MARGIN_MS );
			const held = (await faultLog( page, mark )).held;
			const client = await movementState( page );
			const server = await serverPose( name, crowdNames );
			steps.push( {
				ms,
				action,
				requested,
				// movement.ts truncates the 0x7738 destination to integer region units.
				wire: { x: Math.trunc( requested.x ), y: Math.trunc( requested.y ), z: Math.trunc( requested.z ) },
				settledAtMs,
				held,
				client,
				server,
				crowd: crowdNames.length ? await crowdEvidence( page, crowdNames ) : null,
				xz: Math.hypot( server.x - client.pose.x, server.z - client.pose.z ),
				y: server.y - client.pose.y
			} );
			await record( steps );
			assertStep( lane, steps.at( -1 ) );
			if ( crowdNames.length ) assertCrowd( steps.at( -1 ).crowd );
		}
	}
}

/*
================
serverPose

The named character's live pose as the GameWorld itself reports it, read
after the snapshot cache has turned over so it postdates the settle.
================
*/
async function serverPose( name, crowdNames = [] ) {
	const response = await fetch( OBSERVATORY_URL, { headers: { "X-SRO-Local-Diagnostics": "1" } } );
	assert.ok( response.ok, `observatory ${response.status}` );
	const body = await response.json();
	const player = body.players.find( row => row.name.toLowerCase() === name.toLowerCase() );
	assert.ok( player, `the GameWorld reports no online ${name}` );
	// capturedAt carries 100 ns digits; Date.parse wants milliseconds.
	const capturedAtMs = Date.parse( body.capturedAt.replace( /(\.\d{3})\d+/, "$1" ) );
	assert.ok( Number.isFinite( capturedAtMs ), `observatory capturedAt ${body.capturedAt}` );
	return {
		regionId: player.region,
		x: player.x,
		y: player.y,
		z: player.z,
		hp: player.hp,
		capturedAtMs,
		fetchedAtMs: Date.now(),
		peers: crowdNames.map( name => {
			const peer = body.players.find( row => row.name.toLowerCase() === name.toLowerCase() );
			return { name, hp: peer?.hp ?? null, region: peer?.region ?? null };
		} )
	};
}

/*
================
assertStep

One settled step: same region, nothing in flight, and, for a nonzero hold,
proof that this step's own frames were delayed - the move answer on the
downlink and, in the bidirectional lane, the move command on the uplink.
================
*/
function assertStep( lane, step ) {
	const label = `${lane} ${step.ms} ms ${step.action}`;
	assert.equal( step.server.regionId, step.client.pose.regionId, `${label}: regions differ` );
	assert.ok( !step.client.moving && step.client.pendingMoves === 0, `${label}: not settled` );
	assert.ok( step.client.hp > 0, `${label}: observer must remain alive` );
	assert.ok( step.server.hp > 0, `${label}: server must report a living observer` );
	assert.ok(
		step.server.peers.every( peer => peer.hp > 0 ),
		`${label}: every crowd peer must remain online and alive`
	);
	assert.ok( step.server.capturedAtMs > step.settledAtMs, `${label}: the server snapshot predates the settle` );
	assert.ok(
		step.xz <= SETTLED_POSE_TOLERANCE && Math.abs( step.y ) <= SETTLED_POSE_TOLERANCE,
		`${label}: settled client and server poses differ: ${JSON.stringify( step )}`
	);
	if ( step.ms === 0 ) return;
	const held = ( direction, opcode ) =>
		step.held.some( entry => entry.direction === direction && entry.opcode === opcode );
	assert.ok( held( "rx", OP_PREDICTED_MOVE_RESULT ), `${label}: no move answer was held` );
	if ( lane === "bidirectional-delay" ) {
		assert.ok( held( "tx", OP_PREDICTED_MOVE ), `${label}: no move command was held` );
	}
}

/*
================
crowdEvidence

Keep the worker's peer membership separate from presented actors: camera
culling can change the latter while the real sessions remain nearby.
================
*/
function crowdEvidence( page, names ) {
	return page.evaluate( names => {
		const root = globalThis.__benchRuntime;
		const entities = root.entities(), actors = root.characterActors();
		return {
			atMs: Date.now(),
			worldClock: root.gameplay().worldClock,
			poseAtMs: root.gameplay().poseAtMs,
			peers: names.map( name => {
				const entity = entities.find( row => row.name === name );
				const actor = entity && actors.find( row => row.gid === entity.gid );
				return {
					name,
					gid: entity?.gid ?? null,
					model: actor?.model ?? null,
					clip: actor?.clip ?? null,
					lifeState: entity?.appearanceState?.[0] ?? null
				};
			} ),
			entityCount: entities.length,
			actorCount: actors.length
		};
	}, names );
}

/*
================
assertCrowd
================
*/
function assertCrowd( evidence ) {
	assert.ok( evidence.peers.every( peer => peer.gid !== null ), "every crowd peer remains in the observer's world" );
	assert.ok(
		evidence.peers.every( peer => peer.lifeState !== 2 && !/death|die/i.test( peer.clip ?? "" ) ),
		"crowd peers stay alive"
	);
}

/*
================
verifyCrowdClosed

Socket closure may leave a session in resume grace. Require the authoritative
player list to release every peer before the next test can reuse the world.
================
*/
async function verifyCrowdClosed( names, out ) {
	const owned = new Set( names.map( name => name.toLowerCase() ) );
	const deadline = Date.now() + CROWD_CLEANUP_TIMEOUT_MS;
	let evidence;
	try {
		do {
			await new Promise( resolve => setTimeout( resolve, OBSERVATORY_CACHE_MS + SETTLE_MARGIN_MS ) );
			const response = await fetch( OBSERVATORY_URL, {
				headers: { "X-SRO-Local-Diagnostics": "1" },
				signal: AbortSignal.timeout( SETTLE_TIMEOUT_MS )
			} );
			assert.ok( response.ok, `crowd cleanup observatory ${response.status}` );
			const snapshot = await response.json();
			evidence = {
				atMs: Date.now(),
				capturedAt: snapshot.capturedAt,
				expected: names.length,
				remaining: snapshot.players.filter( row => owned.has( row.name.toLowerCase() ) ).map( row => row.name )
			};
			await writeFile( `${out}/crowd-session-cleanup.json`, JSON.stringify( evidence, null, 2 ) );
			if ( evidence.remaining.length === 0 ) return;
		} while ( Date.now() < deadline );
		throw Error( `crowd sessions remain after cleanup: ${JSON.stringify( evidence.remaining )}` );
	} catch ( error ) {
		await writeFile(
			`${out}/crowd-session-cleanup.json`,
			JSON.stringify(
				{
					...evidence,
					failure: String( error?.stack ?? error )
				},
				null,
				2
			)
		);
		throw error;
	}
}

/*
================
run
================
*/
async function run( options ) {
	assert.ok( Number.isInteger( options.peers ) && options.peers >= 0, "peers must be a nonnegative integer" );
	assert.ok( options.scene === "field" || options.scene === "town", "scene must be field or town" );
	if ( options.scene === "town" ) {
		assert.ok(
			options.only.every( lane => lane === "network-delay" || lane === "bidirectional-delay" ),
			"town supports only network-delay and bidirectional-delay; field-specific paths require --scene field"
		);
	}
	const fixture = options.scene === "town" ? TOWN_FIXTURE : MISSION_MOVEMENT_FIXTURES.region_cross;
	if ( options.peers ) {
		assert.ok( options.tokenPath && options.provisioningUrl, "explicit local crowd authority required" );
	}
	await mkdir( options.out, { recursive: true } );
	const client = await openClient( fixture, {
		spans: true,
		uncapped: false,
		cpuRate: options.cpuRate,
		beforeLogin: installFaults
	} );
	const { page } = client;
	let crowd, failure;
	const crowdNames = [];
	try {
		let worker;
		for ( const candidate of page.workers() ) {
			if ( await candidate.evaluate( () => !!globalThis.__recoveryLink ) ) worker = candidate;
		}
		assert.ok( worker, "instrumented simulation worker must exist" );
		if ( options.peers ) {
			crowd = await createCrowd( {
				count: options.peers,
				fixture,
				provisioningUrl: options.provisioningUrl,
				tokenPath: options.tokenPath,
				journalPath: `${options.out}/crowd-cleanup.json`
			} ).catch( error => {
				crowdNames.push( ...(error.crowdNames ?? []) );
				throw error;
			} );
			crowdNames.push( ...crowd.peers.map( peer => peer.character ) );
			await page.waitForFunction(
				names => {
					const root = globalThis.__benchRuntime, entities = root.entities(), actors = root.characterActors();
					return names.every( name => {
						const entity = entities.find( row => row.name === name );
						return entity && actors.some( actor => actor.gid === entity.gid );
					} );
				},
				crowdNames,
				{ timeout: SETTLE_TIMEOUT_MS }
			);
			const admission = {
				...await crowdEvidence( page, crowdNames ),
				server: await serverPose( CHARACTER, crowdNames )
			};
			await writeFile(
				`${options.out}/crowd-admitted.json`,
				JSON.stringify( admission, null, 2 )
			);
			assertCrowd( admission );
			assert.ok(
				admission.server.hp > 0 && admission.server.peers.every( peer => peer.hp > 0 ),
				"observer and every crowd peer must survive setup before starting recovery"
			);
		}
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
					result.stalls = await stallTurns( page, {
						lane,
						name: CHARACTER,
						crowdNames,
						record: ( stalls, incident ) =>
							writeFile(
								`${options.out}/stall-${lane}.json`,
								JSON.stringify( { lane, partial: true, incident, stalls }, null, 2 )
							)
					} );
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
			if ( crowd ) {
				result.crowdBefore = await crowdEvidence( page, crowdNames );
				assertCrowd( result.crowdBefore );
			}
			let faultMark = 0;
			Object.assign( result, await measure( page, lane, STALL_LANES.includes( lane ) ? 90000 : 7000, drive ) );
			await captures.stop( lane );
			await captures.finish();
			if ( crowd ) {
				result.crowdAfter = await crowdEvidence( page, crowdNames );
				assertCrowd( result.crowdAfter );
				assert.ok(
					crowd.peers.every( peer => peer.ready && !peer.closed ),
					"all real crowd sessions stay connected"
				);
			}
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
				const server = await serverPose( CHARACTER, crowdNames );
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
				assert.ok(
					result.serverPose.xz <= SETTLED_POSE_TOLERANCE &&
						Math.abs( result.serverPose.y ) <= SETTLED_POSE_TOLERANCE,
					`settled client and server poses differ: ${JSON.stringify( result.serverPose )}`
				);
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
				assert.ok( !/death|die/i.test( sample.body.clip ), "recovery does not measure a dead observer" );
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
	} catch ( error ) {
		failure = error;
		await writeFile(
			`${options.out}/failure.json`,
			JSON.stringify(
				{
					error: String( error?.stack ?? error ),
					client: await movementState( page ).catch( error => String( error ) ),
					crowd: await crowdEvidence( page, crowdNames ).catch( error => String( error ) ),
					peers: crowd?.peers ?? []
				},
				null,
				2
			)
		);
		throw error;
	} finally {
		const cleanup = await Promise.allSettled( [ closeClient( client ), crowd?.close() ] );
		const errors = cleanup.filter( row => row.status === "rejected" ).map( row => row.reason );
		if ( crowdNames.length ) {
			await verifyCrowdClosed( crowdNames, options.out ).catch( error => errors.push( error ) );
		}
		if ( errors.length ) {
			throw new AggregateError( failure ? [ failure, ...errors ] : errors, "recovery cleanup failed" );
		}
	}
}

await run( parseOptions(
	process.argv.slice( 2 ),
	{
		cpuRate: 1,
		scene: "field",
		out: "temp/artifacts/recovery",
		trace: false,
		video: false,
		peers: 0,
		tokenPath: "",
		provisioningUrl: "",
		only: [ "main", "worker", "transport", ...STALL_LANES ]
	},
	"recovery-bench.mjs [--cpu-rate 4] [--out DIR] [--only main,worker,transport,network-delay,bidirectional-delay] " +
		"[--trace] [--video] [--scene field|town] [--peers N --provisioning-url URL --token-path PATH]"
) );
