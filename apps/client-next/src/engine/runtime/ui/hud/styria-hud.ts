/*
===========================================================================

styria-hud.ts - the Styria Clash windows' state

Port-only, not native (#3648 slice 5). The windows' own state: the
latest feed and latched result the worker decoded, and the server
clock run forward from them. The feed arrives over the worker lane
(SRO_STYRIA_CLASH); while the feature is off nothing arrives and
the state stays empty. A latched result shows until the player
dismisses it; a newer match supersedes it.

===========================================================================
*/

import type { StyriaFeed } from "@/engine/contracts/styria";

/*
================
StyriaHudState

What the windows draw: the live feed, the result still standing
(null once dismissed or superseded) and the clock to read them by.
================
*/
export interface StyriaHudState {
	readonly feed: StyriaFeed | null;
	readonly result: StyriaFeed | null;
	readonly serverNowMs: number;
}

/*
================
createStyriaHud
================
*/
export function createStyriaHud() {
	let feed: StyriaFeed | null = null;
	let result: StyriaFeed | null = null;
	let dismissedSeq = -1;
	let shownSeq = -1;
	let clockBase = { serverMs: 0, atMs: 0 };

	/*
 ================
 serverNowMs

 The server's clock at a local instant: the last snapshot's clock
 advanced by the local time since it landed.
 ================
 */
	function serverNowMs( nowMs: number ): number {
		return clockBase.serverMs ? clockBase.serverMs + Math.max( 0, nowMs - clockBase.atMs ) : nowMs;
	}

	/*
 ================
 standingResult

 The latched result still standing: not dismissed, and not
 superseded by a newer match already running.
 ================
 */
	function standingResult(): StyriaFeed | null {
		if ( !result || result.matchSeq === dismissedSeq ) return null;
		if ( feed && feed.phase === "match" && feed.matchSeq !== result.matchSeq ) return null;
		return result;
	}

	/*
 ================
 state
 ================
 */
	function state( nowMs: number ): StyriaHudState {
		return { feed, result: standingResult(), serverNowMs: serverNowMs( nowMs ) };
	}

	/*
 ================
 reconcile

 One frame: the feed and latch the worker holds now. A new
 snapshot re-bases the server clock.
 ================
 */
	function reconcile(
		next: StyriaFeed | null | undefined,
		nextResult: StyriaFeed | null | undefined,
		nowMs: number
	) {
		if ( next && next !== feed ) {
			feed = next;
			clockBase = { serverMs: next.serverNowMs, atMs: nowMs };
		} else if ( next === null ) {
			feed = null;
		}
		result = nextResult ?? null;
	}

	/*
 ================
 resultPending

 A result stands that its window has not opened for yet: the
 renderer opens the window once per match.
 ================
 */
	function resultPending(): boolean {
		const standing = standingResult();
		return standing !== null && standing.matchSeq !== shownSeq;
	}

	/*
 ================
 markResultShown
 ================
 */
	function markResultShown() {
		if ( result ) shownSeq = result.matchSeq;
	}

	/*
 ================
 dismissResult

 The player's Confirm: the standing result is done.
 ================
 */
	function dismissResult() {
		if ( result ) dismissedSeq = result.matchSeq;
	}

	/*
 ================
 reset

 Leaving the session forgets the standing and the dismissals.
 ================
 */
	function reset() {
		feed = null;
		result = null;
		dismissedSeq = -1;
		shownSeq = -1;
		clockBase = { serverMs: 0, atMs: 0 };
	}

	return { state, reconcile, resultPending, markResultShown, dismissResult, reset };
}
