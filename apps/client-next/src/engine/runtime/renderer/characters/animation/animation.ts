/*
===========================================================================

animation.ts - character-renderer access to the shared pose evaluator

The renderer passes optional diagnostic observers to the same evaluator used
by ordinary rendering. This seam owns no animation state or source rewriting.

===========================================================================
*/
import { createCharacterPose as createPose } from "@/engine/foundation/animation/animation-pose";
import type { CharacterModel } from "@/engine/contracts/character";
import type { ClipNodeChannelCache } from "@/engine/foundation/animation/animation-timelines";
/*
================
createCharacterPose

nodeChannels is the renderer's shared per-clip channel index
(CharacterPlanCaches).
================
*/
export function createCharacterPose( model: CharacterModel, nodeChannels?: ClipNodeChannelCache ) {
	return createPose( model, nodeChannels );
}
