/*
===========================================================================

most-wanted.ts - the buff board's most-wanted row (port-only, not native)

Turns the 0x3FBA state (worker most-wanted.ts) into the one buff-board
row the streak holder sees beside the HP bar: the dressed job's icon, a
bar of the kills into the next level, and help text naming the level
the way the shard announcements do ("MOST WANTED Lv N"), the bonuses,
the bounty the level is worth and the time until the next decay step.
The original has no such row; the server sends the state only with
SRO_MOST_WANTED on (the shard flag is the only switch, #2721), and no
state leaves the native board.

===========================================================================
*/
import type { MostWantedState } from "@/engine/contracts/most-wanted";

const JOB_ICON_ROOT = "/assets/images/Media_extracted/interface/ifcommon/";

/*
================
mostWantedJobIcon

The served path of a dressed job's icon, or null when no suit is worn.
The names are the overhead marks (86B350, name-visibility.ts jobMark):
1 merchant, 2 thief, 3 hunter.
================
*/
export function mostWantedJobIcon( job: number ): string | null {
	switch ( job ) {
		case 1:
			return JOB_ICON_ROOT + "com_job_merchant.png";
		case 2:
			return JOB_ICON_ROOT + "com_job_thief.png";
		case 3:
			return JOB_ICON_ROOT + "com_job_hunter.png";
	}
	return null;
}

/*
================
mostWantedDamagePercent

The damage dealt and reduced at a wanted level, mirroring the server's
mostWantedDamagePercent (action/mostwanted.go): 1% from 3, 2% from 5,
3% at 10.
================
*/
export function mostWantedDamagePercent( level: number ): number {
	if ( level >= 10 ) return 3;
	if ( level >= 5 ) return 2;
	if ( level >= 3 ) return 1;
	return 0;
}

/*
================
mostWantedDecayText

How long until the next decay step, for the row's help text. The board
redraws every frame, so the text stays current on its own. remainingMs
is a duration counted down from the state's receipt on the frame
clock; it bottoms out at a minute rather than vanishing, because the
server - not this clock - drops the level and tells the row.
================
*/
export function mostWantedDecayText( remainingMs: number ): string {
	const ms = Math.max( 0, remainingMs );
	const days = Math.floor( ms / 86_400_000 );
	if ( days >= 1 ) return `Next decay in ${days} day${days === 1 ? "" : "s"}`;
	const hours = Math.floor( ms / 3_600_000 );
	if ( hours >= 1 ) return `Next decay in ${hours} h ${Math.floor( (ms % 3_600_000) / 60_000 )} min`;
	return `Next decay in ${Math.max( 1, Math.floor( ms / 60_000 ) )} min`;
}

/*
================
MostWantedRow

The board row one state draws; null state draws none.
================
*/
export interface MostWantedRow {
	readonly path: string;
	readonly label: string;
	readonly helpText: string;
	readonly fraction: number;
}

/*
================
mostWantedRow

The streak holder's row, or null: no state yet, no level, or the suit
off (the bonuses shelve with it, so the row waits too). nowMs is the
frame clock the state's receipt was stamped on; only the countdown
reads it. The row never hides on a clock of its own: the server owns
the decay and sends the state that ends the streak.
================
*/
export function mostWantedRow( state: MostWantedState | undefined, nowMs: number ): MostWantedRow | null {
	if ( !state || state.level <= 0 ) return null;
	const path = mostWantedJobIcon( state.dressedJob );
	if ( !path ) return null;
	const level = state.level, perLevel = Math.max( 1, state.killsPerLevel );
	const help = [ `MOST WANTED Lv ${level}` ];
	help.push( `Strength +${level}` );
	help.push( `Intellect +${level}` );
	help.push( `Movement speed on foot +${level}%` );
	const percent = mostWantedDamagePercent( level );
	if ( percent > 0 ) help.push( `Damage dealt +${percent}%, damage taken -${percent}%` );
	help.push(
		state.killsIntoLevel >= perLevel ?
			"Maximum level" :
			`Next level: ${state.killsIntoLevel}/${perLevel} kills`
	);
	help.push( `Bounty: ${state.bounty.toLocaleString( "en-US" )} gold` );
	if ( state.decayInMs > 0 ) {
		help.push( mostWantedDecayText( state.decayInMs - (nowMs - state.receivedAtMs) ) );
	}
	help.push( "Death clears the streak." );
	return {
		path,
		label: `MOST WANTED Lv ${level}`,
		helpText: help.join( "\n" ),
		fraction: Math.min( 1, state.killsIntoLevel / perLevel )
	};
}
