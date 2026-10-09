/*
===========================================================================

player-info-job.ts - the character window's job block (CIFPlayerInfo 59FFA0)

The character window (ifplayerinfo_trijob2) carries a job block: the alias
(control 70), the job icon (71), the grade title (72), the grade (73), the
experience gauge (74) and its text (75). CIFPlayerInfo_vf2C (59FFA0) fills
them from the local player's job: the alias whenever one is set, and the
rest from the joined job, its grade and its experience against the
CLevelData threshold of that grade (7E0F20 payload +0x1C trader, +0x20 thief,
+0x24 hunter; leveldata.txt columns 7-9).

===========================================================================
*/
import type { LocalJob } from "./job-guild";

// 59FFA0: grade 7 and above draws a full gauge and "100%".
const JOB_GRADE_FULL = 7;
// 59FFA0 adds 2^32 to a negative threshold: the column is read unsigned.
const U32_RANGE = 0x100000000;

/*
================
JobExpThresholds

Each grade's experience threshold, as [trader, thief, hunter].
================
*/
export type JobExpThresholds = Readonly<Record<number, readonly [number, number, number]>>;

/*
================
PlayerInfoJob

The values 59FFA0 publishes, keyed by the layout's control names. icon is
the com_job_* image name, or null when the window clears it.
================
*/
export interface PlayerInfoJob {
	readonly alias: string;
	readonly icon: string | null;
	readonly title: string;
	readonly grade: string;
	readonly exp: string;
	readonly fraction: number;
}

/*
================
jobExpThresholds

Reads levelData.json's job columns. A row without all three is left out,
so its grade reads as a zero threshold.
================
*/
export function jobExpThresholds( raw: unknown ): JobExpThresholds {
	const result: Record<number, readonly [number, number, number]> = {};
	if ( !raw || typeof raw !== "object" ) return result;
	for ( const [grade, row] of Object.entries( raw as Record<string, Record<string, unknown>> ) ) {
		const columns = [ row?.jobExpTrader, row?.jobExpThief, row?.jobExpHunter ];
		if ( columns.every( v => Number.isSafeInteger( v ) ) ) {
			result[Number( grade )] = columns as [number, number, number];
		}
	}
	return result;
}

/*
================
playerInfoJob

59FFA0's job block. The grade title follows the character's country byte
(record +0x9C): 0 reads UIIT_STT_CLASS_<JOB>_<grade>, 1 the EU_ form. Any
other country leaves the native format buffer unwritten; the port shows
nothing there.
================
*/
export function playerInfoJob(
	job: LocalJob,
	country: number | undefined,
	thresholds: JobExpThresholds,
	copy: ( key: string ) => string
): PlayerInfoJob {
	const none = "<" + copy( "UIIT_STT_NONE" ) + ">";
	const alias = job.alias ? job.alias : none;
	const name = [ "", "MERCHANT", "THIEF", "HUNTER" ][job.type];
	if ( !name ) {
		return { alias, icon: null, title: none, grade: "", exp: "0%", fraction: 0 };
	}
	const title = country === 0 ?
		copy( "UIIT_STT_CLASS_" + name + "_" + job.grade ) :
		country === 1 ?
		copy( "UIIT_STT_CLASS_EU_" + name + "_" + job.grade ) :
		"";
	const icon = "com_job_" + name.toLowerCase();
	const grade = job.grade + " " + copy( "UIIT_STT_GRADE" );
	if ( job.grade >= JOB_GRADE_FULL ) {
		return { alias, icon, title, grade, exp: "100%", fraction: 1 };
	}
	let need = thresholds[job.grade]?.[job.type - 1] ?? 0;
	if ( need < 0 ) need += U32_RANGE;
	// A zero threshold divides to infinity, which 59FFA0's ftol turns into
	// INT_MIN; leveldata has no such row, so the port shows an empty bar.
	if ( need === 0 ) return { alias, icon, title, grade, exp: "0% (" + job.exp + ")", fraction: 0 };
	// The gauge keeps a float32 ratio; the text truncates ratio * 100 (CRT_ftol2).
	const fraction = Math.fround( job.exp / need );
	return { alias, icon, title, grade, exp: Math.trunc( fraction * 100 ) + "% (" + job.exp + ")", fraction };
}
