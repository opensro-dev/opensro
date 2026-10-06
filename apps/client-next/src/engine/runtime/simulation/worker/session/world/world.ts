/*
===========================================================================

world.ts - transport admission, reconnect and departure for one world session

The network deadline covers tickets, references and native synchronization.
The presentation acknowledgement is a separate barrier: it gates commands,
never retries a successfully received world.

===========================================================================
*/
import { isWorldControl } from "@/engine/foundation/gameplay/commerce-controls";
import { createNetwork } from "@/engine/runtime/simulation/worker/network/network";
import { createWorldCore } from "./core";
import { createDeparture } from "./departure";
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import {
	type ClientIncident,
	type NetworkFailure,
	INCIDENT_DUMP_BYTES,
	INCIDENT_MESSAGE_LENGTH,
	type WireFrame
} from "@/engine/contracts/network";
const ADMISSION_TIMEOUT_MS = 10000;
const REFERENCE_TIMEOUT_MS = 30000;
const RECONNECT_DELAY_MS = 250;
const MAX_RECONNECT_DELAY_MS = 4000;
// The Agent keeps a detached session resumable for its GracePeriod (30 s,
// apps/server/internal/transport/config.go). Retry for all of it: a laptop
// waking or a Wi-Fi hand-over outlasts a few fast attempts.
const RESUME_GRACE_MS = 30000;
const MAX_CHARACTER_NAME_LENGTH = 64;

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
		signal: AbortSignal
	) => Promise<{ refSkillSnapshot: unknown[]; refItemSnapshot: unknown[]; }>,
	reportIncident: ( incident: ClientIncident ) => void
) {
	let failure: string | null = null, epoch = 0, disposed = false, controller: AbortController | null = null;
	let diagnosticSession = "", disconnectMessage = "", incidentID = "";
	/*
================
onNetworkFailure
================
	*/
	function onNetworkFailure( error: string, frame?: WireFrame, reason?: NetworkFailure ) {
		failure = error;
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
				frame ?
				"packet" :
				reason?.category === "software" ?
				"runtime" :
				"transport",
			message: error.slice( 0, INCIDENT_MESSAGE_LENGTH ),
			...(frame ?
				{
					opcode: frame.opcode,
					payload: Array.from( dump!, byte => byte.toString( 16 ).padStart( 2, "0" ) ).join( "" ),
					payloadSize: frame.payload.byteLength
				} :
				{}),
			phase,
			character
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
	let boundThisTransport = false, resumedTransport = false;
	let references: { kind: "idle"; } | { kind: "loading"; } | { kind: "ready"; bootstrap: unknown; } | {
		kind: "failed";
		error: string;
	} = { kind: "idle" };
	let lastError: string | undefined;
	let now = 0,
		deadline = 0,
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
		deadline = now + ADMISSION_TIMEOUT_MS;
		mint( kind, character, controller.signal ).then( token => {
			if ( !disposed && epoch === current ) {
				completion = { epoch: current, kind, token };
			}
		}, () => {
			if ( !disposed && epoch === current ) {
				completion = { epoch: current, kind, error: "Admission request failed" };
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
		if ( references.kind !== "idle" ) return false;
		if ( frame.opcode === 2 ) {
			// Network owner has already checked the exact v2 WELCOME layout.
			const resumed = frame.payload[1] === 1;
			if ( hasWorld && !resumed ) {
				throw new Error( "Server could not resume the previous world session" );
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
				const bootstrap = wrapper.bootstrap, current = epoch;
				controller = new AbortController();
				references = { kind: "loading" };
				deadline = now + REFERENCE_TIMEOUT_MS;
				loadWorldReferences( wrapper.references, transport, controller.signal ).then( refs => {
					if ( !disposed && epoch === current ) {
						// The published item rows and the login's own rows are disjoint
						// (the server skips published ids); the catalog rejects a repeat.
						const own = (bootstrap as { refItemSnapshot?: unknown[]; }).refItemSnapshot ?? [];
						references = {
							kind: "ready",
							bootstrap: { ...bootstrap, ...refs, refItemSnapshot: [ ...refs.refItemSnapshot, ...own ] }
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
			core.bootstrap( wrapper.bootstrap );
			boundThisTransport = true;
			hasWorld = true;
			ready = false;
			deadline = now + ADMISSION_TIMEOUT_MS;
			return;
		}
		if ( frame.opcode === 10 ) {
			if ( boundThisTransport ) {
				core.receive( frame, now );
			}
			return;
		}
		if ( isWorldControl( frame.opcode ) ) {
			if ( !boundThisTransport ) throw Error( "World control packet before EnterWorld" );
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
			deadline = now + ADMISSION_TIMEOUT_MS;
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
			attempt = 0;
			ready = false;
			character = name;
			division = shard;
			// The transport base (possibly an edge route) serves the socket and its references.
			const url = new URL( base );
			url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
			url.pathname = url.pathname.replace( /\/$/, "" ) + "/transport/ws";
			transport = base;
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
		/*
================
command
================
		*/
		command( command: GameplayCommand ) {
			if ( phase !== "world" || (!ready && command.kind !== "navigation") ) {
				throw new Error( "World gameplay is not ready" );
			}
			// The countdown belongs to this session; a ground click cancels it
			// (CGInterface_MoveToWorldPoint sends 0x731F while +0x39C is set).
			const admitted = command.kind === "ground-move" && departure.pending() ?
				{ ...command, departing: true } :
				command;
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
			now = time;
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
							deadline = now + ADMISSION_TIMEOUT_MS;
						} catch ( error ) {
							failure = String( error );
						}
					}
				}
			}
			if ( references.kind === "ready" ) {
				const bootstrap = references.bootstrap;
				references = { kind: "idle" };
				controller = null;
				try {
					core.bootstrap( bootstrap );
					boundThisTransport = true;
					hasWorld = true;
					ready = false;
					deadline = now + ADMISSION_TIMEOUT_MS;
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
			const nextPing = !failure && phase === "world" ? network.pingMs() : null;
			if ( nextPing !== pingMs ) {
				pingMs = nextPing;
				revision++;
			}
			if ( phase === "world" && !failure ) {
				try {
					departure.step( now );
					core.step( now );
				} catch ( error ) {
					onNetworkFailure( String( error ), undefined, {
						category: "software",
						code: "simulation_failed",
						message: "A game error interrupted your session."
					} );
				}
			}
			if ( phase !== "world" || failure ) {
				try {
					core.step( now, false );
				} catch ( error ) {
					if ( !failure ) {
						onNetworkFailure( String( error ), undefined, {
							category: "software",
							code: "simulation_failed",
							message: "A game error interrupted your session.",
							stack: error instanceof Error ? error.stack?.slice( 0, 8192 ) : undefined
						} );
					}
				}
			}
			if ( phase === "entering-world" && boundThisTransport && core.synchronized() ) {
				transition( "world" );
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
				// Only a previously bound, resumable session retries automatically.
				// Native gameplay commands are never replayed by this owner.
				if (
					hasWorld && resume && (attempt === 0 || now - reconnectSince < RESUME_GRACE_MS) &&
					[
						"Transport connection closed",
						"Transport connection failed",
						"World connection timed out",
						"Admission request failed"
					].includes( error )
				) {
					if ( attempt === 0 ) reconnectSince = now;
					retryAt = now + Math.min( RECONNECT_DELAY_MS * 2 ** attempt++, MAX_RECONNECT_DELAY_MS );
					transition( "reconnecting" );
				} else {
					retryAt = 0;
					transition( "disconnected" );
				}
				failure = null;
				lastError = error;
			}
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
