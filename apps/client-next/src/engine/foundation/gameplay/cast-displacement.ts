/*
===========================================================================

cast-displacement.ts - the path a skill displacement moves an entity along

A skill result can move its target (a knockdown or a knockback) and a cast
can move its caster (a dash). This builds the movement segment each kind
travels, at its native speed, with the facing it keeps.

===========================================================================
*/
import type { CastDisplacement, Pose } from "@/engine/contracts/gameplay";
import { type MovementSegment, movementHeading, poseDistance } from "./native-movement";

// 8D5734: a knockback (type 5) travels at 250 units/s; 8E0440 bit 8 (the
// caster's dash) at 500 units/s. A knockdown (type 4) uses state 4's 8E5CD0
// update: delta seconds * 50. Kind 2 is a correction and lands at once.
const KNOCKBACK_SPEED = 250;
const DASH_SPEED = 500;
const KNOCKDOWN_SPEED = 50;
const CORRECTION = 2;
const KNOCKBACK = 5;
const DASH = 8;

/*
================
displacementSegment

The segment from the entity's pose to the displacement's destination. Only
the caster's dash faces where it travels. A knockback (8D56F6 -> 8DDBD0,
stepped by 8DD550) and a knockdown move the entity without aiming it, so a
struck monster recoils backwards instead of turning away from its attacker.
================
*/
export function displacementSegment( from: Pose, command: CastDisplacement, now: number ): MovementSegment {
	const to = { ...command.destination, angle: from.angle };
	if ( command.kind === CORRECTION ) return { from: to, to, start: now, duration: 0 };
	const rate = command.kind === KNOCKBACK ? KNOCKBACK_SPEED : command.kind === DASH ? DASH_SPEED : KNOCKDOWN_SPEED;
	return {
		from,
		to: { ...to, angle: command.kind === DASH ? movementHeading( from, to ) : from.angle },
		start: now,
		duration: poseDistance( from, to ) / rate * 1000
	};
}
