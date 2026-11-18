/*
===========================================================================

knockback-presentation.ts - a struck character recoils when its hit lands

The worker moves a knocked-back target as soon as the result packet
arrives, but the original starts the recoil inside the hit itself:
CIDecoDamageEffect_Initialize (8D5440), run by 8DB770 at the skill's
damage event, starts the 250/s mover (8D56F6 -> 8DDBD0). This owner holds
the drawn body where it stood until that hit is presented, then plays the
displacement path from the hit's own time. The logical position stays the
worker's; only the drawn pose waits.

===========================================================================
*/
import type { MovementPath, Pose } from "@/engine/contracts/gameplay";
import { interpolateMovement } from "@/engine/foundation/gameplay/native-movement";

/*
================
createKnockbackPresentation
================
*/
export function createKnockbackPresentation() {
	// The frame-clock second each struck character's knockback hit landed.
	const landed = new Map<number, number>();
	return {
		/*
		================
		land

		A knockback hit was presented on gid at the frame-clock second at.
		================
		*/
		land( gid: number, at: number ) {
			landed.set( gid, at );
		},
		/*
		================
		pose

		The drawn pose for a character on a displacement leg: held at the
		leg's start while its knockback hit is still pending, then moved
		along the leg from the second the hit landed. Undefined leaves the
		presenter's pose alone (no leg, or the leg is over).
		================
		*/
		pose( gid: number, path: MovementPath | undefined, pending: boolean, seconds: number, drawn: Pose ) {
			if ( !path?.displacement || !path.from || !path.to ) {
				landed.delete( gid );
				return undefined;
			}
			if ( pending ) return { ...path.from, angle: drawn.angle };
			const at = landed.get( gid );
			if ( at === undefined ) return undefined;
			const duration = (path.durationMs ?? 0) / 1000;
			if ( !(duration > 0) || seconds >= at + duration ) {
				landed.delete( gid );
				return undefined;
			}
			const moved = interpolateMovement( path.from, path.to, Math.max( 0, seconds - at ) / duration );
			return { ...moved, angle: drawn.angle };
		},
		/*
		================
		reset
		================
		*/
		reset() {
			landed.clear();
		}
	};
}
