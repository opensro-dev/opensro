/*
===========================================================================

reverse-user-point.ts - the saved reverse return location (port-only 0x3FCE)

The server tells the owner its enrolled location at world entry (only
while one is enrolled) and after every enroll, so the user settings
window can say whether a location exists. This owner decodes the frame
whole or not at all and keeps the latest point; every rule (where a
location may be set, who may be moved to) is the server's. Nothing
arrives on a native shard.

===========================================================================
*/
import {
	OP_REVERSE_USER_POINT,
	decodeReverseUserPoint,
	type ReverseUserPoint
} from "@/engine/foundation/gameplay/reverse-user-point";
import type { WireFrame } from "@/engine/contracts/network";

/*
================
createReverseUserPoint
================
*/
export function createReverseUserPoint() {
	let point: ReverseUserPoint | null = null;
	return {
		/*
================
receive

Consumes a saved-location frame; false for any other opcode. A malformed
frame is a protocol error, never a partial one.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_REVERSE_USER_POINT ) return false;
			point = decodeReverseUserPoint( frame.payload );
			return true;
		},
		/*
================
point

The saved location last told, or null when none is enrolled.
================
		*/
		point: (): ReverseUserPoint | null => point,
		/*
================
clear
================
		*/
		clear() {
			point = null;
		}
	};
}
