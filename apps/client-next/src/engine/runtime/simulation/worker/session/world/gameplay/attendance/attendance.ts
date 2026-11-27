/*
===========================================================================

attendance.ts - the attendance calendar feed (port-only 0x3FD2, not native)

#3255: with SRO_ATTENDANCE on, the server sends one snapshot of the
character's attendance month whenever it changes. This owner decodes the
latest snapshot (foundation/gameplay/attendance.ts) for the calendar
window; nothing arrives when the flag is off.

===========================================================================
*/
import type { AttendanceFeed } from "@/engine/contracts/attendance";
import type { WireFrame } from "@/engine/contracts/network";
import { decodeAttendanceSnapshot, OP_ATTENDANCE_SNAPSHOT } from "@/engine/foundation/gameplay/attendance";

/*
================
createAttendance
================
*/
export function createAttendance() {
	let feed: AttendanceFeed | null = null;
	// Never reset: a revision restarting after a teleport would look
	// like the calendar standing still to the window.
	let revision = 0;

	return {
		/*
		================
		receive

		Consumes an attendance snapshot; false for any other opcode. A
		snapshot replaces the feed wholesale. A malformed snapshot is a
		protocol error and leaves the last feed standing.
		================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_ATTENDANCE_SNAPSHOT ) return false;
			feed = { ...decodeAttendanceSnapshot( frame.payload ), revision: ++revision };
			return true;
		},
		view: () => feed,
		/*
		================
		clear

		Drops the feed but keeps the revision (see above).
		================
		*/
		clear() {
			feed = null;
		}
	};
}
