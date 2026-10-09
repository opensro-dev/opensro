/*
===========================================================================

trade-appearance.ts - how a thief or hunter monster is dressed

A trade bandit (type word 0x10C6 thief, 0x18C6 hunter) has no model of its
own. CICMonster_DeserializeSpawnPacket (861B00) reads the spawn's trade
variant byte and CICMonster_InitializeTradeEquipmentAndSkill (861720)
draws a player body from a skin pool, dresses it through
CICharactor_EquipReferenceAppearance (8703F0) and adds a trade suit. Every
rule here is read from those functions in the v1.150 client.

The bandit stays a monster: 8703F0 hands the weapon's animation set only to
CICScriptObj and CICCos, so its gear carries no type flags here and its
motions and sounds stay its own.

===========================================================================
*/

/*
================
TradeAppearanceInput

What 861720 reads: the spawn's selector byte, the record's country byte
(+0x9d, the Country column), its level (+0x1a8), whether the type word is a
thief's, and the spawn skill's weapon route (skill +0xd8).
================
*/
export interface TradeAppearanceInput {
	readonly selector: number;
	readonly race: number;
	readonly level: number;
	readonly thief: boolean;
	readonly weaponType: number;
}

/*
================
TradeSkinPools

GlobalDataManager_GetChinaSkinPool (+0x220) / GetEuropeSkinPool (+0x248):
[refObjId, sex] in usableresobjiddata.txt order; sex 1 male, 0 female.
================
*/
export interface TradeSkinPools {
	readonly china: readonly (readonly [number, number])[];
	readonly europe: readonly (readonly [number, number])[];
}

/*
================
TradeItem

A codename's worn item: its RefItemID and native socket.
================
*/
export interface TradeItem {
	readonly refObjId: number;
	readonly slot: number;
}

/*
================
CrowdArmorParts

8703F0's armour part codes (g_CrowdArmorPartCodes, 0xCF0190). The array is
static for the whole process: a call that draws an odd rand() overwrites
the first code with "HA" and nothing writes "CA" back, so once one avatar
has worn a head piece every later one does. The owner keeps one for the
session.
================
*/
export interface CrowdArmorParts {
	first: "CA" | "HA";
}

// The default spawn-skill weapon route (861720 starts ebx at 2: a sword).
export const DEFAULT_WEAPON_TYPE = 2;
// The race codes (0xBC8EB4 "CH", 0xBC8EBC "EU"): race 0 and 3 are China.
const RACE_CHINA_A = 0, RACE_CHINA_B = 3, RACE_EUROPE = 1;

/*
================
weaponKind

8703F0's weapon switch (jump table 870C7C) on weaponType 2..15.
================
*/
function weaponKind( route: number ): string {
	return [
		"SWORD",
		"BLADE",
		"SPEAR",
		"TBLADE",
		"BOW",
		"SWORD",
		"TSWORD",
		"AXE",
		"DARKSTAFF",
		"TSTAFF",
		"CROSSBOW",
		"DAGGER",
		"HARP",
		"STAFF"
	][route]!;
}

/*
================
carriesShield

lookup_table_870CBC: 0 adds a shield (the one-hand types 2, 3, 7, 15).
================
*/
function carriesShield( route: number ): boolean {
	return [ 0, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 0 ][route] === 0;
}

/*
================
armorGrade

lookup_table_861AE4 through the switch at 861909: the armour a hunter's
weapon route wears (1 clothes, 2 light, 3 heavy).
================
*/
function armorGrade( route: number ): number {
	return [ 1, 3, 1, 3, 3, 3, 3, 3, 1, 1, 2, 2, 1, 1 ][route]!;
}

/*
================
armorKind
================
*/
function armorKind( grade: number ): string {
	return grade === 1 ? "CLOTHES" : grade === 2 ? "LIGHT" : "HEAVY";
}

/*
================
degreeFromLevel

Item_DegreeFromLevel (86EA30): 1, then one more for each threshold reached,
14 at most.
================
*/
export function degreeFromLevel( level: number ): number {
	// The lowest level of degrees 2..14.
	const thresholds = [ 8, 16, 24, 32, 42, 52, 64, 76, 90, 104, 120, 141, 164 ];
	let degree = 1;
	while ( degree < 14 && level >= thresholds[degree - 1]! ) degree++;
	return degree;
}

/*
================
pad

The %02d of every codename format.
================
*/
function pad( value: number ): string {
	return String( value ).padStart( 2, "0" );
}

/*
================
firstResolving

8703F0 tries a codename from the degree down to 1 and keeps the first that
attaches.
================
*/
function firstResolving(
	degree: number,
	codename: ( degree: number ) => string,
	item: ( codename: string ) => TradeItem | undefined
): TradeItem | undefined {
	for ( let d = degree; d > 0; d-- ) {
		const found = item( codename( d ) );
		if ( found ) return found;
	}
	return undefined;
}

/*
================
tradeAppearance

The body and worn items of one bandit, or null when its race has no pool
(861720 asserts there). rand is CRT rand(): 8703F0 draws first (the head
piece) when it dresses armour, then 861720 the China suit tier.
================
*/
export function tradeAppearance(
	input: TradeAppearanceInput,
	pools: TradeSkinPools,
	item: ( codename: string ) => TradeItem | undefined,
	rand: () => number,
	parts: CrowdArmorParts
): { readonly refObjId: number; readonly equipment: readonly TradeItem[]; } | null {
	const china = input.race === RACE_CHINA_A || input.race === RACE_CHINA_B;
	if ( !china && input.race !== RACE_EUROPE ) return null;
	const pool = china ? pools.china : pools.europe, race = china ? "CH" : "EU";
	if ( pool.length === 0 ) return null;
	const [body, sex] = pool[(input.selector & 0xff) % pool.length]!;
	const level = input.level, degree = degreeFromLevel( level ), equipment: TradeItem[] = [];
	const route = input.weaponType - 2, inRange = route >= 0 && route <= 0xd;
	// 861720: a thief wears no armour; a hunter the grade of its weapon route.
	const grade = !input.thief && inRange ? armorGrade( route ) : -1;

	// 8703F0, weapon then armour then shield.
	const weapon = inRange ?
		firstResolving( degree, d => `ITEM_${race}_${weaponKind( route )}_${pad( d )}_A`, item ) :
		undefined;
	if ( weapon ) equipment.push( weapon );
	if ( grade >= 0 ) {
		if ( (rand() & 1) === 1 ) parts.first = "HA";
		const letter = sex ? "M" : "W", kind = armorKind( grade );
		for ( const part of [ parts.first, "SA", "BA", "LA", "AA", "FA" ] ) {
			const piece = firstResolving( degree, d => `ITEM_${race}_${letter}_${kind}_${pad( d )}_${part}_A`, item );
			if ( piece ) equipment.push( piece );
		}
	}
	if ( inRange && carriesShield( route ) ) {
		const shield = firstResolving( degree, d => `ITEM_${race}_SHIELD_${pad( d )}_A`, item );
		if ( shield ) equipment.push( shield );
	}

	// 861720: the trade suit, F/M by sex. China draws tier 2 or 3; Europe
	// takes it from the level (1 under 40, 3 under 60, 5 under 80, else 7).
	const tier = china ? (rand() & 1) + 2 : level < 40 ? 1 : level < 60 ? 3 : level < 80 ? 5 : 7;
	const suit = item( `ITEM_${race}_${sex ? "M" : "F"}_TRADE_${input.thief ? "THIEF" : "HUNTER"}_${pad( tier )}` );
	if ( suit ) equipment.push( suit );
	return { refObjId: body, equipment };
}
