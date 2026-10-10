/** Return the native BSR animation set named "default", case-insensitively. */
/** @param {any} bsr */
export function findDefaultAnimationSet( bsr ) {
	return findAnimationSet( bsr, "default" );
}

/** Resolve one authored BSR animation set by its native comparator text. */
/** @param {any} bsr @param {string} setName */
export function findAnimationSet( bsr, setName ) {
	const normalizedName = setName.toLowerCase();
	return bsr.animationSets?.find( ( entry ) => entry.name.toLowerCase() === normalizedName ) ?? null;
}

/** Resolve a state from the native default animation set. */
/** @param {any} bsr @param {number} stateId */
export function findDefaultAnimationState( bsr, stateId ) {
	return findDefaultAnimationSet( bsr )?.states.find( ( entry ) => entry.stateId === stateId ) ?? null;
}

/** Resolve the authored BAN path for a default-set state. */
/** @param {any} bsr @param {number} stateId */
export function pickDefaultSetStateClip( bsr, stateId ) {
	return findDefaultAnimationState( bsr, stateId )?.animationPath ?? null;
}

/** Resolve an authored BAN path from a named native animation set and state id. */
/** @param {any} bsr @param {string} setName @param {number} stateId */
export function pickAnimationSetStateClip( bsr, setName, stateId ) {
	return findAnimationSet( bsr, setName )?.states.find( ( entry ) => entry.stateId === stateId )?.animationPath ??
		null;
}

/** Return the retail CResAnimationStateTable event map for a default state. */
/** @param {any} bsr @param {number} stateId */
export function pickDefaultSetTrackEvents( bsr, stateId ) {
	return pickAnimationStateTrackEvents( findDefaultAnimationState( bsr, stateId ) );
}

/** Publish the complete native state-table payload consumed by the runtime. */
/** @param {any} bsr @param {number} stateId */
export function pickDefaultSetStateTableMetadata( bsr, stateId ) {
	return pickAnimationStateTableMetadata( findDefaultAnimationState( bsr, stateId ) );
}

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

/**
 * A set state's track events (foot contacts, eventCode 2), or the default
 * state's when the set authors none: the same clip-only override, and the same
 * open question, as pickSetSoundEvents. Keeps steps and foot effects in sync.
 * @param {any} bsr
 * @param {any} state
 * @param {number} stateId
 */
export function pickSetTrackEvents( bsr, state, stateId ) {
	const own = pickAnimationStateTrackEvents( state );
	return own.length > 0 ? own : pickDefaultSetTrackEvents( bsr, stateId );
}

/** Match CAnimationState_ReadTransitionMap: records with eventCode 0 are absent. */
/** @param {any} state */
export function pickAnimationStateTrackEvents( state ) {
	return (state?.trackEvents ?? []).filter( ( entry ) => entry.eventCode !== 0 );
}

/**
 * The sound events a motion of animation set `setName` plays for `stateId`.
 * The set's own ModDataSound wins; a set without one plays the default set's
 * for the same state.
 *
 * Most weapon sets override a state's CLIP only: two-hand staff, two-hand
 * sword, dagger, dual axe and harp author no run ModDataSound and no run foot
 * contacts. Native falls back to "default" for a motion with no set clip
 * (CRTAniMixer_CreateMotionInstallation ADE9B0) and binds sound markers by the
 * installation key and the clip's track names, not by the weapon prefix
 * (CRTAniInstallation_BindTrackMarkers AE1770). What fills that marker map
 * was not traced (#525), so it is open whether retail plays the default
 * footsteps for those sets or runs silently. If retail is silent, that is a
 * data omission, and this fallback is a deliberate deviation: port-only, not
 * native. Either way players keep their footsteps with every weapon.
 * @param {any} bsr
 * @param {string} setName
 * @param {number} stateId
 */
export function pickSetSoundEvents( bsr, setName, stateId ) {
	const own = bsr.soundModifiers?.find(
		( entry ) =>
			entry.kind === 1 &&
			entry.stateId === stateId &&
			entry.animationSetName.toLowerCase() === String( setName ).toLowerCase()
	);
	if ( !own ) return pickDefaultSetSoundEvents( bsr, stateId );
	const variant = own.entries.find( ( entry ) => entry.animationName.toLowerCase() === "default" ) ?? own.entries[0];
	return (variant?.tracks ?? []).map( ( track ) => ({ cursorMs: track.triggerFrame, cue: track.cueName }) );
}

/**
 * Read the default ModDataSound variant for a native animation state. An
 * expected cue narrows locomotion/combat manifests without changing track
 * order or cursor units.
 * @param {any} bsr
 * @param {number} stateId
 * @param {string | null} [expectedCue]
 */
export function pickDefaultSetSoundEvents( bsr, stateId, expectedCue = null ) {
	const soundSet = bsr.soundModifiers?.find(
		( entry ) =>
			entry.kind === 1 &&
			entry.stateId === stateId &&
			entry.animationSetName.toLowerCase() === "default"
	);
	const variant = soundSet?.entries.find( ( entry ) => entry.animationName.toLowerCase() === "default" ) ??
		soundSet?.entries[0];
	const normalizedExpectedCue = expectedCue?.toLowerCase() ?? null;
	return (variant?.tracks ?? [])
		.filter(
			( track ) => normalizedExpectedCue === null || track.cueName.toLowerCase() === normalizedExpectedCue
		)
		.map( ( track ) => ({
			cursorMs: track.triggerFrame,
			cue: track.cueName
		}) );
}
