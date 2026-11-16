/*
===========================================================================

crowd.mjs - authenticated development peers for live crowd acceptance

Peers use the production codec and real GameWorld sockets. Only accounts
created by this invocation are changed; cleanup closes sockets and disables
those accounts. No credentials or admission tickets enter result artifacts.
A crowd held from saved sessions (crowd-accounts.mjs) reuses connectPeer,
the login gate and the layout from here.

===========================================================================
*/
import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { createCodec, MAX_RESERVED_OPCODE } from "../../../src/engine/runtime/simulation/worker/network/codec/codec.ts";
import {
	openProbeAgentSession,
	fetchProbeSessionJson,
	mintProbeTransportAdmissionToken,
	mintProbeEnterWorldToken
} from "../../../../../scripts/lib/probeSession.mjs";
import { resetMissionMovementFixture } from "../../../../../scripts/lib/missionMovementFixture.mjs";
import { assertCharacterAllowed } from "../../../../../scripts/lib/probeCharacter.mjs";
import { parseActivity, startActivity, WALK_RADIUS } from "./crowd-activity.mjs";

// A crowd admitted here logs every peer in on every run, and the Agent
// paces logins per client address (login_limiter.go: burst 10, then one per
// 6 s), so a fresh crowd of 32 already waits (32 - 10) x 6 s. The cap keeps
// fresh crowds inside the retry budget below. It is no server limit: a crowd
// held from saved sessions (crowd-accounts.mjs) pays the logins once per
// session lifetime and is bounded only by its accounts file and the layout.
const MAX_PEERS = 32;
// The --seed a living crowd uses when none is named.
export const DEFAULT_ACTIVITY_SEED = 1;
const ADMISSION_TIMEOUT_MS = 30000;
// A full 32-peer refill is (32 - 10) x 6 s; retries beyond that are a fault.
const MAX_LOGIN_RETRIES = 40;
export const DEFAULT_RETRY_AFTER_S = 6;
// The layout (crowdLayout): peers stand PITCH apart in rows across the
// fixture's x, the first row LEAD units ahead of the observer along z, at
// least MIN_COLUMNS wide.
const CROWD_PITCH = 15, CROWD_LEAD = 35, CROWD_MIN_COLUMNS = 4;
// The native interest area (server world/interest.go): an object is visible
// when its 320-unit message block is in the 3x3 blocks around the viewer's.
// A point nearer than one block edge to the observer on both axes is in that
// neighbourhood wherever the observer stands inside its own block.
const INTEREST_BLOCK_SIZE = 320;
// Rows that fit in front of the observer with a walker's reach beyond the
// last one: LEAD + (rows - 1) x PITCH + WALK_RADIUS < INTEREST_BLOCK_SIZE.
const CROWD_MAX_ROWS = Math.ceil( (INTEREST_BLOCK_SIZE - CROWD_LEAD - WALK_RADIUS) / CROWD_PITCH );
// Columns that fit either side of the observer the same way:
// (columns - 1) / 2 x PITCH + WALK_RADIUS < INTEREST_BLOCK_SIZE.
const CROWD_MAX_COLUMNS = Math.ceil( 2 * (INTEREST_BLOCK_SIZE - WALK_RADIUS) / CROWD_PITCH );
// An outdoor region's local x and z, as the fixture reset accepts them
// (benchmark_fixture.go validBenchmarkFixtureReset); dungeon regions carry
// their own wide frame and are not bounded here.
const REGION_SIZE = 1920, DUNGEON_REGION_BIT = 0x8000;
// The client's acknowledgement cadence and its large-frame bound
// (network.ts STREAM_ACK_INTERVAL_MS, STREAM_ACK_LARGE_FRAME_BYTES).
const STREAM_ACK_INTERVAL_MS = 1000, STREAM_ACK_LARGE_FRAME_BYTES = 1 << 20;

/*
================
createStreamAck

A peer's stream position, kept and acknowledged as the client's network
owner does (network.ts acknowledge): one count a frame above the
session-internal range, told to the server at most once a second, and at
once after a frame of a megabyte or more. The server keeps every
unacknowledged ordered frame for a resume, in a window of 4 MiB a
session; a peer that never acknowledges is detached once the window
fills ("writer parked on a full resume ring"). A standing crowd takes
hours to fill it, 200 walking and chatting peers about a minute.

counted( bytes ) records one applied frame; flush() tells the server
when the position moved and the interval allows. now() is the clock.
================
*/
export function createStreamAck( send, now = () => performance.now() ) {
	let position = 0, acked = 0, sentAt = -Infinity, large = false;
	const flush = () => {
		if ( position === acked || !large && now() - sentAt < STREAM_ACK_INTERVAL_MS ) return;
		acked = position;
		sentAt = now();
		large = false;
		send( position );
	};
	return {
		counted( bytes ) {
			position = (position + 1) >>> 0;
			if ( bytes >= STREAM_ACK_LARGE_FRAME_BYTES ) large = true;
			flush();
		},
		flush
	};
}

/*
================
connectPeer
================
*/
export async function connectPeer( session, character ) {
	const codec = createCodec();
	const admission = await mintProbeTransportAdmissionToken( session );
	const enter = await mintProbeEnterWorldToken( session, character );
	const endpoint = new URL( session.transportUrl );
	endpoint.protocol = endpoint.protocol === "https:" ? "wss:" : "ws:";
	endpoint.pathname = endpoint.pathname.replace( /\/$/, "" ) + "/transport/ws";
	const socket = new WebSocket( endpoint );
	socket.binaryType = "arraybuffer";
	// dead: gids seen dying (0x3122 life channel 0 = dead 2); a crowd that
	// draws no kills of its own sees only peers die (server-tick-bench.mjs).
	// opcodes: frames received per opcode, so a scenario can count what the
	// server answered (server-tick-bench.mjs --scenario return: 0x3369 resets).
	const evidence = { character, frames: 0, ready: false, closed: false, dead: new Set(), opcodes: new Map() };
	// listen( fn ): a scenario's own reader of every decoded frame (crowd-activity.mjs).
	const listeners = [];
	const stream = createStreamAck( position => {
		if ( socket.readyState === WebSocket.OPEN ) socket.send( codec.encode( codec.streamAck( position ) ) );
	} );
	try {
		await new Promise( ( resolve, reject ) => {
			const timer = setTimeout( () => reject( Error( "Crowd peer admission timed out" ) ), ADMISSION_TIMEOUT_MS );
			const fail = message => {
				clearTimeout( timer );
				reject( Error( message ) );
			};
			socket.onopen = () => socket.send( codec.encode( codec.hello( admission ) ) );
			socket.onerror = () => fail( "Crowd peer transport failed" );
			socket.onclose = () => {
				evidence.closed = true;
				if ( !evidence.ready ) fail( "Crowd peer closed before admission" );
			};
			socket.onmessage = event => {
				try {
					const frame = codec.decode( new Uint8Array( event.data ) );
					evidence.frames++;
					evidence.opcodes.set( frame.opcode, (evidence.opcodes.get( frame.opcode ) ?? 0) + 1 );
					if (
						frame.opcode === 0x3122 && frame.payload.length === 6 && frame.payload[4] === 0 &&
						frame.payload[5] === 2
					) {
						evidence.dead.add(
							new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true )
						);
					}
					if ( frame.opcode === 2 ) {
						codec.welcome( frame.payload );
						socket.send( codec.encode( codec.enterWorld( session.divisionId, character, enter ) ) );
					} else if ( frame.opcode === 3 ) {
						socket.send( codec.encode( { opcode: 4, payload: frame.payload } ) );
						// The server's ping is the peer's clock while no frame arrives.
						stream.flush();
					} else if ( frame.opcode === 7 ) {
						assert.ok( frame.payload.length >= 9 && frame.payload[0] === 1, "Crowd EnterWorld accepted" );
						socket.send( codec.encode( { opcode: 0x3012, payload: new Uint8Array() } ) );
						evidence.ready = true;
						clearTimeout( timer );
						resolve();
					} else if ( frame.opcode === 5 ) {
						evidence.closed = true;
						fail( "Crowd peer received server goodbye" );
					}
					for ( const listener of listeners ) listener( frame );
					if ( frame.opcode > MAX_RESERVED_OPCODE ) stream.counted( frame.payload.byteLength );
				} catch ( error ) {
					fail( String( error ) );
				}
			};
		} );
		return {
			evidence,
			close: () => socket.close(),
			// A scenario's own frames after admission (server-tick-bench.mjs).
			send: ( opcode, payload ) => socket.send( codec.encode( { opcode, payload } ) ),
			listen: listener => listeners.push( listener )
		};
	} catch ( error ) {
		socket.close();
		throw error;
	}
}

/*
================
createLoginGate

The Agent refuses password logins past its per-address budget (burst 10,
one token per 6 s) with 429 and Retry-After. Logins run one at a time and
wait exactly as long as the server asks, so crowd setup spends the real
budget instead of a fixed sleep per peer, and never bypasses the limit.
maxRetries bounds the 429s one login may wait through; a pass that must
wait out the budget however long it takes passes Infinity.
================
*/
export function createLoginGate( maxRetries = MAX_LOGIN_RETRIES ) {
	let tail = Promise.resolve();
	return ( loginId, loginPassword ) => {
		const attempt = async () => {
			for ( let tries = 0;; tries++ ) {
				try {
					return await openProbeAgentSession( { loginId, loginPassword } );
				} catch ( error ) {
					if ( error?.status !== 429 || tries >= maxRetries ) throw error;
					await delay( 1000 * (error.retryAfter || DEFAULT_RETRY_AFTER_S) );
				}
			}
		};
		const result = tail.then( attempt );
		tail = result.catch( () => {} );
		return result;
	};
}

const LOOPBACK_HOSTS = [ "127.0.0.1", "localhost", "[::1]" ];

/*
================
openProvisioning

The Agent's private account API (provisioning.go), loopback only, with the
local authority token. Returns request( path, method, body ), which answers
the parsed JSON body (null for none) and throws an error carrying the HTTP
status and the API's error code.
================
*/
export async function openProvisioning( provisioningUrl, tokenPath ) {
	const url = new URL( provisioningUrl );
	assert.ok( url.protocol === "http:" && LOOPBACK_HOSTS.includes( url.hostname ), "Provisioning must be loopback" );
	const token = (await readFile( tokenPath, "utf8" )).trim();
	return async ( path, method, body ) => {
		const response = await fetch( new URL( path, url ), {
			method,
			headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
			body: body === undefined ? undefined : JSON.stringify( body ),
			signal: AbortSignal.timeout( ADMISSION_TIMEOUT_MS )
		} );
		const answer = await response.json().catch( () => null );
		if ( !response.ok ) {
			throw Object.assign( Error( `Crowd provisioning ${method} failed: HTTP ${response.status}` ), {
				status: response.status,
				code: answer?.code
			} );
		}
		return answer;
	};
}

/*
================
crowdLayout

Where each of count peers stands: rows of columns PITCH apart, centred on
the observer (the fixture's start) across x, the first row LEAD ahead
along z. A crowd grows deeper up to CROWD_MAX_ROWS, then wider, so every
spot and every walker's reach stays within one interest block edge of the
observer: the server sends the observer every peer (18 x 40, 720 peers,
is the most that fits). The layout depends on count alone, so a held
crowd of N stands exactly where an admitted crowd of N would. Throws when
the crowd cannot fit, or an outdoor spot would leave the fixture's region.
================
*/
export function crowdLayout( fixture, count ) {
	const start = fixture.start;
	const columns = Math.max( CROWD_MIN_COLUMNS, Math.ceil( count / CROWD_MAX_ROWS ) );
	if ( columns > CROWD_MAX_COLUMNS ) {
		throw Error(
			`a crowd of ${count} does not fit the observer's interest area (at most ${
				CROWD_MAX_ROWS * CROWD_MAX_COLUMNS
			})`
		);
	}
	const half = (columns - 1) / 2 * CROWD_PITCH + WALK_RADIUS;
	const spots = Array.from( { length: count }, ( _, index ) => ({
		...start,
		x: start.x + (index % columns - (columns - 1) / 2) * CROWD_PITCH,
		z: start.z + CROWD_LEAD + Math.floor( index / columns ) * CROWD_PITCH
	}) );
	if ( !(start.regionId & DUNGEON_REGION_BIT) ) {
		const far = start.z + CROWD_LEAD + Math.floor( (count - 1) / columns ) * CROWD_PITCH + WALK_RADIUS;
		const near = start.z + CROWD_LEAD - WALK_RADIUS;
		if ( start.x - half < 0 || start.x + half >= REGION_SIZE || near < 0 || far >= REGION_SIZE ) {
			throw Error(
				`a crowd of ${count} at ${fixture.id} leaves region ${start.regionId}: ` +
					`x ${start.x - half}..${start.x + half}, z ${near}..${far}`
			);
		}
	}
	return spots;
}

/*
================
createCrowd

Provisioning is explicitly loopback-only and requires the existing local
authority token. Scratch actors form a grid inside the observer's sector.
loadoutFor( index ), when given, names each peer's level, intellect and
skills (server-tick-bench.mjs). act and seed, when given, start a scripted
activity once every peer is in (crowd-activity.mjs); crowd.activity is its
evidence, and close() stops it before the sockets close.
================
*/
export async function createCrowd(
	{ count, fixture, provisioningUrl, tokenPath, journalPath, loadoutFor, act = "", seed = DEFAULT_ACTIVITY_SEED }
) {
	assert.ok(
		Number.isInteger( count ) && count > 0 && count <= MAX_PEERS,
		`a fresh crowd has 1 to ${MAX_PEERS} peers; hold a larger one from an accounts file (crowd-provision.mjs)`
	);
	const provision = await openProvisioning( provisioningUrl, tokenPath );
	const accounts = [], peers = [], characters = [], byIndex = new Map();
	const prefix = randomBytes( 3 ).toString( "hex" );
	const login = createLoginGate();
	// Parsed before any account exists: a bad --act or a crowd that does not
	// fit must not cost logins.
	const activity = parseActivity( act );
	const spots = crowdLayout( fixture, count );
	let running = null;
	/*
	================
	close
	================
	*/
	async function close() {
		running?.stop();
		for ( const peer of peers ) peer.close();
		const results = await Promise.allSettled(
			accounts.map( id => provision( `/v1/accounts/${id}/disabled`, "PUT", { disabled: true } ) )
		);
		const failures = results.filter( result => result.status === "rejected" );
		await writeFile(
			journalPath,
			JSON.stringify(
				{
					accounts: accounts.map( ( id, index ) => ({
						id,
						character: characters[index],
						disabled: results[index].status === "fulfilled"
					}) )
				},
				null,
				2
			)
		);
		if ( failures.length ) {
			throw new AggregateError(
				failures.map( result => result.reason ),
				`Crowd cleanup failed for ${failures.length} accounts; see ${journalPath}`
			);
		}
	}
	/*
	================
	journal

	Rewrites the cleanup journal; writes are chained so concurrent
	admissions never interleave two writes of the same file.
	================
	*/
	let journaled = Promise.resolve();
	const journal = () => {
		const snapshot = JSON.stringify(
			{ accounts: accounts.map( ( id, index ) => ({ id, character: characters[index], disabled: false }) ) },
			null,
			2
		);
		journaled = journaled.then( () => writeFile( journalPath, snapshot ) );
		return journaled;
	};
	/*
	================
	admit

	One peer end to end. Everything but the password login runs
	concurrently with the other peers; the login goes through the gate.
	================
	*/
	const admit = async index => {
		const id = `perf${prefix}${index}`, password = randomBytes( 24 ).toString( "hex" );
		const character = assertCharacterAllowed( `P${prefix}${index}`, { context: "live crowd fixture" } );
		await provision( "/v1/accounts", "POST", { id, password } );
		accounts.push( id );
		characters.push( character );
		await journal();
		const session = await login( id, password );
		await fetchProbeSessionJson( session, "/character/create", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify( {
				characterName: character,
				modelCodename: "CHAR_CH_MAN_ADVENTURER",
				heightIndex: 0,
				volumeIndex: 0,
				weaponIndex: 1,
				protectorIndex: 0,
				armorSelected: false,
				weaponSelected: true
			} )
		} );
		await resetMissionMovementFixture( {
			session,
			characterName: character,
			timeoutMs: ADMISSION_TIMEOUT_MS,
			fixture: {
				...fixture,
				// A load scenario gives each peer its own learned skills.
				...(loadoutFor ? { loadout: loadoutFor( index ) } : {}),
				start: spots[index]
			}
		} );
		const peer = await connectPeer( session, character );
		peers.push( peer );
		byIndex.set( index, peer );
		console.log( `[crowd] admitted ${peers.length}/${count}` );
	};
	try {
		// Settle every admission before judging: a rejected one must not
		// leave others still creating accounts behind close().
		const settled = await Promise.allSettled( Array.from( { length: count }, ( _, index ) => admit( index ) ) );
		const failed = settled.filter( result => result.status === "rejected" );
		if ( failed.length ) throw failed[0].reason;
		const crowd = {
			peers: [ ...byIndex.keys() ].sort( ( a, b ) => a - b ).map( index => byIndex.get( index ).evidence ),
			close,
			// send( index, opcode, payload ) writes one frame as peer index.
			send: ( index, opcode, payload ) => byIndex.get( index )?.send( opcode, payload ),
			listen: ( index, listener ) => byIndex.get( index )?.listen( listener )
		};
		running = startActivity( {
			crowd,
			count,
			activity,
			seed,
			spotFor: index => spots[index]
		} );
		crowd.activity = running.evidence;
		return crowd;
	} catch ( error ) {
		let failure = error instanceof Error ? error : new Error( String( error ) );
		try {
			await close();
		} catch ( cleanup ) {
			failure = new AggregateError( [ failure, cleanup ], "crowd setup and cleanup failed" );
		}
		// The caller never receives a crowd on partial admission. Retain its
		// attempted names so it can verify those sessions left the server too.
		throw Object.assign( failure, { crowdNames: [ ...characters ] } );
	}
}

// ============================================================================

// The loopback crowd host (bench/crowd-host.mjs) listens here by default.
export const CROWD_HOST_URL = "http://127.0.0.1:8796";
const CROWD_POLL_MS = 250;

/*
================
crowdKey

What a held crowd must match for a bench to use it: peer count, fixture
and its start, since the grid is laid out from the start position, and
the activity with its seed, so a standing and a living crowd never stand
in for each other.
================
*/
export function crowdKey( count, fixture, act = "", seed = DEFAULT_ACTIVITY_SEED ) {
	const activity = parseActivity( act );
	return JSON.stringify( {
		count,
		id: fixture.id,
		start: fixture.start,
		act: activity.label,
		seed: activity.label === "still" ? null : seed
	} );
}

/*
================
attachCrowd

Uses a crowd the host already holds instead of admitting a new one. Returns
null when no host answers or it holds another crowd. peers is refreshed
from the host while attached, so a peer that drops during a window shows
closed by the next check; close() detaches and leaves the crowd to the host.
activity is the host's activity evidence, refreshed the same way; source
says how the host came by the crowd (fresh admission or an accounts file).
================
*/
export async function attachCrowd(
	{ count, fixture, act = "", seed = DEFAULT_ACTIVITY_SEED, hostUrl = CROWD_HOST_URL }
) {
	const url = new URL( "/crowd", hostUrl );
	assert.ok( LOOPBACK_HOSTS.includes( url.hostname ), "Crowd host must be loopback" );
	let held;
	try {
		const response = await fetch( url, { signal: AbortSignal.timeout( 2000 ) } );
		if ( !response.ok ) return null;
		held = await response.json();
	} catch {
		return null;
	}
	if ( held.key !== crowdKey( count, fixture, act, seed ) ) {
		console.log( `[crowd] host holds ${held.key}, not this crowd; admitting a new crowd` );
		return null;
	}
	const peers = held.peers, activity = held.activity;
	assert.ok( peers.length === count && peers.every( peer => peer.ready && !peer.closed ), "Held crowd is not whole" );
	assert.ok( activity, "Held crowd names no activity" );
	let polling = true;
	const poll = async () => {
		while ( polling ) {
			await delay( CROWD_POLL_MS );
			try {
				const response = await fetch( url, { signal: AbortSignal.timeout( 2000 ) } );
				const body = await response.json(), fresh = body.peers;
				for ( let i = 0; i < peers.length; i++ ) Object.assign( peers[i], fresh[i] ?? { closed: true } );
				Object.assign( activity, body.activity );
			} catch {
				// A host that stopped answering holds nothing a bench can trust.
				for ( const peer of peers ) peer.closed = true;
			}
		}
	};
	poll();
	console.log( `[crowd] attached to the held crowd of ${count} at ${hostUrl}` );
	return {
		peers,
		activity,
		source: held.source ?? null,
		// The host owns these sessions: a bench must not wait for them to
		// leave the server or disable their accounts.
		held: true,
		close: async () => {
			polling = false;
		}
	};
}

/*
================
obtainCrowd

A held crowd when one matches, else a newly admitted one. Either result
has the same { peers, activity, close } shape; a held one also says
held: true.
================
*/
export async function obtainCrowd( options ) {
	return (await attachCrowd( options )) ?? createCrowd( options );
}
