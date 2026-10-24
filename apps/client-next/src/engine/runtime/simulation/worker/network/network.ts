/*
===========================================================================

network.ts - the world transport socket and its inbound frame queue

One socket owner; callbacks admit packets, simulation ticks consume them in
order. Reconnection is explicit. Never retry commands or reuse admission
tickets here. A frame its consumer cannot apply ends the session, and the
failure names that frame so it can be reported (session.ts).

===========================================================================
*/
import { createCodec, MAX_RESERVED_OPCODE } from "./codec/codec";
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
// The stream position is acknowledged at most this often (#1466): the
// server keeps every unacknowledged ordered frame for a resume.
const STREAM_ACK_INTERVAL_MS = 1000;
// A server close reason is free text; the report keeps a short, plain copy.
const CLOSE_REASON_LENGTH = 64;

/*
================
CloseFacts

What the browser knows when a socket goes away (GH #1428).
================
*/
export interface CloseFacts {
	readonly code: number;
	readonly reason: string;
	readonly wasClean: boolean;
	readonly errored: boolean;
	readonly sinceLastFrameMs: number | null;
	readonly openForMs: number | null;
	readonly online: boolean;
}

/*
================
closeDetail

One line for the incident report: the WebSocket close code and reason,
whether the close was clean, whether an error event preceded it, how long
since the last inbound frame and how long the socket was open, and the
browser's online flag. 1006 with a long silence reads as a network drop;
a clean server code with a reason reads as a server close.
================
*/
export function closeDetail( facts: CloseFacts ): string {
	const seconds = ( ms: number ) => (ms / 1000).toFixed( 1 ) + " s";
	const reason = facts.reason.replace( /[^\x20-\x7e]/g, "?" ).slice( 0, CLOSE_REASON_LENGTH );
	return [
		"WebSocket " + facts.code + (reason ? ' "' + reason + '"' : ""),
		facts.wasClean ? "clean" : "not clean",
		...(facts.errored ? [ "after an error event" ] : []),
		facts.sinceLastFrameMs === null ?
			"no frame received" :
			"last frame " + seconds( facts.sinceLastFrameMs ) + " ago",
		facts.openForMs === null ? "never opened" : "open " + seconds( facts.openForMs ),
		facts.online ? "online" : "offline"
	].join( ", " );
}

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
	// Frames received on any socket (received()); pings and pongs included.
	let receivedFrames = 0;
	// The stream position (#1466): frames above the session-internal range
	// the consumer has applied, counted at drain - never at receipt, so a
	// frame lost with a dying socket's inbox is re-sent on resume. It
	// survives a resume and restarts on a fresh WELCOME.
	let streamPosition = 0, ackedPosition = 0, ackSentAt = -Infinity;
	const inbox: WireFrame[] = [];
	let journal = new Float64Array( NET_JOURNAL_RECORDS * NET_JOURNAL_FIELDS ), journalCount = 0;
	// The incident ring: the bug-report journal above is drained every second,
	// so a failure would find at most one second in it.
	const recent = new Float64Array( INCIDENT_JOURNAL_ROWS * NET_JOURNAL_FIELDS );
	let recentNext = 0, recentCount = 0;
	let ended: string | null = null;
	// The current socket's open and last-inbound times (performance.now()).
	let openedAt = -Infinity, receivedAt = -Infinity;
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
		ackedPosition = 0;
		ackSentAt = -Infinity;
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
	acknowledge

	Tells the server how far the stream has been applied, about once a
	second and only when it moved.
	================
	*/
	function acknowledge() {
		if ( !welcomed || !socket || socket.readyState !== WebSocket.OPEN ) return;
		const now = performance.now();
		if ( streamPosition === ackedPosition || now - ackSentAt < STREAM_ACK_INTERVAL_MS ) return;
		ackedPosition = streamPosition;
		ackSentAt = now;
		send( codec.streamAck( streamPosition ) );
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
		// end() keeps admitted frames for their consumer after the socket
		// is gone, and a closing socket takes no more bytes: a reply one of
		// them raises (the 0x36DD transfer answer, a storage follow-up)
		// belongs to the dead session. It is dropped, never thrown back
		// into the application that is finishing the session, so the end
		// reason drain() reports stays the server's own.
		if ( ended || (socket && socket.readyState > WebSocket.OPEN) ) return;
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
		received

		Every frame received so far, transport pings and pongs included: the
		world session's stall detector reads a live link from its change.
		================
		*/
		received() {
			return receivedFrames;
		},
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
			// Without a resume token the server starts a fresh stream.
			if ( !resume?.length ) streamPosition = 0;
			const hello = codec.hello( admission, resume, streamPosition );
			disconnect();
			const generation = epoch;
			const current = new WebSocket( endpoint );
			socket = current;
			current.binaryType = "arraybuffer";
			const active = () => !disposed && generation === epoch && socket === current;
			// An error event is always followed by a close event (WebSocket
			// close steps), which alone carries the code: report at the close.
			let errored = false;
			openedAt = receivedAt = -Infinity;
			current.onopen = () => {
				if ( active() ) {
					openedAt = performance.now();
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
					receivedAt = performance.now();
					const frame = codec.decode( new Uint8Array( event.data ) );
					receivedFrames++;
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
						if ( !codec.welcome( frame.payload ).resumed ) streamPosition = 0;
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
				if ( active() ) errored = true;
			};
			current.onclose = event => {
				if ( !active() ) return;
				const now = performance.now();
				const detail = closeDetail( {
					code: event.code,
					reason: event.reason,
					wasClean: event.wasClean,
					errored,
					sinceLastFrameMs: Number.isFinite( receivedAt ) ? now - receivedAt : null,
					openForMs: Number.isFinite( openedAt ) ? now - openedAt : null,
					online: globalThis.navigator?.onLine ?? true
				} );
				if ( errored ) {
					fail( "Transport connection failed", undefined, {
						category: "connection",
						code: "connection_failed",
						message: "Connection lost. Please reconnect.",
						detail
					} );
					return;
				}
				end( "Transport connection closed", {
					category: "unknown",
					code: "connection_closed",
					message: "Connection lost. The cause is unknown.",
					detail
				} );
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
					if ( current.opcode > MAX_RESERVED_OPCODE ) streamPosition = (streamPosition + 1) >>> 0;
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
				return;
			}
			// Outside the consume try: a full outbound buffer is a transport
			// failure, not a packet the game could not apply.
			try {
				if ( epoch === generation ) acknowledge();
			} catch ( error ) {
				fail( String( error ) );
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
