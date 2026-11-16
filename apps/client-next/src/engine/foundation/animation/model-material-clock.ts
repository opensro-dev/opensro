/*
===========================================================================

model-material-clock.ts - per-instance material clocks of character models

Render-owner clocks, advanced by the existing frame. A visual instance
owns its modifiers independently of changing BAN clips and presentation
GIDs: equipment glow, colour timelines, texture motion and atlases, and a
material modifier's TEXTUREFACTOR pulse.

===========================================================================
*/
import { createEquipmentGlowClock } from "@/engine/foundation/rendering/equipment-glow";
import { createModifierDelta } from "@/engine/foundation/rendering/modifier-delta";
import { createAnimatedMaterial, type MaterialClock } from "./animated-material";
import { characterMaterialClockBytes, CHARACTER_RENDER_BYTES } from "./character-budget";
import type { CharacterActor, CharacterModel } from "@/engine/contracts/character";
import { createMaterialTimeline } from "@/engine/foundation/rendering/material-timeline";
import { createTextureMotion } from "@/engine/foundation/rendering/texture-motion";
import { createTextureAtlas } from "@/engine/foundation/rendering/texture-atlas";
import { createTextureFactorPulse } from "@/engine/foundation/rendering/texture-factor-pulse";

/*
================
materialClockShape

What a model's immutable primitives fix for its clocks: the bytes one
instance reserves, whether any material runs a clock, and per primitive
whether a kind-1 modifier samples it through the animated material.
================
*/
function materialClockShape( model: CharacterModel ) {
	const animated = model.primitives.map( p => {
		const mods = p.modifierSource?.modifiers;
		return !!mods &&
			(mods.materialModifiers.some( m => m.kind === 1 ) || mods.textureModifiers.some( m => m.kind === 1 ));
	} );
	return {
		bytes: characterMaterialClockBytes( model ),
		active: model.primitives.some( ( p, i ) =>
			p.equipmentGlow || animated[i] || p.geometry.material?.colorTimeline || p.geometry.material?.uvVelocity ||
			p.geometry.material?.uvAtlas || p.geometry.material?.textureFactorPulse
		),
		animated
	};
}
/*
================
createModelMaterialClocks

One owner id owns one material row. Producers hand every model instance
its own owner, so two actors on one id are a producer bug: the renderer
steps the owner's row once and carries on, while a test that passes
assertUniqueOwners keeps the throw as its assertion.
================
*/
export function createModelMaterialClocks( assertUniqueOwners = false ) {
	const rows = new Map<
		number,
		{
			model: CharacterModel;
			animated: ReturnType<typeof createAnimatedMaterial>;
			clocks: (MaterialClock | null)[];
		}
	>();
	// Keyed by the admitted model object: a replacement or a catalog
	// extension is a new object, so it never reads another model's shape.
	const shapes = new WeakMap<CharacterModel, ReturnType<typeof materialClockShape>>();
	const shapeOf = ( resource: CharacterModel ) => {
		let shape = shapes.get( resource );
		if ( !shape ) {
			shape = materialClockShape( resource );
			shapes.set( resource, shape );
		}
		return shape;
	};
	let bytes = 0, deltaFor = createModifierDelta();
	return {
		step(
			actors: readonly CharacterActor[],
			seconds: number,
			model: ( path: string ) => CharacterModel | undefined
		) {
			const delta = deltaFor( seconds ), keep = new Set<number>();
			let required = 0;
			for ( const actor of actors ) {
				const resource = model( actor.model );
				if ( resource ) required += shapeOf( resource ).bytes;
			}
			if ( required > CHARACTER_RENDER_BYTES ) throw Error( "Material clock budget exceeded" );
			bytes = required;
			for ( const actor of actors ) {
				const resource = model( actor.model );
				if ( !resource ) continue;
				const shape = shapeOf( resource );
				if ( !shape.active ) continue;
				const id = actor.modifierId ?? actor.gid;
				if ( keep.has( id ) ) {
					if ( assertUniqueOwners ) throw Error( "Duplicate material instance owner" );
					continue;
				}
				keep.add( id );
				let row = rows.get( id );
				if ( !row || row.model !== resource ) {
					row = {
						model: resource,
						animated: createAnimatedMaterial(),
						clocks: resource.primitives.map( p => {
							const m = p.geometry.material;
							if (
								!p.equipmentGlow && !m?.colorTimeline && !m?.uvVelocity && !m?.uvAtlas &&
								!m?.textureFactorPulse
							) return null;
							return {
								glow: p.equipmentGlow ? createEquipmentGlowClock( p.equipmentGlow ) : undefined,
								color: m?.colorTimeline ? createMaterialTimeline( m.colorTimeline ) : undefined,
								texture: m?.uvVelocity ?
									createTextureMotion( m.uvVelocity ) :
									m?.uvAtlas ?
									createTextureAtlas( m.uvAtlas ) :
									undefined,
								pulse: m?.textureFactorPulse ?
									createTextureFactorPulse( m.textureFactorPulse ) :
									undefined,
								flags: m?.colorTimeline?.flags ?? 0,
								colorChanged: false,
								textureChanged: false,
								pulseChanged: false
							};
						} )
					};
					rows.set( id, row );
				}
				row.animated.begin( seconds );
				for ( let i = 0; i < row.clocks.length; i++ ) {
					const p = resource.primitives[i]!;
					const glow = row.clocks[i]?.glow;
					glow?.step( delta, (actor.animationLod?.fraction ?? 0) <= .5 && (actor.opacity ?? 1) === 1 );
					if ( shape.animated[i] ) row.clocks[i] = { ...row.animated.sample( p, actor ), glow };
					else {
						const clock = row.clocks[i];
						if ( clock ) {
							clock.colorChanged = clock.color?.step( seconds ) ?? false;
							clock.textureChanged = clock.texture?.step( seconds ) ?? false;
							clock.pulseChanged = clock.pulse?.step( seconds ) ?? false;
						}
					}
				}
			}
			for ( const id of rows.keys() ) if ( !keep.has( id ) ) rows.delete( id );
		},
		get( actor: CharacterActor ) {
			return rows.get( actor.modifierId ?? actor.gid )?.clocks;
		},
		bytes: () => bytes,
		reset() {
			rows.clear();
			bytes = 0;
			deltaFor = createModifierDelta();
		}
	};
}
