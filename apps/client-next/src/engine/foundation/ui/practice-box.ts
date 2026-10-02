/*
===========================================================================

practice-box.ts - what the native training confirmation shows

CIFSkillPracticeBox confirms every skill and mastery level-up before the
request is sent. CIFSkillPracticeBox_SetMode (5DDF00) switches the box
between its skill face (mode 0: icon, name, "Lv n") and its mastery face
(mode 1: mastery name and decoration, titled
UIIT_STT_CIRCULATION_PRACTICE_MASTERY_WND), and
CIFSkillPracticeBox_ConfigureRecord (5DE040) fills it. The commit (5DE690)
re-checks the stashed SP cost before it sends.

===========================================================================
*/

// CIFSkillPracticeBox_SetMode's two faces.
export const PRACTICE_SKILL = 0;
export const PRACTICE_MASTERY = 1;

/*
================
PracticeRequest

The level-up the open box will commit: a skill or a mastery id.
================
*/
export interface PracticeRequest {
	readonly mode: typeof PRACTICE_SKILL | typeof PRACTICE_MASTERY;
	readonly id: number;
}

/*
================
masteryPractice

5DE040's mastery face: the message names the next level (current + 1), and
the SP cost is the level-data cost for the current mastery level, or 0 when
the mastery is still at level 0 (5DE040 skips the lookup then). Null when
the cost row is missing, so the box never invents a price.
================
*/
export function masteryPractice(
	level: number,
	costs: Readonly<Record<number, number>>
): { readonly nextLevel: number; readonly cost: number; } | null {
	if ( level === 0 ) return { nextLevel: 1, cost: 0 };
	const cost = costs[level];
	return cost === undefined ? null : { nextLevel: level + 1, cost };
}
