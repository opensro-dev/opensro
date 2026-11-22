/*
===========================================================================

job-honor-hud.ts - the Job Honor Rank window's state

Port-only, not native (#3530). The window's own state: the tab, the
period and the page, plus the latest feed the worker decoded and the
server clock run forward from it. The feed arrives over the worker
lane (SRO_JOB_HONOR); while the Experimental row is off, reconcile
drops the feed and the window is never offered, so turning it back on
never shows a stale board.

===========================================================================
*/

import type { JobHonorFeed } from "@/engine/contracts/job-honor";
import type { JobHonorPeriod, JobHonorTab } from "@/engine/foundation/ui/job-honor";

/*
================
JobHonorHudState

What the window draws: the latest feed (null until one arrives or
while the row is off) and the clock to read it by.
================
*/
export interface JobHonorHudState {
	readonly feed: JobHonorFeed | null;
	readonly tab: JobHonorTab;
	readonly period: JobHonorPeriod;
	readonly page: number;
	readonly serverNowMs: number;
}

/*
================
createJobHonorHud
================
*/
export function createJobHonorHud() {
	let feed: JobHonorFeed | null = null;
	let tab: JobHonorTab = "all";
	let period: JobHonorPeriod = "current";
	let page = 0;
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
 state
 ================
 */
	function state( nowMs: number ): JobHonorHudState {
		return { feed, tab, period, page, serverNowMs: serverNowMs( nowMs ) };
	}

	/*
 ================
 reconcile

 One frame: the feed the state holds, or none while the row is off.
 A new snapshot re-bases the server clock.
 ================
 */
	function reconcile( next: JobHonorFeed | null | undefined, enabled: boolean, nowMs: number ) {
		if ( !enabled ) {
			feed = null;
			return;
		}
		if ( next && next !== feed ) {
			feed = next;
			clockBase = { serverMs: next.serverNowMs, atMs: nowMs };
		}
	}

	/*
 ================
 selectTab
 ================
 */
	function selectTab( next: JobHonorTab ) {
		tab = next;
		page = 0;
	}

	/*
 ================
 selectPeriod
 ================
 */
	function selectPeriod( next: JobHonorPeriod ) {
		period = next;
		page = 0;
	}

	/*
 ================
 turnPage

 One page either way over pageCount pages; the ends hold.
 ================
 */
	function turnPage( delta: number, pageCount: number ) {
		page = Math.min( Math.max( 0, pageCount - 1 ), Math.max( 0, page + delta ) );
	}

	/*
 ================
 reset

 Leaving the session forgets the feed and the window's choices.
 ================
 */
	function reset() {
		feed = null;
		tab = "all";
		period = "current";
		page = 0;
		clockBase = { serverMs: 0, atMs: 0 };
	}

	return { state, reconcile, selectTab, selectPeriod, turnPage, reset };
}
