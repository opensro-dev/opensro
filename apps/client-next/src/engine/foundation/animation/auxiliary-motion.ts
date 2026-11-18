/*
===========================================================================

auxiliary-motion.ts - when a cast holds a character's motion, and the clip
an auxiliary model (a wing dress's wings) asks for

gameplay.casts keeps a cast long after its animation: a completed cast
(phase 3, a projectile still flying) and a buff whose cast token is also its
effect token stay until the effect's 0xB6A0 teardown, which for a self buff
is the buff's whole duration. Membership in that table is therefore not
"casting". The body's motion (actor-motion.ts) counts a cast only while it
is uncancelled and its action layers play. The auxiliary pass used plain
membership, so after a self buff or the Wizard's teleport the wings held
"stand" while the body ran on (issue #3564: "wings stop working").

===========================================================================
*/

/*
================
castAnimating

Whether a character's cast still drives its motion: the cast is not
cancelled and its action layers are playing this frame.
================
*/
export function castAnimating( cast: { readonly cancelledAtMs?: number; } | undefined, actionLayers: number ): boolean {
	return cast !== undefined && cast.cancelledAtMs === undefined && actionLayers > 0;
}

/*
================
AuxiliaryMotionInput
================
*/
export interface AuxiliaryMotionInput {
	readonly mounted: boolean;
	readonly dead: boolean;
	readonly casting: boolean;
	readonly moving: boolean;
	readonly gait: "walk" | "run";
}

/*
================
auxiliaryLocomotion

The locomotion clip an auxiliary model asks for. A rider, a corpse and a
character in a playing cast stand; otherwise it follows the owner's gait.
A clip the auxiliary lacks (the wing BSRs author only stand and run) is
skipped by the caller, which keeps the current track as 8E8340 does.
================
*/
export function auxiliaryLocomotion( input: AuxiliaryMotionInput ): "stand" | "walk" | "run" {
	if ( input.mounted || input.dead || input.casting ) return "stand";
	return input.moving ? input.gait : "stand";
}
