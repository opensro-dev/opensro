/*
===========================================================================

cos-status-lines.test.mjs - the COS status lines' worker behaviour

#2290: the specialty refusal at the COS move commands, the merchant
refusal for a non-transport COS bag, the HGP 30%-fall line on both
satiety packets, and the free-battle cape warning on the equip move.
Each refusal prints its key into the notice queue and sends nothing;
the warning prints and the equip still sends.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);

const u16 = n => [ n & 255, n >>> 8 & 255 ];
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const u64 = n => {
	const bytes = [];
	for ( let v = BigInt( n ), i = 0; i < 8; i++, v >>= 8n ) bytes.push( Number( v & 255n ) );
	return bytes;
};
// (typeFlags & 0x7FE) === 0x46C is the trade-goods class (550300's bits);
// 0x86C a stackable potion; the cape is TID 3/1/7/5 (593310).
const TRADE_GOODS = 0x46c, POTION = 0x86c, CAPE = 3 << 2 | 1 << 5 | 7 << 7 | 5 << 11;
const SPECIALTY = "UIIT_MSG_COS_CANNOT_BUY_SPECIALTY_TO_INVENTORY";
const TRADE_SHOP = "UIIT_MSG_COSPETERR_CANT_TRADE_SHOP";
const HGP_LOW = "UIIT_MSG_COSPETERR_HGP_30LOW";
const CAPE_WARNING = "UIIT_MSG_COSPETERR_UNSUMMON_FREEBATTLE";

/**
 * @typedef {{ slot: number, refObjId: number, typeFlags: number }} BagRow
 * @typedef {{ gid: number, refObjId: number, band: number, satiety?: number, status?: number, dead?: boolean, items?: BagRow[] }} CompanionRow
 */

/*
================
itemBody

The wire body of one bag row: quantity for a stackable etc item, the
equipment fields (plus, variance, durability, options) otherwise.
================
*/
function itemBody( refObjId, typeFlags, quantity = 5 ) {
	if ( (typeFlags & 0x60) === 0x20 ) {
		return [ ...u32( refObjId ), 0, ...u64( 0 ), ...u32( 100 ), 0 ];
	}
	const bytes = [ ...u32( refObjId ), ...u16( quantity ) ];
	// The specialty-good group (tid & 0x780 === 0x400) carries a label
	// after the quantity; an empty one keeps the fixture minimal.
	if ( (typeFlags & 0x780) === 0x400 ) bytes.push( ...u16( 0 ) );
	return bytes;
}

/*
================
cosSpawn

One owned COS record (830EC0's layout as decodeCosRecord reads it) for
the 0x3158 lane: identity, vitals, the attack pet's growth fields, the
bag and the trailing flags of its band.
================
*/
/** @param {CompanionRow} row */
function cosSpawn( { gid, refObjId, band, satiety, status = 0, dead = false, items = [] } ) {
	const bytes = [ ...u32( gid ), ...u32( refObjId ), ...u32( 100 ), ...u32( 50 ) ];
	if ( band === 3 ) bytes.push( ...u32( 0 ), ...u32( 0 ), 1, ...u16( satiety ?? 10000 ) );
	if ( band === 3 || band === 4 ) bytes.push( ...u32( 0 ), ...u16( 0 ) );
	bytes.push( status );
	if ( status !== 0 ) {
		bytes.push( items.length );
		for ( const item of items ) bytes.push( item.slot, ...itemBody( item.refObjId, item.typeFlags ) );
	}
	if ( band !== 1 && band !== 5 && band !== 6 ) bytes.push( ...u32( dead ? 1 : 0 ) );
	if ( band === 3 || band === 4 ) bytes.push( 13 );
	return Uint8Array.from( bytes );
}

/*
================
world

A local player (gid 1) with the given companions summoned and the given
bag rows. Bag rows are {slot, refObjId, typeFlags}; companions are
cosSpawn rows keyed by their own refObjId/band.
================
*/
/** @param {{ companions?: CompanionRow[], bagItems?: BagRow[], capeReqGender?: number, wearerSex?: number }} [options] */
function world( { companions = [], bagItems = [], capeReqGender, wearerSex } = {} ) {
	const sent = [],
		readEntity = wearerSex === undefined ?
			undefined :
			(gid =>
				gid === 1 ?
					{ gid: 1, refObjId: 500, kind: "player", regionId: 1, x: 0, y: 0, z: 0, heading: 0, name: "" } :
					undefined),
		game = createGameplay( f => sent.push( f ), () => {}, () => {}, readEntity );
	if ( wearerSex !== undefined ) {
		game.bindReferences( {
			country: () => 0,
			playerModels: () => [ { refObjId: 500, sex: wearerSex } ],
			item: () => undefined,
			monster: () => undefined,
			npcs: () => []
		} );
	}
	game.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refObjSnapshot: companions.map( row => ({
			kind: "cos",
			refObjId: row.refObjId,
			tidWord: (row.band << 11) | 0x1c6
		}) ),
		refItemSnapshot: [
			{ refObjId: 8, typeFlags: TRADE_GOODS, nativeFields: { maxStack: 50 } },
			{ refObjId: 9, typeFlags: POTION, nativeFields: { maxStack: 50 } },
			{
				refObjId: 10,
				typeFlags: CAPE,
				...(capeReqGender === undefined ? {} : { nativeFields: { reqGender: capeReqGender } })
			}
		],
		equipItems: bagItems.map( item => ({
			slot: item.slot,
			refObjId: item.refObjId,
			body: itemBody( item.refObjId, item.typeFlags )
		}) )
	} );
	game.seed( {
		gid: 1,
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		...(wearerSex === undefined ? {} : { countryByte9c: 0 })
	} );
	for ( const row of companions ) game.receive( { opcode: 0x3158, payload: cosSpawn( row ) }, 0 );
	return { game, sent };
}

/** @param {number} band @param {Partial<CompanionRow>} [extra] @returns {CompanionRow} */
const pet = ( band, extra = {} ) => ({ gid: 7, refObjId: 102, band, status: 4, ...extra });
const cosEntity = { gid: 7, refObjId: 102, kind: "cos", ownerGid: 1 };
const noticeKeys = game => (game.take()?.notices ?? []).map( notice => notice.key );

test("a specialty good moved into a pickup pet prints the line and sends nothing", () => {
	const { game, sent } = world( {
		companions: [ pet( 4 ) ],
		bagItems: [
			{ slot: 20, refObjId: 8, typeFlags: TRADE_GOODS },
			{ slot: 21, refObjId: 9, typeFlags: POTION }
		]
	} );
	assert.equal( game.command( { kind: "cos-transfer", gid: 7, toCos: true, source: 20 }, 1, cosEntity ), null );
	assert.equal( sent.length, 0 );
	assert.deepEqual( noticeKeys( game ), [ SPECIALTY ] );
	// An ordinary item crosses into the same pet without a word.
	const frame = game.command( { kind: "cos-transfer", gid: 7, toCos: true, source: 21 }, 2, cosEntity );
	assert.equal( frame.opcode, 0x706d );
	assert.deepEqual( noticeKeys( game ), [ SPECIALTY ] );
});

test("the same specialty good enters a transport freely", () => {
	const { game } = world( {
		companions: [ pet( 2 ) ],
		bagItems: [ { slot: 20, refObjId: 8, typeFlags: TRADE_GOODS } ]
	} );
	const frame = game.command( { kind: "cos-transfer", gid: 7, toCos: true, source: 20 }, 1, cosEntity );
	assert.equal( frame.opcode, 0x706d );
	assert.deepEqual( noticeKeys( game ), [] );
});

test("rearranging a specialty good inside a non-transport bag is refused the same way", () => {
	const goods = [ { slot: 0, refObjId: 8, typeFlags: TRADE_GOODS } ];
	const pickup = world( { companions: [ pet( 4, { items: goods } ) ] } );
	assert.equal(
		pickup.game.command(
			{ kind: "cos-inventory-move", gid: 7, source: 0, destination: 1, quantity: 5 },
			1,
			cosEntity
		),
		null
	);
	assert.equal( pickup.sent.length, 0 );
	assert.deepEqual( noticeKeys( pickup.game ), [ SPECIALTY ] );
	const transport = world( { companions: [ pet( 2, { items: goods } ) ] } );
	const frame = transport.game.command(
		{ kind: "cos-inventory-move", gid: 7, source: 0, destination: 1, quantity: 5 },
		1,
		cosEntity
	);
	assert.equal( frame.opcode, 0x706d );
	assert.deepEqual( noticeKeys( transport.game ), [] );
});

/*
================
shopWorld

The specialty refusal at the shop: a merchant (gid 17) selected and open,
its catalog offering one trade good, with the given companion summoned.
================
*/
function shopWorld( band, items = [] ) {
	const { game, sent } = world( { companions: [ pet( band, { items } ) ] } );
	const npc = { gid: 17, kind: "npc", regionId: 1, x: 0, y: 0, z: 0, heading: 0 };
	game.command( { kind: "select", gid: 17 }, 1, npc );
	// The selection grant: type 1, capability word 0x801 (a merchant).
	game.receive(
		{ opcode: 0xb45a, payload: Uint8Array.from( [ 1, ...u32( 17 ), 0, ...u32( 0x801 ), 0 ] ) },
		2
	);
	game.command( { kind: "shop-open", gid: 17 }, 3, npc );
	const catalog = {
		version: 1,
		npc: 17,
		name: "Merchant",
		offers: [ {
			tab: 0,
			slot: 0,
			refObjId: 8,
			name: "Trade goods",
			price: "5",
			maxStack: 50,
			previews: [ {
				refObjId: 8,
				typeFlags: TRADE_GOODS,
				name: "Trade goods",
				body: itemBody( 8, TRADE_GOODS )
			} ]
		}, {
			tab: 0,
			slot: 1,
			refObjId: 9,
			name: "Potion",
			price: "5",
			maxStack: 50,
			previews: [ {
				refObjId: 9,
				typeFlags: POTION,
				name: "Potion",
				body: itemBody( 9, POTION )
			} ]
		} ]
	};
	game.receive( { opcode: 11, payload: new TextEncoder().encode( JSON.stringify( catalog ) ) }, 4 );
	return { game, sent };
}

test("buying a specialty good into a pickup pet prints the line and sends nothing", () => {
	const { game, sent } = shopWorld( 4 );
	const before = sent.length;
	assert.equal( game.command( { kind: "cos-shop-buy", gid: 7, tab: 0, slot: 0, quantity: 1 }, 5, cosEntity ), null );
	assert.equal( sent.length, before );
	assert.deepEqual( noticeKeys( game ), [ SPECIALTY ] );
});

test("buying the same good into a transport sends the purchase", () => {
	const { game } = shopWorld( 2 );
	const frame = game.command( { kind: "cos-shop-buy", gid: 7, tab: 0, slot: 0, quantity: 1 }, 5, cosEntity );
	assert.equal( frame.opcode, 0x706d );
	assert.deepEqual( noticeKeys( game ), [] );
});

test("selling from a pickup pet at a shop prints the trade-shop line and sends nothing", () => {
	const goods = [ { slot: 0, refObjId: 9, typeFlags: POTION } ];
	const { game, sent } = shopWorld( 4, goods );
	const before = sent.length;
	assert.equal( game.command( { kind: "cos-shop-sell", gid: 7, slot: 0, quantity: 1 }, 5, cosEntity ), null );
	assert.equal( sent.length, before );
	assert.deepEqual( noticeKeys( game ), [ TRADE_SHOP ] );
	// An ordinary purchase into the same pet is refused the same way;
	// only the specialty good above keeps its own line.
	assert.equal( game.command( { kind: "cos-shop-buy", gid: 7, tab: 0, slot: 1, quantity: 1 }, 6, cosEntity ), null );
	assert.equal( sent.length, before );
	assert.deepEqual( noticeKeys( game ), [ TRADE_SHOP, TRADE_SHOP ] );
});

test("selling from a transport at a shop sends the sale", () => {
	const goods = [ { slot: 0, refObjId: 9, typeFlags: POTION } ];
	const { game } = shopWorld( 2, goods );
	const frame = game.command( { kind: "cos-shop-sell", gid: 7, slot: 0, quantity: 1 }, 5, cosEntity );
	assert.equal( frame.opcode, 0x706d );
	assert.deepEqual( noticeKeys( game ), [] );
});

test("the HGP line follows the 0x3508 satiety fall, once per crossing", () => {
	const { game } = world( { companions: [ pet( 3, { satiety: 5000, status: 0 } ) ] } );
	const satietyTo = value =>
		game.receive( { opcode: 0x3508, payload: Uint8Array.from( [ ...u32( 7 ), 4, ...u16( value ) ] ) }, 1 );
	const falls = () => noticeKeys( game ).filter( key => key === HGP_LOW ).length;
	satietyTo( 3000 );
	assert.equal( falls(), 0, "landing on 3000 is the halved state, not the printed fall" );
	satietyTo( 2999 );
	assert.equal( falls(), 1 );
	satietyTo( 2500 );
	assert.equal( falls(), 1, "already low: no second line" );
	satietyTo( 9000 );
	assert.equal( falls(), 1, "the rise back prints nothing" );
	satietyTo( 2500 );
	assert.equal( falls(), 2, "a new fall prints again" );
});

test("the HGP line follows the 0x33A6 satiety fall the same way", () => {
	const { game } = world( { companions: [ pet( 3, { satiety: 5000, status: 0 } ) ] } );
	const vitalsTo = value =>
		game.receive(
			{ opcode: 0x33a6, payload: Uint8Array.from( [ ...u32( 7 ), ...u16( 0x400 ), 8, ...u16( value ) ] ) },
			1
		);
	const falls = () => noticeKeys( game ).filter( key => key === HGP_LOW ).length;
	vitalsTo( 2999 );
	assert.equal( falls(), 1 );
	vitalsTo( 2000 );
	assert.equal( falls(), 1 );
	vitalsTo( 9000 );
	assert.equal( falls(), 1 );
	vitalsTo( 100 );
	assert.equal( falls(), 2 );
});

test("the HGP line prints once when both satiety packets report one fall", () => {
	const { game } = world( { companions: [ pet( 3, { satiety: 5000, status: 0 } ) ] } );
	const satietyTo = value =>
		game.receive( { opcode: 0x3508, payload: Uint8Array.from( [ ...u32( 7 ), 4, ...u16( value ) ] ) }, 1 );
	const vitalsTo = value =>
		game.receive(
			{ opcode: 0x33a6, payload: Uint8Array.from( [ ...u32( 7 ), ...u16( 0x400 ), 8, ...u16( value ) ] ) },
			1
		);
	const falls = () => noticeKeys( game ).filter( key => key === HGP_LOW ).length;
	// The record is the one satiety store (the original's +0x10 word):
	// the fall the vitals lane printed is already stored when the
	// satiety packet lands lower still.
	vitalsTo( 2500 );
	assert.equal( falls(), 1 );
	satietyTo( 2400 );
	assert.equal( falls(), 1, "the second lane sees the same stored fall" );
	satietyTo( 9000 );
	assert.equal( falls(), 1, "the rise back prints nothing" );
	vitalsTo( 2000 );
	assert.equal( falls(), 2, "a fresh fall on either lane prints again" );
	satietyTo( 1500 );
	assert.equal( falls(), 2 );
});

test("equipping a free-battle cape warns while a growth pet is out, and still equips", () => {
	const bagItems = [ { slot: 20, refObjId: 10, typeFlags: CAPE } ];
	const withPet = world( { companions: [ pet( 3, { status: 0 } ) ], bagItems } );
	const frame = withPet.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.equal( frame.opcode, 0x706d, "the warning never blocks the equip" );
	// 594980 prints on both of the original's surfaces: the guide lane
	// (67A600 type 5, 594AE5) and the centre banner (67D030, 594B00).
	const notices = withPet.game.take()?.notices ?? [];
	assert.deepEqual( notices.map( notice => notice.key ), [ CAPE_WARNING ] );
	assert.equal( notices[0].nativeType, 5 );
	assert.equal( notices[0].notificationBanner, true );
	const alone = world( { bagItems } );
	alone.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( alone.game ), [] );
});

test("activating a free-battle cape by double-click equips it without the warning", () => {
	// 594980 is the equipment drop action alone: the key is pushed
	// nowhere else (594AA9) and a double-click activation (567290)
	// composes the same equip straight into the socket (destination
	// kind 0x46 at 5678B0), so the activation stays silent while the
	// move still sends.
	const bagItems = [ { slot: 20, refObjId: 10, typeFlags: CAPE } ];
	const clicked = world( { companions: [ pet( 3, { status: 0 } ) ], bagItems } );
	const frame = clicked.game.command(
		{ kind: "inventory-move", source: 20, destination: 8, quantity: 0, activation: true },
		1
	);
	assert.equal( frame.opcode, 0x706d, "the activation equip still sends" );
	assert.deepEqual( noticeKeys( clicked.game ), [] );
});

test("the cape warning yields to a guild soldier and to non-cape moves", () => {
	const bagItems = [
		{ slot: 20, refObjId: 10, typeFlags: CAPE },
		{ slot: 21, refObjId: 9, typeFlags: POTION }
	];
	const guild = { gid: 8, refObjId: 103, band: 5, status: 0 };
	const both = world( { companions: [ pet( 3, { status: 0 } ), guild ], bagItems } );
	both.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( both.game ), [], "a guild soldier out silences the warning" );
	// A cape to any other socket, and a potion onto the cape socket, are silent.
	const pet3 = world( { companions: [ pet( 3, { status: 0 } ) ], bagItems } );
	pet3.game.command( { kind: "inventory-move", source: 20, destination: 6, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( pet3.game ), [] );
	const pet4 = world( { companions: [ pet( 3, { status: 0 } ) ], bagItems } );
	pet4.game.command( { kind: "inventory-move", source: 21, destination: 8, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( pet4.game ), [] );
});

test("swapping a cape onto a worn cape socket does not warn", () => {
	// 594980 requires the target socket's record empty (+0x28 clear):
	// the warning belongs to the fresh equip, not the swap.
	const bagItems = [
		{ slot: 20, refObjId: 10, typeFlags: CAPE },
		{ slot: 8, refObjId: 10, typeFlags: CAPE }
	];
	const worn = world( { companions: [ pet( 3, { status: 0 } ) ], bagItems } );
	const frame = worn.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.equal( frame.opcode, 0x706d, "the swap itself still sends" );
	assert.deepEqual( noticeKeys( worn.game ), [] );
});

test("a dead growth pet still counts as out for the cape warning", () => {
	// 82EF30 finds the class-3 entry in the manager's list and tests
	// nothing else: a pet killed but not yet dismissed still warns.
	const bagItems = [ { slot: 20, refObjId: 10, typeFlags: CAPE } ];
	const dead = world( { companions: [ pet( 3, { status: 0, dead: true } ) ], bagItems } );
	dead.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( dead.game ), [ CAPE_WARNING ] );
});

test("the cape warning obeys the cape's gender requirement", () => {
	const bagItems = [ { slot: 20, refObjId: 10, typeFlags: CAPE } ];
	const companions = [ pet( 3, { status: 0 } ) ];
	// 789C60 with mask 0x40: a female-only cape (ReqGender 0) on a male
	// wearer (selector 1) fails the leg, so 594980 prints nothing; the
	// equip itself still sends.
	const wrong = world( { companions, bagItems, capeReqGender: 0, wearerSex: 1 } );
	const frame = wrong.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.equal( frame.opcode, 0x706d, "the equip itself still sends" );
	assert.deepEqual( noticeKeys( wrong.game ), [] );
	const match = world( { companions, bagItems, capeReqGender: 0, wearerSex: 0 } );
	match.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( match.game ), [ CAPE_WARNING ] );
	// ReqGender 2 is cut for both sexes and warns for either wearer.
	const both = world( { companions, bagItems, capeReqGender: 2, wearerSex: 1 } );
	both.game.command( { kind: "inventory-move", source: 20, destination: 8, quantity: 0 }, 1 );
	assert.deepEqual( noticeKeys( both.game ), [ CAPE_WARNING ] );
});
