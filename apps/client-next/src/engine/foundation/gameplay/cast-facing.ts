/*
===========================================================================

cast-facing.ts - a caster keeps turning toward its target during a skill

Natively the server sets the caster's yaw once, when the cast starts. Every
frame after that, CIDecoSkill_Update (8DC440) turns the caster toward the
target's current position, so a bow keeps aiming at a peer who walks away
(the projectile already follows the target). The port's logical heading is
the server's, fixed at cast start; this owner supplies the tracked heading
the presentation draws instead.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import { movementHeading } from "@/engine/foundation/gameplay/native-movement";

// 8DC440: Math_ApproachAngle( current, target, g_fDeltaTimeSeconds_Move * 10.0 ).
const CAST_TURN_RADIANS_PER_SECOND = 10;
// One full turn of the native heading word.
const HEADING_WORDS = 65536;

/*
================
CastFacing

The tracked heading word, the logical heading and movement revision it
replaced, and the frame clock it was last advanced at.
================
*/
export interface CastFacing {
	readonly angle: number;
	readonly heading: number;
	readonly revision: number;
	readonly at: number;
}

/*
================
CastFacingInput

tracking is true while a live, uncancelled cast names a target other than
the caster: 8DC440 skips the turn once +0xD0 is set (RequestCancellation
8DD0D7, StopLoopingMotionStage 8D97B9, ExtinguishAndCancel 8DD1FC) and
when CIDecoSkill_GetTargetOrSecondaryEntity finds nobody or the caster.
================
*/
export interface CastFacingInput {
	readonly caster: Pose;
	readonly target: Pose | undefined;
	readonly tracking: boolean;
	readonly moving: boolean;
	readonly revision: number;
	readonly seconds: number;
}

/*
================
approachHeading

Math_ApproachAngle on heading words: the shortest arc, at most limit words.
================
*/
function approachHeading( from: number, to: number, limit: number ) {
	const half = HEADING_WORDS / 2;
	const delta = ((to - from + HEADING_WORDS + half) % HEADING_WORDS) - half;
	return (from + Math.max( -limit, Math.min( limit, delta ) ) + HEADING_WORDS) % HEADING_WORDS;
}

/*
================
castFacing

Returns the heading to draw, or undefined for the logical one. 8DC440 writes
the character's yaw through CICharactor_SetYaw, so the turned yaw outlives
the cast: it is held until movement or a new server heading replaces it.
================
*/
export function castFacing( held: CastFacing | undefined, input: CastFacingInput ): CastFacing | undefined {
	if ( input.moving ) return undefined;
	const current = held && held.heading === input.caster.angle && held.revision === input.revision ?
		held :
		undefined;
	if ( !input.tracking || !input.target ) return current && { ...current, at: input.seconds };
	const from = current?.angle ?? input.caster.angle;
	const seconds = current ? Math.max( 0, input.seconds - current.at ) : 0;
	return {
		angle: approachHeading(
			from,
			movementHeading( input.caster, input.target ),
			seconds * CAST_TURN_RADIANS_PER_SECOND * HEADING_WORDS / (Math.PI * 2)
		),
		heading: input.caster.angle,
		revision: input.revision,
		at: input.seconds
	};
}
