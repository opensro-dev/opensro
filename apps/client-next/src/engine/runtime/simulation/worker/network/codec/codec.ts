/*
===========================================================================

codec.ts - the world transport envelope: frame bytes, HELLO, WELCOME, ACK

Encodes and decodes the transport frames the network owner (network.ts)
sends and receives. The server's envelope.go is the wire authority.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
// Wire authority: apps/server/internal/transport/envelope.go (protocol v2).
const SERVER_LIMIT = 16 << 20, CLIENT_LIMIT = 64 << 10;
// The session-internal range (envelope.go maxReservedOpcode): HELLO,
// WELCOME, PING, PONG, BYE. Frames above it count on the stream position.
export const MAX_RESERVED_OPCODE = 5;
export const OP_STREAM_ACK = 0x000b;
/*
================
createCodec
================
*/
export function createCodec() {
	const text = new TextEncoder();
	return {
		encode( frame: WireFrame ): Uint8Array<ArrayBuffer> {
			if (
				!Number.isInteger( frame.opcode ) || frame.opcode < 0 || frame.opcode > 65535 ||
				frame.payload.byteLength + 2 > CLIENT_LIMIT
			) {
				throw new Error( "Invalid outbound frame" );
			}
			const bytes = new Uint8Array( frame.payload.byteLength + 2 );
			new DataView( bytes.buffer ).setUint16( 0, frame.opcode, true );
			bytes.set( frame.payload, 2 );
			return bytes;
		},
		decode( bytes: Uint8Array ): WireFrame {
			if ( bytes.byteLength < 2 || bytes.byteLength > SERVER_LIMIT ) {
				throw new Error( "Invalid inbound frame length" );
			}
			return {
				opcode: new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength ).getUint16( 0, true ),
				payload: bytes.subarray( 2 )
			};
		},
		// A resume carries the stream position this client last applied
		// (u32 tail, #1466): the server re-sends what came after it.
		hello( admission: string, resume: Uint8Array = new Uint8Array(), lastApplied = 0 ): WireFrame {
			const ticket = text.encode( admission );
			if ( ticket.length < 1 || ticket.length > 512 || (resume.length !== 0 && resume.length !== 16) ) {
				throw new Error( "Invalid transport admission" );
			}
			const tail = resume.length ? 4 : 0;
			const payload = new Uint8Array( 4 + resume.length + ticket.length + tail );
			payload[0] = 2;
			payload[1] = resume.length;
			payload.set( resume, 2 );
			new DataView( payload.buffer ).setUint16( 2 + resume.length, ticket.length, true );
			payload.set( ticket, 4 + resume.length );
			if ( tail ) {
				new DataView( payload.buffer ).setUint32( payload.length - 4, lastApplied >>> 0, true );
			}
			return { opcode: 1, payload };
		},
		streamAck( position: number ): WireFrame {
			const payload = new Uint8Array( 4 );
			new DataView( payload.buffer ).setUint32( 0, position >>> 0, true );
			return { opcode: OP_STREAM_ACK, payload };
		},
		welcome( payload: Uint8Array ) {
			if ( payload.length !== 27 || payload[0] !== 2 || payload[1]! > 1 || payload[10] !== 16 ) {
				throw new Error( "Invalid transport welcome" );
			}
			return {
				resumed: payload[1] === 1,
				sessionId: new DataView( payload.buffer, payload.byteOffset, payload.byteLength ).getBigUint64(
					2,
					true
				),
				resume: payload.slice( 11 )
			};
		},
		enterWorld( division: string, character: string, admission: string ): WireFrame {
			const parts = [ text.encode( division ), text.encode( character ), text.encode( admission ) ];
			if ( parts.some( p => p.length > 65535 ) || parts[2]!.length < 1 || parts[2]!.length > 512 ) {
				throw new Error( "Invalid world admission" );
			}
			const size = parts.reduce( ( sum, p ) => sum + 2 + p.length, 0 );
			if ( size + 2 > CLIENT_LIMIT ) {
				throw new Error( "World admission exceeds frame limit" );
			}
			const payload = new Uint8Array( size ), view = new DataView( payload.buffer );
			let offset = 0;
			for ( const part of parts ) {
				view.setUint16( offset, part.length, true );
				payload.set( part, offset + 2 );
				offset += part.length + 2;
			}
			return { opcode: 6, payload };
		}
	};
}
