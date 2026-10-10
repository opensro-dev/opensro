/*
===========================================================================

attachedMotionClips.mjs - the motions attached skill effects impose on their
host

A skilleffect row with an ActionWait animation installs that motion on its
host while the effect lives (SkillEffectObj_Init 8E06E0 records the row's
animation group and ANI_* id). Every motion install, attached or not, goes
through CRTAniMixer_CreateMotionInstallation (ADE9B0, whose one caller is
CCompChar_FindOrCreateMotionInstallation): it takes the exact (set, state)
binding (0xADEAB5) and only a missing one falls back to ("default", state)
(0xADEADE). So an attached motion's sound list is the shared selector's
for the request's set and state, independent of which set supplied the
clip.

===========================================================================
*/
import path from "node:path";
import { parseSkillAniSet } from "./parseSkillEffect.mjs";
import { SKILL_EFFECT_ANIMATION_ID_BY_NAME } from "./native/skillEffectAnimationRegistry.ts";
import { retailTextdataRoot } from "../world/paths.mjs";
import { findAnimationSet, pickAnimationSetSoundEvents, pickAnimationStateTableMetadata } from "./animationUtils.mjs";

let required;

/*
================
attachedMotionRequests

One request per distinct (animation group, ActionWait id) among the rows.
================
*/
export function attachedMotionRequests( rows ) {
	const result = new Map();
	for ( const row of rows ) {
		const name = row.actionWaitAnims[0];
		if ( !name ) continue;
		const id = SKILL_EFFECT_ANIMATION_ID_BY_NAME.get( name ), set = row.aniGroup.toLowerCase();
		result.set( `${set}:${id}`, { id, set, role: `attached-${set.replaceAll( "_", "-" )}-${id}` } );
	}
	return [ ...result.values() ];
}

/*
================
pickAttachedMotionClips

The clip for each request: the request's own set's state, else the
default set's.
================
*/
export function pickAttachedMotionClips( bsr, requests ) {
	if ( !requests ) {
		required ??= attachedMotionRequests(
			parseSkillAniSet( path.join( retailTextdataRoot, "skilleffect.txt" ) ).values()
		);
	}
	return (requests ?? required).flatMap( ( request ) => {
		const find = ( set ) =>
			findAnimationSet( bsr, set )?.states.find( ( s ) => s.stateId === request.id && s.animationPath );
		const state = find( request.set ) ?? find( "default" );
		return state ? [ { ...request, state, path: state.animationPath } ] : [];
	} );
}

/*
================
attachedMotionMetadata

An attached motion's published sound list and state-table metadata: the
sound comes from the request's whole binding (exact set, else default),
the table from the state that supplied the clip.
================
*/
export function attachedMotionMetadata( bsr, motion ) {
	return {
		soundEvents: pickAnimationSetSoundEvents( bsr, motion.set, motion.id ),
		...pickAnimationStateTableMetadata( motion.state )
	};
}
