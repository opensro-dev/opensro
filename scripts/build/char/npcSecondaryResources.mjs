/*
===========================================================================

npcSecondaryResources.mjs - path-keyed secondary character resource bakes

Rides, death bodies and stall booths share the primary character compiler,
output ownership and BSR cache. Their animation policy belongs to each request.
The caller publishes one manifest only after all required resources succeed.

===========================================================================
*/

import { claimResourceOutput, resourceGlbOutput } from "./resourceGlbOutput.mjs";

/*
================
bakeNpcSecondaryResources

The context is the existing NPC publisher's mutable build state. Injecting
its compiler keeps secondary resources on the same mesh/animation path and
lets fixture tests exercise publication without writing the shared tree.
================
*/
export async function bakeNpcSecondaryResources( context, requests ) {
	const counts = { built: 0, covered: 0, reused: 0 };
	for ( const { bsrPath: key, kind, fields, isMob, requiredStates } of requests ) {
		const output = resourceGlbOutput( key, { namespace: "npc", publicAssetsRoot: context.publicAssetsRoot } );
		claimResourceOutput( context.outputOwners, key, output.publicPath );
		const entry = { codename: key, kind, bsr: key, ...fields };
		context.retailAnimationModels.set( key, { codename: key, refObjId: null, kind, bsr: key } );
		try {
			const prior = context.bakedByBsr.get( key );
			if ( prior ) {
				if ( prior.isMob !== isMob ) {
					throw new Error( `${key}: ${kind} resource has an incompatible clip policy` );
				}
				Object.assign( entry, prior.baked );
				counts.reused += 1;
			} else {
				const baked = await context.bake( key, output, isMob, requiredStates );
				const { retailAnimationCatalog, ...missionBaked } = baked;
				Object.assign( entry, missionBaked );
				context.bakedByBsr.set( key, { isMob, baked: missionBaked } );
				context.retailAnimationResources.set( key, {
					bsr: key,
					glb: missionBaked.glb,
					animations: retailAnimationCatalog
				} );
				counts.built += 1;
			}
			counts.covered += 1;
			console.log( `[npc] OK   ${key} -> ${entry.glb} (${prior ? "shared bake" : `${entry.bytes} B`})` );
		} catch ( error ) {
			entry.error = String( error?.message ?? error );
			console.warn( `[npc] FAIL ${key} ${entry.error}` );
		}
		context.models.push( entry );
	}
	return counts;
}
