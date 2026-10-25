/*
===========================================================================

displacement-queue.ts - the cast displacements one phase hands to movement

Combat validates a cast phase; movement owns the bodies it displaces.
Between them sits this outbox: an impact's knockback or push (kinds 4
and 5), the caster's own travel (kind 8) and its landing correction
(kind 2), each marked self when it is the caster's own motion - the one
displacement the main-thread mover steps (#1569), as opposed to a
knockback struck onto a target. Core drains the queue once into
movement.displace; nothing here knows the wire or the clock.

===========================================================================
*/
import type { CastDisplacement, Pose } from "@/engine/contracts/gameplay";

/*
================
createDisplacementQueue

One session's outbox: filled by combat's phase application, drained by
its takeDisplacements.
================
*/
export function createDisplacementQueue() {
	const pending: CastDisplacement[] = [];
	return {
		/*
		================
		impact

		An impact's forced move of its target: the caster's own only when
		it struck itself.
		================
		*/
		impact(
			token: number,
			caster: number,
			target: number,
			kind: 4 | 5,
			destination: Omit<Pose, "angle">
		): void {
			pending.push( { gid: target, token, kind, destination, self: target === caster } );
		},
		/*
		================
		own

		The caster's own travel (kind 8) or landing correction (kind 2).
		================
		*/
		own( token: number, caster: number, kind: 2 | 8, destination: Omit<Pose, "angle"> ): void {
			pending.push( { gid: caster, token, kind, destination, self: true } );
		},
		/*
		================
		take

		Transfer the committed displacements to the motion owner once.
		================
		*/
		take(): CastDisplacement[] {
			return pending.splice( 0 );
		},
		/*
		================
		clear

		Drop everything pending (the session's combat owner clears).
		================
		*/
		clear(): void {
			pending.length = 0;
		}
	};
}
