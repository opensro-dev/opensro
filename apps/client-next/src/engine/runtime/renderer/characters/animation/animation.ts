/*
===========================================================================

animation.ts - character-renderer access to the shared pose evaluator

The renderer passes optional diagnostic observers to the same evaluator used
by ordinary rendering. This seam owns no animation state or source rewriting.

===========================================================================
*/
import {
	createCharacterPose as createPose,
	type AnimationPoseProbe
} from "@/engine/foundation/animation/animation-pose";
import type { CharacterModel } from "@/engine/contracts/character";
/*
================
createCharacterPose
================
*/
export function createCharacterPose( model: CharacterModel, probe?: AnimationPoseProbe ) {
	return createPose( model, probe );
}
