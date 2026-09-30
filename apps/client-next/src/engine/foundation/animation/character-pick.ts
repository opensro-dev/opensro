/*
===========================================================================

character-pick.ts - choosing the character under the pointer's rays

World_PickEntityAtScreenPoint (692680) casts nine rays and keeps the nearest
aggregate-box hit, preferring the exact-center ray. This module owns that
choice over precomputed box hits, plus the mesh refinement the renderer
applies when several candidates compete (see meshUnderRays).

===========================================================================
*/
import type { CharacterActor, CharacterModel } from "@/engine/contracts/character";
import { pickGeometry, type PickRay } from "@/engine/foundation/rendering/picking";
import type { createCharacterPose } from "./animation-pose";

// Index of the exact-center ray in the 3x3 pick fan.
const CENTER_RAY = 4;

/*
================
PickCandidate

An actor whose aggregate box one or more rays hit, with its drawn matrix.
================
*/
export interface PickCandidate {
	readonly actor: CharacterActor;
	readonly matrix: Float32Array;
	readonly model: CharacterModel;
	readonly hits: readonly { readonly ray: number; readonly depth: number; readonly distance: number; }[];
}

/*
================
selectPickCandidate

69282B: a center hit may replace an off-center winner; a closer subsequent
hit still wins, including off-center. Candidates keep actor order.
================
*/
export function selectPickCandidate( candidates: readonly PickCandidate[] ) {
	let result: { candidate: PickCandidate; gid: number; depth: number; ray: number; } | null = null,
		best = Infinity;
	for ( const candidate of candidates ) {
		for ( const hit of candidate.hits ) {
			if ( hit.distance < best || (result?.ray !== CENTER_RAY && hit.ray === CENTER_RAY) ) {
				best = hit.distance;
				result = { candidate, gid: candidate.actor.gid, depth: hit.depth, ray: hit.ray };
			}
		}
	}
	return result;
}

/*
================
meshUnderRays

True when any ray meets the candidate's triangles at its current clip and
time, evaluated on the caller's scratch pose for this model. This is a presentation refinement, not native: 856540 tests only the
box, which for a T-posed giant covers far more than its body.
================
*/
export function meshUnderRays(
	candidate: PickCandidate,
	rays: readonly PickRay[],
	pose: ReturnType<typeof createCharacterPose>
): boolean {
	const { actor, matrix, model } = candidate;
	pose.evaluate( actor.clip, actor.time, actor.loop );
	for ( const primitive of model.primitives ) {
		if ( primitive.emission ) continue;
		const palette = new Float32Array( primitive.joints.length * 16 );
		pose.palette( primitive, palette );
		for ( const hit of candidate.hits ) {
			if (
				pickGeometry( rays[hit.ray]!, primitive.geometry, matrix, palette.length ? palette : undefined ) !==
					null
			) {
				return true;
			}
		}
	}
	return false;
}
