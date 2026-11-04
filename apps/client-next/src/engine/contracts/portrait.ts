/*
===========================================================================

portrait.ts - borrowed character models for portrait and mannequin projection

The character renderer retains model and texture lifetimes. A preview owns
its pose and GPU handles, including children with their own skeletons.

===========================================================================
*/
import type { CharacterActor, CharacterModel } from "./character";
import type { WorldTexture } from "./texture";

/*
================
PortraitPart

effect marks a worn item's glow. It runs its own clock from the frame the
preview first shows it, where a model part follows the body's clock.
================
*/
export interface PortraitPart {
	readonly actor: CharacterActor;
	readonly model: CharacterModel;
	readonly images: readonly WorldTexture[];
	readonly effect?: boolean;
}

/*
================
PortraitSource
================
*/
export interface PortraitSource extends PortraitPart {
	readonly children?: readonly PortraitPart[];
}
