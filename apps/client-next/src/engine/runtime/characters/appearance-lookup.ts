/*
===========================================================================

appearance-lookup.ts - which model, skin and equipment an entity presents

Resolves an entity to what it is drawn as: a transform skin (a mask's own
model, msch 1) or a reference disguise (msch 3, the body redressed) before
its own refObjId, the equipment the active skin wears, and the catalogue
resource with any structure stage or monster material variant applied. A
thief or hunter monster wears its trade appearance (trade-appearance.ts,
861720): a player body dressed once per spawn.

It keeps no state of its own; it reads the reference appearances, the
published catalogue and the structure visuals, which characters.ts owns.

===========================================================================
*/
import type { createReferenceAppearances } from "@/engine/foundation/animation/reference-appearance";
import { monsterMaterialSlot } from "@/engine/foundation/rendering/monster-scale";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type { EntityState, TransformSkin } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { DeathModel, Resource } from "./internal/presentation-contract";
import {
	type CrowdArmorParts,
	DEFAULT_WEAPON_TYPE,
	tradeAppearance,
	type TradeSkinPools
} from "@/engine/foundation/animation/trade-appearance";

// The thief and hunter type words (TID4 2 and 3 over the monster word).
const THIEF_TYPE_WORD = 0x10c6, HUNTER_TYPE_WORD = 0x18c6;
// CRT rand() returns 0..RAND_MAX.
const RAND_MAX = 0x7fff;
// Dressed bandits remembered at once; a revisit past it dresses again.
const TRADE_SKIN_LIMIT = 4096;

/*
================
AppearanceSources

The owners the lookups read. The catalogue is read through the object on
every call, so a newly admitted catalogue is current.
================
*/
export interface AppearanceSources {
	readonly referenceAppearances: Pick<ReturnType<typeof createReferenceAppearances>, "skin" | "get">;
	readonly published: {
		readonly catalog: ReadonlyMap<number, Resource>;
		readonly deathModels: ReadonlyMap<string, DeathModel>;
		readonly tradeSkinPools: TradeSkinPools;
		readonly itemIds: ReadonlyMap<string, number>;
		readonly dress: { readonly equipment?: Readonly<Record<string, { readonly slot?: number | null; }>>; };
	};
	// Whether presentation entered death with the characterInfo death model
	// (presentation-state enterDeath, native 8E64F0).
	readonly deathShown: ( gid: number ) => boolean;
	// CRT rand() for the trade appearance's draws (0..RAND_MAX); Math.random
	// unless a caller supplies its own.
	readonly random?: () => number;
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
	const { referenceAppearances, published, structureVisuals, deathShown } = sources;
	// 8703F0's part codes are one array for the whole session.
	const parts: CrowdArmorParts = { first: "CA" };
	const tradeSkins = new Map<number, { readonly key: string; readonly skin: TransformSkin | undefined; }>();
	/*
	================
	tradeSkin

	A thief or hunter's dress, drawn once per spawn (861720 runs in the spawn
	reader): the rand() draws repeat only for a new spawn of the gid.
	================
	*/
	function tradeSkin( entity: EntityState ): TransformSkin | undefined {
		const word = (entity.tidWord ?? 0) & 0xfffe;
		if ( entity.kind !== "monster" || entity.tradeVariant === undefined ) return undefined;
		if ( word !== THIEF_TYPE_WORD && word !== HUNTER_TYPE_WORD ) return undefined;
		const key = entity.refObjId + ":" + entity.tradeVariant + ":" + published.tradeSkinPools.china.length;
		const seen = tradeSkins.get( entity.gid );
		if ( seen?.key === key ) return seen.skin;
		// Every bandit's spawn skill (characterdata column 88, skill 3000) routes
		// weapon 2 (skill +0xd8, column 50), the same as 861720's default.
		const dress = tradeAppearance(
			{
				selector: entity.tradeVariant,
				race: entity.countryByte9c ?? -1,
				level: entity.level ?? 1,
				thief: word === THIEF_TYPE_WORD,
				weaponType: DEFAULT_WEAPON_TYPE
			},
			published.tradeSkinPools,
			codename => {
				const id = published.itemIds.get( codename ),
					slot = id === undefined ?
						undefined :
						published.dress.equipment?.[String( id )]?.slot;
				return id === undefined || typeof slot !== "number" ? undefined : { refObjId: id, slot };
			},
			sources.random ?? (() => Math.floor( Math.random() * (RAND_MAX + 1) )),
			parts
		);
		const skin = dress ?
			{
				refObjId: dress.refObjId,
				player: true,
				equipment: dress.equipment.map( item => ({ ...item, typeFlags: 0, plus: 0 }) ),
				revision: 0
			} :
			undefined;
		if ( tradeSkins.size >= TRADE_SKIN_LIMIT ) tradeSkins.clear();
		tradeSkins.set( entity.gid, { key, skin } );
		return skin;
	}
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
		return transformSkinRef( entity ) ?? referenceAppearances.get( entity.gid )?.model ??
			tradeSkin( entity )?.refObjId ?? entity.refObjId;
	}
	// The skin in force: a Duplicate (player skin) wears the copied player's
	// items; a mask wears nothing of the player's (85C060).
	/*
	================
	activeSkin
	================
	*/
	function activeSkin( entity: EntityState ) {
		return transformSkinRef( entity ) !== undefined ? entity.transformSkin : tradeSkin( entity );
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
		// 8E64F0 reloads the mesh from characterInfo +0x28 (LoadVisualMeshFromBSR,
		// keeping scale and transform); the record and its sounds stay the body's.
		const death = resource && deathShown( entity.gid ) ? published.deathModels.get( resource.codename ) : undefined;
		if ( resource && death ) {
			const { materialVariants: _variants, previewGlb: _previewGlb, previewClips: _previewClips, ...body } =
				resource;
			return { ...body, ...death };
		}
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
