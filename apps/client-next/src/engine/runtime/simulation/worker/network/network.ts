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
import type { NetworkOwner, WireFrame, NetworkFailure } from "@/engine/contracts/network";

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
	const inbox: WireFrame[] = [];
	let ended: string | null = null;
	let endReason: NetworkFailure | undefined;
	/*
	================
	disconnect
	================
	*/
	function disconnect() {
		epoch++;
		welcomed = false;
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
	}
	return {
		disconnect,
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
