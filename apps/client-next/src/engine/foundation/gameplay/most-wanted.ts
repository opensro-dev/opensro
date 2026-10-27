/*
===========================================================================

most-wanted.ts - the Most Wanted buff-board row (port-only, not native)

#1849: the original has no such display. The server (SRO_MOST_WANTED)
reports the local player's wanted state (contracts/most-wanted.ts) and
this owner turns it into the buff board's row: the dressed job's icon,
"Most Wanted Lv N", a fraction bar of the counted kills toward the next
level, and a tooltip listing the effects the level actually grants - the
same numbers the server's stat projection applies (+1 STR, +1 INT and
+1% movement speed on foot per level; the damage dealt and reduced the
frame itself reports). Below level 1 there is no row: the streak exists
but nothing is granted yet.

===========================================================================
*/
import type { MostWantedState } from "@/engine/contracts/most-wanted";

/*
================
MostWantedRow

The board row's content: its com_job_* icon base name (the interface's
ifcommon art, as the overhead job mark draws), label, tooltip and bar.
================
*/
export interface MostWantedRow {
	readonly icon: string;
	readonly label: string;
	readonly helpText: string;
	readonly fraction: number;
}

// The dressed job's board icon, by job (1 trader, 2 thief, 3 hunter):
// the same com_job_* art the character window and the overhead mark use.
const JOB_ICONS = [ "", "com_job_merchant", "com_job_thief", "com_job_hunter" ] as const;

/*
================
mostWantedRow

The row for a wanted state, or null while there is nothing to show: no
state, no level yet, or a job the board has no icon for.
================
*/
export function mostWantedRow( state: MostWantedState | null | undefined ): MostWantedRow | null {
	if ( !state || state.level < 1 ) {
		return null;
	}
	const icon = JOB_ICONS[state.job];
	if ( !icon ) {
		return null;
	}
	const level = state.level;
	const lines = [
		`Most Wanted Lv ${level}`,
		`Strength +${level}, Intellect +${level}`,
		`Movement speed +${level}% on foot`
	];
	if ( state.damagePercent > 0 ) {
		lines.push( `Damage dealt +${state.damagePercent}%, damage taken -${state.damagePercent}%` );
	}
	lines.push(
		state.killsIntoLevel >= state.killsPerLevel ?
			"Highest wanted level" :
			`${state.killsIntoLevel}/${state.killsPerLevel} counted kills to Lv ${level + 1}`
	);
	return {
		icon,
		label: `Most Wanted Lv ${level}`,
		helpText: lines.join( "\n" ),
		fraction: state.killsPerLevel > 0 ? Math.min( 1, state.killsIntoLevel / state.killsPerLevel ) : 1
	};
}
