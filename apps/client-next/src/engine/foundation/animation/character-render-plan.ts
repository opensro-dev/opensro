/*
===========================================================================

character-render-plan.ts - the per-model render plan of a character

What an admitted model's topology fixes for every frame: clip lookup,
whether it emits, batch capacity and its byte cost, and whether its
materials run a clock.

===========================================================================
*/
import type { CharacterModel, CharacterClip } from "@/engine/contracts/character";
import { CHARACTER_ACTORS, characterPoseBytes, characterBatchBytes } from "./character-budget";
/*
================
createCharacterRenderPlan

Immutable admitted model topology owns this plan. Frame clocks, visibility,
event cursors and matrices never enter it. Replacement models get new plans.
================
*/
export function createCharacterRenderPlan( model: CharacterModel ) {
	const clips = new Map<string, CharacterClip>();
	for ( const clip of model.clips ) if ( !clips.has( clip.name ) ) clips.set( clip.name, clip );
	const emission = model.primitives.some( p => p.emission ),
		exactCapacity = model.primitives.some( p => p.ribbon );
	// Ordinary power-of-two batches have a fixed allocation plus a per-slot cost.
	// Store two scalars instead of a model-by-actor table. Emitted slots scale
	// with the padded actor capacity too. Ribbons retain their exact capacity.
	const one = exactCapacity ? 0 : characterBatchBytes( model, 1 ),
		two = exactCapacity ? 0 : characterBatchBytes( model, 2 ),
		base = one * 2 - two,
		stride = two - one;
	return {
		animationMaterial: model.primitives.some( p =>
			p.modifierSource?.modifiers.materialModifiers.some( m => m.kind === 1 ) ||
			p.modifierSource?.modifiers.textureModifiers.some( m => m.kind === 1 )
		),
		materialClocked: model.primitives.some( p =>
			p.equipmentGlow || p.modifierSource?.modifiers.materialModifiers.some( m => m.kind === 1 ) ||
			p.modifierSource?.modifiers.textureModifiers.some( m => m.kind === 1 ) ||
			p.geometry.material?.colorTimeline || p.geometry.material?.uvVelocity || p.geometry.material?.uvAtlas ||
			p.geometry.material?.textureFactorPulse
		),
		clips,
		emission,
		exactCapacity,
		poseBytes: characterPoseBytes( model ),
		clocked: model.primitives.some( p => p.emission || p.materialFrames ),
		billboard: model.primitives.some( p => p.billboard || p.ribbon ),
		continuousGraph: model.particleGraph?.some( e => e.loop || e.emission ) ?? false,
		sharedPalette: !model.primitives.some( p =>
			p.emission || p.ribbon || p.billboard || !p.geometry.joints || !p.geometry.weights
		),
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
