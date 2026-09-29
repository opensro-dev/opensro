/*
===========================================================================

equipment-appearance.ts - one item-to-appearance assembly for every actor

The native client dresses the world, the character-select dock, the creation
preview, transforms and the title crowd through one routine,
CCObjCharacter_SetEquipSlotVisual, fed by RefItemIDs (character list,
inventory, creation choices) or by item codenames resolved through
GlobalDataManager_GetItemRecordByCodeName (reference appearance, crowd).
This module is that routine for the port: it owns the per-item catalog
types, the item -> slot resolution, the codename index and the assembly of
attachments, default wear, fortress clothing, Hwan hair and avatar extras.

It does not own resource readiness, renderer registration or per-actor
state; the character owner calls it and commits the result.

===========================================================================
*/
import type { CharacterAttachment } from "@/engine/contracts/character";
import type { CharacterItem } from "@/engine/contracts/session";
import type { EquipmentBranch } from "@/engine/foundation/animation/equipment-sockets";
import { equipmentSocket } from "@/engine/foundation/animation/equipment-sockets";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import { refreshDefaultWear } from "@/engine/foundation/animation/default-wear-policy";
import {
	selectDefaultWear,
	selectEquipmentVisuals,
	type WornItem
} from "@/engine/foundation/animation/equipment-visuals";

export type { WornItem };

// Worn sockets 0..8 (SCharacterInfo_BuildDisplayActor walks nine).
const WORN_SLOT_COUNT = 9;
const WEAPON_SLOT = 6;
const SHIELD_SLOT = 7;

export type SetEntry = {
	branches?: readonly EquipmentBranch[];
	glb: string;
	parts: string[];
	covers?: Record<string, number[]>;
};

export type AuxiliaryEntry = SetEntry & { bone: string; clips: readonly string[]; };

export interface EquipmentRecord {
	code?: string;
	slot: number | null;
	avatarSlot?: number;
	armorClass: number;
	thiefSuit: boolean;
	visualMask: number;
	visualPriority: number;
	model: string | null;
	source: number | null;
	bodies: Record<string, SetEntry | null>;
}

export interface DressCatalog {
	defaultWear?: Record<string, SetEntry>;
	fortressWear?: Record<string, SetEntry>;
	defaultWearLanguage?: number;
	specialGlows?: Record<string, readonly ModelParticle[]>;
	avatarVisualOverrides?: Record<string, { animation: string; priority: number; additionalBsr: string; }>;
	avatarAuxiliary?: Record<string, AuxiliaryEntry>;
	equipment?: Record<string, EquipmentRecord>;
	hwan?: Record<string, SetEntry & { clip: string; bone: string; }>;
}

export interface AppearanceResource {
	readonly codename: string;
	readonly cover?: Record<string, number>;
}

export interface AppearanceInput {
	readonly resource: AppearanceResource;
	readonly dress: DressCatalog;
	// Worn sockets 0..8; other inventory rows are ignored.
	readonly equipment: readonly WornItem[];
	readonly avatars: readonly { readonly refObjId: number; }[];
	readonly hwanHair: boolean;
	readonly mounted: boolean;
	readonly weaponHidden: boolean;
	readonly attachmentsHidden: boolean;
	readonly fortressIndex: number;
	readonly player: boolean;
	// Ownerless previews (dock, creation) pass the native player gate for both races.
	readonly ownerless: boolean;
	readonly committedWear: readonly string[];
	readonly freezeWear: boolean;
}

export interface AppearanceAssembly {
	readonly parts: CharacterAttachment[];
	readonly auxiliary: { id: number; entry: AuxiliaryEntry; }[];
	// Enhancement glows only; the caller adds the model's own ambient particles.
	readonly particles: ModelParticle[];
	readonly defaultWear: readonly string[];
	readonly avatarIds: readonly number[];
}

/*
================
bodyPrefix

"CH_M" / "EU_W" for a player model codename, "" for anything else.
================
*/
export function bodyPrefix( codename: string ): string {
	const race = /^CHAR_(CH|EU)_(MAN|WOMAN)_/.exec( codename );
	return race ? `${race[1]}_${race[2] === "MAN" ? "M" : "W"}` : "";
}

/*
================
coverIndices

The wearer prims an attachment hides: its native cover keys mapped through
the wearer's own {coverKey -> prim} table.
================
*/
function coverIndices( entry: SetEntry, resource: AppearanceResource ): number[] {
	return entry.parts.flatMap( part =>
		(entry.covers?.[part] ?? []).map( index => resource.cover?.[String( index )] ).filter( (
			index
		): index is number => index !== undefined )
	);
}

/*
================
wornItemsFromList

Native character-list and creation items carry no slot:
SCharacterInfo_BuildDisplayActor asks the item record for it
(ItemRecord_GetEquipSlotIndex). Items without a visual socket (jewelry,
ammunition) are dropped, as the native slot index is negative for them.
================
*/
export function wornItemsFromList( items: readonly CharacterItem[], dress: DressCatalog ): WornItem[] {
	const worn: WornItem[] = [];
	for ( const item of items ) {
		const record = dress.equipment?.[String( item.refObjId )];
		if ( !record ) throw Error( "Missing native equipment visual catalog/reference " + item.refObjId );
		if ( record.slot === null ) continue;
		worn.push( { slot: record.slot, refObjId: item.refObjId, plus: item.plus } );
	}
	return worn;
}

/*
================
createItemCodenameIndex

codename -> RefItemID over the published catalog, the port of
GlobalDataManager_GetItemRecordByCodeName for appearance paths.
================
*/
export function createItemCodenameIndex( dress: DressCatalog ): ReadonlyMap<string, number> {
	const index = new Map<string, number>();
	for ( const [id, record] of Object.entries( dress.equipment ?? {} ) ) {
		if ( record.code ) index.set( record.code, Number( id ) );
	}
	return index;
}

/*
================
assembleEquipmentAppearance

Native slot visuals for one wearer. Validation is strict: a referenced item
missing from the catalog, or published for another socket or body, is an
error, never a silently undressed actor.
================
*/
export function assembleEquipmentAppearance( input: AppearanceInput ): AppearanceAssembly {
	const { resource, dress } = input;
	const prefix = bodyPrefix( resource.codename ), race = prefix.slice( 0, 2 );
	const parts: CharacterAttachment[] = [];
	const auxiliary: { id: number; entry: AuxiliaryEntry; }[] = [];
	const particles: ModelParticle[] = [];
	const worn = [
		...input.equipment.filter( i => i.slot >= 0 && i.slot < WORN_SLOT_COUNT ).map( i => ({
			slot: i.slot,
			refObjId: i.refObjId,
			avatar: false
		}) ),
		...input.avatars.map( i => ({ slot: -1, refObjId: i.refObjId, avatar: true }) )
	];
	for ( const item of worn ) {
		const visual = dress.equipment?.[String( item.refObjId )];
		if ( !visual ) throw Error( "Missing native equipment visual catalog/reference " + item.refObjId );
		if (
			item.avatar ?
				visual.avatarSlot === undefined :
				visual.slot !== null && visual.slot !== item.slot
		) throw Error( "Equipment visual socket mismatch " + item.refObjId );
		if ( (visual.slot !== null || item.avatar) && visual.bodies[prefix] === undefined ) {
			throw Error( "Equipment visual body mismatch " + item.refObjId + ": " + prefix );
		}
	}
	const resolvable = new Set(
		worn.filter( item => {
			const v = dress.equipment![String( item.refObjId )]!;
			return (item.avatar || v.slot !== null) && v.bodies[prefix] !== null;
		} ).map( item => item.refObjId )
	);
	const selectedVisuals = selectEquipmentVisuals(
		worn,
		dress.equipment ?? {},
		resolvable,
		input.hwanHair,
		input.mounted,
		input.fortressIndex >= 0
	);
	const equipmentIds = new Set( selectedVisuals.filter( i => !i.avatar ).map( i => i.refObjId ) ),
		avatarIds = new Set( selectedVisuals.filter( i => i.avatar ).map( i => i.refObjId ) );
	const defaultWear = refreshDefaultWear(
		input.committedWear,
		input.fortressIndex >= 0 ?
			[] :
			selectDefaultWear(
				worn,
				selectedVisuals,
				dress.equipment ?? {},
				// Only player bodies wear default garments; previews pass the gate for both races.
				!!race && (input.ownerless || race === "CH" || !input.player)
			),
		input.freezeWear
	);
	if ( input.fortressIndex >= 0 && (!input.player || race === "CH") ) {
		const entry = dress.fortressWear?.[prefix + "_" + input.fortressIndex];
		if ( !entry ) throw Error( "Missing native fortress clothing " + prefix + "_" + input.fortressIndex );
		parts.push( { model: entry.glb, parts: entry.parts, covers: coverIndices( entry, resource ) } );
	}
	for ( const key of defaultWear ) {
		const entry = dress.defaultWear?.[prefix + "_" + key];
		if ( !entry ) throw Error( "Missing native default clothing " + prefix + "_" + key );
		parts.push( { model: entry.glb, parts: entry.parts, covers: coverIndices( entry, resource ) } );
	}
	for ( const item of input.equipment ) {
		if ( item.slot < 0 || item.slot >= WORN_SLOT_COUNT ) continue;
		if ( !equipmentIds.has( item.refObjId ) ) continue;
		if ( item.slot === WEAPON_SLOT && input.weaponHidden ) continue;
		if ( item.slot >= WEAPON_SLOT && input.attachmentsHidden ) continue;
		const visual = dress.equipment![String( item.refObjId )]!;
		// Inventory-only categories (jewelry/ammunition) have no compound slot.
		if ( visual.slot === null ) continue;
		const entry = visual.bodies[prefix];
		// Explicit null means BOTH native resource sources are empty. A
		// missing conversion remains an error, never a fabricated helmet.
		if ( entry === undefined ) throw Error( `Equipment visual body mismatch ${item.refObjId}: ${prefix}` );
		if ( entry === null ) continue;
		const armament = item.slot === WEAPON_SLOT || item.slot === SHIELD_SLOT;
		parts.push( {
			model: entry.glb,
			parts: entry.parts,
			...(entry.branches ? { branches: { slot: item.slot, entries: entry.branches } } : {}),
			...(armament ? { equipment: { refObjId: item.refObjId, plus: item.plus } } : {}),
			covers: coverIndices( entry, resource )
		} );
		if ( !armament ) continue;
		for ( const effect of dress.specialGlows?.[item.refObjId] ?? [] ) {
			for ( const part of entry.parts ) {
				if ( !entry.branches?.find( b => b.part === part )?.nodes.some( n => n.name === effect.bone ) ) {
					throw Error( "Missing published equipment particle socket" );
				}
				particles.push( {
					...effect,
					source: "equipment",
					bone: equipmentSocket( item.slot, part, effect.bone )
				} );
			}
		}
	}
	for ( const item of input.avatars ) {
		if ( !avatarIds.has( item.refObjId ) ) continue;
		const visual = dress.equipment?.[String( item.refObjId )];
		if ( visual?.avatarSlot === undefined ) throw Error( `Missing native avatar visual ${item.refObjId}` );
		const entry = visual.bodies[prefix];
		if ( entry === undefined ) throw Error( `Missing cosmetic attachment ${prefix}/${item.refObjId}` );
		if ( entry === null ) continue;
		// Native item masks already decide admission. Mesh coverage hides
		// base-body geometry; it must not remove another accepted item.
		parts.push( { model: entry.glb, parts: entry.parts, covers: coverIndices( entry, resource ) } );
		if ( dress.avatarVisualOverrides?.[item.refObjId]?.additionalBsr ) {
			const extra = dress.avatarAuxiliary?.[item.refObjId];
			if ( !extra ) throw Error( "Missing auxiliary avatar " + item.refObjId );
			auxiliary.push( { id: item.refObjId, entry: extra } );
			// Reserve resource/coverage with the body transaction. The animated
			// geometry is published as a private-skeleton socket child.
			parts.push( { model: extra.glb, parts: [], covers: coverIndices( extra, resource ) } );
		}
	}
	if ( input.hwanHair ) {
		const hair = dress.hwan?.[prefix];
		if ( !hair ) throw new Error( `Missing Hwan hair ${prefix}` );
		parts.push( { model: hair.glb, parts: [], covers: coverIndices( hair, resource ) } );
	}
	return { parts, auxiliary, particles, defaultWear, avatarIds: [ ...avatarIds ] };
}
