/*
===========================================================================

network.ts - the world transport and its inbound frame queue

One frame-queue owner over whichever link the session connected with: a
WebSocket (one message is one frame), or, behind the Experimental
WebTransport row (#1448), a WebTransport control stream carrying the
same frames u32-length-prefixed (the server envelope's stream form).
Callbacks admit packets, simulation ticks consume them in order.
Reconnection is explicit. Never retry commands here; an admission is
presented once per transport and never after a WELCOME (the server
consumes it at the WELCOME write, #1447), which is what lets a dead
WebTransport path hand its connect to the socket below. A frame its
consumer cannot apply ends the session, and the failure names that
frame so it can be reported (session.ts).

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
// A WebTransport attempt gets this long to fetch its certificate pin and
// establish (session plus control stream) before the socket takes the
// same admission over, and this long after the hello for the WELCOME
// before the path counts as failed and the socket takes over instead: a
// QUIC path that stalls pre-WELCOME never silently holds a login.
const WT_CONNECT_DEADLINE_MS = 3000;
const WT_WELCOME_DEADLINE_MS = 10000;
// The serverCertificateHashes pinning window: the browser pins only
// certificates valid for at most 14 days (apps/server certs.go), which
// is exactly the development certificate; a public one is used plainly.
const WT_CERT_PIN_WINDOW_MS = 14 * 24 * 60 * 60 * 1000;

/*
================
Link

One connected transport under the frame queue. Sends take codec bytes
(the control-stream link adds its length prefix); state mirrors the
socket's readyState closely enough for send()'s drop-versus-throw rule:
a closing link drops quietly, a connecting or absent one throws.
================
*/
interface Link {
	readonly state: "connecting" | "open" | "closing";
	send( bytes: Uint8Array ): void;
	buffered(): number;
	close(): void;
}

// The stream position is acknowledged at most this often (#1466): the
// server keeps every unacknowledged ordered frame for a resume.
const STREAM_ACK_INTERVAL_MS = 1000;
// A server close reason is free text; the report keeps a short, plain copy.
const CLOSE_REASON_LENGTH = 64;
// The WebSocket normal-closure code: with a clean close it is the one
// goodbye the browser proves (GH #2148) - the peer closed politely and
// said so, as opposed to a drop the client only notices by silence.
const WS_NORMAL_CLOSE_CODE = 1000;

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
	let link: Link | null = null, epoch = 0, disposed = false, welcomed = false;
	// Experimental WebTransport (#1448): try QUIC before the socket.
	// wtFailed is the session's memory of a dead QUIC path: after any
	// WebTransport failure, connects go straight to the socket.
	let wtEnabled = false, wtFailed = false;
	// Why the fastest path failed (codec.transportReport owns the
	// numbers: 1 unsupported, 2 timeout, 3 refused, 4 closed early).
	// Remembered with wtFailed for the session; each socket takeover
	// copies it into transportFallback so the report names the attempt.
	let wtFailReason = 0;
	// The connect in flight: endpoint and hello for the socket fallback,
	// and whether that fallback already ran. It runs at most once - see
	// wtClosed - so an admission is never presented a third time.
	let attempt: { endpoint: URL; hello: WireFrame; fellBack: boolean; } | null = null;
	let welcomeTimer: ReturnType<typeof setTimeout> | null = null;
	let bytes = 0;
	let pingToken = 0, pendingPing = false, pingSentAt = -Infinity, pingReceivedAt = -Infinity;
	let measuredPing: number | null = null;
	// Frames received on any socket (received()); pings and pongs included.
	let receivedFrames = 0;
	// Inbound bytes received on any link (receivedBytes()), counted as
	// they arrive: a frame still being reassembled moves this long
	// before receivedFrames can.
	let receivedByteCount = 0;
	// The stream position (#1466): frames above the session-internal range
	// the consumer has applied, counted at drain - never at receipt, so a
	// frame lost with a dying socket's inbox is re-sent on resume. It
	// survives a resume and restarts on a fresh WELCOME.
	let streamPosition = 0, ackedPosition = 0, ackSentAt = -Infinity;
	// Why this connect is not on the fastest transport (#1449), reported
	// once after each WELCOME (codec.transportReport). 0 = no fallback on
	// this connect. The WebTransport-first connect path sets it before
	// the socket takes over, so the reason names that failed attempt.
	let transportFallback = 0;
	const inbox: WireFrame[] = [];
	let journal = new Float64Array( NET_JOURNAL_RECORDS * NET_JOURNAL_FIELDS ), journalCount = 0;
	// The incident ring: the bug-report journal above is drained every second,
	// so a failure would find at most one second in it.
	const recent = new Float64Array( INCIDENT_JOURNAL_ROWS * NET_JOURNAL_FIELDS );
	let recentNext = 0, recentCount = 0;
	let ended: string | null = null;
	// The current socket's open and last-inbound times (performance.now());
	// receivedAt is the one last-inbound stamp (receivedAt(), #1474).
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
		attempt = null;
		clearWelcomeDeadline();
		ackedPosition = 0;
		ackSentAt = -Infinity;
		const previous = link;
		link = null;
		previous?.close();
	}
	/*
	================
	clearWelcomeDeadline
	================
	*/
	function clearWelcomeDeadline() {
		if ( welcomeTimer !== null ) {
			clearTimeout( welcomeTimer );
			welcomeTimer = null;
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
		if ( !welcomed || !link || link.state !== "open" ) return;
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
		// A completion packet and link close can arrive before the same tick.
		// Preserve admitted FIFO entries until their consumer has seen them.
		ended = message;
		endReason = reason;
		const previous = link;
		link = null;
		previous?.close();
	}
	/*
	================
	send
	================
	*/
	function send( frame: WireFrame ) {
		// end() keeps admitted frames for their consumer after the link
		// is gone, and a closing link takes no more bytes: a reply one of
		// them raises (the 0x36DD transfer answer, a storage follow-up)
		// belongs to the dead session. It is dropped, never thrown back
		// into the application that is finishing the session, so the end
		// reason drain() reports stays the server's own.
		if ( ended || link?.state === "closing" ) return;
		if ( !link || link.state !== "open" ) {
			throw new Error( "Transport is not open" );
		}
		const encoded = codec.encode( frame );
		if ( link.buffered() + encoded.byteLength > (1 << 20) ) {
			throw new Error( "Transport outbound backlog exceeded" );
		}
		link.send( encoded );
		note( NET_JOURNAL_OUT, frame.opcode, encoded.byteLength );
	}
	/*
	================
	receiveBytes

	One inbound frame's bytes, from a socket message or one stream
	record: the single dispatch both links feed (the server goodbye,
	the WELCOME gate, ping answers, the bounded inbox).
	================
	*/
	function receiveBytes( data: Uint8Array ) {
		try {
			receivedAt = performance.now();
			const frame = codec.decode( data );
			receivedFrames++;
			note( NET_JOURNAL_IN, frame.opcode, data.byteLength );
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
				clearWelcomeDeadline();
				send( codec.transportReport( transportFallback ) );
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
			if ( inbox.length >= 4096 || bytes + data.byteLength > (16 << 20) ) {
				throw new Error( "Transport inbound backlog exceeded" );
			}
			inbox.push( frame );
			bytes += data.byteLength;
		} catch ( error ) {
			fail( String( error ) );
		}
	}
	/*
	================
	connectSocket

	The WebSocket link: one message is one frame.
	================
	*/
	function connectSocket( endpoint: URL, hello: WireFrame, generation: number ) {
		const current = new WebSocket( endpoint );
		current.binaryType = "arraybuffer";
		const mine: Link = {
			get state() {
				return current.readyState === WebSocket.OPEN ?
					"open" :
					current.readyState === WebSocket.CONNECTING ?
					"connecting" :
					"closing";
			},
			send: bytes => current.send( bytes ),
			buffered: () => current.bufferedAmount,
			close() {
				current.onopen =
					current.onmessage =
					current.onerror =
					current.onclose =
						null;
				current.close();
			}
		};
		link = mine;
		const active = () => !disposed && generation === epoch && link === mine;
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
			if ( !(event.data instanceof ArrayBuffer) ) {
				fail( String( new Error( "Non-binary transport frame" ) ) );
				return;
			}
			receivedByteCount += event.data.byteLength;
			receiveBytes( new Uint8Array( event.data ) );
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
			// A clean normal close is a goodbye, not a loss: the session
			// ends as expected (no incident), and the player is told the
			// session ended rather than that the cause is unknown. Every
			// other close keeps the one code; the detail line names its
			// cause (silence, never opened, offline) from the journal
			// facts above (GH #2148).
			const goodbye = event.wasClean && event.code === WS_NORMAL_CLOSE_CODE;
			end( "Transport connection closed", {
				category: goodbye ? "expected" : "unknown",
				code: "connection_closed",
				message: goodbye ? "Your session ended." : "Connection lost. The cause is unknown.",
				detail
			} );
		};
	}
	/*
	================
	wtEndpoint

	The WebTransport URL beside the socket's: same authority, the
	transport's WT path over https. Null when the socket path is not the
	transport's own (an edge route the client cannot map): no attempt.
	================
	*/
	function wtEndpoint( endpoint: URL ): string | null {
		if ( !endpoint.pathname.endsWith( "/transport/ws" ) ) return null;
		const url = new URL( endpoint );
		url.protocol = "https:";
		url.pathname = endpoint.pathname.slice( 0, endpoint.pathname.length - 2 ) + "wt";
		return url.toString();
	}
	/*
	================
	certHashes

	The development certificate's pin, fetched from the transport's own
	cert-hash document before the attempt (the server's stated client
	flow). Only a certificate inside the browser's 14-day pinning window
	is pinned; a public certificate is used plainly. Best effort: any
	failure means no pins, and a path that then cannot verify falls back
	to the socket like any other failure.
	================
	*/
	async function certHashes( endpoint: URL ): Promise<WebTransportHash[] | undefined> {
		try {
			if ( !endpoint.pathname.endsWith( "/transport/ws" ) ) return undefined;
			const url = new URL( endpoint );
			url.protocol = endpoint.protocol === "wss:" ? "https:" : "http:";
			url.pathname = endpoint.pathname.slice( 0, endpoint.pathname.length - 2 ) + "cert-hash";
			const response = await fetch( url, { signal: AbortSignal.timeout( WT_CONNECT_DEADLINE_MS ) } );
			if ( !response.ok ) return undefined;
			const doc = await response.json() as {
				sha256Base64?: unknown;
				notBefore?: unknown;
				notAfter?: unknown;
			};
			if (
				typeof doc.sha256Base64 !== "string" || typeof doc.notBefore !== "string" ||
				typeof doc.notAfter !== "string"
			) return undefined;
			const span = Date.parse( doc.notAfter ) - Date.parse( doc.notBefore );
			if ( !(span >= 0) || span > WT_CERT_PIN_WINDOW_MS ) return undefined;
			const binary = atob( doc.sha256Base64 );
			const value = new Uint8Array( binary.length );
			for ( let i = 0; i < binary.length; i++ ) value[i] = binary.charCodeAt( i );
			return [ { algorithm: "sha-256", value } ];
		} catch {
			return undefined;
		}
	}
	/*
	================
	establish
	================
	*/
	async function establish( transport: WebTransport ) {
		await transport.ready;
		return transport.createBidirectionalStream();
	}
	/*
	================
	withDeadline
	================
	*/
	function withDeadline<T>( promise: Promise<T>, ms: number ): Promise<T> {
		return new Promise<T>( ( resolve, reject ) => {
			const timer = setTimeout( () => reject( new Error( "Transport connect deadline" ) ), ms );
			promise.then(
				value => {
					clearTimeout( timer );
					resolve( value );
				},
				( error: unknown ) => {
					clearTimeout( timer );
					reject( error instanceof Error ? error : new Error( String( error ) ) );
				}
			);
		} );
	}
	/*
	================
	armWelcomeDeadline

	A WebTransport path that handshakes but stalls before the WELCOME has
	failed: end it inside this window through wtClosed, which hands the
	connect to the socket, instead of silently holding the login.
	================
	*/
	function armWelcomeDeadline( generation: number, mine: Link ) {
		clearWelcomeDeadline();
		welcomeTimer = setTimeout( () => {
			welcomeTimer = null;
			// A stall is a timeout (reason 2), not a close.
			wtFailReason = 2;
			wtClosed( generation, mine );
		}, WT_WELCOME_DEADLINE_MS );
	}
	/*
	================
	wtClosed

	The WebTransport session or its control stream ended. After the
	WELCOME the link simply closed, as a socket would. Before it the
	path itself failed: it is remembered for the session, and the socket
	takes the connect over with the same admission - the server consumes
	a ticket at the WELCOME write (#1447), so an attempt that died
	before it leaves the ticket presentable. The fallback runs once: if
	the socket also dies before the WELCOME (a ticket the server did
	consume under a WELCOME the client never saw is refused), the
	socket's own failure paths report it and the session's retry mints a
	fresh admission; the ticket is never presented a third time.
	================
	*/
	function wtClosed( generation: number, mine: Link ) {
		if ( disposed || generation !== epoch || link !== mine ) return;
		if ( welcomed ) {
			end( "Transport connection closed", {
				category: "unknown",
				code: "connection_closed",
				message: "Connection lost. The cause is unknown."
			} );
			return;
		}
		wtFailed = true;
		// Anything that ends the session this early closed early
		// (reason 4), unless the welcome deadline already named it a
		// timeout (reason 2).
		if ( !wtFailReason ) wtFailReason = 4;
		const current = attempt;
		if ( current && !current.fellBack ) {
			current.fellBack = true;
			clearWelcomeDeadline();
			transportFallback = wtFailReason;
			link = null;
			mine.close();
			connectSocket( current.endpoint, current.hello, generation );
			return;
		}
		fail( "Transport connection failed", undefined, {
			category: "connection",
			code: "connection_failed",
			message: "Connection lost. Please reconnect."
		} );
	}
	/*
	================
	wtPump

	Read the control stream: reassemble u32-length-prefixed records
	across chunks and feed each frame's bytes to the one dispatch. A
	malformed stream fails like a bad socket frame; a stream that ends
	or errors reports through wtClosed.
	================
	*/
	async function wtPump(
		reader: ReadableStreamDefaultReader<Uint8Array>,
		generation: number,
		mine: Link
	): Promise<void> {
		const invalid = new Error( "Invalid inbound frame length" );
		let pending = new Uint8Array( 0 ), failure: string | undefined;
		try {
			for ( ;; ) {
				const { done, value } = await reader.read();
				if ( done ) break;
				if ( !value?.byteLength ) continue;
				// Bytes prove the link is delivering while a record is
				// still being reassembled: the 0x0007 answer is one
				// ~1 MB record, invisible to receivedFrames until its
				// last byte lands (#2311).
				if ( !disposed && generation === epoch && link === mine ) {
					receivedByteCount += value.byteLength;
				}
				const merged = new Uint8Array( pending.byteLength + value.byteLength );
				merged.set( pending );
				merged.set( value, pending.byteLength );
				pending = merged;
				let offset = 0;
				while ( pending.byteLength - offset >= 4 ) {
					const size = new DataView( pending.buffer, pending.byteOffset + offset, 4 ).getUint32( 0, true );
					if ( size < 2 || size > (16 << 20) ) throw invalid;
					if ( pending.byteLength - offset - 4 < size ) break;
					const record = pending.subarray( offset + 4, offset + 4 + size );
					offset += 4 + size;
					if ( !disposed && generation === epoch && link === mine ) receiveBytes( record.slice() );
				}
				pending = pending.slice( offset );
			}
		} catch ( error ) {
			if ( error === invalid ) failure = String( error );
		}
		if ( disposed || generation !== epoch || link !== mine ) return;
		if ( failure ) fail( failure );
		else wtClosed( generation, mine );
	}
	/*
	================
	connectWebTransport

	One WebTransport attempt on the control stream (datagrams stay
	unused: reliable parity first). True when the link is live with the
	hello sent. False when the path failed before the hello went out -
	the admission never left the client, so the caller falls back to
	the socket with it untouched. A failure after the hello hands the
	connect to the socket through wtClosed; either way, a failed path
	is remembered for the session.
	================
	*/
	async function connectWebTransport( endpoint: URL, hello: WireFrame, generation: number ): Promise<boolean> {
		const active = () => !disposed && generation === epoch;
		let transport: WebTransport | undefined;
		try {
			const hashes = await certHashes( endpoint );
			if ( !active() ) return false;
			const url = wtEndpoint( endpoint );
			if ( !url ) {
				// This route cannot be mapped onto a WT endpoint: the
				// client cannot attempt QUIC here (reason 1).
				wtFailReason = 1;
				return false;
			}
			transport = new WebTransport( url, hashes ? { serverCertificateHashes: hashes } : undefined );
			const session = transport;
			const stream = await withDeadline( establish( session ), WT_CONNECT_DEADLINE_MS );
			if ( !active() ) {
				session.close();
				return false;
			}
			const writer = stream.writable.getWriter(), reader = stream.readable.getReader();
			let bufferedBytes = 0, closing = false;
			const mine: Link = {
				get state() {
					return closing ? "closing" : "open";
				},
				buffered: () => bufferedBytes,
				send( bytes ) {
					const framed = new Uint8Array( bytes.byteLength + 4 );
					new DataView( framed.buffer ).setUint32( 0, bytes.byteLength, true );
					framed.set( bytes, 4 );
					bufferedBytes += framed.byteLength;
					writer.write( framed ).then(
						() => {
							bufferedBytes -= framed.byteLength;
						},
						() => {
							bufferedBytes -= framed.byteLength;
							wtClosed( generation, mine );
						}
					);
				},
				close() {
					closing = true;
					void reader.cancel().catch( () => {} );
					void writer.close().catch( () => {} );
					session.close();
				}
			};
			link = mine;
			session.closed.then(
				() => wtClosed( generation, mine ),
				() => wtClosed( generation, mine )
			);
			void wtPump( reader, generation, mine );
			send( hello );
			armWelcomeDeadline( generation, mine );
			return true;
		} catch ( error ) {
			transport?.close();
			if ( active() ) {
				wtFailed = true;
				// The establish deadline is a timeout (reason 2); any
				// other refusal of the session or stream is refused (3).
				wtFailReason = error instanceof Error && error.message === "Transport connect deadline" ? 2 : 3;
			}
			return false;
		}
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
		receivedBytes

		Every inbound byte so far, counted as it arrives: a record still
		being reassembled (the ~1 MB 0x0007 answer) moves this long
		before received() can, so the stall detector reads a link that
		is still streaming as alive (#2311).
		================
		*/
		receivedBytes() {
			return receivedByteCount;
		},
		/*
		================
		receivedAt

		The last-inbound time on the event clock, stamped in onmessage
		(#1474): the close detail below and the world session's stall
		detector both read this one stamp.
		================
		*/
		receivedAt() {
			return receivedAt;
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
			if ( !link || !welcomed || link.state !== "open" ) return null;
			const now = performance.now();
			if ( now - pingSentAt >= PING_INTERVAL_MS && link.buffered() < PING_BACKLOG_LIMIT ) {
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
		webTransport( enabled: boolean ) {
			wtEnabled = enabled;
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
			attempt = { endpoint, hello, fellBack: false };
			const generation = epoch;
			transportFallback = 0;
			if ( wtEnabled && !wtFailed && typeof WebTransport !== "undefined" ) {
				void connectWebTransport( endpoint, hello, generation ).then( linked => {
					// The attempt failed before the hello went out: the
					// admission is untouched, so the socket takes it over.
					if ( !linked && !disposed && generation === epoch && !link ) {
						transportFallback = wtFailReason;
						connectSocket( endpoint, hello, generation );
					}
				} );
				return;
			}
			if ( wtEnabled && wtFailed ) {
				// The session already knows the QUIC path is dead here:
				// the socket goes straight in, and the report still names
				// the remembered failure.
				transportFallback = wtFailReason;
			} else if ( wtEnabled ) {
				// The browser has no WebTransport at all (reason 1).
				transportFallback = 1;
			}
			connectSocket( endpoint, hello, generation );
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
