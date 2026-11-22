/*
===========================================================================

job-honor.ts - the Job Honor window's words and row shaping

Pure shaping for the Job Honor Rank window (port-only, #3530): the
job and tier names, the rows a tab and a period show, and the texts
the My honor panel speaks. The server composes the board (its
BoardRows: points down, alias breaking ties, each row keeping its
own job); the tabs re-run that same composition over one job's rows,
so the in-game order can never disagree with the public board's.

===========================================================================
*/

import type { JobHonorFeed, JobHonorRow } from "@/engine/contracts/job-honor";

export type JobHonorTab = "all" | "trader" | "thief";
export type JobHonorPeriod = "current" | "previous";

export const JOB_HONOR_TABS = [
	{ id: "all", label: "All" },
	{ id: "trader", label: "Trader" },
	{ id: "thief", label: "Thief" }
] as const;

// The wire's job numbers (ledger's JobTrader..JobHunter).
export const JOB_TRADER = 1;
export const JOB_THIEF = 2;
export const JOB_HUNTER = 3;

/*
================
jobName

The word a row's job number speaks; anything else is no job.
================
*/
export function jobName( job: number ): string {
	switch ( job ) {
		case JOB_TRADER:
			return "Trader";
		case JOB_THIEF:
			return "Thief";
		case JOB_HUNTER:
			return "Hunter";
		default:
			return "";
	}
}

/*
================
tierName

The word a tier number speaks (0 none, 1 silver, 2 gold, 3 king).
================
*/
export function tierName( tier: number ): string {
	switch ( tier ) {
		case 1:
			return "Silver";
		case 2:
			return "Gold";
		case 3:
			return "King";
		default:
			return "None";
	}
}

/*
================
honorRows

The rows one tab of one period shows, in board order: the All tab
keeps the order the server sent; a job tab re-runs the board's
composition (points down, alias breaking ties) over its own rows.
================
*/
export function honorRows( feed: JobHonorFeed, tab: JobHonorTab, period: JobHonorPeriod ): readonly JobHonorRow[] {
	const rows = period === "current" ? feed.current : feed.previous;
	if ( tab === "all" ) return rows;
	const job = tab === "trader" ? JOB_TRADER : JOB_THIEF;
	return rows
		.filter( row => row.job === job )
		.sort( ( a, b ) => b.points - a.points || (a.alias < b.alias ? -1 : a.alias > b.alias ? 1 : 0) );
}

/*
================
honorToday

What the viewer kept today, over every job.
================
*/
export function honorToday( feed: JobHonorFeed ): number {
	const today = feed.own.today;
	return today[0] + today[1] + today[2];
}

/*
================
honorMonthText

The month line: one job's kept points each, in job order.
================
*/
export function honorMonthText( feed: JobHonorFeed ): string {
	const month = feed.own.month;
	return `Trader ${month[0]} · Thief ${month[1]} · Hunter ${month[2]}`;
}

/*
================
honorCountdownText

How long until the next tier change, as H:MM from the server clock.
================
*/
export function honorCountdownText( feed: JobHonorFeed, serverNowMs: number ): string {
	const left = Math.max( 0, feed.nextChangeMs - serverNowMs );
	const minutes = Math.floor( left / 60_000 );
	return Math.floor( minutes / 60 ) + ":" + String( minutes % 60 ).padStart( 2, "0" );
}
