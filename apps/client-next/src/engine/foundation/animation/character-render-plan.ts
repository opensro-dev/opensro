/*
===========================================================================

character-render-plan.ts - the per-model render plan of a character

What an admitted model's topology fixes for every frame: clip lookup,
whether it emits, batch capacity and its byte cost, and whether its
materials run a clock.

===========================================================================
*/
import type { CharacterModel, CharacterClip } from "@/engine/contracts/character";
import {
	CHARACTER_ACTORS,
	characterPoseBytes,
	characterBatchBytes,
	characterMeshBytes,
	characterSharedMesh,
	characterSharedPalette,
	type CharacterPlanCaches
} from "./character-budget";
import { meshArrays } from "@/engine/foundation/rendering/geometry-vertices";
import { createParticleGraphPlan } from "@/engine/foundation/animation/particle-graph-plan";
/*
================
isCharacterAnimationExtension

Lazy clips extend a catalog, not mesh, skeleton or texture identity. Require
the exact admitted prefix so replacement clips still take the replacement path.
================
*/
export function isCharacterAnimationExtension( previous: CharacterModel, next: CharacterModel ): boolean {
	return previous.nodes === next.nodes && previous.primitives === next.primitives &&
		previous.images === next.images && previous.aggregateBox === next.aggregateBox &&
		previous.equipmentGlows === next.equipmentGlows && previous.particleGraph === next.particleGraph &&
		previous.clips.length <= next.clips.length &&
		previous.clips.every( ( clip, index ) => next.clips[index] === clip );
}
/*
================
createCharacterRenderPlan

Immutable admitted model topology owns this plan. Frame clocks, visibility,
event cursors and matrices never enter it. Replacement models get new plans;
the plan caches keep a re-plan from hashing its clips' key times or its
primitives' bindings again.
================
*/
export function createCharacterRenderPlan( model: CharacterModel, caches?: CharacterPlanCaches ) {
	const clips = new Map<string, CharacterClip>();
	for ( const clip of model.clips ) if ( !clips.has( clip.name ) ) clips.set( clip.name, clip );
	const emission = model.primitives.some( p => p.emission ),
		exactCapacity = model.primitives.some( p => p.ribbon );
	// By primitive: a material clock (model-material-clock.ts) rewrites its
	// material every frame, so its draw uploads a dynamic material.
	const dynamicMaterials: readonly boolean[] = model.primitives.map( p =>
		!!(p.equipmentGlow || p.modifierSource?.modifiers.materialModifiers.some( m => m.kind === 1 ) ||
			p.modifierSource?.modifiers.textureModifiers.some( m => m.kind === 1 ) ||
			p.geometry.material?.colorTimeline || p.geometry.material?.uvVelocity || p.geometry.material?.uvAtlas ||
			p.geometry.material?.textureFactorPulse)
	);
	// Ordinary power-of-two batches have a fixed allocation plus a per-slot cost.
	// Store two scalars instead of a model-by-actor table. Emitted slots scale
	// with the padded actor capacity too. Ribbons retain their exact capacity.
	const one = exactCapacity ? 0 : characterBatchBytes( model, 1 ),
		two = exactCapacity ? 0 : characterBatchBytes( model, 2 ),
		base = one * 2 - two,
		stride = two - one;
	return {
		cloth: model.primitives.some( p => p.cloth ),
		equipmentGlow: model.primitives.some( p => p.equipmentGlow ),
		animationMaterial: model.primitives.some( p =>
			p.modifierSource?.modifiers.materialModifiers.some( m => m.kind === 1 ) ||
			p.modifierSource?.modifiers.textureModifiers.some( m => m.kind === 1 )
		),
		materialClocked: dynamicMaterials.includes( true ),
		dynamicMaterials,
		clips,
		emission,
		exactCapacity,
		poseBytes: characterPoseBytes( model, caches ),
		// The shared meshes this model draws, each with its one GPU copy's
		// bytes; the frame plan charges each once, whatever batches draw it.
		meshes: model.primitives.filter( characterSharedMesh ).map( p => ({
			positions: p.geometry.positions,
			arrays: meshArrays( p.geometry ),
			bytes: characterMeshBytes( p.geometry )
		}) ),
		clocked: model.primitives.some( p => p.emission || p.materialFrames ),
		// An authored alpha-blended material: the group draws in the
		// translucent tier, back to front, whatever its opacity.
		blended: model.primitives.some( p => !!p.geometry.material?.blend ),
		billboard: model.primitives.some( p => p.billboard || p.ribbon ),
		continuousGraph: model.particleGraph?.some( e => e.loop || e.emission ) ?? false,
		// The tables every tick of the model's particle graphs reads.
		particleGraph: model.particleGraph ?
			createParticleGraphPlan( model.particleGraph, model.primitives ) :
			undefined,
		sharedPalette: characterSharedPalette( model ),
		particleBytes: model.primitives.reduce(
			( sum, p ) => sum + (p.emission?.capacity ?? p.emission?.births.length ?? 0) * 320,
			0
		) + (model.particleGraph?.reduce( ( sum, e ) => sum + (e.capacity ?? e.births.length) * 640, 0 ) ?? 0),
		capacity: ( count: number ) => exactCapacity ? count : 2 ** Math.ceil( Math.log2( count ) ),
		/*
		================
		batchBytes

		The bytes a batch of count actors costs.
		================
		*/
		batchBytes( count: number ) {
			if ( !Number.isInteger( count ) || count < 0 || count > CHARACTER_ACTORS ) {
				throw Error( "Character plan capacity exceeded" );
			}
			return count === 0 ?
				0 :
				exactCapacity ?
				characterBatchBytes( model, count ) :
				base + stride * 2 ** Math.ceil( Math.log2( count ) );
		}
	};
}
