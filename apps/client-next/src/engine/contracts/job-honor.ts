/*
===========================================================================

job-honor.ts - the Job Honor feed's shapes (port-only, not native)

#3530: with SRO_JOB_HONOR on, the server keeps every entered session
told of the job honor board and its own standing (wire 0x3FC6). A row
shows the character's job alias (never its name), the job it scored
in, its kept points and the tiers it holds now and after tonight's
change (0 none, 1 silver, 2 gold, 3 king). The own standing adds the
viewer's place, its account's daily cap left and its points today and
this month per job, so the Job Honor Rank window never sums anything
the server did not say.

===========================================================================
*/

export interface JobHonorRow {
	readonly job: number;
	readonly alias: string;
	readonly points: number;
	readonly tier: number;
	readonly tonight: number;
}

export interface JobHonorOwn {
	readonly present: boolean;
	readonly job: number;
	readonly place: number;
	readonly tier: number;
	readonly tonight: number;
	readonly capLeft: number;
	readonly today: readonly [number, number, number];
	readonly month: readonly [number, number, number];
}

export interface JobHonorFeed {
	readonly serverNowMs: number;
	readonly nextChangeMs: number;
	// 0 when no tier change has landed yet, so there is no previous period.
	readonly previousEndMs: number;
	readonly current: readonly JobHonorRow[];
	readonly previous: readonly JobHonorRow[];
	readonly own: JobHonorOwn;
	// Rises with every snapshot; never resets (see the worker lane).
	readonly revision: number;
}
