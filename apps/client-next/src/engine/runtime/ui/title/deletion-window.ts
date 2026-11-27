/*
===========================================================================

deletion-window.ts - how long a deleted character waits on the select screen

The select screen counts down from the deletion reservation to the instant
the server archives the character. The server names that instant
(deleteMaturesAt) from the shard's deletion window, so a one-day shard
(SRO_QUICK_CHARACTER_DELETE) counts one day; an older server that names
none keeps the native seven.

===========================================================================
*/

// The native deletion wait: the countdown's span when the server names no
// matured instant (an older server).
const NATIVE_DELETION_WINDOW_MS = 7 * 24 * 3600000;

/*
================
deletionWindowMs

The countdown span from reservedAt to maturesAt, or the native seven days
when either is missing or the span is not positive.
================
*/
export function deletionWindowMs( reservedAt: number, maturesAt: number ): number {
	return Number.isFinite( reservedAt ) && Number.isFinite( maturesAt ) && maturesAt > reservedAt ?
		maturesAt - reservedAt :
		NATIVE_DELETION_WINDOW_MS;
}
