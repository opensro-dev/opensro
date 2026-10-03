/*
===========================================================================

cast-motion-lock.ts - the local player cannot walk out of a skill's action

CIDecoSkill_InitializeCast (8E0A23) enters action state 2 on the caster, and
CICharactor_OnCastAnimationEvent leaves it only when the skill's last motion
stage ends (CIDecoSkill_AdvanceMotionStage), or a cancellation extinguishes
the skill. CICharactor_CanPerformLocomotion (877240) refuses locomotion while
any bit of 0x2476 is set in the action-state word (+0x644), and bit 2 is in
it. A ground click meanwhile is only stored: CNavigationDeadreckon keeps the
destination and sends the walk once locomotion is allowed again
(TickOrCheckArrival).

The motion is authored to the skill's action actor: Action_CastingTime then
Action_ActionDuration (skilldata columns 12 and 13, the same lifetime the
server closes the cast with). The lock is timed on the local clock from the
cast's opening, so a slow round trip never lengthens it.

===========================================================================
*/
import type { CastState } from "@/engine/contracts/gameplay";
import type { SkillMetadata } from "./skill-catalog";

/*
================
createCastMotionLock

Owns the action window of each catalogued skill.
================
*/
export function createCastMotionLock() {
	let windows = new Map<number, number>();
	return {
		/*
		================
		catalog

		Index the action windows of an admitted skill catalogue.
		================
		*/
		catalog( rows: readonly SkillMetadata[] ) {
			const next = new Map<number, number>();
			for ( const row of rows ) if ( row.actionMs ) next.set( row.id, row.actionMs );
			windows = next;
		},
		/*
		================
		locked

		Whether a cast of caster still holds action state 2 at now: open (not
		extinguished by a cancellation or the server's close) and inside its
		skill's action window. A skill without a known window holds while its
		cast is open and committed (the server-count fallback).
		================
		*/
		locked( casts: readonly CastState[], caster: number, now: number, committed: boolean ): boolean {
			for ( const cast of casts ) {
				if ( cast.caster !== caster || cast.resultOnly || cast.cancelledAtMs !== undefined ) continue;
				const window = windows.get( cast.skill );
				if ( window === undefined ) {
					if ( committed ) return true;
					continue;
				}
				if ( now < (cast.receivedAtMs ?? now) + window ) return true;
			}
			return false;
		},
		/*
		================
		clear
		================
		*/
		clear() {
			windows = new Map();
		}
	};
}
