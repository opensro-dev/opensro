/*
===========================================================================

appearance-lookup.ts - which model, skin and equipment an entity presents

Resolves an entity to what it is drawn as: a transform skin (a mask's own
model, msch 1) or a reference disguise (msch 3, the body redressed) before
its own refObjId, the equipment the active skin wears, and the catalogue
resource with any structure stage or monster material variant applied.

It keeps no state of its own; it reads the reference appearances, the
published catalogue and the structure visuals, which characters.ts owns.

===========================================================================
*/
import type { createReferenceAppearances } from "@/engine/foundation/animation/reference-appearance";
import { monsterMaterialSlot } from "@/engine/foundation/rendering/monster-scale";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { Resource } from "./internal/presentation-contract";

/*
================
AppearanceSources

The owners the lookups read. The catalogue is read through the object on
every call, so a newly admitted catalogue is current.
================
*/
export interface AppearanceSources {
	readonly referenceAppearances: Pick<ReturnType<typeof createReferenceAppearances>, "skin" | "get">;
	readonly published: { readonly catalog: ReadonlyMap<number, Resource>; };
	readonly structureVisuals: {
		appearance(
			gid: number,
			glb: string,
			particles: readonly ModelParticle[]
		): { glb: string; particles: readonly ModelParticle[]; } | undefined;
	};
}

/*
================
createAppearanceLookup
================
*/
export function createAppearanceLookup( sources: AppearanceSources ) {
	const { referenceAppearances, published, structureVisuals } = sources;
	// A mask's skin (msch 1) replaces the model outright; an msch 3 disguise
	// keeps the body and redresses it.
	/*
	================
	transformSkinRef
	================
	*/
	function transformSkinRef( entity: EntityState ) {
		return referenceAppearances.skin( entity.gid, entity.transformSkin );
	}
	/*
	================
	appearanceRef
	================
	*/
	function appearanceRef( entity: EntityState ) {
		return transformSkinRef( entity ) ?? referenceAppearances.get( entity.gid )?.model ?? entity.refObjId;
	}
	// The skin in force: a Duplicate (player skin) wears the copied player's
	// items; a mask wears nothing of the player's (85C060).
	/*
	================
	activeSkin
	================
	*/
	function activeSkin( entity: EntityState ) {
		return transformSkinRef( entity ) !== undefined ? entity.transformSkin : undefined;
	}
	/*
	================
	wornEquipment
	================
	*/
	function wornEquipment(
		entity: EntityState,
		gameplay: GameplayState | null
	): readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly typeFlags: number;
		readonly plus: number;
	}[] {
		const skin = activeSkin( entity );
		if ( skin ) return skin.equipment;
		return entity.gid === gameplay?.localGid ? gameplay.inventory : entity.equipment ?? [];
	}
	/*
	================
	resourceFor
	================
	*/
	function resourceFor( entity: EntityState ): Resource | undefined {
		const resource = published.catalog.get( appearanceRef( entity ) );
		const staged = resource?.structureVisuals &&
			structureVisuals.appearance( entity.gid, resource.glb, resource.ambientParticles ?? [] );
		if ( resource && staged ) return { ...resource, glb: staged.glb, ambientParticles: staged.particles };
		const variant = resource && entity.kind === "monster" ?
			resource.materialVariants
				?.[String( monsterMaterialSlot( entity.rarity ?? 0, entity.tidWord ?? 0, resource.materialKind ) )] :
			undefined;
		return resource && variant ? { ...resource, glb: variant } : resource;
	}
	return { appearanceRef, activeSkin, wornEquipment, resourceFor };
}
