/*
===========================================================================

sro-macro-potion.ts - the SRO Macro's auto-potion loop (port-only)

Port-only, not native (#3907): the v1.696 macro's auto-potion, read from
the v1.696 client (sro_client.exe 8af7afc2..., research note sro-macro.md).
Each tick the executor (006931b0) walks its rows in order and the first
row whose reader fires uses its item; that row then sits out a 500 ms
re-arm (table 0x17b3688) and rejoins at the back of the order.

This module is pure: the rows' conditions and the ready/waiting order.
The worker supplies character vitals, pet facts, and live buff skill ids
for row 3, and performs the use through the same inventory gates as the
v1.150 auto-potion (with Grass / summoner tails for pet revive / summon).

Character: 00692e00 / 00692f60 / 00692d20 / 006930a0.
Pet: 00691c70 / 00691e20 / 00691a20 / 006921a0 / 00691b60.

===========================================================================
*/
import type { AutoPotionFacts } from "./auto-potion";
import {
	MACRO_POTION_SLOTS,
	MACRO_ROW_BUFF_ITEM,
	MACRO_ROW_CURE,
	MACRO_ROW_HP,
	MACRO_ROW_MP,
	MACRO_ROW_PET_CURE,
	MACRO_ROW_PET_HP,
	MACRO_ROW_PET_HUNGER,
	MACRO_ROW_PET_RESURRECT,
	MACRO_ROW_PET_SUMMON,
	type MacroSettings,
	potionRowOn,
	potionSlot,
	withPotionRowItem
} from "./sro-macro";

export {
	MACRO_ROW_BUFF_ITEM,
	MACRO_ROW_PET_CURE,
	MACRO_ROW_PET_HP,
	MACRO_ROW_PET_HUNGER,
	MACRO_ROW_PET_RESURRECT,
	MACRO_ROW_PET_SUMMON
};

// 00692ba0 / 006918a0 seed every row's re-arm with 0x1f4.
export const MACRO_POTION_REARM_MS = 500;
// 00692e00 / 00691c70 refuse HP while this abnormal bit stands.
const HP_BLOCKING_ABNORMAL = 0x20;
// 00692d20 / 00691a20 fire while any of these abnormal bits stands.
export const MACRO_CURE_ABNORMAL = 0x17fafff;
// 00692e00 / 00692f60 / 00691c70 / 00691e20 scale the percent by this double (0x12befb8).
const PERCENT_SCALE = 0.01;
// Native pet HGP / satiety scale (CosRecord.satiety, item +0x34).
export const PET_SATIETY_MAX = 10000;
// Slots below this are the equipment sockets, never a potion source.
export const BAG_FIRST_SLOT = 13;
// Expendable class bits, same mask autoPotionItemSlot reads.
const CONSUMABLE_MASK = 0x1c;
const CONSUMABLE = 0x0c;
const CONSUMABLE_BAND = 0x60;
// typeFlags low-word families: PackTypeFlags(TID1,TID2,TID3,0) & 0x7fe.
// Native TypeId mid-byte (>>16) is TID3; (>>10)&0x3f is TID2; high byte is TID4.
const TYPE_FAMILY_MASK = 0x7fe;
const POTION_FAMILY = 0xec; // 3/3/1 - potion helper 0x690f30
const PILL_FAMILY = 0x16c; // 3/3/2 - pill helper 0x690ec0
const SUMMONER_FAMILY = 0xcc; // 3/2/1 - summoner helper 0x6908e0
// Grass of Life / dead summoner state (696490 case 6 / f0f190).
export const PET_SUMMON_STATE_DEAD = 4;
// Summon Trigger admits states 1 and 3 (00691b60).
export const PET_SUMMON_STATE_OK = [ 1, 3 ] as const;
/*


================
macroPotionFamily

Native 0x690f30 / 0x690ec0 / 0x6908e0 mid-byte helpers, mapped onto the
port's typeFlags (TID2/TID3 in the low word). Potion and pill also need
the expendable band; summoners sit in TID2=2 and skip that band check.
================
*/
function macroIsExpendable( typeFlags: number ): boolean {
	return (typeFlags & 2) === 0 &&
		(typeFlags & CONSUMABLE_MASK) === CONSUMABLE &&
		(typeFlags & CONSUMABLE_BAND) === CONSUMABLE_BAND;
}

function macroIsPotion( typeFlags: number ): boolean {
	return macroIsExpendable( typeFlags ) && (typeFlags & TYPE_FAMILY_MASK) === POTION_FAMILY;
}

function macroIsPill( typeFlags: number ): boolean {
	return macroIsExpendable( typeFlags ) && (typeFlags & TYPE_FAMILY_MASK) === PILL_FAMILY;
}

function macroIsSummoner( typeFlags: number ): boolean {
	return (typeFlags & TYPE_FAMILY_MASK) === SUMMONER_FAMILY;
}

/*
================
MacroPetFacts

What the pet readers need from the selected companion and its summoner
bag item. Absent when no eligible pet / summoner is in play.
================
*/
export interface MacroPetFacts {
	readonly present: boolean;
	readonly hp: number;
	readonly maxHp: number;
	readonly satiety: number;
	readonly abnormal: number;
	readonly dead: boolean;
	// Summoner bag item's summon.state (f0f190), or 0 when unbound.
	readonly summonState: number;
}

/*
================
macroPotionAdmits

CNIFMacroAutoPotionSlot drop (0xdafff0): kind 0x46 then per-row TypeId
checks. Native high byte (>>24) is the port's TID4; mid-byte helpers
0x690f30 / 0x690ec0 / 0x6908e0 are TID3 (and TID2) via the families above.
================
*/
export function macroPotionAdmits( row: number, typeFlags: number ): boolean {
	if ( !Number.isInteger( row ) || row < 0 || row >= 9 ) return false;
	const tid3 = typeFlags >>> 7 & 15;
	const tid4 = typeFlags >>> 11 & 31;
	// 0x690aa0: 0x6908e0 then high 1/2/3 (0x690880 covers 1/2; high 3 is OR'd).
	if ( row === MACRO_ROW_PET_SUMMON ) {
		return macroIsSummoner( typeFlags ) && (tid4 === 1 || tid4 === 2 || tid4 === 3);
	}
	// 690b70 / 690bf0: potion helper then high 1 or 3 / 2 or 3.
	if ( row === MACRO_ROW_HP ) return macroIsPotion( typeFlags ) && (tid4 === 1 || tid4 === 3);
	if ( row === MACRO_ROW_MP ) return macroIsPotion( typeFlags ) && (tid4 === 2 || tid4 === 3);
	// 690b00: pill helper then high 1/6/8/9.
	if ( row === MACRO_ROW_CURE ) {
		return macroIsPill( typeFlags ) && (tid4 === 1 || tid4 === 6 || tid4 === 8 || tid4 === 9);
	}
	// 690c70 CodeName whitelist; port approximates with scroll TID3s on the expendable band.
	if ( row === MACRO_ROW_BUFF_ITEM ) {
		return macroIsExpendable( typeFlags ) && (tid3 === 3 || tid3 === 13);
	}
	// 690a20 / 6909e0 / 690a60: potion helper then high 4 / 9 / 6.
	if ( row === MACRO_ROW_PET_HP ) return macroIsPotion( typeFlags ) && tid4 === 4;
	if ( row === MACRO_ROW_PET_HUNGER ) return macroIsPotion( typeFlags ) && tid4 === 9;
	if ( row === MACRO_ROW_PET_RESURRECT ) return macroIsPotion( typeFlags ) && tid4 === 6;
	// 690990: pill helper then high 7.
	if ( row === MACRO_ROW_PET_CURE ) return macroIsPill( typeFlags ) && tid4 === 7;
	return false;
}

/*
================
scrubMacroPotion
================
*/
export function scrubMacroPotion(
	settings: MacroSettings,
	inventory: readonly { readonly slot: number; readonly refObjId: number; readonly typeFlags: number; }[]
): MacroSettings {
	let potion = settings.potion;
	for ( let row = 0; row < MACRO_POTION_SLOTS; row++ ) {
		const ref = potionSlot( potion, row ).item;
		if ( !ref ) continue;
		const item = inventory.find( entry => entry.slot >= BAG_FIRST_SLOT && entry.refObjId === ref );
		if ( item && !macroPotionAdmits( row, item.typeFlags ) ) {
			potion = withPotionRowItem( potion, row, 0 );
		}
	}
	return potion === settings.potion ? settings : { ...settings, potion };
}

export type MacroPotionRow =
	| typeof MACRO_ROW_HP
	| typeof MACRO_ROW_MP
	| typeof MACRO_ROW_CURE
	| typeof MACRO_ROW_BUFF_ITEM
	| typeof MACRO_ROW_PET_HP
	| typeof MACRO_ROW_PET_HUNGER
	| typeof MACRO_ROW_PET_CURE
	| typeof MACRO_ROW_PET_RESURRECT
	| typeof MACRO_ROW_PET_SUMMON;

// Character 004c2ce0 then pet 004c2b90 (00690790 builds both).
export const MACRO_POTION_ORDER = [ 0, 1, 2, 3, 4, 5, 6, 7, 8 ] as const;

/*
================
MacroPotionQueue
================
*/
export interface MacroPotionQueue {
	readonly ready: readonly MacroPotionRow[];
	readonly waiting: readonly { readonly row: MacroPotionRow; readonly readyAt: number; }[];
}

/*
================
emptyMacroPotionQueue
================
*/
export function emptyMacroPotionQueue(): MacroPotionQueue {
	return { ready: MACRO_POTION_ORDER, waiting: [] };
}

/*
================
emptyMacroPetFacts
================
*/
export function emptyMacroPetFacts(): MacroPetFacts {
	return { present: false, hp: 0, maxHp: 0, satiety: 0, abnormal: 0, dead: false, summonState: 0 };
}

/*
================
macroPetLiving

The one companion the pet readers share (native 0xc92b80): the selected
COS when it is a living band 1..3 pet, else the first living owned one.
================
*/
export function macroPetLiving(
	records: readonly {
		readonly gid: number;
		readonly band: number;
		readonly hp: number;
		readonly dead: boolean;
	}[],
	selectedGid: number
): number {
	const living = ( row: { readonly band: number; readonly hp: number; readonly dead: boolean; } ) =>
		!row.dead && row.hp > 0 && row.band >= 1 && row.band <= 3;
	const selected = records.find( row => row.gid === selectedGid );
	if ( selected && living( selected ) ) return selected.gid;
	return records.find( living )?.gid ?? 0;
}

/*
================
macroPetFacts

Facts the pet readers need. When a companion is out, rows 4..6 read it;
when it is not, rows 7/8 read the bound summoner's state (f0f190).
================
*/
export function macroPetFacts( input: {
	readonly records: readonly {
		readonly gid: number;
		readonly band: number;
		readonly hp: number;
		readonly dead: boolean;
		readonly satiety?: number;
		readonly inventorySlot?: number;
		// Catalog / training max HP (vitals 0x33A6 never carry max for a COS).
		readonly maxHp?: number;
	}[];
	readonly selectedGid: number;
	readonly vitals: readonly {
		readonly gid: number;
		readonly hp?: number;
		readonly maxHp?: number;
		readonly abnormal?: number;
		readonly satiety?: number;
	}[];
	readonly bag: readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly typeFlags: number;
		readonly summon?: { readonly state: number; };
	}[];
	readonly summonRef: number;
} ): MacroPetFacts {
	const summoner = input.summonRef === 0 ?
		undefined :
		input.bag.find( entry =>
			entry.slot >= BAG_FIRST_SLOT && entry.refObjId === input.summonRef &&
			(entry.typeFlags & TYPE_FAMILY_MASK) === SUMMONER_FAMILY
		) ??
			input.bag.find( entry => entry.slot >= BAG_FIRST_SLOT && entry.refObjId === input.summonRef );
	const summonState = summoner?.summon?.state ?? 0;
	const gid = macroPetLiving( input.records, input.selectedGid );
	if ( !gid ) {
		return { ...emptyMacroPetFacts(), summonState };
	}
	const record = input.records.find( row => row.gid === gid )!;
	const vital = input.vitals.find( row => row.gid === gid );
	const hp = vital?.hp ?? record.hp;
	const linked = record.inventorySlot !== undefined ?
		input.bag.find( entry => entry.slot === record.inventorySlot ) :
		undefined;
	return {
		present: true,
		hp,
		// Prefer a live vital, then the caller's catalog/training stamp.
		maxHp: vital?.maxHp || record.maxHp || 0,
		satiety: vital?.satiety ?? record.satiety ?? 0,
		abnormal: vital?.abnormal ?? 0,
		dead: record.dead || hp <= 0,
		summonState: linked?.summon?.state ?? summonState
	};
}

/*
================
macroPotionWants

buffSkillId is the bound item's associatedSkillId for the buff-item row;
pet is the selected companion's facts for rows 4..8. Other rows ignore them.
================
*/
export function macroPotionWants(
	potion: readonly number[],
	row: MacroPotionRow,
	facts: AutoPotionFacts,
	buffSkillId = 0,
	pet: MacroPetFacts = emptyMacroPetFacts()
): boolean {
	if ( !potionRowOn( potion, row ) ) return false;
	const slot = potionSlot( potion, row );
	if ( slot.item === 0 ) return false;

	if ( row === MACRO_ROW_BUFF_ITEM ) {
		if ( !facts.alive ) return false;
		if ( !buffSkillId ) return false;
		return !(facts.activeBuffSkills ?? []).includes( buffSkillId );
	}
	if ( row === MACRO_ROW_CURE ) {
		if ( !facts.alive ) return false;
		return (facts.abnormal & MACRO_CURE_ABNORMAL) !== 0;
	}
	if ( row === MACRO_ROW_HP || row === MACRO_ROW_MP ) {
		if ( !facts.alive ) return false;
		if ( row === MACRO_ROW_HP && (facts.abnormal & HP_BLOCKING_ABNORMAL) !== 0 ) return false;
		const current = row === MACRO_ROW_HP ? facts.hp : facts.mp;
		const max = row === MACRO_ROW_HP ? facts.maxHp : facts.maxMp;
		return max * (slot.a * PERCENT_SCALE) >= current;
	}

	// Pet rows 4..6 need a living companion in the world (00691c70 / 00691e20 / 00691a20).
	if ( row === MACRO_ROW_PET_HP || row === MACRO_ROW_PET_HUNGER || row === MACRO_ROW_PET_CURE ) {
		if ( !pet.present || pet.dead || pet.hp <= 0 || !(pet.maxHp > 0) ) return false;
		if ( row === MACRO_ROW_PET_CURE ) return (pet.abnormal & MACRO_CURE_ABNORMAL) !== 0;
		if ( row === MACRO_ROW_PET_HP ) {
			if ( (pet.abnormal & HP_BLOCKING_ABNORMAL) !== 0 ) return false;
			return pet.maxHp * (slot.a * PERCENT_SCALE) >= pet.hp;
		}
		// 00691e20: satiety[+0x34] <= floor(max * pct * 0.01) via 00fc5590.
		return pet.satiety <= PET_SATIETY_MAX * (slot.a * PERCENT_SCALE);
	}

	// 006921a0: Grass + dead summoner (state 4), pet not already out.
	if ( row === MACRO_ROW_PET_RESURRECT ) {
		if ( pet.present ) return false;
		if ( pet.summonState !== PET_SUMMON_STATE_DEAD ) return false;
		return potionSlot( potion, MACRO_ROW_PET_SUMMON ).item !== 0;
	}
	// 00691b60: summoner item alone while state is 1 or 3, pet not out.
	if ( row === MACRO_ROW_PET_SUMMON ) {
		if ( pet.present ) return false;
		return (PET_SUMMON_STATE_OK as readonly number[]).includes( pet.summonState );
	}
	return false;
}

/*
================
macroPotionBuffSkillId
================
*/
export function macroPotionBuffSkillId(
	potion: readonly number[],
	inventory: readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly nativeFields?: Readonly<Record<string, number>>;
	}[]
): number {
	const ref = potionSlot( potion, MACRO_ROW_BUFF_ITEM ).item;
	if ( !ref ) return 0;
	for ( const entry of inventory ) {
		if ( entry.slot < BAG_FIRST_SLOT || entry.refObjId !== ref ) continue;
		const id = entry.nativeFields?.associatedSkillId ?? 0;
		if ( Number.isInteger( id ) && id > 0 ) return id;
	}
	return 0;
}

/*
================
macroPotionBagSlot
================
*/
export function macroPotionBagSlot(
	potion: readonly number[],
	row: MacroPotionRow,
	inventory: readonly { readonly slot: number; readonly refObjId: number; }[]
): number | undefined {
	const item = potionSlot( potion, row ).item;
	if ( item === 0 ) return undefined;
	let best: number | undefined;
	for ( const entry of inventory ) {
		if ( entry.slot >= BAG_FIRST_SLOT && entry.refObjId === item && (best === undefined || entry.slot < best) ) {
			best = entry.slot;
		}
	}
	return best;
}

/*
================
rearmMacroPotion
================
*/
export function rearmMacroPotion( queue: MacroPotionQueue, now: number ): MacroPotionQueue {
	const back = queue.waiting.findIndex( entry => now >= entry.readyAt );
	if ( back < 0 ) return queue;
	return {
		ready: [ ...queue.ready, queue.waiting[back]!.row ],
		waiting: queue.waiting.filter( ( _, i ) => i !== back )
	};
}

/*
================
macroPotionFired
================
*/
export function macroPotionFired( queue: MacroPotionQueue, row: MacroPotionRow, now: number ): MacroPotionQueue {
	return {
		ready: queue.ready.filter( r => r !== row ),
		waiting: [ ...queue.waiting, { row, readyAt: now + MACRO_POTION_REARM_MS } ]
	};
}
