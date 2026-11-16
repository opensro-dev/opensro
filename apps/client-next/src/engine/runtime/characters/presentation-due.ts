/*
===========================================================================

presentation-due.ts - when an entity's motion must be decided again

actor-motion.ts decides an actor's layers from its inputs every frame. A
standing entity whose inputs did not change decides the same thing every
frame: its last steady decision (MotionRetained) holds, and only advance
(the clocks, sounds and published actor) runs. actor-appearance.ts
resolves its resource and dress the same way (AppearanceRetained). This
module is the rule for both: work again unless a steady result from the
previous frame exists and every input it was taken from is the same
(MotionStamp, AppearanceStamp). It holds no state.

===========================================================================
*/
import type {
	AppearanceRetained,
	AppearanceStamp,
	KeptRecord,
	MotionRetained,
	MotionStamp,
	PresentationDue,
	RecordStamp
} from "./internal/presentation-contract";

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

/*
================
sameAppearanceStamp

Every term compared by value or identity, none skipped.
================
*/
export function sameAppearanceStamp( a: AppearanceStamp, b: AppearanceStamp ): boolean {
	return a.entity === b.entity && a.manifest === b.manifest && a.resource === b.resource &&
		a.disguise === b.disguise && a.skin === b.skin && a.local === b.local && a.weaponHidden === b.weaponHidden &&
		a.emoteHidden === b.emoteHidden && a.dead === b.dead && a.fortressIndex === b.fortressIndex &&
		a.localTeam === b.localTeam && a.normalFortressClothes === b.normalFortressClothes &&
		a.fortress === b.fortress && a.social === b.social && a.serverName === b.serverName &&
		a.enchantSparks === b.enchantSparks && a.jobSuitHidesArmor === b.jobSuitHidesArmor &&
		a.moonlitThiefSuit === b.moonlitThiefSuit;
}

/*
================
appearanceDue

The production rule for resolve, as motionDue is for decide.
================
*/
export function appearanceDue(
	retained: AppearanceRetained | undefined,
	stamp: AppearanceStamp | undefined
): boolean {
	return !retained || !stamp || !sameAppearanceStamp( retained.stamp, stamp );
}

/*
================
sameRecordStamp

Every term compared by value or identity, none skipped.
================
*/
export function sameRecordStamp( a: RecordStamp, b: RecordStamp ): boolean {
	return a.resolved === b.resolved && a.manifest === b.manifest && a.localGid === b.localGid &&
		a.shadowSize === b.shadowSize && a.bloodEffects === b.bloodEffects && a.appearance === b.appearance &&
		a.fraction === b.fraction && a.crowded === b.crowded &&
		a.optional === b.optional && a.regionId === b.regionId && a.x === b.x && a.y === b.y && a.z === b.z &&
		a.yaw === b.yaw && a.height === b.height && a.petResolve === b.petResolve && a.modifierId === b.modifierId;
}

/*
================
recordDue

The production rule for the published actor, as motionDue is for decide.
================
*/
export function recordDue( kept: KeptRecord | undefined, stamp: RecordStamp | undefined ): boolean {
	return !kept || !stamp || !sameRecordStamp( kept.stamp, stamp );
}

// The production rules, one frozen set for the presenter's lifetime.
export const PRESENTATION_DUE: PresentationDue = Object.freeze( {
	appearance: appearanceDue,
	motion: motionDue,
	record: recordDue
} );
