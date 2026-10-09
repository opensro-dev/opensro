/*
===========================================================================

trade-appearance.test.mjs - thieves and hunters dressed as 861720 does

The pure rules (Item_DegreeFromLevel 86EA30, CICharactor_EquipReferenceAppearance
8703F0, CICMonster_InitializeTradeEquipmentAndSkill 861720) over a synthetic
item set, then the appearance lookup turning a bandit spawn into a player
body that wears the result.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { degreeFromLevel, tradeAppearance } = await import(
	"../../src/engine/foundation/animation/trade-appearance.ts"
);
const { createAppearanceLookup } = await import( "../../src/engine/runtime/characters/appearance-lookup.ts" );

/** @typedef {import("../../src/engine/foundation/animation/trade-appearance.ts").TradeSkinPools} TradeSkinPools */
/** @typedef {import("../../src/engine/foundation/animation/trade-appearance.ts").CrowdArmorParts} CrowdArmorParts */
/** @typedef {import("../../src/engine/foundation/animation/trade-appearance.ts").TradeItem} TradeItem */

/** @type {TradeSkinPools} */
const POOLS = { china: [ [ 1907, 1 ], [ 1920, 0 ] ], europe: [ [ 14875, 1 ] ] };
// Socket per armour part: head 0, chest 1, shoulders 2, hands 3, legs 4, feet 5.
/** @type {Record<string, number>} */
const PART_SLOTS = { HA: 0, CA: 0, SA: 2, BA: 1, LA: 4, AA: 3, FA: 5 };

/*
================
parts

A fresh session's 8703F0 part array.
================
*/
/** @returns {CrowdArmorParts} */
function parts() {
	return { first: "CA" };
}

/*
================
draws

A rand() that returns values in order.
================
*/
/** @param {number[]} values @returns {() => number} */
function draws( values ) {
	return () => defined( values.shift() );
}

/*
================
itemSet

Resolves the codenames named, each to a stable id and its socket: weapon 6,
shield 7, trade suit 8, armour by part.
================
*/
/** @param {string[]} codenames @returns {(code: string) => TradeItem | undefined} */
function itemSet( codenames ) {
	const ids = new Map( codenames.map( ( code, index ) => [ code, 100 + index ] ) );
	return code => {
		const id = ids.get( code );
		if ( id === undefined ) return undefined;
		const part = /_([A-Z]{2})_A$/.exec( code )?.[1];
		const slot = code.includes( "_TRADE_" ) ?
			8 :
			code.includes( "_SHIELD_" ) ?
			7 :
			part && /_(CLOTHES|LIGHT|HEAVY)_/.test( code ) ?
			defined( PART_SLOTS[part] ) :
			6;
		return { refObjId: id, slot };
	};
}

/*
================
codes

The codenames an outfit resolved to, in order.
================
*/
/** @param {{ equipment: readonly TradeItem[]; } | null} outfit @param {string[]} names */
function codes( outfit, names ) {
	const byId = new Map( names.map( ( code, index ) => [ 100 + index, code ] ) );
	return defined( outfit ).equipment.map( item => byId.get( item.refObjId ) );
}

test("Item_DegreeFromLevel steps at each native threshold and stops at 14", () => {
	assert.deepEqual( [ 1, 7, 8, 15, 16, 41, 42, 163, 164, 200 ].map( degreeFromLevel ), [
		1,
		1,
		2,
		2,
		3,
		5,
		6,
		13,
		14,
		14
	] );
});

test("a Chinese thief: pool body by selector, sword with fallback, shield, no armour, tier 2 or 3 suit", () => {
	const names = [ "ITEM_CH_SWORD_02_A", "ITEM_CH_SHIELD_03_A", "ITEM_CH_F_TRADE_THIEF_03" ];
	const rand = [ 1 ];
	const outfit = tradeAppearance(
		{ selector: 3, race: 3, level: 24, thief: true, weaponType: 2 },
		POOLS,
		itemSet( names ),
		draws( rand ),
		parts()
	);
	// 3 % 2 = 1: the second China body, a woman ("F" suit).
	assert.equal( defined( outfit ).refObjId, 1920 );
	// Degree 4 at level 24 falls back to the sword and shield that exist.
	assert.deepEqual( codes( outfit, names ), [
		"ITEM_CH_SWORD_02_A",
		"ITEM_CH_SHIELD_03_A",
		"ITEM_CH_F_TRADE_THIEF_03"
	] );
	assert.deepEqual( rand, [], "only the suit tier draws for a thief" );
});

test("a hunter wears its route's clothes, and one odd draw keeps HA for every later avatar", () => {
	const names = [
		"ITEM_CH_SWORD_01_A",
		"ITEM_CH_M_CLOTHES_01_CA_A",
		"ITEM_CH_M_CLOTHES_01_HA_A",
		"ITEM_CH_M_CLOTHES_01_BA_A",
		"ITEM_CH_SHIELD_01_A",
		"ITEM_CH_M_TRADE_HUNTER_02"
	];
	const crowd = parts(), items = itemSet( names );
	const hunter = { selector: 0, race: 0, level: 1, thief: false, weaponType: 2 };
	assert.deepEqual( codes( tradeAppearance( hunter, POOLS, items, draws( [ 0, 0 ] ), crowd ), names ), [
		"ITEM_CH_SWORD_01_A",
		"ITEM_CH_M_CLOTHES_01_CA_A",
		"ITEM_CH_M_CLOTHES_01_BA_A",
		"ITEM_CH_SHIELD_01_A",
		"ITEM_CH_M_TRADE_HUNTER_02"
	] );
	tradeAppearance( hunter, POOLS, items, () => 1, crowd );
	const later = codes( tradeAppearance( hunter, POOLS, items, () => 0, crowd ), names );
	assert.equal( crowd.first, "HA" );
	assert.ok( later.includes( "ITEM_CH_M_CLOTHES_01_HA_A" ) );
	assert.ok( !later.includes( "ITEM_CH_M_CLOTHES_01_CA_A" ) );
});

test("a European bandit takes its suit tier from the level; an unknown race has no dress", () => {
	const names = [ "ITEM_EU_M_TRADE_THIEF_05" ];
	const outfit = tradeAppearance(
		{ selector: 9, race: 1, level: 65, thief: true, weaponType: 2 },
		POOLS,
		itemSet( names ),
		() => assert.fail( "Europe draws no tier" ),
		parts()
	);
	assert.equal( defined( outfit ).refObjId, 14875 );
	assert.deepEqual( codes( outfit, names ), [ "ITEM_EU_M_TRADE_THIEF_05" ] );
	const unknown = { selector: 0, race: 2, level: 1, thief: true, weaponType: 2 };
	assert.equal( tradeAppearance( unknown, POOLS, () => undefined, () => 0, parts() ), null );
});

test("the appearance lookup draws a bandit spawn as its dressed body, once per spawn", () => {
	/** @type {any} */
	const body = { codename: "CHAR_CH_MAN_ADVENTURER", glb: "/assets/char/a.glb", clips: [] };
	const lookup = createAppearanceLookup( {
		referenceAppearances: { skin: () => undefined, get: () => undefined },
		published: {
			catalog: new Map( [ [ 1907, body ] ] ),
			deathModels: new Map(),
			tradeSkinPools: { china: [ [ 1907, 1 ] ], europe: [] },
			itemIds: new Map( [ [ "ITEM_CH_SWORD_01_A", 77 ], [ "ITEM_CH_M_TRADE_THIEF_02", 2163 ] ] ),
			dress: { equipment: { 77: { slot: 6 }, 2163: { slot: 8 } } }
		},
		structureVisuals: { appearance: () => undefined },
		deathShown: () => false,
		// Even draws: the China suit tier is 2.
		random: () => 0
	} );
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const bandit = /** @type {any} */ ({
		gid: 9,
		refObjId: 2222,
		kind: "monster",
		tidWord: 0x10c6,
		tradeVariant: 4,
		countryByte9c: 3,
		level: 1
	});
	assert.equal( lookup.appearanceRef( bandit ), 1907 );
	assert.deepEqual(
		lookup.wornEquipment( bandit, null ).map( item => [ item.slot, item.refObjId, item.typeFlags ] ),
		[
			[ 6, 77, 0 ],
			[ 8, 2163, 0 ]
		]
	);
	assert.equal(
		lookup.activeSkin( bandit ),
		lookup.activeSkin( { ...bandit, x: 5 } ),
		"a moved bandit keeps its dress"
	);
	assert.equal( lookup.resourceFor( bandit )?.codename, "CHAR_CH_MAN_ADVENTURER" );
	// An ordinary monster keeps its own model.
	assert.equal( lookup.appearanceRef( { ...bandit, gid: 10, tidWord: 0xc6, tradeVariant: undefined } ), 2222 );
});
