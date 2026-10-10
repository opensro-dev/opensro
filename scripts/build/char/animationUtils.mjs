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

/** Match CAnimationState_ReadTransitionMap: records with eventCode 0 are absent. */
/** @param {any} state */
export function pickAnimationStateTrackEvents( state ) {
	return (state?.trackEvents ?? []).filter( ( entry ) => entry.eventCode !== 0 );
}

/**
 * The sound events a motion of animation set `setName` plays for `stateId`.
 * The set's own ModDataSound wins; a set without one plays the default set's
 * for the same state. Native binds sound markers by the installation key and
 * the clip's track names (CRTAniInstallation_BindTrackMarkers AE1770), never
 * by the weapon prefix, and a motion with no set clip falls back to "default"
 * (CRTAniMixer_CreateMotionInstallation ADE9B0). INFERENCE: which CRTModSound
 * instances fill the character's marker map was not traced; weapon sets carry
 * no run/walk ModDataSound of their own, yet retail characters keep their
 * footsteps with any weapon, so the default set's tracks must apply to them.
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
