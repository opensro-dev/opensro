/*
===========================================================================

NPC Animation Contact Markers

Keeps combat probes bound to the same published NPC animation manifest the
runtime consumes. Contact cursors are model data, not Mangyang constants.

===========================================================================
*/

import { npcManifestModels } from "../build/shared/npcManifest.mjs";

const ATTACK_STATE_IDS = new Set( [ 2, 5, 0x10, 0x11 ] );

export function npcAttackContactMarkerKeys( manifest, codename ) {
	const model = npcManifestModels( manifest )[String( codename ?? "" ).trim()];
	if ( !model || !model.animationStates || typeof model.animationStates !== "object" ) {
		return [];
	}

	const keys = new Set();
	for ( const [role, state] of Object.entries( model.animationStates ) ) {
		if ( !/^attack\d*$/iu.test( role ) && !ATTACK_STATE_IDS.has( Number( state?.stateId ) ) ) {
			continue;
		}
		for ( const event of state?.trackEvents ?? [] ) {
			const cursorMs = Number( event?.cursorMs );
			if ( Number( event?.eventCode ) === 1 && Number.isFinite( cursorMs ) && cursorMs >= 0 ) {
				keys.add( cursorMs );
			}
		}
	}
	return [ ...keys ].sort( ( left, right ) => left - right );
}
