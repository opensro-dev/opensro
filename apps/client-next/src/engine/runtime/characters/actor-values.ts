/*
===========================================================================

actor-values.ts - a rebuilt actor's value objects, carried while they hold

A published actor is frozen and keeps every object it references unchanged
(actor-advance.ts). The renderer copies only the clocks of an actor whose
other fields are the same objects as its last one (actor-snapshots.ts
clocksOnly); a new object of the same value makes it copy and validate the
whole actor again. A decision that changes nothing (most decides of a
crowd member, of a walker between path samples, of a held tint) rebuilds
the actor, so actor-advance hands each value object through here: the
last actor's object while the value holds, the new one once it changes.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";

/*
================
carriedPose
================
*/
export function carriedPose(
	previous: CharacterActor["pose"] | undefined,
	next: CharacterActor["pose"]
): CharacterActor["pose"] {
	return previous && previous.regionId === next.regionId && previous.x === next.x && previous.y === next.y &&
			previous.z === next.z && previous.yaw === next.yaw ?
		previous :
		next;
}

/*
================
carriedLod
================
*/
export function carriedLod(
	previous: CharacterActor["animationLod"],
	next: NonNullable<CharacterActor["animationLod"]>
): NonNullable<CharacterActor["animationLod"]> {
	return previous && previous.fraction === next.fraction && previous.crowded === next.crowded &&
			previous.optional === next.optional ?
		previous :
		next;
}

/*
================
carriedTint
================
*/
export function carriedTint(
	previous: CharacterActor["materialTint"],
	next: CharacterActor["materialTint"]
): CharacterActor["materialTint"] {
	return previous && next && previous[0] === next[0] && previous[1] === next[1] && previous[2] === next[2] ?
		previous :
		next;
}
