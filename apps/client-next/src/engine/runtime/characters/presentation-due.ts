/*
===========================================================================

presentation-due.ts - when an entity's motion must be decided again

actor-motion.ts decides an actor's layers from its inputs every frame. A
standing entity whose inputs did not change decides the same thing every
frame: its last steady decision (MotionRetained) holds, and only advance
(the clocks, sounds and published actor) runs. This module is the rule:
decide again unless a steady decision from the previous frame exists and
every input it was taken from is the same (MotionStamp). It holds no state.

===========================================================================
*/
import type { MotionRetained, MotionStamp } from "./internal/presentation-contract";

/*
================
sameMotionStamp

Every term compared by value or identity, none skipped.
================
*/
export function sameMotionStamp( a: MotionStamp, b: MotionStamp ): boolean {
	return a.entity === b.entity && a.glb === b.glb && a.codename === b.codename && a.clips === b.clips &&
		a.clipFacts === b.clipFacts && a.animationStates === b.animationStates && a.commit === b.commit &&
		a.manifest === b.manifest && a.hpZero === b.hpZero && a.abnormal === b.abnormal &&
		a.healthDead === b.healthDead && a.pendingDeath === b.pendingDeath && a.hitToken === b.hitToken &&
		a.cast === b.cast && a.waiting === b.waiting && a.actionLayers === b.actionLayers &&
		a.hostMotions === b.hostMotions && a.stance === b.stance && a.posture === b.posture && a.idle === b.idle &&
		a.poseMoving === b.poseMoving && a.weaponSet === b.weaponSet && a.armed === b.armed && a.local === b.local;
}

/*
================
motionDue

The production rule: decide again unless the previous frame's steady
decision exists and was taken from the same inputs.
================
*/
export function motionDue( retained: MotionRetained | undefined, stamp: MotionStamp | undefined ): boolean {
	return !retained || !stamp || !sameMotionStamp( retained.stamp, stamp );
}
