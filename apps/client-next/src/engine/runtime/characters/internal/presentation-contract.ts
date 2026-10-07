/*
===========================================================================

presentation-contract.ts - the shapes character presentation's modules share

A type-only contract owned by characters.ts. Every module under
runtime/characters may import it; the modules never import each other. The
catalogue row types are what presentation-catalog.ts publishes and every
assembly, animation and sound phase reads.

===========================================================================
*/

import type { AnimationParticleSet } from "@/engine/foundation/animation/animation-emission";
import type { ModifierSelector, ModelAnimationBinding } from "@/engine/foundation/animation/model-animation";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type { StructureVisuals } from "@/engine/foundation/rendering/structure-stage";
import type { AnimationMetadata } from "@/engine/foundation/animation/animation-metadata";

/*
================
Resource
================
*/
export interface Resource {
	particleModifiers?: unknown;
	animationParticles?: readonly AnimationParticleSet[];
	animationParticlePaths?: readonly string[];
	animationBindings?: unknown;
	modifierSets?: unknown;
	modifierSelectors?: readonly ModifierSelector[];
	modifierBindings?: readonly ModelAnimationBinding[];
	ambientParticles?: readonly ModelParticle[];
	// The manifest's atstructeffect fields, read into structureVisuals.
	structureStages?: unknown;
	structureSounds?: unknown;
	structureDamageEffects?: unknown;
	structureVisuals?: StructureVisuals;
	materialKind?: number;
	materialVariants?: Readonly<Record<string, string>>;
	scalePercent?: number;
	eventRain?: boolean;
	soundProfileName?: string;
	animationStates?: Record<string, AnimationMetadata>;
	codename: string;
	cover?: Record<string, number>;
	refObjId: number;
	glb: string;
	clips: readonly string[];
	previewGlb?: string;
	previewClips?: readonly string[];
}

/*
================
LinkedRide

A characterInfo "ride" BSR (skilleffect.txt section characterInfo, columns
Ride Type and ride), published as an npc manifest row of kind "ride".
================
*/
export interface LinkedRide {
	readonly glb: string;
	readonly clips: readonly string[];
}

/*
================
ItemPresentation

One item's presentation row, keyed by its RefObjID (itemsByRefObjId).
================
*/
export interface ItemPresentation {
	codename: string;
	dropModelPath?: string;
	wornModelPath?: string | null;
}

/*
================
DropModel

One ground-drop model from the item-drop manifest.
================
*/
export interface DropModel {
	glb: string;
	clips: readonly string[];
	clipLoop: boolean;
	particleModifiers?: unknown;
	ambientParticles?: readonly ModelParticle[];
}
