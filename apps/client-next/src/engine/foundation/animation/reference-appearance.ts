/*
===========================================================================

reference-appearance.ts - random reference appearances (transforms, crowd)

A reference transform dresses its target in a random native player look
(CIDecoSkill_SetupMotionMetadata -> CICharactor_EquipReferenceAppearance
0x8703F0). The native routine builds item codenames, walks the degree down
until GlobalDataManager_GetItemRecordByCodeName finds the item, and equips
it through the ordinary slot visuals. This module chooses the look and
names its items; the character owner dresses them like any worn item.

===========================================================================
*/
import type { WornItem } from "@/engine/foundation/animation/equipment-visuals";

export interface ReferenceAppearance {
	readonly type: number;
	readonly cap: number;
}

export interface AppearanceChoice {
	readonly model: number;
	readonly race: 0 | 1;
	readonly armor: number;
	readonly weapon: number;
	readonly level: number;
	readonly head: "CA" | "HA";
}

const MAX_DEGREE = 14;
const DEFAULT_LEVEL_RANGE = 140;
const WEAPON_SLOT = 6;
const SHIELD_SLOT = 7;

/*
================
weaponKind

Weapon class -> codename kind (the 0x8703F0 switch, case = class - 2).
================
*/
function weaponKind( weaponClass: number ): string | undefined {
	switch ( weaponClass ) {
		case 2:
		case 7:
			return "SWORD";
		case 3:
			return "BLADE";
		case 4:
			return "SPEAR";
		case 5:
			return "TBLADE";
		case 6:
			return "BOW";
		case 8:
			return "TSWORD";
		case 9:
			return "AXE";
		case 10:
			return "DARKSTAFF";
		case 11:
			return "TSTAFF";
		case 12:
			return "CROSSBOW";
		case 13:
			return "DAGGER";
		case 14:
			return "HARP";
		case 15:
			return "STAFF";
		default:
			return undefined;
	}
}

/*
================
weaponClasses

The weapon classes a race's reference look draws from, in draw order.
================
*/
function weaponClasses( race: 0 | 1 ): readonly number[] {
	return race ? [ 7, 8, 9, 15, 11, 12, 13, 14, 10 ] : [ 3, 2, 5, 4, 6 ];
}

/*
================
carriesShield

One-hand classes that also carry a shield (lookup_table_870cbc).
================
*/
function carriesShield( weaponClass: number ): boolean {
	return weaponClass === 2 || weaponClass === 3 || weaponClass === 7 || weaponClass === 15;
}

/*
================
armorType
================
*/
function armorType( armor: number ): string | undefined {
	switch ( armor ) {
		case 1:
			return "CLOTHES";
		case 2:
			return "LIGHT";
		case 3:
			return "HEAVY";
		default:
			return undefined;
	}
}

/*
================
partSlot

The worn socket of an armor part.
================
*/
function partSlot( part: string ): number {
	switch ( part ) {
		case "CA":
		case "HA":
			return 0;
		case "BA":
			return 1;
		case "SA":
			return 2;
		case "AA":
			return 3;
		case "LA":
			return 4;
		case "FA":
			return 5;
		default:
			throw Error( "Unknown armor part " + part );
	}
}

/*
================
chooseReferenceAppearance

8DD6F0: five CRT draws, then 8703F0's separate head-piece draw.
================
*/
export function chooseReferenceAppearance(
	cap: number,
	stores: readonly (readonly number[])[],
	rand: () => number
): AppearanceChoice {
	const race = rand() % 2 === 0 ? 1 : 0, list = stores[race];
	if ( !list?.length ) throw Error( "Native appearance store is empty" );
	const model = list[rand() % list.length]!,
		armor = [ 3, 2, 1 ][rand() % 3]!,
		weapons = weaponClasses( race ),
		weapon = weapons[rand() % weapons.length]!,
		level = rand() % (cap & 255 || DEFAULT_LEVEL_RANGE),
		head = rand() % 2 ? "HA" : "CA";
	return { model, race, armor, weapon, level, head };
}

/*
================
degreeFromLevel

Item_DegreeFromLevel 0x86EA30: level thresholds after degree 1, capped at 14.
================
*/
function degreeFromLevel( level: number ): number {
	let degree = 1;
	for ( const threshold of [ 8, 16, 24, 32, 42, 52, 64, 76, 90, 104, 120, 141, 164 ] ) {
		if ( level < threshold || degree >= MAX_DEGREE ) break;
		degree++;
	}
	return degree;
}

/*
================
referenceAppearanceItems

The worn items of a reference look. Each piece tries its codename from the
level's degree downward and takes the first item the catalog holds, as the
native EquipItemByCodeName loop does; a piece with no item at any degree is
left out, as natively.
================
*/
export function referenceAppearanceItems(
	choice: AppearanceChoice,
	male: boolean,
	itemIds: ReadonlyMap<string, number>
): WornItem[] {
	const race = choice.race ? "EU" : "CH", sex = male ? "M" : "W", top = degreeFromLevel( choice.level );
	const items: WornItem[] = [];
	function equip( slot: number, codename: ( degree: string ) => string ) {
		for ( let degree = top; degree > 0; degree-- ) {
			const refObjId = itemIds.get( codename( String( degree ).padStart( 2, "0" ) ) );
			if ( refObjId === undefined ) continue;
			items.push( { slot, refObjId, plus: 0 } );
			return;
		}
	}
	const kind = weaponKind( choice.weapon );
	if ( kind ) equip( WEAPON_SLOT, degree => `ITEM_${race}_${kind}_${degree}_A` );
	const armor = armorType( choice.armor );
	if ( armor ) {
		// The random head piece, then atexit_ReferenceAppearanceArmorParts.
		for ( const part of [ choice.head, "SA", "BA", "LA", "AA", "FA" ] ) {
			equip( partSlot( part ), degree => `ITEM_${race}_${sex}_${armor}_${degree}_${part}_A` );
		}
	}
	if ( carriesShield( choice.weapon ) ) equip( SHIELD_SLOT, degree => `ITEM_${race}_SHIELD_${degree}_A` );
	return items;
}

/*
================
createReferenceAppearances

Every msch instance, whatever its word, restores the original model when it
ends or stops (CIDecoSkill_ExtinguishAndCancel 8DD131). A skin a mask applied
(0x323A, spawn row) is therefore shown only until the next such end on its
gid: ends counts them, and a skin revision is pinned to the count current
when it was first seen.
================
*/
export function createReferenceAppearances( rand: () => number ) {
	let references: ReadonlyMap<number, ReferenceAppearance> | undefined, stores: readonly (readonly number[])[] = [];
	const entries = new Map<string, { gid: number; hasAppearance: boolean; stopped: boolean; }>(),
		current = new Map<number, AppearanceChoice>();
	const ends = new Map<number, number>(), skins = new Map<number, { revision: number; ends: number; }>();
	const restore = ( gid: number ) => {
		current.delete( gid );
		ends.set( gid, (ends.get( gid ) ?? 0) + 1 );
	};
	return {
		setReferences( refs: ReadonlyMap<number, ReferenceAppearance>, pools: readonly (readonly number[])[] ) {
			references = refs;
			stores = pools;
		},
		step( effects: readonly { key: string; gid: number; skill: number; stopped: boolean; }[] ) {
			const present = new Set( effects.map( e => e.key ) );
			for ( const [key, row] of entries ) {
				if ( !present.has( key ) ) {
					if ( row.hasAppearance && !row.stopped ) restore( row.gid );
					entries.delete( key );
				}
			}
			if ( !references ) return;
			for ( const effect of effects ) {
				let entry = entries.get( effect.key );
				if ( !entry ) {
					const ref = references.get( effect.skill );
					entry = { gid: effect.gid, hasAppearance: !!ref, stopped: false };
					entries.set( effect.key, entry );
					if ( ref?.type === 3 ) {
						current.set( effect.gid, chooseReferenceAppearance( ref.cap, stores, rand ) );
					}
				}
				// Stop restores the original object even if another transform instance remains.
				if ( effect.stopped && !entry.stopped ) {
					if ( entry.hasAppearance ) restore( effect.gid );
					entry.stopped = true;
				}
			}
		},
		get( gid: number ) {
			return current.get( gid );
		},
		// The RefObj a gid is drawn as while its transform skin holds.
		skin( gid: number, skin: { readonly refObjId: number; readonly revision: number; } | undefined ) {
			if ( !skin ) {
				skins.delete( gid );
				return undefined;
			}
			let seen = skins.get( gid );
			if ( !seen || seen.revision !== skin.revision ) {
				seen = { revision: skin.revision, ends: ends.get( gid ) ?? 0 };
				skins.set( gid, seen );
			}
			return seen.ends === (ends.get( gid ) ?? 0) ? skin.refObjId : undefined;
		},
		reset() {
			entries.clear();
			current.clear();
			ends.clear();
			skins.clear();
		}
	};
}
