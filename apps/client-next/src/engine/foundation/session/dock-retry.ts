/*
===========================================================================

dock-retry.ts - leaving for character select is repeated until it lands

Restart (leave the world for character select) asks the Agent to forget
the remembered character (/title/character-select). Until it does, a
reload re-enters the world instead of showing the roster (ops #329).
The roster is already on screen when that request fails, so nothing
else would repeat it: a dropped connection or a server error is repeated
after DOCK_RETRY_DELAYS_MS. Any other answer is final. The outdated 426
is final too, and the Agent forgets on that route even while refusing.

===========================================================================
*/

const DOCK_RETRY_DELAYS_MS = [ 250, 1000, 3000 ] as const;
const SERVER_ERROR_STATUS = 500;

/*
================
DockFailure

How a /title/character-select request ended without an accepted answer:
status is absent when no answer arrived at all.
================
*/
export interface DockFailure {
	readonly status?: number;
}

/*
================
dockRetryDelayMs

The wait before repeat number attempt + 1, or null when the failure is
final (an answered refusal, or the repeats are spent).
================
*/
export function dockRetryDelayMs( failure: DockFailure, attempt: number ): number | null {
	if ( attempt < 0 || attempt >= DOCK_RETRY_DELAYS_MS.length ) return null;
	if ( failure.status !== undefined && failure.status < SERVER_ERROR_STATUS ) return null;
	return DOCK_RETRY_DELAYS_MS[attempt] ?? null;
}
