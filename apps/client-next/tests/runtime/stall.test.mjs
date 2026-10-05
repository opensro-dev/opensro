/*
===========================================================================

stall.test.mjs - the street stall and stall network, wire to prompt

The naming prompt and its create-then-greeting request (695420 case 9,
5A2890), a visitor's window (751720) and purchase (5A3870), the owner's
sale (755960), the network's rows and purchase (766FC0, 5AE020), the
prompt answers, the result sort, the category tree and a stall's visit
reach (698740).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const stall = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const hud = await import( "../../src/engine/runtime/ui/hud/stall-hud.ts" );
const { decodeStallNetworkCategories } = await import(
	"../../src/engine/foundation/ui/stall-network-categories.ts"
);
const { interactionApproach, keepsStall } = await import(
	"../../src/engine/foundation/gameplay/interaction-approach.ts"
);
const { textAllowed } = await import( "../../src/engine/foundation/ui/character-create.ts" );
const { createChat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/chat/chat.ts"
);

// ITEM_ETC_HP_POTION_01 (etc band, one u16 count).
const POTION = 3630;
const refs = new Map( [ [ POTION, 0x60 | 0x0c ] ] );
const LOCAL = 0x30d40, OWNER = 0x30d41;
const ctx = { localGid: LOCAL, refs };

/**
 * @param {number} opcode
 * @param {number[]} bytes
 */
const frame = ( opcode, bytes ) => ({ opcode, payload: Uint8Array.from( bytes ) });
const u16 = ( /** @type {number} */ v ) => [ v & 255, v >> 8 & 255 ];
const u32 = ( /** @type {number} */ v ) => [ v & 255, v >> 8 & 255, v >> 16 & 255, v >>> 24 ];
const u64 = ( /** @type {number} */ v ) => [ ...u32( v ), 0, 0, 0, 0 ];
const wstr = (
	/** @type {string} */ s
) => [ ...u16( s.length ), ...Array.from( s ).flatMap( c => u16( c.charCodeAt( 0 ) ) ) ];
// [slot][CSOItem: ref, u16 count][bag slot][u16 count][u32 price]
const offer = (
	/** @type {number} */ slot,
	/** @type {number} */ bag,
	/** @type {number} */ count,
	/** @type {number} */ price
) => [
	slot,
	...u32( POTION ),
	...u16( count ),
	bag,
	...u16( count ),
	...u32( price )
];

test("naming opens on the action and its answer sends the stall and its greeting", () => {
	let state = stall.emptyStall();
	assert.throws( () => stall.stallRequest( state, { kind: "stall-create", title: "Potions", greeting: "Hi" } ) );
	state = stall.stallRequest( state, { kind: "stall-name" } ).state;
	assert.equal( state.phase, "naming" );
	const created = stall.stallRequest( state, { kind: "stall-create", title: "Potions", greeting: "Hi" } );
	assert.deepEqual( created.frames.map( f => f.opcode ), [ 0x7049, 0x71a8 ] );
	assert.deepEqual( [ ...created.frames[0].payload ], wstr( "Potions" ) );
	assert.deepEqual( [ ...created.frames[1].payload ], [ 6, ...wstr( "Hi" ) ] );
	state = defined( stall.stallFrame( created.state, frame( 0xb049, [ 1 ] ), ctx ) ).state;
	assert.equal( state.phase, "owner" );
	assert.equal( state.title, "Potions" );
	const cancelled = stall.stallRequest( stall.stallRequest( stall.emptyStall(), { kind: "stall-name" } ).state, {
		kind: "stall-name-cancel"
	} );
	assert.equal( cancelled.state.phase, "none" );
	assert.equal( cancelled.frames.length, 0 );
});

test("a visitor sees the offers, buys one into the bag, and the owner gives it up", () => {
	const visit = frame( 0xb61f, [
		1,
		...u32( OWNER ),
		...wstr( "Welcome" ),
		1,
		0,
		...offer( 2, 20, 5, 300 ),
		0xff,
		1,
		...u32( 7 )
	] );
	let state = defined( stall.stallFrame( stall.emptyStall(), visit, ctx ) ).state;
	assert.equal( state.phase, "visitor" );
	assert.equal( state.greeting, "Welcome" );
	assert.deepEqual( state.offers.map( r => [ r.slot, r.bagSlot, r.quantity, r.price ] ), [ [ 2, 20, 5, 300 ] ] );
	assert.deepEqual( state.visitors, [ 7 ] );
	const buy = stall.stallRequest( state, { kind: "stall-buy", slot: 2 } );
	assert.deepEqual( [ buy.frames[0].opcode, ...buy.frames[0].payload ], [ 0x73f9, 2 ] );
	const bought = defined( stall.stallFrame( state, frame( 0xb3f9, [ 1, 2 ] ), ctx ) );
	assert.equal( bought.receive?.[0]?.quantity, 5 );

	const owner = { ...stall.emptyStall(), phase: /** @type {const} */ ("owner"), owner: LOCAL, offers: state.offers };
	const sold = defined( stall.stallFrame( owner, frame( 0x3260, [ 3, 2, ...u16( 3 ), 66, 111, 98, 0xff ] ), ctx ) );
	assert.deepEqual( sold.give, [ { bagSlot: 20, quantity: 5 } ] );
	assert.equal( sold.message?.key, "UIIT_MSG_STREET_STORE_SELL_COMMODITY" );
	assert.equal( sold.message?.args[0], "Bob" );
	assert.equal( sold.state.offers.length, 0 );
	// 74F8F0: the stall closing under a visitor closes the window.
	state = defined( stall.stallFrame( state, frame( 0x33d1, [ ...u32( OWNER ), 0 ] ), ctx ) ).state;
	assert.equal( state.phase, "none" );
});

test("the network lists rows and hands the bought one over", () => {
	let state = stall.stallRequest( stall.emptyStall(), { kind: "stall-network-open", open: true } ).state;
	const search = stall.stallRequest( state, { kind: "stall-network-search", category: 40, page: 1, degree: 3 } );
	assert.deepEqual( [ ...search.frames[0].payload ], [ 0, 1, ...u32( 40 ), 3 ] );
	const row = [ ...u32( POTION ), ...u16( 4 ), ...u32( OWNER ), 0, ...u16( 4 ), 0, ...u64( 900 ), ...u64( 77 ) ];
	state = defined( stall.stallFrame( search.state, frame( 0xb6f9, [ 1, 1, 2, ...row ] ), ctx ) ).state;
	assert.equal( state.network.pages, 2 );
	assert.equal( state.network.rows[0]?.price, 900n );
	const buy = stall.stallRequest( state, { kind: "stall-network-buy", row: 0 } );
	assert.deepEqual( [ ...buy.frames[0].payload ], [
		...u32( OWNER ),
		0,
		...u64( 900 ),
		...u16( 4 ),
		0,
		...u64( 77 )
	] );
	const bought = defined( stall.stallFrame( buy.state, frame( 0xb2ca, [ 1 ] ), ctx ) );
	assert.equal( bought.receive?.[0]?.quantity, 4 );
	assert.equal( bought.state.network.rows.length, 0 );
	// 0x49 outside a town closes the window.
	const town = defined( stall.stallFrame( state, frame( 0xb6f9, [ 2, 0x49 ] ), ctx ) );
	assert.equal( town.state.network.open, false );
});

test("the prompts answer with the native commands", () => {
	const naming = { ...stall.emptyStall(), phase: /** @type {const} */ ("naming") };
	assert.deepEqual( hud.stallPromptCommand( { kind: "title", text: " Shop " }, naming, true, "Welcome" ), {
		kind: "stall-create",
		title: "Shop",
		greeting: "Welcome"
	} );
	// 5A2890 reuses the greeting last typed.
	const remembered = { ...naming, savedGreeting: "Cheap!" };
	assert.equal(
		/** @type {any} */ (hud.stallPromptCommand( { kind: "title", text: "Shop" }, remembered, true, "x" )).greeting,
		"Cheap!"
	);
	assert.deepEqual( hud.stallPromptCommand( { kind: "title", text: "Shop" }, naming, false, "x" ), {
		kind: "stall-name-cancel"
	} );
	const owner = { ...stall.emptyStall(), phase: /** @type {const} */ ("owner") };
	const price = {
		kind: /** @type {const} */ ("price"),
		slot: 1,
		bagSlot: 14,
		carried: 20,
		quantity: "5",
		price: "300",
		modify: false
	};
	assert.deepEqual( hud.stallPromptCommand( price, owner, true, "" ), {
		kind: "stall-add",
		slot: 1,
		bagSlot: 14,
		quantity: 5,
		price: 300
	} );
	assert.equal( hud.stallPromptCommand( { ...price, price: "" }, owner, true, "" ), null );
	assert.deepEqual( hud.stallPromptCommand( { kind: "register" }, owner, false, "" ), {
		kind: "stall-open",
		open: true,
		network: false
	} );
	assert.equal( hud.stallPromptLive( price, { ...owner, open: true } ), false );

	const box = hud.createStallHud();
	box.open( price );
	box.type( "quantity", "99" );
	box.type( "price", "12a3" );
	assert.deepEqual( [ /** @type {any} */ (box.prompt()).quantity, /** @type {any} */ (box.prompt()).price ], [
		"20",
		"123"
	] );
});

test("results sort by a column and reverse on a second press", () => {
	const row = ( /** @type {string} */ name, /** @type {number} */ price ) => ({
		item: { name },
		owner: 1,
		slot: 0,
		quantity: 1,
		price: BigInt( price ),
		serial: 0n
	});
	const rows = /** @type {any} */ ([ row( "b", 30 ), row( "a", 10 ), row( "c", 20 ) ]);
	const box = hud.createStallHud();
	assert.deepEqual( hud.stallNetworkOrder( rows, box.network(), [] ), [ 0, 1, 2 ] );
	box.sortBy( "price" );
	assert.deepEqual( hud.stallNetworkOrder( rows, box.network(), [] ), [ 1, 2, 0 ] );
	box.sortBy( "price" );
	assert.deepEqual( hud.stallNetworkOrder( rows, box.network(), [] ), [ 0, 2, 1 ] );
	box.sortBy( "name" );
	assert.deepEqual( hud.stallNetworkOrder( rows, box.network(), [] ), [ 1, 0, 2 ] );
	assert.equal( box.searchReady( 0 ), true );
	box.searched( 0 );
	assert.equal( box.searchReady( hud.STALL_SEARCH_COOLDOWN_MS - 1 ), false );
	assert.equal( box.searchReady( hud.STALL_SEARCH_COOLDOWN_MS ), true );
});

test("the category tree nests children under their roots", () => {
	const text = "﻿1\tWK_CH_WEAPON\tUIIT_CTL_WK_CH_WEAPON\txxx\t0\t0\r\n" +
		"1\tWK_CH_WEAPON_SWORD\tUIIT_CTL_WK_CH_WEAPON_SWORD\tWK_CH_WEAPON\t1\t8\r\n" +
		"0\tWK_OFF\tUIIT_CTL_WK_OFF\txxx\t0\t0\r\n";
	const bytes = Buffer.from( text, "utf16le" );
	const roots = decodeStallNetworkCategories(
		bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.length )
	);
	assert.equal( roots.length, 1 );
	assert.deepEqual( roots[0]?.children.map( c => [ c.codename, c.id, c.degrees ] ), [ [
		"WK_CH_WEAPON_SWORD",
		1,
		8
	] ] );
	const orphan = Buffer.from( "1\tX\tY\tNOPE\t3\t0\n", "utf16le" );
	assert.throws( () =>
		decodeStallNetworkCategories( orphan.buffer.slice( orphan.byteOffset, orphan.byteOffset + orphan.length ) )
	);
});

test("a stall is visited inside 100 units and approached to 80", () => {
	const pose = { regionId: 0x62a8, x: 0, y: 0, z: 0, angle: 0 };
	const keeper = /** @type {any} */ ({
		kind: "player",
		gid: 9,
		regionId: 0x62a8,
		x: 300,
		y: 0,
		z: 0,
		appearanceState: [ 1, 0, 0, 0, 0, 0, 4 ]
	});
	assert.equal( keepsStall( keeper ), true );
	assert.equal( keepsStall( { ...keeper, appearanceState: [ 1, 0, 0, 0, 0, 0, 0 ] } ), false );
	const walk = defined( interactionApproach( pose, keeper ) );
	assert.equal( Math.round( walk.x ), 220 );
	assert.equal( interactionApproach( { ...pose, x: 210 }, keeper ), null );
});

test("a stall title is held to the abuse filter's words, not the name characters", () => {
	const rules = { allowed: new Set(), forbidden: [ "bad" ], wholeWords: [ "gm" ] };
	assert.equal( textAllowed( "Good Shop!", rules ), true );
	assert.equal( textAllowed( "BADshop", rules ), false );
	assert.equal( textAllowed( "the GM shop", rules ), false );
});

test("stall chat is sent as type 9 and received by name", () => {
	/** @type {{ opcode: number; payload: Uint8Array }[]} */
	const sent = [];
	const chat = createChat( frame => sent.push( frame ) );
	chat.request( stall.STALL_CHAT_CHANNEL, "hi", "", 0 );
	assert.deepEqual( [ ...defined( sent[0] ).payload ], [ 9, 0xff, ...wstr( "hi" ) ] );
	chat.receive( frame( 0xb367, [ 1, 9, 0xff ] ), LOCAL );
	// [9][ascii name][wide message] (753760).
	chat.receive( frame( 0x3667, [ 9, ...u16( 3 ), 66, 111, 98, ...wstr( "yo" ) ] ), LOCAL );
	assert.deepEqual( chat.state().lines.map( line => [ line.channel, line.text, line.outgoing ] ), [
		[ 9, "hi", true ],
		[ 9, "yo", false ]
	] );
});
