/*
===========================================================================

attendance-hud.ts - the Attendance Calendar window's state

Port-only, not native (#3255). The window's own state: the tab and the
selected day, plus the latest feed the worker decoded and the server
clock run forward from it. The feed arrives over the worker lane
(SRO_ATTENDANCE); while the Experimental row is off, reconcile drops the
feed and activate sends nothing, so turning it back on never shows a
stale month. A click on Check Attendance or a Receive button returns the
gameplay command for ui.ts to send; the server decides each one.

===========================================================================
*/

import type { AttendanceFeed } from "@/engine/contracts/attendance";
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import { type AttendanceColumn, dayChecked, tierClaimed, tierEarned } from "@/engine/foundation/gameplay/attendance";

export type AttendanceTab = "calendar" | "rewards";
export type AttendanceSend = Extract<GameplayCommand, { kind: "attendance"; }>;

/*
================
AttendanceHudState

What the window draws: the latest feed (null until one arrives or while
the row is off), the tab, the selected day (0 none) and the clock to read
the feed by.
================
*/
export interface AttendanceHudState {
	readonly feed: AttendanceFeed | null;
	readonly tab: AttendanceTab;
	readonly selectedDay: number;
	readonly serverNowMs: number;
}

/*
================
attendanceCheckInRequest

The request that checks in today.
================
*/
export function attendanceCheckInRequest(): AttendanceSend {
	return { kind: "attendance", operation: 0, tier: 0 };
}

/*
================
attendanceClaimRequest

The request that claims tier `index` of a column.
================
*/
export function attendanceClaimRequest( column: AttendanceColumn, index: number ): AttendanceSend {
	return { kind: "attendance", operation: column === "continuity" ? 1 : 2, tier: index };
}

/*
================
createAttendanceHud
================
*/
export function createAttendanceHud() {
	let feed: AttendanceFeed | null = null;
	let tab: AttendanceTab = "calendar";
	let selectedDay = 0;
	let clockBase = { serverMs: 0, atMs: 0 };

	/*
	================
	state
	================
	*/
	function state( nowMs: number ): AttendanceHudState {
		// The server's clock at a local instant: the snapshot's clock
		// advanced by the local time since it landed.
		const serverNowMs = clockBase.serverMs ? clockBase.serverMs + Math.max( 0, nowMs - clockBase.atMs ) : nowMs;
		return { feed, tab, selectedDay, serverNowMs };
	}

	/*
	================
	reconcile

	One frame: the feed the state holds, or none while the row is off. A
	new snapshot re-bases the server clock.
	================
	*/
	function reconcile( next: AttendanceFeed | null | undefined, enabled: boolean, nowMs: number ) {
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
	activate

	One click on an attendance-* control: a tab or a day is the window's
	own; Check Attendance and Receive return the command to send, or null
	when there is nothing to ask (no feed, today already checked, a tier
	unearned or already received).
	================
	*/
	function activate( id: string ): AttendanceSend | null {
		if ( id === "attendance-tab:calendar" || id === "attendance-tab:rewards" ) {
			tab = id === "attendance-tab:rewards" ? "rewards" : "calendar";
			return null;
		}
		if ( id.startsWith( "attendance-day:" ) ) {
			const day = Number( id.slice( "attendance-day:".length ) );
			selectedDay = Number.isInteger( day ) && day >= 1 && day <= 31 && day !== selectedDay ? day : 0;
			return null;
		}
		if ( !feed ) return null;
		if ( id === "attendance-check-in" ) {
			return dayChecked( feed.checked, feed.today ) ? null : attendanceCheckInRequest();
		}
		const claim = /^attendance-claim:(continuity|accumulation):(\d+)$/.exec( id );
		if ( claim ) {
			const column = claim[1] as AttendanceColumn, index = Number( claim[2] );
			const mask = column === "continuity" ? feed.continuityClaimed : feed.accumulationClaimed;
			return tierEarned( feed, column, index ) && !tierClaimed( mask, index ) ?
				attendanceClaimRequest( column, index ) :
				null;
		}
		return null;
	}

	/*
	================
	reset

	Leaving the session forgets the feed and the window's choices.
	================
	*/
	function reset() {
		feed = null;
		tab = "calendar";
		selectedDay = 0;
		clockBase = { serverMs: 0, atMs: 0 };
	}

	return { state, reconcile, activate, reset };
}
