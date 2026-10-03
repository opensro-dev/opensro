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
createModelMaterialClocks
================
*/
export function createModelMaterialClocks() {
	const rows = new Map<
		number,
		{
			model: CharacterModel;
			animated: ReturnType<typeof createAnimatedMaterial>;
			clocks: (MaterialClock | null)[];
		}
	>();
	const dynamic = new WeakMap<CharacterModel, boolean>();
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
				if ( resource ) required += characterMaterialClockBytes( resource );
			}
			if ( required > CHARACTER_RENDER_BYTES ) throw Error( "Material clock budget exceeded" );
			bytes = required;
			for ( const actor of actors ) {
				const resource = model( actor.model );
				if ( !resource ) continue;
				let active = dynamic.get( resource );
				if ( active === undefined ) {
					active = resource.primitives.some( p =>
						p.equipmentGlow || p.modifierSource?.modifiers.materialModifiers.some( m => m.kind === 1 ) ||
						p.modifierSource?.modifiers.textureModifiers.some( m => m.kind === 1 ) ||
						p.geometry.material?.colorTimeline || p.geometry.material?.uvVelocity ||
						p.geometry.material?.uvAtlas || p.geometry.material?.textureFactorPulse
					);
					dynamic.set( resource, active );
				}
				if ( !active ) continue;
				const id = actor.modifierId ?? actor.gid;
				if ( keep.has( id ) ) throw Error( "Duplicate material instance owner" );
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
								pulse: m?.textureFactorPulse ? createTextureFactorPulse( m.textureFactorPulse ) : undefined,
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
					const p = resource.primitives[i]!, mods = p.modifierSource?.modifiers;
					const glow = row.clocks[i]?.glow;
					glow?.step( delta, (actor.animationLod?.fraction ?? 0) <= .5 && (actor.opacity ?? 1) === 1 );
					if (
						mods &&
						(mods.materialModifiers.some( m => m.kind === 1 ) ||
							mods.textureModifiers.some( m => m.kind === 1 ))
					) row.clocks[i] = { ...row.animated.sample( p, actor ), glow };
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
