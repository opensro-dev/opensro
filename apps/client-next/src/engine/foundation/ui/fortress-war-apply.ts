/*
===========================================================================

fortress-war-apply.ts - what the fortress war application window shows

CIFFortressWarApplyWnd (resinfo iffortresswarapplywnd.txt) lists, for the
official in conversation, the fortresses whose applications he takes:
each slot (iffortresswarapplywndslot.txt) names the fortress and its
occupying guild and carries one button whose caption and question follow
the guild's standing (663EB0). Above the list sit the war's date and the
application period, both derived from the war's start (660C70). Pure
presentation: the window state is the fortress-war HUD's.

===========================================================================
*/
import type { FortressApplication, FortressState, FortressWarTime } from "@/engine/foundation/gameplay/fortress";

// 662D00 always shows eight rows; the unused ones are empty bars.
export const FORTRESS_WAR_APPLY_ROWS = 8;
// 662BF0: each slot is 397 x 33.
export const FORTRESS_WAR_APPLY_SLOT_WIDTH = 397;
export const FORTRESS_WAR_APPLY_SLOT_HEIGHT = 33;
// 660C70's FILETIME offsets: the war lasts two hours; applications run
// from three days before the war for two days.
const WAR_HOURS = 2;
const APPLY_OPENS_DAYS = 3;
const APPLY_DAYS = 2;

/*
================
FortressWarQuestion

The confirmation a slot's button raises (6649C0's message box types).
================
*/
export type FortressWarQuestion = 0x64 | 0x65 | 0x66 | 0x67;

/*
================
FortressWarSlot
================
*/
export interface FortressWarSlot {
	readonly fortress: number;
	readonly nameSymbol: string;
	readonly owner: string;
	readonly caption: string;
	readonly enabled: boolean;
	readonly question: FortressWarQuestion;
}

/*
================
fortressWarSlots

662E80 / 663EB0: the official's fortresses and each one's button. A guild
that already holds a fortress cannot apply anywhere; a guild that applied
elsewhere sees every other button disabled. The owner's own guild and its
allies apply to defend (union), everyone else to besiege (occupy).
================
*/
export function fortressWarSlots(
	state: FortressState,
	officialRefObjId: number,
	application: FortressApplication | null,
	guildName: string,
	allyNames: readonly string[]
): FortressWarSlot[] {
	const owns = !!guildName && state.wars.some( r => r.name === guildName );
	return state.fortresses.filter( r => r.officialRefObjId === officialRefObjId ).map( row => {
		const owner = state.wars.find( r => r.id === row.id )?.name ?? "";
		const defending = !!owner && (owner === guildName || allyNames.includes( owner ));
		const applied = application?.applied ?? null;
		if ( applied && applied.fortress === row.id ) {
			return {
				fortress: row.id,
				nameSymbol: row.nameStrId ?? row.code,
				owner,
				caption: applied.kind === 0 ?
					"UIIT_CTL_FORT_OFFICAL_OCCUPYAPPLY_CANCEL" :
					"UIIT_CTL_FORT_OFFICAL_UNIONAPPLY_CANCEL",
				enabled: !owns,
				question: applied.kind === 0 ? 0x66 : 0x67
			};
		}
		return {
			fortress: row.id,
			nameSymbol: row.nameStrId ?? row.code,
			owner,
			caption: defending ? "UIIT_CTL_FORT_OFFICAL_UNIONAPPLY" : "UIIT_CTL_FORT_OFFICAL_OCCUPYAPPLY",
			enabled: !applied && !owns,
			question: defending ? 0x65 : 0x64
		};
	} );
}

/*
================
fortressWarQuestionKey
================
*/
export function fortressWarQuestionKey( question: FortressWarQuestion ): string {
	return question === 0x64 ?
		"UIIT_MSG_FORT_OFFICIAL_WARAPPLY_WINDOW" :
		question === 0x65 ?
		"UIIT_MSG_FORT_OFFICIAL_UNIONAPPLY_WINDOW" :
		question === 0x66 ?
		"UIIT_MSG_FORT_OFFICIAL_WARAPPLY_CANCEL_WINDOW" :
		"UIIT_MSG_FORT_OFFICIAL_UNIONAPPLY_CANCEL_WINDOW";
}

/*
================
fortressWarRequest

The 0x71E1 a confirmed question sends: an application (7) or withdrawal
(8), as an attacker (0) or the owner's ally (1).
================
*/
export function fortressWarRequest( question: FortressWarQuestion ): { withdraw: boolean; request: number; } {
	return { withdraw: question >= 0x66, request: question === 0x65 || question === 0x67 ? 1 : 0 };
}

/*
================
fortressWarDates

660C70's two lines' numbers: the war's month, day, start and end hour,
and the application period's start and end month and day.
================
*/
export function fortressWarDates( start: FortressWarTime ): { war: number[]; apply: number[]; } {
	const at = Date.UTC( start.year, start.month - 1, start.day, start.hour, start.minute );
	const end = new Date( at + WAR_HOURS * 3600e3 );
	const opens = new Date( at - APPLY_OPENS_DAYS * 86400e3 ),
		closes = new Date( opens.getTime() + APPLY_DAYS * 86400e3 );
	return {
		war: [ start.month, start.day, start.hour, end.getUTCHours() ],
		apply: [ opens.getUTCMonth() + 1, opens.getUTCDate(), closes.getUTCMonth() + 1, closes.getUTCDate() ]
	};
}

/*
================
fortressWarFormat

The client's printf for these texts: each %d or %s takes the next value.
================
*/
export function fortressWarFormat( template: string, values: readonly (string | number)[] ): string {
	let index = 0;
	return template.replace( /%[ds]/g, token => index < values.length ? String( values[index++] ) : token );
}
