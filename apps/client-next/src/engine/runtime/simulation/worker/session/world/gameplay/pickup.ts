/*
===========================================================================

pickup.ts - one outstanding intent for each selected ground target

The native 75BAA0 response owns action release; 6955FA emits the same pickup
for shortcut and direct interaction. The port coalesces repeated intent while
that reply is in flight, rather than submitting stale copies after a grant.
This is an intentional reliability improvement over native burst throttling.
There is no guessed delay: release, replacement, despawn and world reset own
the lifetime. Server ownership and range checks remain authoritative.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

const OP_TARGET_ACTION = 0x72cd;
const OP_ACTION_STATE = 0xb2cd;
const ACTION_PICKUP = 2;
const ACTION_NOTICE = 3;

/*
================
createPickup
================
*/
export function createPickup() {
	let target = 0;
	return {
		/*
================
request

A different target replaces the approach. Repeating the current intent does
not enqueue another grant attempt behind the one already on the wire.
================
		*/
		request( gid: number ): WireFrame | null {
			if ( target === gid ) return null;
			const payload = Uint8Array.of( 1, ACTION_PICKUP, 1, 0, 0, 0, 0 );
			new DataView( payload.buffer ).setUint32( 3, gid, true );
			target = gid;
			return { opcode: OP_TARGET_ACTION, payload };
		},
		/*
================
sent

Another object action replaces pickup intent. Its own response must remain
free to pass through the common feedback and combat owners.
================
		*/
		sent( frame: WireFrame ) {
			if (
				frame.opcode === OP_TARGET_ACTION && !(frame.payload[0] === 1 && frame.payload[1] === ACTION_PICKUP)
			) {
				target = 0;
			}
		},
		/*
================
receive
================
		*/
		receive( frame: WireFrame ) {
			if ( frame.opcode !== OP_ACTION_STATE ) return false;
			const payload = frame.payload;
			if ( payload.length !== (payload[0] === ACTION_NOTICE ? 3 : 2) ) {
				throw new Error( "Invalid object action state" );
			}
			if ( payload[1] === 0 ) target = 0;
			return true;
		},
		/*
================
remove
================
		*/
		remove( gid: number ) {
			if ( target === gid ) target = 0;
		},
		/*
================
clear
================
		*/
		clear() {
			target = 0;
		}
	};
}
