/*
===========================================================================

gacha-pool.test.mjs - tests for gacha-pool.ts and the v1.696 Magic POP lane

The published v1.696 pool (gachaPool): its validation, the select window's
category tree and combo boxes (CIFGhachaSelectWnd), the grid's cap, type and
degree filter and its page arithmetic (CIFGhaCha 0xA71220 / 0xA715FC), the
hud that holds the choice and the page, and the worker session, which must
roll only an entry the pool shows and still send the v1.150 frame.

Loads the TypeScript sources through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const pool = await import( "../../src/engine/foundation/gameplay/gacha-pool.ts" );
const hudModule = await import( "../../src/engine/runtime/ui/hud/gacha-hud.ts" );
const { createGacha } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/gacha/gacha.ts"
);

// v1.150 type words (TID1 << 2 | TID2 << 5 | TID3 << 7 | TID4 << 11).
const TICKET_FLAGS = 0x0f6c; // 3.3.14.1, ITEM_MALL_GACHA_CARD
const CH_SWORD_FLAGS = 3 << 2 | 1 << 5 | 6 << 7 | 2 << 11; // 3.1.6.2
const CH_BLADE_FLAGS = 3 << 2 | 1 << 5 | 6 << 7 | 3 << 11; // 3.1.6.3
const STONE_FLAGS = 3 << 2 | 3 << 5 | 11 << 7 | 1 << 11; // 3.3.11.1
/** @type {readonly [number, number, number, number]} */
const SWORD_TID = [ 3, 1, 6, 2 ];
const CAP = 90;

/*
================
sample

A published pool in the server's shape: two Chinese weapon rows, a
European one and a stone, with a cap-gated degree-10 sword.
================
*/
/** @returns {any} */
function sample() {
	return {
		npc: 9251,
		visibleSet: 1,
		levelCap: CAP,
		entries: [
			{
				entry: 1,
				refObjId: 101,
				quantity: 1,
				minLevelCap: 1,
				plus: 3,
				options: [ { name: "str", value: 3, paramId: 7 } ]
			},
			{ entry: 2, refObjId: 102, quantity: 1, minLevelCap: 91, plus: 0, options: [] },
			{ entry: 3, refObjId: 103, quantity: 1, minLevelCap: 1, plus: 0, options: [ { name: "int", value: 2 } ] },
			{ entry: 4, refObjId: 104, quantity: 5, minLevelCap: 1, plus: 0, options: [] }
		],
		tree: [
			{
				tid: [ 3, 1, 6, 3 ],
				group: 100,
				groupLabel: "UIIT_CTL_WK_CH_WEAPON",
				sub: 102,
				subLabel: "UIIT_CTL_WK_CH_WEAPON_BLADE",
				degrees: 15
			},
			{
				tid: SWORD_TID,
				group: 100,
				groupLabel: "UIIT_CTL_WK_CH_WEAPON",
				sub: 101,
				subLabel: "UIIT_CTL_WK_CH_WEAPON_SWORD",
				degrees: 15
			},
			{
				tid: [ 3, 1, 6, 7 ],
				group: 400,
				groupLabel: "UIIT_CTL_WK_EU_WEAPON",
				sub: 401,
				subLabel: "UIIT_CTL_WK_EU_WEAPON_SWORD",
				degrees: 15
			},
			{
				tid: [ 3, 3, 11, 1 ],
				group: 700,
				groupLabel: "UIIT_CTL_WK_ETC_ETC",
				sub: 702,
				subLabel: "UIIT_CTL_WK_ALCHEMY_MAGICSTONE",
				degrees: 0
			}
		]
	};
}

/*
================
rows

Presented prize rows as the worker builds them (gachaPrizeRows).
================
*/
function rows() {
	return [
		{
			entry: 1,
			refObjId: 101,
			quantity: 1,
			nameSymbol: "",
			typeFlags: CH_SWORD_FLAGS,
			itemClass: 25,
			minLevelCap: 1
		},
		{
			entry: 2,
			refObjId: 102,
			quantity: 1,
			nameSymbol: "",
			typeFlags: CH_SWORD_FLAGS,
			itemClass: 28,
			minLevelCap: 91
		},
		{
			entry: 3,
			refObjId: 103,
			quantity: 1,
			nameSymbol: "",
			typeFlags: CH_SWORD_FLAGS,
			itemClass: 3,
			minLevelCap: 1
		},
		{ entry: 4, refObjId: 104, quantity: 5, nameSymbol: "", typeFlags: STONE_FLAGS, itemClass: 9, minLevelCap: 1 },
		{
			entry: 5,
			refObjId: 105,
			quantity: 1,
			nameSymbol: "",
			typeFlags: CH_BLADE_FLAGS,
			itemClass: 25,
			minLevelCap: 1
		},
		{ entry: 6, refObjId: 106, quantity: 1, nameSymbol: "", minLevelCap: 1 }
	];
}

test("an absent pool is the v1.150 window; a published one decodes with its defaults", () => {
	assert.equal( pool.decodeGachaPool( undefined ), undefined );
	const raw = sample();
	delete raw.entries[3].minLevelCap;
	delete raw.entries[3].plus;
	delete raw.entries[3].options;
	const decoded = pool.decodeGachaPool( raw );
	assert.ok( decoded );
	assert.equal( decoded.levelCap, CAP );
	assert.deepEqual( decoded.entries[0].options, [ { name: "str", value: 3, paramId: 7 } ] );
	assert.deepEqual( decoded.entries[2].options, [ { name: "int", value: 2 } ] );
	assert.deepEqual( { ...decoded.entries[3], options: [ ...decoded.entries[3].options ] }, {
		entry: 4,
		refObjId: 104,
		quantity: 5,
		minLevelCap: 1,
		plus: 0,
		options: []
	} );
	assert.deepEqual( decoded.tree[1].tid, SWORD_TID );
});

test("a malformed pool is refused rather than offered", () => {
	const broken = [
		raw => (raw.extra = 1),
		raw => (raw.entries[1].entry = 1),
		raw => (raw.entries[0].options[0].name = "toolong"),
		raw => (raw.entries[0].options = Array.from( { length: 6 }, () => ({ name: "str", value: 1 }) )),
		raw => (raw.entries[0].options[0].paramId = 0x10000),
		raw => (raw.entries[0].refObjId = 0),
		raw => (raw.tree[0].tid = [ 3, 1, 6 ]),
		raw => (raw.tree[0].groupLabel = ""),
		raw => (raw.levelCap = 0),
		raw => (raw.entries[0].unknown = true)
	];
	for ( const breakIt of broken ) {
		const raw = sample();
		breakIt( raw );
		assert.throws( () => pool.decodeGachaPool( raw ) );
	}
	assert.throws( () => pool.decodeGachaPool( [] ) );
});

test("the tree sorts groups and subgroups by id and keeps a node's first row", () => {
	const groups = pool.gachaTreeGroups( sample().tree );
	assert.deepEqual( groups.map( group => group.id ), [ 100, 400, 700 ] );
	assert.deepEqual( groups[0].subs.map( sub => sub.id ), [ 101, 102 ] );
	// Group 100 was created by the blade row, so its own type word is the blade's.
	assert.deepEqual( groups[0].tid, [ 3, 1, 6, 3 ] );
	assert.deepEqual( groups[0].subs[0].tid, SWORD_TID );
});

test("the boxes hide the other country's labels and only a country byte of 0 or 1 hides anything", () => {
	const groups = pool.gachaTreeGroups( sample().tree );
	const labels = country => pool.gachaVisibleGroups( groups, country ).map( group => group.id );
	assert.deepEqual( labels( 0 ), [ 100, 700 ] );
	assert.deepEqual( labels( 1 ), [ 400, 700 ] );
	assert.deepEqual( labels( 3 ), [ 100, 400, 700 ] );
	assert.deepEqual( labels( undefined ), [ 100, 400, 700 ] );
	assert.equal( pool.gachaLabelHidden( "UIIT_CTL_WK_ETC_ETC", 0 ), false );
});

test("Start needs a class and a type; the filter is the leaf's type and the chosen degree", () => {
	const groups = pool.gachaTreeGroups( sample().tree );
	const boxes = choice => pool.gachaSelectBoxes( groups, 0, choice );
	assert.equal( boxes( { group: 0, sub: 0, degree: 0 } ).filter, null );
	assert.equal( boxes( { group: 1, sub: 0, degree: 0 } ).filter, null );
	assert.deepEqual( boxes( { group: 1, sub: 0, degree: 0 } ).subs.map( sub => sub.id ), [ 101, 102 ] );
	const sword = boxes( { group: 1, sub: 1, degree: 9 } );
	assert.equal( sword.degrees, 15 );
	assert.deepEqual( sword.filter, { tid: SWORD_TID, degree: 9 } );
	assert.deepEqual( boxes( { group: 1, sub: 1, degree: 0 } ).filter, { tid: SWORD_TID, degree: 0 } );
	// The stone leaf offers no degrees: any degree, whatever box 3 last held.
	const stone = boxes( { group: 2, sub: 1, degree: 4 } );
	assert.equal( stone.degrees, 0 );
	assert.deepEqual( stone.filter, { tid: [ 3, 3, 11, 1 ], degree: 0 } );
	// A choice past the visible lists is no choice.
	assert.equal( boxes( { group: 9, sub: 1, degree: 0 } ).filter, null );
});

test("the type word and degree read as the v1.696 grid reads them", () => {
	assert.deepEqual( pool.gachaTypeIds( TICKET_FLAGS ), [ 3, 3, 14, 1 ] );
	assert.deepEqual( pool.gachaTypeIds( CH_SWORD_FLAGS ), SWORD_TID );
	// Equipment: (ItemClass - 1) / 3 + 1.
	assert.equal( pool.gachaItemDegree( CH_SWORD_FLAGS, 1 ), 1 );
	assert.equal( pool.gachaItemDegree( CH_SWORD_FLAGS, 3 ), 1 );
	assert.equal( pool.gachaItemDegree( CH_SWORD_FLAGS, 4 ), 2 );
	assert.equal( pool.gachaItemDegree( CH_SWORD_FLAGS, 28 ), 10 );
	assert.equal( pool.gachaItemDegree( CH_SWORD_FLAGS, undefined ), 0 );
	// Anything else: the raw class byte.
	assert.equal( pool.gachaItemDegree( STONE_FLAGS, 9 ), 9 );
});

test("the grid keeps list order and drops capped, mistyped, wrong-degree and unresolved prizes", () => {
	const all = rows();
	const ids = ( filter, cap = CAP ) => pool.gachaFilterPrizes( all, filter, cap ).map( row => row.entry );
	assert.deepEqual( ids( { tid: SWORD_TID, degree: 0 } ), [ 1, 3 ] );
	assert.deepEqual( ids( { tid: SWORD_TID, degree: 9 } ), [ 1 ] );
	assert.deepEqual( ids( { tid: SWORD_TID, degree: 1 } ), [ 3 ] );
	// Column 16: the degree-10 sword shows once the cap reaches 91.
	assert.deepEqual( ids( { tid: SWORD_TID, degree: 0 }, 110 ), [ 1, 2, 3 ] );
	assert.deepEqual( ids( { tid: [ 3, 3, 11, 1 ], degree: 0 } ), [ 4 ] );
	assert.deepEqual( ids( { tid: [ 3, 1, 6, 3 ], degree: 0 } ), [ 5 ] );
});

test("the last page follows 0xA715FC and slot i shows row page * 12 + i", () => {
	const expected = [ [ 0, 0 ], [ 1, 0 ], [ 11, 0 ], [ 12, 0 ], [ 13, 1 ], [ 24, 1 ], [ 25, 2 ] ];
	for ( const [count, last] of expected ) assert.equal( pool.gachaLastPage( count ), last, `count ${count}` );
	const list = Array.from( { length: 30 }, ( _, i ) => i );
	assert.deepEqual( pool.gachaPageRows( list, 0 ), list.slice( 0, 12 ) );
	assert.deepEqual( pool.gachaPageRows( list, 2 ), [ 24, 25, 26, 27, 28, 29 ] );
	assert.deepEqual( pool.gachaPageRows( list, 3 ), [] );
});

test("a resolved option becomes an encoded magic value; an unresolved one draws nothing", () => {
	const encoded = pool.gachaOptionMagic( [ { name: "str", value: 3, paramId: 7 }, { name: "int", value: 2 } ] );
	assert.deepEqual( encoded, [ (3n << 32n | 7n).toString() ] );
	assert.equal( Number( BigInt( encoded[0] ) & 0xffffn ), 7 );
	assert.equal( Number( BigInt( encoded[0] ) >> 32n ), 3 );
});

test("the hud opens on the select window, cascades the boxes and starts the grid on page 0", () => {
	const hud = hudModule.createGachaHud();
	hud.reset();
	assert.equal( hud.selectState().open, true );
	hud.toggleCombo( hudModule.GACHA_SELECT_COMBO_CLASS );
	assert.equal( hud.selectState().combo, hudModule.GACHA_SELECT_COMBO_CLASS );
	hud.chooseCombo( 1 );
	hud.toggleCombo( hudModule.GACHA_SELECT_COMBO_TYPE );
	hud.chooseCombo( 2 );
	hud.toggleCombo( hudModule.GACHA_SELECT_COMBO_DEGREE );
	hud.chooseCombo( 9 );
	assert.deepEqual( hud.selectState().choice, { group: 1, sub: 2, degree: 9 } );
	assert.equal( hud.selectState().combo, 0 );
	// A new class empties the type and degree boxes.
	hud.toggleCombo( hudModule.GACHA_SELECT_COMBO_CLASS );
	hud.chooseCombo( 2 );
	assert.deepEqual( hud.selectState().choice, { group: 2, sub: 0, degree: 0 } );
	assert.equal( hud.startSelect( null ), false );
	assert.equal( hud.selectState().open, true );
	const filter = { tid: SWORD_TID, degree: 0 };
	assert.equal( hud.startSelect( filter ), true );
	assert.equal( hud.selectState().open, false );
	assert.equal( hud.filter(), filter );
	assert.equal( hud.page( 5 ), 0 );
	// Reopening the machine starts on the select window again.
	hud.reset();
	assert.equal( hud.selectState().open, true );
	assert.equal( hud.filter(), null );
});

test("the open list scrolls inside the range its last paint reported", () => {
	const hud = hudModule.createGachaHud();
	hud.toggleCombo( hudModule.GACHA_SELECT_COMBO_CLASS );
	hud.setComboRange( 2 );
	for ( let i = 0; i < 5; i++ ) hud.scrollCombo( 1 );
	assert.equal( hud.selectState().comboFirst, 2 );
	hud.scrollCombo( -9 );
	assert.equal( hud.selectState().comboFirst, 0 );
});

test("a page change clears the chosen prize, stays in range and waits out a roll", () => {
	const hud = hudModule.createGachaHud();
	hud.startSelect( { tid: SWORD_TID, degree: 0 } );
	assert.equal( hud.select( 3, false ), true );
	assert.equal( hud.setPage( 1, 2, true ), false );
	assert.equal( hud.setPage( 1, 2, false ), true );
	const frame = hud.frame( { gacha: undefined, now: 0, items: [] } );
	assert.equal( frame.entry, 0 );
	assert.equal( hud.setPage( 7, 2, false ), true );
	assert.equal( hud.page( 2 ), 2 );
	assert.equal( hud.setPage( 2, 2, false ), false );
	// A shorter list clamps the page it shows.
	assert.equal( hud.page( 0 ), 0 );
});

test("the grid list is the filter's survivors and is rebuilt only when its inputs change", () => {
	const hud = hudModule.createGachaHud();
	const prizes = rows();
	assert.deepEqual( hud.filtered( prizes, CAP ), [] );
	hud.startSelect( { tid: SWORD_TID, degree: 0 } );
	const first = hud.filtered( prizes, CAP );
	assert.deepEqual( first.map( row => row.entry ), [ 1, 3 ] );
	assert.equal( hud.filtered( prizes, CAP ), first );
	assert.deepEqual( hud.filtered( prizes, 110 ).map( row => row.entry ), [ 1, 2, 3 ] );
});

/*
================
openSession

A worker session opened at the machine, as the B338 acknowledgement does.
================
*/
function openSession( published ) {
	const gacha = createGacha();
	gacha.setPool( published && pool.decodeGachaPool( published ) );
	gacha.open( 17 );
	const ack = new Uint8Array( 5 );
	ack[0] = 1;
	new DataView( ack.buffer ).setUint32( 1, 0x10000, true );
	assert.equal( gacha.opened( ack ), true );
	return gacha;
}

test("with a pool the session rolls only the entries the cap shows, on the v1.150 frame", () => {
	/** @type {any} */
	const ticket = { slot: 20, quantity: 1, typeFlags: TICKET_FLAGS };
	const gacha = openSession( sample() );
	// Entry 2 sits above the level cap; entry 99 is not in the pool.
	assert.throws( () => gacha.start( 2, 20, ticket, 0 ) );
	assert.throws( () => gacha.start( 99, 20, ticket, 0 ) );
	gacha.start( 3, 20, ticket, 0 );
	const roll = gacha.step( 4000, ticket );
	assert.ok( roll );
	assert.equal( roll.opcode, 0x7053 );
	assert.equal( roll.payload.length, 9 );
	const view = new DataView( roll.payload.buffer );
	assert.equal( view.getUint32( 0, true ), 17 );
	assert.equal( view.getUint32( 4, true ), 3 );
	assert.equal( roll.payload[8], 20 );
});

test("without a pool the session keeps the v1.150 twelve, and a reset forgets the pool", () => {
	/** @type {any} */
	const ticket = { slot: 20, quantity: 1, typeFlags: TICKET_FLAGS };
	const v150 = openSession( undefined );
	assert.throws( () => v150.start( 13, 20, ticket, 0 ) );
	v150.start( 12, 20, ticket, 0 );
	const gacha = openSession( sample() );
	assert.notEqual( gacha.pool(), undefined );
	gacha.reset();
	assert.equal( gacha.pool(), undefined );
});
