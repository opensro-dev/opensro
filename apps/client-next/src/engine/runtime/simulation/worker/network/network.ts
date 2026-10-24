/*
===========================================================================

network.ts - the world transport and its inbound frame queue

One frame-queue owner over whichever link the session connected with: a
WebSocket (one message is one frame), or, behind the Experimental
WebTransport row (#1448), a WebTransport control stream carrying the
same frames u32-length-prefixed (the server envelope's stream form).
Callbacks admit packets, simulation ticks consume them in order.
Reconnection is explicit. Never retry commands or reuse admission
tickets here. A frame its consumer cannot apply ends the session, and
the failure names that frame so it can be reported (session.ts).

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
// A WebTransport attempt gets this long to fetch its certificate pin and
// establish (session plus control stream) before the socket takes the
// same admission over, and this long after the hello for the WELCOME
// before the path counts as failed for the session: a QUIC path that
// stalls pre-WELCOME never silently holds a login.
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
	let welcomeTimer: ReturnType<typeof setTimeout> | null = null;
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
		clearWelcomeDeadline();
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
			const frame = codec.decode( data );
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
				codec.welcome( frame.payload );
				welcomed = true;
				clearWelcomeDeadline();
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
			if ( !(event.data instanceof ArrayBuffer) ) {
				fail( String( new Error( "Non-binary transport frame" ) ) );
				return;
			}
			receiveBytes( new Uint8Array( event.data ) );
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
	failed: end the attempt inside this window as the socket's connection
	failure, so the session's retry reaches the socket with a fresh
	admission instead of knocking on the dead path again.
	================
	*/
	function armWelcomeDeadline( generation: number ) {
		clearWelcomeDeadline();
		welcomeTimer = setTimeout( () => {
			welcomeTimer = null;
			if ( disposed || generation !== epoch || welcomed ) return;
			wtFailed = true;
			fail( "Transport connection failed", undefined, {
				category: "connection",
				code: "connection_failed",
				message: "Connection lost. Please reconnect."
			} );
		}, WT_WELCOME_DEADLINE_MS );
	}
	/*
	================
	wtClosed

	The WebTransport session or its control stream ended. Before the
	WELCOME the path itself failed - remembered for the session, and
	reported as the socket's connection failure; after it, the link
	simply closed, as a socket would.
	================
	*/
	function wtClosed( generation: number, mine: Link ) {
		if ( disposed || generation !== epoch || link !== mine ) return;
		if ( !welcomed ) {
			wtFailed = true;
			fail( "Transport connection failed", undefined, {
				category: "connection",
				code: "connection_failed",
				message: "Connection lost. Please reconnect."
			} );
		} else {
			end( "Transport connection closed", {
				category: "unknown",
				code: "connection_closed",
				message: "Connection lost. The cause is unknown."
			} );
		}
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
	the socket with it untouched. A failure after the hello ends this
	attempt through the usual failure paths; either way, a failed path
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
			if ( !url ) return false;
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
			armWelcomeDeadline( generation );
			return true;
		} catch {
			transport?.close();
			if ( active() ) wtFailed = true;
			return false;
		}
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
			const hello = codec.hello( admission, resume );
			disconnect();
			const generation = epoch;
			if ( wtEnabled && !wtFailed && typeof WebTransport !== "undefined" ) {
				void connectWebTransport( endpoint, hello, generation ).then( linked => {
					// The attempt failed before the hello went out: the
					// admission is untouched, so the socket takes it over.
					if ( !linked && !disposed && generation === epoch && !link ) {
						connectSocket( endpoint, hello, generation );
					}
				} );
				return;
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
