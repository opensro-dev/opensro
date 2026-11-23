/*
===========================================================================

character-pick.ts - choosing the character under the pointer's rays

World_PickEntityAtScreenPoint (692680) casts nine rays and keeps the nearest
aggregate-box hit, preferring the exact-center ray. This module owns that
choice over precomputed box hits. meshUnderRays stays as a tested geometry
helper; selection itself is the native nearest-box rule and never queries
meshes.

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
dropOccludedCandidates

#4125 (owner ruling 2026-11-23: a native bug is not copied): native
692680 keeps the nearest box hit and never asks whether scenery stands
between the camera and it, so an NPC behind a wall can be selected. The
port drops a candidate whose every hit ray is occluded before it
reaches the box (visible answers that for one ray at its hit depth);
a candidate with one clear ray stays, because the player can see and
click that part of it. The winner rule itself is untouched: selection
still runs selectPickCandidate over what remains.
================
*/
export function dropOccludedCandidates(
	candidates: readonly PickCandidate[],
	visible: ( ray: number, depth: number ) => boolean
): PickCandidate[] {
	return candidates.filter( candidate => candidate.hits.some( hit => visible( hit.ray, hit.depth ) ) );
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
