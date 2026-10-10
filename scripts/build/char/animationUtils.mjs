/*
===========================================================================
animationUtils.mjs - authored animation state and modifier selection.
Shared by character and model publishers so motion binding fallback preserves
explicit silence independently of which animation clip is published.
===========================================================================
*/
const MOTION_MODIFIER_KIND = 1;

/*
================
findDefaultAnimationSet
================
*/
/** Return the native BSR animation set named "default", case-insensitively. */
/** @param {any} bsr */
export function findDefaultAnimationSet( bsr ) {
	return findAnimationSet( bsr, "default" );
}

/*
================
findAnimationSet
================
*/
/** Resolve one authored BSR animation set by its native comparator text. */
/** @param {any} bsr @param {string} setName */
export function findAnimationSet( bsr, setName ) {
	const normalizedName = setName.toLowerCase();
	return bsr.animationSets?.find( ( entry ) => entry.name.toLowerCase() === normalizedName ) ?? null;
}

/*
================
findDefaultAnimationState
================
*/
/** Resolve a state from the native default animation set. */
/** @param {any} bsr @param {number} stateId */
export function findDefaultAnimationState( bsr, stateId ) {
	return findDefaultAnimationSet( bsr )?.states.find( ( entry ) => entry.stateId === stateId ) ?? null;
}

/*
================
pickDefaultSetStateClip
================
*/
/** Resolve the authored BAN path for a default-set state. */
/** @param {any} bsr @param {number} stateId */
export function pickDefaultSetStateClip( bsr, stateId ) {
	return findDefaultAnimationState( bsr, stateId )?.animationPath ?? null;
}

/*
================
pickAnimationSetStateClip
================
*/
/** Resolve an authored BAN path from a named native animation set and state id. */
/** @param {any} bsr @param {string} setName @param {number} stateId */
export function pickAnimationSetStateClip( bsr, setName, stateId ) {
	return findAnimationSet( bsr, setName )?.states.find( ( entry ) => entry.stateId === stateId )?.animationPath ??
		null;
}

/*
================
pickDefaultSetTrackEvents
================
*/
/** Return the retail CResAnimationStateTable event map for a default state. */
/** @param {any} bsr @param {number} stateId */
export function pickDefaultSetTrackEvents( bsr, stateId ) {
	return pickAnimationStateTrackEvents( findDefaultAnimationState( bsr, stateId ) );
}

/*
================
pickDefaultSetStateTableMetadata
================
*/
/** Publish the complete native state-table payload consumed by the runtime. */
/** @param {any} bsr @param {number} stateId */
export function pickDefaultSetStateTableMetadata( bsr, stateId ) {
	return pickAnimationStateTableMetadata( findDefaultAnimationState( bsr, stateId ) );
}

/*
================
pickAnimationStateTableMetadata
================
*/
/**
 * Preserve CResAnimationStateTable+0x04 events and the embedded +0x10 curve.
 * Even an identity curve is a real, required native object; omitting it turns
 * a valid event callback into a mid-advance exception before cursor commit.
 * @param {any} state
 */
export function pickAnimationStateTableMetadata( state ) {
	return {
		trackEvents: pickAnimationStateTrackEvents( state ),
		timeWarpCurve: {
			scale: Number.isFinite( state?.timeWarpScale ) ? state.timeWarpScale : 0,
			records: (state?.timeWarpPoints ?? []).map( ( point ) => ({
				phase: point.input,
				value: point.output
			}) )
		}
	};
}

/*
================
pickAnimationStateTrackEvents
================
*/
/** Match CAnimationState_ReadTransitionMap: records with eventCode 0 are absent. */
/** @param {any} state */
export function pickAnimationStateTrackEvents( state ) {
	return (state?.trackEvents ?? []).filter( ( entry ) => entry.eventCode !== 0 );
}

/*
================
pickDefaultSetSoundEvents
================
*/
/**
 * Read the default ModDataSound variant for a native animation state. An
 * expected cue narrows locomotion/combat manifests without changing track
 * order or cursor units.
 * @param {any} bsr
 * @param {number} stateId
 * @param {string | null} [expectedCue]
 */
export function pickDefaultSetSoundEvents( bsr, stateId, expectedCue = null ) {
	return pickAnimationSetSoundEvents( bsr, "default", stateId, expectedCue );
}

/*
================
pickAnimationSetSoundEvents

CCObjAnimation at 0xADEAB5 tests the whole motion binding; only a missing
binding reaches the default/same-state lookup at 0xADEADE. Empty bindings
and bindings containing only other modifier types deliberately stay silent.
Variant selection retains the existing default/first-entry policy.
================
*/
/** @param {any} bsr @param {string} animationSetName @param {number} stateId
 * @param {string | null} [expectedCue] */
export function pickAnimationSetSoundEvents( bsr, animationSetName, stateId, expectedCue = null ) {
	const requestedName = animationSetName.toLowerCase();
	const matchesRequested = ( entry ) =>
		entry.kind === MOTION_MODIFIER_KIND &&
		entry.stateId === stateId && entry.animationSetName.toLowerCase() === requestedName;
	// Parsed whole bindings include zero modifiers and non-sound modifiers.
	// Sound rows also establish presence for callers supplying projected metadata.
	const hasBinding = (bsr.modifierSets ?? []).some( matchesRequested ) ||
		(bsr.soundModifiers ?? []).some( matchesRequested );
	const selectedName = hasBinding ? requestedName : "default";
	const soundSet = bsr.soundModifiers?.find( ( entry ) =>
		entry.kind === MOTION_MODIFIER_KIND && entry.stateId === stateId &&
		entry.animationSetName.toLowerCase() === selectedName
	);
	const variant = soundSet?.entries.find( ( entry ) => entry.animationName.toLowerCase() === "default" ) ??
		soundSet?.entries[0];
	const normalizedExpectedCue = expectedCue?.toLowerCase() ?? null;
	return (variant?.tracks ?? [])
		.filter( ( track ) => normalizedExpectedCue === null || track.cueName.toLowerCase() === normalizedExpectedCue )
		.map( ( track ) => ({ cursorMs: track.triggerFrame, cue: track.cueName }) );
}
