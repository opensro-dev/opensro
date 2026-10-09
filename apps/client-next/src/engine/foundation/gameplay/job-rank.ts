/*
===========================================================================

job-rank.ts - the job guilds' rank windows (CIFJobRank, CIFJobContributionRank)

Both windows page a cached 0xB37E list ten rows at a time (CIFJobRank_FillPage
647E50, CIFJobContributionRank_FillPage 646D10). An activity row shows the
rank, alias, grade, the experience share of that grade and the grade's
title; a contribution row the rank, alias, grade and the amount grouped by
thousands. The contribution window also shows the viewer's own entry when
they belong to that job, have an alias and have job experience
(CIFJobContributionRank_Configure 646FC0); otherwise its "no entry" line.

===========================================================================
*/
import type { JobRankList, LocalJob } from "./job-guild";
import type { JobExpThresholds } from "./player-info-job";

// The rows on one page (both windows).
export const JOB_RANK_PAGE_ROWS = 10;
// 647E50 reads the threshold column as unsigned.
const U32_RANGE = 0x100000000;

/*
================
JobRankSlot

One row's texts, keyed by the slot layout's control ids 10-14.
================
*/
export type JobRankSlot = Readonly<Partial<Record<number, string>>>;

/*
================
JobRankPage
================
*/
export interface JobRankPage {
	readonly title: string;
	readonly slots: readonly JobRankSlot[];
	readonly pages: number;
}

/*
================
jobRankPages

648110 and 646FC0: one page for an empty list, otherwise ceil(n / 10).
================
*/
export function jobRankPages( rows: number ): number {
	return Math.max( 1, Math.ceil( rows / JOB_RANK_PAGE_ROWS ) );
}

/*
================
jobRankName
================
*/
function jobRankName( job: number ): string {
	return [ "", "MERCHANT", "THIEF", "HUNTER" ][job] ?? "";
}

/*
================
jobRankPage

The window title and one page of slots for a cached list.
================
*/
export function jobRankPage(
	list: JobRankList,
	page: number,
	thresholds: JobExpThresholds,
	copy: ( key: string ) => string
): JobRankPage {
	const menu = [ "", "TRADER", "THIEF", "HUNTER" ][list.job] ?? "";
	const title = list.kind === 0 ?
		copy( "UIIT_STT_JOBGUILD_" + menu + "_MENU_JOBRANK" ) :
		copy(
			list.job === 1 ?
				"UIIT_STT_JOBGUILD_TRADER_MENU_DONATIONRANK" :
				"UIIT_STT_JOBGUILD_HUNTER_MENU_CONTRIBUTERANK"
		);
	const first = page * JOB_RANK_PAGE_ROWS;
	const slots = list.rows.slice( first, first + JOB_RANK_PAGE_ROWS ).map( row => {
		if ( list.kind !== 0 ) {
			return {
				10: String( row.rank ),
				11: row.alias,
				12: String( row.grade ),
				13: row.value.toLocaleString( "en-US" )
			};
		}
		let need = thresholds[row.grade]?.[list.job - 1] ?? 0;
		if ( need < 0 ) need += U32_RANGE;
		// The share truncates under the x87 chop mode and is kept as a byte.
		const share = need > 0 ? Math.trunc( Math.fround( row.value / need ) * 100 ) & 0xff : 0;
		return {
			10: String( row.rank ),
			11: row.alias,
			12: String( row.grade ),
			13: share + "%",
			14: copy( "UIIT_STT_CLASS_" + jobRankName( list.job ) + "_" + row.grade )
		};
	} );
	return { title, slots, pages: jobRankPages( list.rows.length ) };
}

/*
================
JobContributionSelf

The contribution window's own entry (controls 10, 13-16), or null when
646FC0 hides it and shows control 60 instead.
================
*/
export interface JobContributionSelf {
	readonly note: string;
	readonly label: string;
	readonly alias: string;
	readonly grade: string;
	readonly amount: string;
}

/*
================
jobContributionSelf
================
*/
export function jobContributionSelf(
	job: LocalJob,
	windowJob: number,
	copy: ( key: string ) => string
): JobContributionSelf | null {
	if ( job.type !== windowJob || !job.alias || job.exp === 0 ) return null;
	const trader = windowJob === 1;
	return {
		note: copy( trader ? "UIIT_STT_JOBGUILD_MYCONTRIBUTE" : "UIIT_STT_JOBGUILD_MYCONTRIBUTE2" ).replace(
			"%s",
			job.alias
		),
		label: copy( trader ? "UIIT_STT_DONATION" : "UIIT_STT_CONTRIBUTE" ),
		alias: job.alias,
		grade: String( job.grade ),
		amount: String( job.contribution )
	};
}
