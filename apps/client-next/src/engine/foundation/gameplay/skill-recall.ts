/*
===========================================================================

skill-recall.ts - the skill reset scroll's control body

Port-only, not native. After a skill reset scroll's use (v1.696
ITEM_MALL_SKILL_POINT_RECALL_A, behind SRO_SKILL_RESET_SCROLL) the server
sends world control 21 (commerce-controls.ts SKILL_RECALL_CONTROL; server
progression/skillrecall.go SkillRecallControl) as JSON:
{"version":1,"skills":[kept ids],"masteries":[{"id":n,"level":0},...]}.
The native withdrawal acknowledgements answer an herbalist request in
flight and cannot carry a whole build, hence the control. The SP and the
stats arrive on their own native frames (0x30B3, 0x303D) before it.

===========================================================================
*/

import { commerceJson } from "./commerce";

// The bootstrap's own bounds (skillBindings: 4096 learned skills) and a
// mastery list far above any race's.
const MAX_SKILLS = 4096;
const MAX_MASTERIES = 256;

/*
================
SkillRecall

What a reset leaves: the learned skills kept and every mastery's level.
================
*/
export interface SkillRecall {
	readonly skills: readonly number[];
	readonly masteries: readonly { readonly id: number; readonly level: number; }[];
}

/*
================
skillRecallFrame

Decodes control 21 strictly: version 1, distinct positive u32 skill ids,
masteries with a positive u32 id and a level 0-255.
================
*/
export function skillRecallFrame( p: Uint8Array ): SkillRecall {
	const r = commerceJson( p );
	const skills = r.skills, masteries = r.masteries;
	if (
		!Array.isArray( skills ) || skills.length > MAX_SKILLS ||
		skills.some( id => !Number.isInteger( id ) || id <= 0 || id > 0xffffffff ) ||
		new Set( skills ).size !== skills.length
	) throw Error( "Invalid skill recall skills" );
	if (
		!Array.isArray( masteries ) || masteries.length > MAX_MASTERIES ||
		masteries.some( row =>
			!row || typeof row !== "object" || !Number.isInteger( row.id ) || row.id <= 0 || row.id > 0xffffffff ||
			!Number.isInteger( row.level ) || row.level < 0 || row.level > 255
		)
	) throw Error( "Invalid skill recall masteries" );
	return {
		skills: [ ...skills as number[] ],
		masteries: (masteries as { id: number; level: number; }[]).map( row => ({ id: row.id, level: row.level }) )
	};
}
