/*
===========================================================================

world.ts - transport admission, reconnect and departure for one world session

The network deadline covers tickets, references and native synchronization.
The presentation acknowledgement is a separate barrier: it gates commands,
never retries a successfully received world.

===========================================================================
*/
import { createNetwork } from "@/engine/runtime/simulation/worker/network/network";
import { createWorldCore } from "./core";
import { createDeparture } from "./departure";
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
const ADMISSION_TIMEOUT_MS = 10000;
const REFERENCE_TIMEOUT_MS = 30000;
const RECONNECT_DELAY_MS = 250;
const MAX_RECONNECT_ATTEMPTS = 3;
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
	) => Promise<{ refSkillSnapshot: unknown[]; }>
) {
	let failure: string | null = null, epoch = 0, disposed = false, controller: AbortController | null = null;
	/*
================
onNetworkFailure
================
	*/
	function onNetworkFailure( error: string ) {
		failure = error;
	}
	const network = createNetwork( onNetworkFailure );
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
	let now = 0, deadline = 0, retryAt = 0, attempt = 0, hasWorld = false, ready = false, revision = 0;
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
						references = { kind: "ready", bootstrap: { ...bootstrap, ...refs } };
					}
				}, error => {
					if ( !disposed && epoch === current ) references = { kind: "failed", error: String( error ) };
				} );
				return;
			}
			if ( wrapper.v !== 1 ) {
				throw new Error( "Unsupported EnterWorld blob version" );
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
		if ( frame.opcode === 11 || frame.opcode === 12 || frame.opcode === 13 || frame.opcode === 14 ) {
			if ( !boundThisTransport ) throw Error( "Commerce packet before EnterWorld" );
			core.receive( frame, now );
			return;
		}
		if ( frame.opcode <= 255 ) {
			if ( frame.opcode === 4 ) {
				return;
			}
			throw new Error( `Unsupported transport control ${frame.opcode}` );
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
			core.command( command, now );
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
					failure = String( error );
				}
			} else if ( references.kind === "failed" ) {
				failure = references.error;
				references = { kind: "idle" };
			}
			if ( !failure ) network.drain( receive );
			if ( phase === "world" && !failure ) {
				try {
					departure.step( now );
					core.step( now );
				} catch ( error ) {
					failure = String( error );
				}
			}
			if ( phase !== "world" || failure ) core.step( now, false );
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
					hasWorld && resume && attempt < MAX_RECONNECT_ATTEMPTS &&
					[
						"Transport connection closed",
						"Transport connection failed",
						"World connection timed out",
						"Admission request failed"
					].includes( error )
				) {
					retryAt = now + RECONNECT_DELAY_MS * 2 ** attempt++;
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
				character,
				entities: core.count(),
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
