/*
===========================================================================

server-event.ts - the Server Mission gauge the server event message sets

Native to the later v1.696 client, which is native here: it reads 0x34F2
(0x00CFA790) and draws CNIFServerEvent, one mission's count towards its
goal. Every row it is given replaces the window's mission (0x0080F4E0);
a count alone moves the open window against the goal it holds
(0x00E1E7A0); the end closes it (0x00808620). Only the gauge-flagged
missions reach a client (the server's winter_gauge.go). Byte 0 is the
message type:

  1 completion  u32 ID, u8 target, [str winner if 2], u8 data,
                [u32 count if data 1 or 3][u32 goal if data 3], reward
  2 sync        u8 sub (1 count, 3 count and goal), u32 N, rows
  3 update      u32 ID, u8 kind, [u32 goal if kind 2]
  5 start       u32 ID, u8 flag, [u32 goal if flag]
  6 end         u32 ID

Types 4, 7, 8 and 9 (rollback, reward times) touch the reward board, not
the gauge, and are consumed unread. A completion's reward tail is not
read either: its layout differs between v1.188 and v1.696, and the
notices the server sends already say it.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const OP_SERVER_EVENT = 0x34f2;

const TYPE_COMPLETION = 1, TYPE_SYNC = 2, TYPE_UPDATE = 3, TYPE_START = 5, TYPE_END = 6;
const SYNC_COUNT = 1, SYNC_FULL = 3;
const DATA_COUNT = 1, DATA_COUNT_GOAL = 3;
const TARGET_WINNER = 2;
const UPDATE_GOAL = 2;

/*
================
ServerEventGauge

The mission the window shows.
================
*/
export interface ServerEventGauge {
	readonly id: number;
	readonly count: number;
	readonly goal: number;
}

/*
================
Reader

A bounds-checked cursor over one body; running past the end is a
protocol error.
================
*/
interface Reader {
	u8(): number;
	u16(): number;
	u32(): number;
	skip( n: number ): void;
	left(): number;
}

function reader( p: Uint8Array ): Reader {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let at = 0;
	const need = ( n: number ) => {
		if ( at + n > p.length ) throw new Error( "Invalid server event" );
		const here = at;
		at += n;
		return here;
	};
	return {
		u8: () => v.getUint8( need( 1 ) ),
		u16: () => v.getUint16( need( 2 ), true ),
		u32: () => v.getUint32( need( 4 ), true ),
		skip( n: number ) {
			need( n );
		},
		left: () => p.length - at
	};
}

/*
================
decode

The gauge after one body, from the gauge before it. Throws on a body
that cannot be read whole; the caller then keeps the old gauge.
================
*/
function decode( p: Uint8Array, gauge: ServerEventGauge | null ): ServerEventGauge | null {
	const r = reader( p );
	const type = r.u8();
	let next = gauge;
	switch ( type ) {
		case TYPE_COMPLETION: {
			const id = r.u32();
			if ( r.u8() === TARGET_WINNER ) r.skip( r.u16() );
			const data = r.u8();
			if ( data === DATA_COUNT_GOAL ) {
				const count = r.u32();
				next = { id, count, goal: r.u32() };
			} else if ( data === DATA_COUNT ) {
				const count = r.u32();
				if ( gauge ) next = { ...gauge, count };
			}
			// The reward tail: see the banner.
			return next;
		}
		case TYPE_SYNC: {
			const sub = r.u8(), n = r.u32();
			for ( let i = 0; i < n; i++ ) {
				const id = r.u32(), count = r.u32();
				if ( sub === SYNC_FULL ) next = { id, count, goal: r.u32() };
				else if ( sub === SYNC_COUNT && next ) next = { ...next, count };
			}
			break;
		}
		case TYPE_UPDATE: {
			const id = r.u32();
			if ( r.u8() === UPDATE_GOAL ) next = { id, count: 0, goal: r.u32() };
			break;
		}
		case TYPE_START: {
			const id = r.u32();
			if ( r.u8() !== 0 ) next = { id, count: 0, goal: r.u32() };
			break;
		}
		case TYPE_END:
			r.u32();
			next = null;
			break;
		default:
			// Reward-board messages: consumed, not read.
			return gauge;
	}
	if ( r.left() !== 0 ) throw new Error( "Invalid server event" );
	return next;
}

/*
================
createServerEvent
================
*/
export function createServerEvent() {
	let gauge: ServerEventGauge | null = null;
	return {
		/*
================
receive

Consumes a server event frame; false for any other opcode. A malformed
body is a protocol error and leaves the gauge as it was.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_SERVER_EVENT ) return false;
			gauge = decode( frame.payload, gauge );
			return true;
		},
		state: () => gauge,
		/*
================
clear
================
		*/
		clear() {
			gauge = null;
		}
	};
}
