/*
===========================================================================

world.ts - transport admission, reconnect and departure for one world session

The network deadline covers tickets, references and native synchronization.
It is a stall deadline (#1010): arriving frames and reference bytes extend
it by its window, and only a full window of silence fails the attempt, so a
slow link that keeps delivering enters. Progress extends the deadline only
up to a bounded ceiling from the arm (DEADLINE_MAX_WINDOWS windows): a peer
that proves the wire with keepalive pings while building nothing is wedged,
not slow, and must still fail into the timeout path (#1471 review).
The presentation acknowledgement is a separate barrier: it gates commands,
never retries a successfully received world.
A transient failure before the world binds retries the admission inside
the same grace a bound session resumes in; only an exhausted grace
rejects the entry.

===========================================================================
*/
import { isWorldControl } from "@/engine/foundation/gameplay/commerce-controls";
import { createNetwork, outOfMemoryFailure } from "@/engine/runtime/simulation/worker/network/network";
import { createWorldCore } from "./core";
import { createDeparture } from "./departure";
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import {
	type ClientIncident,
	DEGRADED_CODES_PER_SESSION,
	INVENTORY_TRANSACTION_TIMEOUT,
	MOVEMENT_RECEIPT_TIMEOUT,
	TARGET_RELEASE_TIMEOUT,
	TRANSPORT_STALLED,
	type NetworkFailure,
	INCIDENT_DUMP_BYTES,
	INCIDENT_MESSAGE_LENGTH,
	type WireFrame
} from "@/engine/contracts/network";
const ADMISSION_TIMEOUT_MS = 10000;
const REFERENCE_TIMEOUT_MS = 30000;
// Progress may slide a deadline this many windows past its arm, no further:
// eighteen admission windows is three minutes (nine for references), long
// enough never to cut a healthy slow entry, finite enough that a wedged
// one still times out and retries instead of loading forever.
const DEADLINE_MAX_WINDOWS = 18;
const RECONNECT_DELAY_MS = 250;
const MAX_RECONNECT_DELAY_MS = 4000;
// A request waiting this long with no frame of any kind arriving since it
// left means the link stalled, not the request: the client pings every 5 s
// (network.ts PING_INTERVAL_MS) and a live link answers within the round
// trip. Prod's 125 movement receipt timeouts (#1418) were such stalls,
// found only by the lane's 10 s watchdog.
const TRANSPORT_STALL_MS = 7000;
// The stall must still hold this much later: after a frozen tab the browser
// can deliver the frames that arrived meanwhile only after a step has run.
const TRANSPORT_STALL_CONFIRM_MS = 1000;
// The Agent keeps a detached session resumable for its GracePeriod (30 s,
// apps/server/internal/transport/config.go). Retry for all of it: a laptop
// waking or a Wi-Fi hand-over outlasts a few fast attempts.
const RESUME_GRACE_MS = 30000;
const MAX_CHARACTER_NAME_LENGTH = 64;

/*
================
lostAnswer

The timeouts that mean an answer was lost across the transport (doc.go
keeps no frame written to a dying socket), not a game error: each resumes
the session, whose bootstrap retires the request without replaying it.
================
*/
function lostAnswer( cause: unknown ): boolean {
	return cause === MOVEMENT_RECEIPT_TIMEOUT || cause === INVENTORY_TRANSACTION_TIMEOUT ||
		cause === TARGET_RELEASE_TIMEOUT || cause === TRANSPORT_STALLED;
}

/*
================
createWorldSession
================

*/

export function createWorldSession(
	mint: ( kind: "transport" | "enterworld", character: string, signal: AbortSignal ) => Promise<string>,
	loadWorldReferences: (
		value: unknown,
		base: string,
		signal: AbortSignal,
		progress?: () => void
	) => Promise<
		{
			refSkillSnapshot: unknown[];
			refItemSnapshot: unknown[];
			refObjSnapshot: unknown[];
			huntingGuide?: import("@/engine/contracts/hunting-guide").HuntingGuideSource;
		}
	>,
	reportIncident: ( incident: ClientIncident ) => void
) {
	let failure: string | null = null, epoch = 0, disposed = false, controller: AbortController | null = null;
	let diagnosticSession = "", disconnectMessage = "", incidentID = "", failureCode = "";
	// Degraded codes reported, keyed "<diagnostic session>|<code>" (reportDegradations).
	const reportedDegradations = new Set<string>();
	/*
================
reportDegradations

The owners' skipped cosmetic steps as non-fatal "degraded" reports, with the
same evidence as a fatal one (the frame and the #800 journal). Each code is
reported once per diagnostic session, and at most DEGRADED_CODES_PER_SESSION
codes: one bad item cannot flood the Agent.
================
	*/
	function reportDegradations() {
		for ( const degradation of core.takeDegradations() ) {
			const key = diagnosticSession + "|" + degradation.code;
			if ( reportedDegradations.has( key ) ) continue;
			let codes = 0;
			for ( const reported of reportedDegradations ) {
				if ( reported.startsWith( diagnosticSession + "|" ) ) codes++;
			}
			if ( codes >= DEGRADED_CODES_PER_SESSION ) continue;
			reportedDegradations.add( key );
			const frame = degradation.frame, dump = frame?.payload.subarray( 0, INCIDENT_DUMP_BYTES );
			reportIncident( {
				id: crypto.randomUUID().replaceAll( "-", "" ),
				category: "degraded",
				session: diagnosticSession || undefined,
				code: degradation.code,
				kind: "degraded",
				message: degradation.message.slice( 0, INCIDENT_MESSAGE_LENGTH ),
				...(frame ?
					{
						opcode: frame.opcode,
						payload: Array.from( dump!, byte => byte.toString( 16 ).padStart( 2, "0" ) ).join( "" ),
						payloadSize: frame.payload.byteLength
					} :
					{}),
				phase,
				character,
				region: core.region(),
				journal: network.recentJournal()
			} );
		}
	}
	/*
================
onNetworkFailure
================
	*/
	function onNetworkFailure( error: string, frame?: WireFrame, reason?: NetworkFailure ) {
		failure = error;
		failureCode = reason?.code ?? "";
		disconnectMessage = reason?.message ?? "A game error interrupted your session.";
		if ( reason?.category === "expected" ) {
			incidentID = "";
			return;
		}
		incidentID = crypto.randomUUID().replaceAll( "-", "" );
		// The socket closes on the client side, so the server only sees a
		// disconnect. Report what failed and on which frame (client_incident.go).
		const dump = frame?.payload.subarray( 0, INCIDENT_DUMP_BYTES );
		reportIncident( {
			id: incidentID,
			stack: reason?.stack,
			category: reason?.category,
			session: diagnosticSession || undefined,
			code: reason?.code ?? (frame ? "packet_application_failed" : "connection_closed"),
			kind: reason?.code === "unsupported_feature" ?
				"unsupported" :
				reason?.code === "reference_loading_failed" ?
				"asset" :
				reason?.code === "out_of_memory" ?
				"runtime" :
				frame ?
				"packet" :
				reason?.category === "software" ?
				"runtime" :
				"transport",
			// The detail rides only in the report: the retry rule matches the bare error.
			message: (reason?.detail ? error + " (" + reason.detail + ")" : error).slice( 0, INCIDENT_MESSAGE_LENGTH ),
			...(frame ?
				{
					opcode: frame.opcode,
					payload: Array.from( dump!, byte => byte.toString( 16 ).padStart( 2, "0" ) ).join( "" ),
					payloadSize: frame.payload.byteLength
				} :
				{}),
			phase,
			character,
			region: core.region(),
			journal: network.recentJournal()
		} );
	}
	const network = createNetwork( onNetworkFailure );
	let pingMs: number | null = null;
	const core = createWorldCore( network.send );
	const departure = createDeparture( network.send, core.notice );
	let completedDeparture: 1 | 2 | 0 = 0;
	let phase: "disconnected" | "connecting" | "entering-world" | "world" | "reconnecting" = "disconnected";
	let character = "", division = "", transport = "", endpoint = "", resume: Uint8Array | undefined;
	let completion: {
		epoch: number;
		kind: "transport" | "enterworld";
		token?: string;
		error?: string;
	} | null = null;
	let boundThisTransport = false, resumedTransport = false, entryAttempt = false;
	let references: { kind: "idle"; } | { kind: "loading"; } | {
		kind: "ready";
		bootstrap: unknown;
		huntingGuide?: import("@/engine/contracts/hunting-guide").HuntingGuideSource;
		receivedAtMs: number;
	} | {
		kind: "failed";
		error: string;
	} = { kind: "idle" };
	let lastError: string | undefined;
	// The stall watch: the oldest outstanding request's send time (step
	// clock), the event-clock moment that request was first seen
	// outstanding, and when the current stall was first seen. The world
	// keeps no last-inbound time of its own (#1474): a frame proves the
	// link alive after a request only on the network's clock, never by
	// the step that happened to sample it, so a frozen tab's backlog is
	// not dated fresh.
	let stallSince: number | undefined, stallBaseAt = -Infinity;
	let stallSeenAtMs: number | undefined;
	let huntingGuide: import("@/engine/contracts/hunting-guide").HuntingGuideSource | undefined;
	let now = 0,
		deadline = 0,
		deadlineWindow = 0,
		deadlineCeiling = 0,
		inboundSeen = 0,
		retryAt = 0,
		attempt = 0,
		reconnectSince = 0,
		hasWorld = false,
		ready = false,
		revision = 0;
	/*
================
transition
================
	*/
	function transition( next: typeof phase ) {
		phase = next;
		revision++;
	}
	/*
================
armDeadline

Arm the admission deadline as a stall window of windowMs: noteProgress
extends it, and only a full window without progress fails the attempt.
The arm also fixes the progress ceiling: progress may slide the deadline
at most DEADLINE_MAX_WINDOWS windows past this moment, never indefinitely.
================
	*/
	function armDeadline( windowMs: number ) {
		deadlineWindow = windowMs;
		deadline = now + windowMs;
		deadlineCeiling = now + windowMs * DEADLINE_MAX_WINDOWS;
	}
	/*
================
noteProgress

A frame on the socket or a chunk of the reference body proves the link is
still delivering; push the armed deadline out by its window, but never past
the ceiling its arm fixed. A keepalive-only wire therefore still times out
once the ceiling passes. A cleared deadline stays cleared (the presentation
barrier owns that wait).
================
	*/
	function noteProgress() {
		if ( deadline ) deadline = Math.min( now + deadlineWindow, deadlineCeiling );
	}
	/*
================
inboundAge

How long ago the last frame arrived, for the lost-answer incidents: the
network owner's event-clock stamp (receivedAt), so this age and the
close detail's silence are the same number in two units (#1474).
================
	*/
	function inboundAge(): string {
		const at = network.receivedAt();
		return Number.isFinite( at ) ?
			`last inbound ${Math.max( 0, Math.round( performance.now() - at ) )} ms ago` :
			"no frame received yet";
	}
	/*
================
stepStall

A request has waited TRANSPORT_STALL_MS with nothing at all arriving since
it left, and still so TRANSPORT_STALL_CONFIRM_MS later: the link stalled.
Resume now (the lost-answer path), naming the link instead of the request.
================
	*/
	function stepStall() {
		const since = core.outstandingSince();
		if ( since === undefined ) {
			stallSince = undefined;
			stallSeenAtMs = undefined;
			return;
		}
		// A newly oldest request bases its watch now, on the event clock:
		// frames already delivered (entry's setup frames, a frozen tab's
		// backlog, one landing in the request's own step) left before it
		// did, however late a step notices them (#1460, #1474).
		if ( stallSince !== since ) {
			stallSince = since;
			stallBaseAt = performance.now();
		}
		// Alive only if a frame arrived after the request left (strictly,
		// on the network's event clock).
		if ( now - since < TRANSPORT_STALL_MS || network.receivedAt() > stallBaseAt ) {
			stallSeenAtMs = undefined;
			return;
		}
		stallSeenAtMs ??= now;
		if ( now - stallSeenAtMs < TRANSPORT_STALL_CONFIRM_MS ) return;
		stallSeenAtMs = undefined;
		onNetworkFailure(
			`Transport stalled: a request waited ${
				Math.round( now - since )
			} ms with no frame arriving; ${inboundAge()}`,
			undefined,
			{
				category: "unknown",
				code: TRANSPORT_STALLED,
				message: "The connection stalled. Reconnecting..."
			}
		);
	}
	// Cancelling transport work does not revoke the admitted world snapshot.
	// Only logout/fresh admission may clear equipment, identity and HUD data.
	/*
================
cancel
================
	*/
	function cancel() {
		epoch++;
		controller?.abort();
		controller = null;
		completion = null;
		references = { kind: "idle" };
		network.disconnect();
	}
	/*
================
request
================
	*/
	function request( kind: "transport" | "enterworld" ) {
		const current = epoch;
		controller = new AbortController();
		armDeadline( ADMISSION_TIMEOUT_MS );
		mint( kind, character, controller.signal ).then( token => {
			if ( !disposed && epoch === current ) {
				completion = { epoch: current, kind, token };
			}
		}, error => {
			if ( !disposed && epoch === current ) {
				completion = {
					epoch: current,
					kind,
					error: error instanceof Error && error.message === "Authentication required" ?
						error.message :
						"Admission request failed"
				};
			}
		} );
	}
	/*
================
connect
================
	*/
	function connect() {
		cancel();
		boundThisTransport = false;
		resumedTransport = false;
		failure = null;
		failureCode = "";
		disconnectMessage = "";
		incidentID = "";
		lastError = undefined;
		transition( hasWorld ? "reconnecting" : "connecting" );
		request( "transport" );
	}
	/*
================
receive
================
	*/
	function receive( frame: WireFrame ) {
		// Keep native brackets in the network owner's bounded FIFO until the
		// matching reference resource has passed integrity and schema ingress.
		// A queued head frame is re-presented on every step, so it must not
		// note progress: reference bytes alone prove the link during a load.
		if ( references.kind !== "idle" ) return false;
		noteProgress();
		if ( frame.opcode === 2 ) {
			// Network owner has already checked the exact v2 WELCOME layout.
			const resumed = frame.payload[1] === 1;
			if ( hasWorld && !resumed ) {
				// The server's resume grace ended before this client came back
				// (a background tab can wake too late). The session is over:
				// an expected end the player is told about, not a game error.
				onNetworkFailure( "Server could not resume the previous world session", undefined, {
					category: "expected",
					code: "resume_expired",
					message: "Your session ended while the connection was gone. Please log in again."
				} );
				// false stops the drain, as the old throw did: no later frame of
				// this batch reaches an ended session.
				return false;
			}
			resumedTransport = resumed && hasWorld;
			resume = frame.payload.slice( 11 );
			transition( "entering-world" );
			request( "enterworld" );
			return;
		}
		if ( frame.opcode === 7 ) {
			if ( phase !== "entering-world" ) {
				throw new Error( "Unexpected EnterWorld result" );
			}
			const p = frame.payload;
			if ( p.length < 9 ) {
				throw new Error( "Truncated EnterWorld result" );
			}
			const view = new DataView( p.buffer, p.byteOffset, p.byteLength ), length = view.getUint32( 5, true );
			if ( p.length !== 9 + length || p[0] !== 1 ) {
				throw new Error( `EnterWorld rejected: ${view.getUint32( 1, true )}` );
			}
			const wrapper = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 9 ) ) ) as {
				v?: number;
				bootstrap?: unknown;
				references?: unknown;
			};
			const sessionKey = (wrapper.bootstrap as { diagnosticSessionId?: unknown; } | undefined)
				?.diagnosticSessionId;
			if ( typeof sessionKey === "string" && /^[a-f0-9]{32}:[0-9]{1,20}$/.test( sessionKey ) ) {
				diagnosticSession = sessionKey;
			}
			if ( wrapper.v === 2 ) {
				if (
					!wrapper.bootstrap || typeof wrapper.bootstrap !== "object" || Array.isArray( wrapper.bootstrap ) ||
					"refSkillSnapshot" in wrapper.bootstrap
				) throw Error( "Invalid reference bootstrap" );
				const bootstrap = wrapper.bootstrap, current = epoch, receivedAtMs = now;
				controller = new AbortController();
				references = { kind: "loading" };
				armDeadline( REFERENCE_TIMEOUT_MS );
				loadWorldReferences( wrapper.references, transport, controller.signal, noteProgress ).then( refs => {
					if ( !disposed && epoch === current ) {
						// The published item and object rows and the login's own rows are
						// disjoint (the server skips published ids); the catalogs reject a repeat.
						const { huntingGuide: guide, ...tables } = refs;
						const own = bootstrap as { refItemSnapshot?: unknown[]; refObjSnapshot?: unknown[]; };
						references = {
							kind: "ready",
							receivedAtMs,
							huntingGuide: guide,
							bootstrap: {
								...bootstrap,
								...tables,
								refItemSnapshot: [ ...refs.refItemSnapshot, ...(own.refItemSnapshot ?? []) ],
								refObjSnapshot: [ ...refs.refObjSnapshot, ...(own.refObjSnapshot ?? []) ]
							}
						};
					}
				}, error => {
					if ( !disposed && epoch === current ) references = { kind: "failed", error: String( error ) };
				} );
				return;
			}
			if ( wrapper.v !== 1 ) {
				throw new Error( "Unsupported EnterWorld blob version", { cause: "unsupported_feature" } );
			}
			huntingGuide = undefined;
			core.bootstrap( wrapper.bootstrap, resumedTransport, now );
			boundThisTransport = true;
			hasWorld = true;
			ready = false;
			armDeadline( ADMISSION_TIMEOUT_MS );
			return;
		}
		if ( frame.opcode === 10 ) {
			if ( boundThisTransport ) {
				core.receive( frame, now );
			}
			return;
		}
		if ( isWorldControl( frame.opcode ) ) {
			// A resumed transport replays the frames the old one lost, which
			// can precede its EnterWorld result (a purchase's item reference
			// delta). Native packets are admitted there too; the world they
			// apply to is the one the resume keeps.
			if ( !boundThisTransport && !resumedTransport ) throw Error( "World control packet before EnterWorld" );
			core.receive( frame, now );
			return;
		}
		if ( frame.opcode <= 255 ) {
			if ( frame.opcode === 4 ) {
				return;
			}
			throw new Error( `Unsupported transport control ${frame.opcode}`, { cause: "unsupported_feature" } );
		}
		if ( !boundThisTransport && !resumedTransport ) {
			throw new Error( `Native packet 0x${frame.opcode.toString( 16 )} before EnterWorld` );
		}
		const departed = departure.receive( frame, now );
		if ( departed !== null ) {
			if ( departed ) {
				completedDeparture = departed;
				cancel();
				core.clear();
				hasWorld = false;
				ready = false;
				resume = undefined;
				deadline = 0;
				retryAt = 0;
				failure = null;
				transition( "disconnected" );
			}
			return;
		}
		core.receive( frame, now );
		// The complete object bracket ends network admission. Presentation
		// can take longer (cold assets, a background tab, or a slow GPU). Its
		// acknowledgement still gates world commands, but must not reconnect
		// a healthy socket and enqueue another complete bootstrap.
		if ( frame.opcode === 0x330a && boundThisTransport ) deadline = 0;
		if ( frame.opcode === 0x3369 || frame.opcode === 0x366a ) {
			if ( frame.opcode === 0x3369 ) network.send( { opcode: 0x36dd, payload: new Uint8Array() } );
			ready = false;
			transition( "entering-world" );
			armDeadline( ADMISSION_TIMEOUT_MS );
		}
	}
	return {
		/*
================
enter
================
		*/
		enter( name: string, shard: string, base: string ) {
			if ( disposed || !name || name.length > MAX_CHARACTER_NAME_LENGTH ) {
				throw new Error( "Invalid character selection" );
			}
			cancel();
			departure.reset();
			completedDeparture = 0;
			core.clear();
			hasWorld = false;
			resume = undefined;
			entryAttempt = true;
			attempt = 0;
			ready = false;
			character = name;
			division = shard;
			// The transport base (possibly an edge route) serves the socket and its references.
			// An advertised base may already name the socket itself (the public WebTransport
			// posture's advertised URL ends /transport/ws): normalise once here, so the
			// references base never carries the socket suffix and the socket path is
			// never doubled.
			const url = new URL( base );
			const advertised = url.pathname.replace( /\/$/, "" );
			const basePath = advertised.endsWith( "/transport/ws" ) ?
				advertised.slice( 0, advertised.length - "/transport/ws".length ) :
				advertised;
			url.pathname = basePath || "/";
			transport = url.toString();
			url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
			url.pathname = basePath + "/transport/ws";
			endpoint = url.toString();
			connect();
		},
		chatBlocks: core.chatBlocks,
		/*
================
depart
================
		*/
		depart( type: 1 | 2 ) {
			if ( phase !== "world" || !ready ) throw Error( "World gameplay is not ready" );
			departure.request( type );
		},
		/*
================
takeDeparture
================
		*/
		takeDeparture() {
			const value = completedDeparture;
			completedDeparture = 0;
			return value;
		},
		options: core.options,
		experimental( value: import("@/engine/foundation/ui/experimental-options").ExperimentalGameplay ) {
			network.webTransport( value.webTransport );
			core.experimental( value );
		},
		/*
================
command
================
		*/
		command( command: GameplayCommand, at = now, inputSeq?: number ) {
			// Record the input as taken before any gate below can drop it
			// (#1441a): a click refused while loading or reconnecting is still
			// an input the main thread must not replay.
			if ( inputSeq !== undefined ) core.noteInput( inputSeq );
			if ( phase !== "world" || (!ready && command.kind !== "navigation") ) {
				throw new Error( "World gameplay is not ready" );
			}
			// The countdown belongs to this session; a ground click cancels it
			// (CGInterface_MoveToWorldPoint sends 0x731F while +0x39C is set).
			const admitted = command.kind === "ground-move" && departure.pending() ?
				{ ...command, departing: true } :
				command;
			// Commands arrive between fixed steps. Reusing the previous tick
			// starts a turn behind the pose already drawn on the frame clock.
			now = Math.max( now, at );
			core.command( admitted, now );
		},
		/*
================
ready
================
		*/
		ready( travelRevision = core.readyRevision() ) {
			if ( travelRevision !== core.readyRevision() ) return;
			if ( phase !== "world" ) {
				throw new Error( "World is not bound" );
			}
			if ( !ready ) {
				network.send( { opcode: 0x3012, payload: new Uint8Array() } );
				ready = true;
				core.travelReady();
			}
		},
		/*
================
reconnect
================
		*/
		reconnect() {
			if ( disposed || !hasWorld || !resume ) {
				throw new Error( "No resumable world" );
			}
			attempt = 0;
			retryAt = 0;
			connect();
		},
		/*
================
disconnect
================
		*/
		disconnect( forget = false ) {
			cancel();
			departure.reset();
			completedDeparture = 0;
			entryAttempt = false;
			if ( forget ) {
				diagnosticSession = "";
				disconnectMessage = "";
				incidentID = "";
				if ( hasWorld ) {
					core.clear();
				}
				hasWorld = false;
				resume = undefined;
			}
			deadline = 0;
			retryAt = 0;
			failure = null;
			transition( "disconnected" );
		},
		/*
================
step
================
		*/
		step( time: number ) {
			// A command may already have advanced this clock while an overdue
			// fixed tick was queued. Catch-up must never rewind that command.
			now = Math.max( now, time );
			if ( disposed ) {
				return;
			}
			if ( retryAt && now >= retryAt ) {
				retryAt = 0;
				connect();
			}
			if ( completion ) {
				const result = completion;
				completion = null;
				controller = null;
				if ( result.epoch === epoch ) {
					if ( result.error ) {
						failure = result.error;
					} else {
						try {
							if ( result.kind === "transport" ) {
								network.connect( endpoint, result.token!, resume );
							} else {
								network.enterWorld( division, character, result.token! );
							}
							armDeadline( ADMISSION_TIMEOUT_MS );
						} catch ( error ) {
							failure = String( error );
						}
					}
				}
			}
			if ( references.kind === "ready" ) {
				const { bootstrap, receivedAtMs, huntingGuide: guide } = references;
				references = { kind: "idle" };
				controller = null;
				try {
					core.bootstrap( bootstrap, resumedTransport, receivedAtMs );
					huntingGuide = guide;
					boundThisTransport = true;
					hasWorld = true;
					ready = false;
					armDeadline( ADMISSION_TIMEOUT_MS );
				} catch ( error ) {
					onNetworkFailure( String( error ), undefined, {
						category: "software",
						code: "bootstrap_invalid",
						message: "Game data could not be applied. Please try again.",
						stack: error instanceof Error ? error.stack?.slice( 0, 8192 ) : undefined
					} );
				}
			} else if ( references.kind === "failed" ) {
				onNetworkFailure( references.error, undefined, {
					category: "unknown",
					code: "reference_loading_failed",
					message: "Game data could not be loaded. Please try again."
				} );
				references = { kind: "idle" };
			}
			if ( !failure ) network.drain( receive );
			// Transport liveness is progress too. A loading scene is silent by
			// design (the server is building its entry), so the server pings it
			// on the keepalive's loading cadence and the network owner answers
			// without surfacing a frame: the only sign here is the inbound
			// count moving. A wire that still answers is not stalled, and its
			// pings slide the window - up to the arm's ceiling, past which the
			// entry is wedged, not slow, and fails the attempt (#1417, #1471).
			// The bytes count too: the EnterWorld answer is one ~1 MB record
			// (0x0007) that yields no frame until its last byte lands, so a
			// link still streaming it must slide the window exactly as a
			// finished frame does (#2311).
			const activity = network.received() + network.receivedBytes();
			if ( activity !== inboundSeen ) {
				inboundSeen = activity;
				noteProgress();
			}
			const nextPing = !failure && phase === "world" ? network.pingMs() : null;
			if ( nextPing !== pingMs ) {
				pingMs = nextPing;
				revision++;
			}
			// The world advances only once the scene is ready (0x3012 sent). On
			// the loading screen the native loop is frozen, so nothing automatic
			// (an auto-potion, a skill queue, a pet order) can send a request
			// the server, still loading the scene, would discard unanswered.
			const live = phase === "world" && ready && !failure;
			if ( live ) stepStall();
			if ( live && !failure ) {
				try {
					departure.step( now );
					core.step( now );
				} catch ( error ) {
					// A lost answer, not a game error: every such lane resumes through
					// the bootstrap, which rebuilds the state the request left pending.
					// An engine out-of-memory refusal is not a game error either: the
					// step only met the heap's limit, and reload is the remedy. A lost
					// answer keeps precedence - its throw never reads as a refusal.
					const cause = error instanceof Error ? error.cause : undefined;
					const lost = lostAnswer( cause );
					const oom = lost ? null : outOfMemoryFailure( error );
					onNetworkFailure( lost ? `${String( error )}; ${inboundAge()}` : String( error ), undefined, {
						category: lost ? "unknown" : "software",
						code: lost ? cause as string : oom ? oom.code : "simulation_failed",
						message: cause === MOVEMENT_RECEIPT_TIMEOUT ?
							"Movement synchronization timed out. Please reconnect." :
							cause === TARGET_RELEASE_TIMEOUT ?
							"Target synchronization timed out. Please reconnect." :
							lost ?
							"Item synchronization timed out. Please reconnect." :
							oom ?
							oom.message :
							"A game error interrupted your session.",
						stack: error instanceof Error ? error.stack?.slice( 0, 8192 ) : undefined
					} );
				}
			}
			reportDegradations();
			if ( !live || failure ) {
				try {
					core.step( now, false );
				} catch ( error ) {
					if ( !failure ) {
						// Same refusal rule as the live step: memory names
						// itself, anything else is a simulation failure.
						onNetworkFailure(
							String( error ),
							undefined,
							outOfMemoryFailure( error ) ?? {
								category: "software",
								code: "simulation_failed",
								message: "A game error interrupted your session.",
								stack: error instanceof Error ? error.stack?.slice( 0, 8192 ) : undefined
							}
						);
					}
				}
			}
			if ( phase === "entering-world" && boundThisTransport && core.synchronized() ) {
				transition( "world" );
				entryAttempt = false;
				deadline = 0;
				attempt = 0;
			}
			if ( deadline && now >= deadline ) {
				failure = "World connection timed out";
			}
			if ( failure ) {
				const error = failure;
				cancel();
				deadline = 0;
				// A bound session retries as a resume; an entry that never
				// bound retries the whole admission (a fresh EnterWorld, no
				// resume token to present) - but only on a transport-class
				// failure: an admission refusal is definitive and returns to
				// character select at once. Native gameplay commands are
				// never replayed by this owner: a resume re-enters through
				// its bootstrap, and an unbound attempt has issued none.
				// A receipt deadline can expire while the host CPU is stalled.
				// Reuse admission/resume to replace stale prediction with server state.
				const bound = hasWorld && resume &&
					(lostAnswer( failureCode ) || [
						"Transport connection closed",
						"Transport connection failed",
						"World connection timed out",
						"Admission request failed"
					].includes( error ));
				const unbound = entryAttempt && !hasWorld &&
					[
						"Transport connection closed",
						"Transport connection failed",
						"World connection timed out"
					].includes( error );
				if (
					(bound || unbound) && (attempt === 0 || now - reconnectSince < RESUME_GRACE_MS)
				) {
					if ( attempt === 0 ) reconnectSince = now;
					retryAt = now + Math.min( RECONNECT_DELAY_MS * 2 ** attempt++, MAX_RECONNECT_DELAY_MS );
					transition( "reconnecting" );
				} else {
					retryAt = 0;
					entryAttempt = false;
					transition( "disconnected" );
				}
				failure = null;
				lastError = error;
			}
		},
		/*
================
takeNetJournal

The network owner's frame journal since the last take (network.ts).
================
		*/
		takeNetJournal() {
			return network.takeJournal();
		},
		/*
================
status
================
		*/
		status() {
			return {
				phase,
				ready,
				admitted: hasWorld,
				revision,
				error: lastError,
				disconnectMessage,
				incidentID,
				character,
				entities: core.count(),
				pingMs,
				huntingGuide: hasWorld ? huntingGuide : undefined,
				attempt
			};
		},
		/*
================
take
================
		*/
		take: () => core.take(),
		/*
================
ack
================
		*/
		ack: ( sequence: number ) => core.ack( sequence ),
		/*
================
dispose
================
		*/
		dispose() {
			if ( !disposed ) {
				disposed = true;
				cancel();
				network.dispose();
				core.dispose();
			}
		}
	};
}
