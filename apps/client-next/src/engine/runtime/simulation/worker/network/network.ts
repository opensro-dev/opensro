/*
===========================================================================

network.ts - the world transport socket and its inbound frame queue

One socket owner; callbacks admit packets, simulation ticks consume them in
order. Reconnection is explicit. Never retry commands or reuse admission
tickets here. A frame its consumer cannot apply ends the session, and the
failure names that frame so it can be reported (session.ts).

===========================================================================
*/
import { createCodec } from "./codec/codec";
import {
	INCIDENT_JOURNAL_AGE_MS,
	INCIDENT_JOURNAL_ROWS,
	NET_JOURNAL_FIELDS,
	NET_JOURNAL_GAP,
	NET_JOURNAL_IN,
	NET_JOURNAL_OUT
} from "@/engine/contracts/network";
import type { NetworkOwner, WireFrame, NetworkFailure } from "@/engine/contracts/network";

const PING_INTERVAL_MS = 5000;
const PING_STALE_MS = 15000;
const PING_TOKEN_BYTES = 4;
const PING_BACKLOG_LIMIT = 65536;
// The frame journal for bug reports (takeJournal) holds at most this many
// frames between takes. Opcodes and sizes only: no payload ever enters it.
const NET_JOURNAL_RECORDS = 4096;

/*
================
createNetwork
================
*/
export function createNetwork(
	onFailure: ( error: string, frame?: WireFrame, reason?: NetworkFailure ) => void
): NetworkOwner {
	const codec = createCodec();
	let socket: WebSocket | null = null, epoch = 0, disposed = false, welcomed = false;
	let bytes = 0;
	let pingToken = 0, pendingPing = false, pingSentAt = -Infinity, pingReceivedAt = -Infinity;
	let measuredPing: number | null = null;
	const inbox: WireFrame[] = [];
	let journal = new Float64Array( NET_JOURNAL_RECORDS * NET_JOURNAL_FIELDS ), journalCount = 0;
	// The incident ring: the bug-report journal above is drained every second,
	// so a failure would find at most one second in it.
	const recent = new Float64Array( INCIDENT_JOURNAL_ROWS * NET_JOURNAL_FIELDS );
	let recentNext = 0, recentCount = 0;
	let ended: string | null = null;
	/*
	================
	note

	Journals one frame. A full journal drops its older half: takes happen
	about once a second, so this only bounds a stalled consumer. The drop is
	never silent: a gap record (NET_JOURNAL_GAP) stands in for the dropped
	frames with their count, so a hole cannot read as "nothing was sent".
	================
	*/
	function note( direction: number, opcode: number, bytes: number ) {
		if ( journalCount === NET_JOURNAL_RECORDS ) {
			const keep = (NET_JOURNAL_RECORDS >> 1) - 1, dropped = NET_JOURNAL_RECORDS - keep;
			const earlierGap = journal[1] === NET_JOURNAL_GAP ? journal[3]! - 1 : 0;
			const lastDroppedAt = journal[(dropped - 1) * NET_JOURNAL_FIELDS]!;
			journal.copyWithin( NET_JOURNAL_FIELDS, dropped * NET_JOURNAL_FIELDS );
			journal[0] = lastDroppedAt;
			journal[1] = NET_JOURNAL_GAP;
			journal[2] = 0;
			journal[3] = dropped + earlierGap;
			journalCount = keep + 1;
		}
		const at = journalCount++ * NET_JOURNAL_FIELDS;
		journal[at] = performance.timeOrigin + performance.now();
		journal[at + 1] = direction;
		journal[at + 2] = opcode;
		journal[at + 3] = bytes;
		recent.set( journal.subarray( at, at + NET_JOURNAL_FIELDS ), recentNext * NET_JOURNAL_FIELDS );
		recentNext = (recentNext + 1) % INCIDENT_JOURNAL_ROWS;
		recentCount = Math.min( recentCount + 1, INCIDENT_JOURNAL_ROWS );
	}
	let endReason: NetworkFailure | undefined;
	/*
	================
	disconnect
	================
	*/
	function disconnect() {
		epoch++;
		welcomed = false;
		pendingPing = false;
		pingSentAt = pingReceivedAt = -Infinity;
		measuredPing = null;
		bytes = 0;
		inbox.length = 0;
		ended = null;
		endReason = undefined;
		const previous = socket;
		socket = null;
		if ( previous ) {
			previous.onopen =
				previous.onmessage =
				previous.onerror =
				previous.onclose =
					null;
			previous.close();
		}
	}
	/*
	================
	fail

	frame is the frame being applied when the failure happened, if any.
	================
	*/
	function fail( message: string, frame?: WireFrame, reason?: NetworkFailure ) {
		disconnect();
		onFailure(
			message,
			frame,
			reason ??
				{
					category: "software",
					code: frame ? "packet_application_failed" : "transport_protocol_failed",
					message: "A game error interrupted your session."
				}
		);
	}
	/*
	================
	end
	================
	*/
	function end( message: string, reason: NetworkFailure ) {
		// A completion packet and socket close can arrive before the same tick.
		// Preserve admitted FIFO entries until their consumer has seen them.
		ended = message;
		endReason = reason;
		const previous = socket;
		socket = null;
		if ( previous ) {
			previous.onopen =
				previous.onmessage =
				previous.onerror =
				previous.onclose =
					null;
			previous.close();
		}
	}
	/*
	================
	send
	================
	*/
	function send( frame: WireFrame ) {
		if ( !socket || socket.readyState !== WebSocket.OPEN ) {
			throw new Error( "Transport is not open" );
		}
		const encoded = codec.encode( frame );
		if ( socket.bufferedAmount + encoded.byteLength > (1 << 20) ) {
			throw new Error( "Transport outbound backlog exceeded" );
		}
		socket.send( encoded );
		note( NET_JOURNAL_OUT, frame.opcode, encoded.byteLength );
	}
	return {
		disconnect,
		/*
  ================
  pingMs

  Uses the existing transport echo on the gameplay socket (server OpPing).
  No HTTP estimate or clock synchronization. A sample expires and is never
  carried across a socket generation. One tiny probe per five seconds.
  ================
  */
		pingMs() {
			if ( !socket || !welcomed || socket.readyState !== WebSocket.OPEN ) return null;
			const now = performance.now();
			if ( now - pingSentAt >= PING_INTERVAL_MS && socket.bufferedAmount < PING_BACKLOG_LIMIT ) {
				const payload = new Uint8Array( PING_TOKEN_BYTES );
				new DataView( payload.buffer ).setUint32( 0, ++pingToken, true );
				pingSentAt = now;
				pendingPing = true;
				send( { opcode: 3, payload } );
			}
			return now - pingReceivedAt < PING_STALE_MS ? measuredPing : null;
		},
		enterWorld( division, character, token ) {
			if ( !welcomed ) throw new Error( "Transport handshake incomplete" );
			send( codec.enterWorld( division, character, token ) );
		},
		connect( url, admission, resume ) {
			if ( disposed ) {
				throw new Error( "Transport disposed" );
			}
			const endpoint = new URL( url );
			if ( endpoint.protocol !== "ws:" && endpoint.protocol !== "wss:" ) {
				throw new Error( "Invalid transport URL" );
			}
			const hello = codec.hello( admission, resume );
			disconnect();
			const generation = epoch;
			const current = new WebSocket( endpoint );
			socket = current;
			current.binaryType = "arraybuffer";
			const active = () => !disposed && generation === epoch && socket === current;
			current.onopen = () => {
				if ( active() ) {
					try {
						send( hello );
					} catch ( error ) {
						fail( String( error ) );
					}
				}
			};
			current.onmessage = event => {
				if ( !active() ) {
					return;
				}
				try {
					if ( !(event.data instanceof ArrayBuffer) ) {
						throw new Error( "Non-binary transport frame" );
					}
					const frame = codec.decode( new Uint8Array( event.data ) );
					note( NET_JOURNAL_IN, frame.opcode, event.data.byteLength );
					if ( frame.opcode === 5 ) {
						if ( frame.payload.length !== 1 ) throw new Error( "Invalid transport goodbye" );
						const reason = frame.payload[0]!,
							names = [
								"normal",
								"protocol error",
								"hello timeout",
								"idle timeout",
								"slow consumer",
								"shutdown",
								"replaced",
								"server busy",
								"unauthorized"
							];
						const expected = [ 0, 5, 6, 8 ].includes( reason );
						const messages: Record<number, string> = {
							0: "Your session ended.",
							5: "The server is restarting.",
							6: "This session was replaced by another login.",
							8: "Your session authorization ended."
						};
						end( `Server ended transport session: ${names[reason] ?? "unknown"} (${reason})`, {
							category: expected ? "expected" : "unknown",
							code: `server_bye_${reason}`,
							message: messages[reason] ?? "The server ended your connection."
						} );
						return;
					}
					if ( !welcomed ) {
						if ( frame.opcode !== 2 ) {
							throw new Error( "Expected transport welcome" );
						}
						codec.welcome( frame.payload );
						welcomed = true;
					} else if ( frame.opcode === 1 || frame.opcode === 2 ) {
						throw new Error( "Unexpected transport handshake" );
					}
					if ( frame.opcode === 3 ) {
						if ( frame.payload.length > 64 ) {
							throw new Error( "Oversized transport ping" );
						}
						send( { opcode: 4, payload: frame.payload } );
						return;
					}
					if ( frame.opcode === 4 ) {
						if ( frame.payload.length > 64 ) throw new Error( "Oversized transport pong" );
						if (
							pendingPing && frame.payload.length === PING_TOKEN_BYTES &&
							new DataView( frame.payload.buffer, frame.payload.byteOffset, PING_TOKEN_BYTES ).getUint32(
									0,
									true
								) === (pingToken >>> 0)
						) {
							pingReceivedAt = performance.now();
							measuredPing = Math.max( 0, Math.round( pingReceivedAt - pingSentAt ) );
							pendingPing = false;
						}
						return;
					}
					if ( inbox.length >= 4096 || bytes + event.data.byteLength > (16 << 20) ) {
						throw new Error( "Transport inbound backlog exceeded" );
					}
					inbox.push( frame );
					bytes += event.data.byteLength;
				} catch ( error ) {
					fail( String( error ) );
				}
			};
			current.onerror = () => {
				if ( active() ) {
					fail( "Transport connection failed", undefined, {
						category: "connection",
						code: "connection_failed",
						message: "Connection lost. Please reconnect."
					} );
				}
			};
			current.onclose = () => {
				if ( active() ) {
					end( "Transport connection closed", {
						category: "unknown",
						code: "connection_closed",
						message: "Connection lost. The cause is unknown."
					} );
				}
			};
		},
		send( frame ) {
			if ( !welcomed ) {
				throw new Error( "Transport handshake incomplete" );
			}
			send( frame );
		},
		/*
		================
		takeJournal

		The frames journaled since the last take, or null when there are none.
		================
		*/
		recentJournal() {
			const now = performance.timeOrigin + performance.now(), rows: number[] = [];
			for ( let i = 0; i < recentCount; i++ ) {
				const at = ((recentNext - recentCount + i + INCIDENT_JOURNAL_ROWS) % INCIDENT_JOURNAL_ROWS) *
					NET_JOURNAL_FIELDS;
				const age = Math.min( 0, Math.round( recent[at]! - now ) );
				if ( age < -INCIDENT_JOURNAL_AGE_MS ) continue;
				rows.push( age, recent[at + 1]!, recent[at + 2]!, recent[at + 3]! );
			}
			return rows;
		},
		takeJournal() {
			if ( !journalCount ) return null;
			const taken = journal.slice( 0, journalCount * NET_JOURNAL_FIELDS );
			journalCount = 0;
			return taken;
		},
		drain( consume ) {
			const generation = epoch, count = inbox.length;
			let current: WireFrame | undefined;
			try {
				let consumed = 0;
				for ( let index = 0; index < count; index++ ) {
					current = inbox[index]!;
					if ( consume( current ) === false ) break;
					consumed++;
					if ( epoch !== generation ) {
						return;
					}
				}
				inbox.splice( 0, consumed );
				bytes = inbox.reduce( ( total, frame ) => total + frame.payload.byteLength + 2, 0 );
				if ( !inbox.length && ended ) fail( ended, undefined, endReason );
			} catch ( error ) {
				const unsupported = error instanceof Error && error.cause === "unsupported_feature";
				fail( `Packet application failed: ${String( error )}`, current, {
					category: "software",
					code: unsupported ? "unsupported_feature" : "packet_application_failed",
					message: unsupported ?
						"An unsupported game operation interrupted your session." :
						"A game error interrupted your session.",
					stack: error instanceof Error ? error.stack?.slice( 0, 8192 ) : undefined
				} );
			}
		},
		dispose() {
			if ( !disposed ) {
				disposed = true;
				disconnect();
			}
		}
	};
}
