/*
===========================================================================

gold-caps.test.mjs - the port-only raised stall and warehouse gold caps

SRO_RAISED_GOLD_CAPS raises the stall price editor from 1,000,000,000 to
10,000,000,000 and the warehouse gold deposit dialog from 100,000,000 to
1,000,000,000. Off, both stay native, and the offer wire stays u32.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";

const stall = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const hud = await import( "../../src/engine/runtime/ui/hud/stall-hud.ts" );
const storage = await import( "../../src/engine/foundation/gameplay/storage-room.ts" );

const POTION = 3630, OWNER = 0x30d41;
const refs = new Map( [ [ POTION, 0x60 | 0x0c ] ] );
const u16 = ( /** @type {number} */ v ) => [ v & 255, v >> 8 & 255 ];
const u32 = ( /** @type {number} */ v ) => [ v & 255, v >> 8 & 255, v >> 16 & 255, v >>> 24 ];
const u64 = ( /** @type {number|bigint} */ v ) => {
	const b = BigInt( v );
	return Array.from( { length: 8 }, ( _, i ) => Number( b >> BigInt( i * 8 ) & 255n ) );
};
const wstr = ( /** @type {string} */ s ) => [
	...u16( s.length ),
	...Array.from( s ).flatMap( c => u16( c.charCodeAt( 0 ) ) )
];
const frame = ( /** @type {number} */ opcode, /** @type {number[]} */ bytes ) => ({
	opcode,
	payload: Uint8Array.from( bytes )
});

/*
================
pricePrompt

The owner's price prompt for five potions.
================
*/
const pricePrompt = () => ({
	kind: /** @type {const} */ ("price"),
	slot: 0,
	bagSlot: 13,
	carried: 5,
	quantity: "5",
	price: "0",
	modify: false
});

test("the price prompt clamps at the native billion or the raised ten billion", () => {
	for (
		const [raised, limit] of /** @type {[boolean, number][]} */ ([
			[ false, hud.STALL_PRICE_LIMIT ],
			[ true, hud.STALL_PRICE_LIMIT_RAISED ]
		])
	) {
		const box = hud.createStallHud( raised );
		box.open( pricePrompt() );
		box.type( "price", "99999999999" );
		assert.equal( /** @type {any} */ (box.prompt()).price, String( limit ) );
		// The raised cap needs the eleventh digit; the native one stops at ten.
		assert.equal( String( limit ).length, raised ? 11 : 10 );
	}
});

test("raised gold caps carry a ten-billion offer across the stall wire", () => {
	const owner = { ...stall.emptyStall(), phase: /** @type {const} */ ("owner") };
	const added = stall.stallRequest( owner, {
		kind: "stall-add",
		slot: 0,
		bagSlot: 13,
		quantity: 1,
		price: 10000000000
	}, true );
	const p = added.frames[0].payload;
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	// [kind 2][slot][bag][u16 count][u64 price][u32 category][u8]
	assert.equal( p[0], 2 );
	assert.equal( v.getBigUint64( 5, true ), 10000000000n );

	// [1][u32 owner][wstr greeting][u8 open][u8 mode] offers [u8 visitors]
	const visit = frame( 0xb61f, [
		1,
		...u32( OWNER ),
		...wstr( "Hi" ),
		1,
		0,
		0,
		...u32( POTION ),
		...u16( 1 ),
		13,
		...u16( 1 ),
		...u64( 10000000000 ),
		0xff,
		0
	] );
	const outcome = defined( stall.stallFrame( stall.emptyStall(), visit, {
		localGid: OWNER,
		refs,
		widePrice: true
	} ) );
	assert.equal( outcome.state.offers[0]?.price, 10000000000 );

	// Off, the same command keeps the native u32 field.
	const narrow = stall.stallRequest(
		owner,
		{ kind: "stall-add", slot: 0, bagSlot: 13, quantity: 1, price: 1 },
		false
	);
	assert.equal( narrow.frames[0].payload.length, p.length - 4 );
});

test("the warehouse deposit dialog caps at 100m natively and 1b with the raised caps", () => {
	assert.equal( storage.storageGoldDepositLimit( false ), 100000000n );
	assert.equal( storage.storageGoldDepositLimit( true ), 1000000000n );
	for (
		const [raised, limit] of /** @type {[boolean, number][]} */ ([
			[ false, 100000000 ],
			[ true, 1000000000 ]
		])
	) {
		const sent = [],
			f = uiFixture(
				/** @type {() => void} */ (/** @param {any} command */ command => {
					sent.push( command );
				})
			);
		try {
			const game = {
				...f.state.gameplay,
				inventoryPending: false,
				raisedGoldCaps: raised,
				progression: { gold: "2000000000", masteries: [] },
				storage: { npc: 7, phase: "open", capacity: 150, gold: "0", items: [] }
			};
			const state = { ...f.state, gameplay: game };
			let now = 0, scene;
			const click = ( /** @type {string} */ id ) => {
				f.ui.event( { kind: "activate", id } );
				scene = f.ui.step( state, ++now ) ?? scene;
			};
			f.ui.step( state, now );
			click( "inventory-gold" );
			const raw = String( limit * 2 );
			f.ui.event( {
				kind: "edit",
				id: "gold-amount",
				value: raw,
				start: raw.length,
				end: raw.length,
				composing: false
			} );
			scene = f.ui.step( state, ++now ) ?? scene;
			assert.equal(
				defined( scene ).controls.find( c => c.id === "gold-amount" )?.value,
				String( limit ),
				"the deposit draft clamps to the shard's cap"
			);
			click( "drop-gold" );
			assert.deepEqual( sent.pop(), {
				kind: "gameplay",
				command: {
					kind: "storage-move",
					move: { type: 0x0c, source: 0, destination: 0, quantity: 0, gold: limit }
				}
			} );
		} finally {
			f.dispose();
		}
	}
});

/*
================
str

A length-prefixed byte string (the sold notice's buyer name).
================
*/
const str = ( /** @type {string} */ value ) => [
	...u16( value.length ),
	...Array.from( value, c => c.charCodeAt( 0 ) )
];

/*
================
priceOffer

An owner's offer in the local state, before a frame replaces its price.
================
*/
const priceOffer = ( /** @type {number} */ slot, /** @type {number} */ price ) => ({
	slot,
	bagSlot: 13 + slot,
	quantity: 5,
	price,
	item: {
		slot: 13 + slot,
		refObjId: POTION,
		typeFlags: 0x60c,
		quantity: 5,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		name: "Potion"
	}
});

test("the stall-modify request and both offer decoders follow the price width", () => {
	const owner = { ...stall.emptyStall(), phase: /** @type {const} */ ("owner") };
	const wide = stall.stallRequest( owner, {
		kind: "stall-modify",
		slot: 0,
		quantity: 5,
		price: 10000000000
	}, true );
	const widePayload = wide.frames[0].payload;
	const wideView = new DataView( widePayload.buffer, widePayload.byteOffset, widePayload.byteLength );
	// [kind 1][slot][u16 count][u64 price][u8]
	assert.equal( widePayload.length, 13 );
	assert.equal( wideView.getBigUint64( 4, true ), 10000000000n );

	const narrow = stall.stallRequest( owner, {
		kind: "stall-modify",
		slot: 0,
		quantity: 5,
		price: 1000000000
	}, false );
	const narrowPayload = narrow.frames[0].payload;
	assert.equal( narrowPayload.length, 9 );
	assert.equal( new DataView( narrowPayload.buffer ).getUint32( 4, true ), 1000000000 );

	// 0xB1A8 kind 1: [1][kind][slot][u16 count][price][u8 code]
	const ownerState = { ...owner, offers: [ priceOffer( 0, 1 ) ] };
	const modified = ( /** @type {number[]} */ priceBytes ) =>
		frame( 0xb1a8, [ 1, 1, 0, ...u16( 5 ), ...priceBytes, 0 ] );
	const wideOutcome = defined( stall.stallFrame(
		ownerState,
		modified( u64( 10000000000 ) ),
		{ localGid: OWNER, refs, widePrice: true }
	) );
	assert.equal( wideOutcome.state.offers[0]?.price, 10000000000 );
	const narrowOutcome = defined( stall.stallFrame(
		ownerState,
		modified( u32( 1000000000 ) ),
		{ localGid: OWNER, refs, widePrice: false }
	) );
	assert.equal( narrowOutcome.state.offers[0]?.price, 1000000000 );

	// 0x3260 kind 4: [4][slot][str buyer] offers left.
	const sold = ( /** @type {number[]} */ priceBytes ) =>
		frame( 0x3260, [
			4,
			0,
			...str( "Buyer" ),
			1,
			...u32( POTION ),
			...u16( 5 ),
			14,
			...u16( 5 ),
			...priceBytes,
			0xff
		] );
	const soldState = { ...owner, offers: [ priceOffer( 0, 1 ), priceOffer( 1, 1 ) ] };
	const wideSold = defined( stall.stallFrame( soldState, sold( u64( 10000000000 ) ), {
		localGid: OWNER,
		refs,
		widePrice: true
	} ) );
	assert.equal( wideSold.state.offers[0]?.price, 10000000000 );
	const narrowSold = defined( stall.stallFrame( soldState, sold( u32( 1000000000 ) ), {
		localGid: OWNER,
		refs,
		widePrice: false
	} ) );
	assert.equal( narrowSold.state.offers[0]?.price, 1000000000 );
});

test("the price edit control publishes the raised digit width and accepts eleven digits", () => {
	for (
		const [raised, maxLength, expected] of /** @type {[boolean, number, string][]} */ ([
			[ false, 10, "1000000000" ],
			[ true, 11, "10000000000" ]
		])
	) {
		const f = uiFixture();
		try {
			const item = {
				slot: 13,
				refObjId: POTION,
				typeFlags: 0x60c,
				quantity: 5,
				plus: 0,
				durability: 0,
				variance: "0",
				magic: [],
				name: "Potion",
				icon: "item/etc/hp_potion_01.ddj"
			};
			Object.assign( f.state.gameplay, {
				raisedGoldCaps: raised,
				inventory: [ item ],
				stall: {
					...stall.emptyStall(),
					phase: /** @type {const} */ ("owner"),
					owner: 1,
					open: false,
					offers: [ { slot: 0, bagSlot: 13, quantity: 5, price: 1, item } ]
				}
			} );
			f.ui.step( f.state, 0 );
			f.ui.event( { kind: "key", code: "KeyI" } );
			let scene = f.ui.step( f.state, 1 );
			f.ui.event( { kind: "activate", id: "stall-modify:0" } );
			scene = f.ui.step( f.state, 2 ) ?? scene;
			const priceControl = () => defined( scene ).controls.find( c => c.id === "stall-prompt-price" );
			assert.equal( priceControl()?.maxLength, maxLength );
			f.ui.event( {
				kind: "edit",
				id: "stall-prompt-price",
				value: "10000000000",
				start: 11,
				end: 11,
				composing: false
			} );
			scene = f.ui.step( f.state, 3 ) ?? scene;
			assert.equal( priceControl()?.value, expected );
		} finally {
			f.dispose();
		}
	}
});
