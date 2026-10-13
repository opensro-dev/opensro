/*
===========================================================================

open-casts.ts - the bound on a session's open casts

A cast stays open until its B505 close, a B6A0 naming its token, or its
caster leaving. The table is bounded; an overflow means a server cast
whose close never arrived, and its error names that cast.

===========================================================================
*/
import type { CastState } from "@/engine/contracts/gameplay";
// Open casts one session may hold. Native has no bound: a CIDecoSkill lives
// until its B505 close or B6A0 retirement (8DC440 ends only a cancelled
// deco), so only a server cast with no close can fill this table.
export const MAX_OPEN_CASTS = 2048;
/*
================
openCastOverflow

The overflow names the skill holding the most open casts, and the oldest
cast's skill and caster, so an incident points at the cast whose close the
server never sent.
================
*/
export function openCastOverflow( casts: ReadonlyMap<number, CastState>, now: number ): Error {
	const bySkill = new Map<number, number>();
	let oldest: CastState | undefined;
	for ( const cast of casts.values() ) {
		bySkill.set( cast.skill, (bySkill.get( cast.skill ) ?? 0) + 1 );
		if ( !oldest || (cast.receivedAtMs ?? now) < (oldest.receivedAtMs ?? now) ) oldest = cast;
	}
	let top = 0, topCount = 0;
	for ( const [skill, count] of bySkill ) {
		if ( count > topCount ) {
			top = skill;
			topCount = count;
		}
	}
	return Error(
		`Unfinalized cast capacity exceeded: skill ${top} holds ${topCount}; oldest skill ${oldest?.skill} ` +
			`from ${oldest?.caster}, ${Math.round( (now - (oldest?.receivedAtMs ?? now)) / 1000 )} s open`
	);
}
