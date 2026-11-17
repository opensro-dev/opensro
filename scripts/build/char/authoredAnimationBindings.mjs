/*
===========================================================================

authoredAnimationBindings.mjs - a BSR's animation states as manifest bindings

State identity belongs to the BSR selector, not the BAN filename. Several
selectors may share one exported clip while retaining different event maps.

===========================================================================
*/
import { parseBan } from "./formats.mjs";
import { pickAnimationStateTableMetadata } from "./animationUtils.mjs";

// A state keyed 0xFFFFFFFF is one no action can select. The client keys a
// set's states in an ordered map of unsigned ids (AnimationStateMap_Find
// 0xA78D90) and finds one only for the id an action asks for
// (CResAnimationSet_FindState 0xA78E70, from CRTAniMixer_CreateMotionInstallation
// 0xADE9B0); action states are small enumerated ids, so the entry is stored
// and never reached. A later client's jupiter.bsr authors one
// (jupiter_attack10.ban). The manifest's binding contract
// (model-animation.ts, 0..65535) has no place for it, so it is not baked.
// Inference recorded: no v1.150 action requests that id.
export const UNSELECTABLE_STATE_ID = 0xFFFFFFFF;

/*
================
authoredAnimationBindings
================
*/
export async function authoredAnimationBindings( bsr, clips, load ) {
	const paths = new Map( clips.map( row => [ row.path.toLowerCase(), row ] ) ), attempted = new Map(), bindings = [];
	for ( const set of bsr.animationSets ?? [] ) {
		for ( const state of set.states ) {
			if ( state.stateId === UNSELECTABLE_STATE_ID ) continue;
			const path = state.animationPath;
			if ( !path ) {
				bindings.push( { set: set.name, stateId: state.stateId, clip: null, reason: "no-authored-animation" } );
				continue;
			}
			const key = path.toLowerCase();
			let row = paths.get( key ), failure;
			if ( !row ) {
				if ( !attempted.has( key ) ) {
					const bytes = await load( path );
					let clip;
					if ( bytes !== null ) {
						try {
							clip = parseBan( bytes, path );
						} catch {
							failure = "unreadable-animation";
						}
					}
					if ( clip ) {
						const role = `native:${set.name}:${state.stateId}`;
						if ( clips.some( r => r.role === role ) ) {
							throw Error( "Conflicting authored animation role " + role );
						}
						row = { role, path, clip };
						clips.push( row );
						paths.set( key, row );
					} else attempted.set( key, failure ?? "absent-animation" );
				}
				failure = attempted.get( key );
			}
			bindings.push( {
				set: set.name,
				stateId: state.stateId,
				path,
				clip: row?.role ?? null,
				...(row ?
					{
						durationMs: row.clip.durationMs,
						loop: row.clip.field2 !== 0,
						...pickAnimationStateTableMetadata( state ),
						soundModifiers: (bsr.soundModifiers ?? []).filter( m =>
							m.kind === 1 && m.stateId === state.stateId &&
							m.animationSetName.toLowerCase() === set.name.toLowerCase()
						)
					} :
					{ reason: failure })
			} );
		}
	}
	return bindings;
}
