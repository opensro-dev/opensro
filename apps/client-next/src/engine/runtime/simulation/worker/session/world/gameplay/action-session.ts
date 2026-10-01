/*
===========================================================================

action-session.ts - the server-owned object-action queue and movement cancel

75BAA0 stores B2CD's second byte in interface+618. It is a command count,
not a pickup type or a rendering state. Ground movement cancels exactly one
queued command (6932D7 and 67D140); two commands must first drain to one.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { targetActionCancel } from "@/engine/foundation/gameplay/direction-movement";

const OP_ACTION_STATE = 0xb2cd;
const ACTION_NOTICE = 3;
const SINGLE_COMMAND = 1;

/*
================
createActionSession

Only replies change the authoritative count. Coalesce movement cancellation
while its response is pending, independently of the latest held destination.
================
*/
export function createActionSession() {
	let count = 0, cancellationPending = false;
	return {
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
			if ( payload[0] !== ACTION_NOTICE || payload[1] !== count ) cancellationPending = false;
			count = payload[1]!;
			return true;
		},
		/*
================
cancelForMovement

A committed skill can refuse cancellation. Keep waiting for its normal
release instead of sending a cancel on every simulation frame.
================
		*/
		cancelForMovement(): WireFrame | null {
			if ( count !== SINGLE_COMMAND || cancellationPending ) return null;
			return targetActionCancel();
		},
		/*
================
sentCancellation

Advance only after transport accepts the request, so backpressure can retry.
================
		*/
		sentCancellation() {
			cancellationPending = true;
		},
		/*
================
released

The server reports no queued command: a cancelled basic attack is over even
while its swing's cast has not formally closed. CGObjPC_IsMotionChangeLocked
(server 4EF880) refuses movement only for a committed front command, so a
held move may go now; a committed skill keeps the count and holds the move.
================
		*/
		released(): boolean {
			return count === 0;
		},
		/*
================
clear
================
		*/
		clear() {
			count = 0;
			cancellationPending = false;
		}
	};
}
