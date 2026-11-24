/*
===========================================================================

rental-items.test.mjs - the rental deadline leg on item bodies and its tooltip

The server stamps a rental deadline at delivery (native _RentItemInfo,
#4240); the equipment body of a rental kind carries it as a u64 of unix
milliseconds after the magic options, and the tooltip renders the
remainder with the later client's UIIT_STT_RENT_... rows.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { decodeInventoryItem } = await import( "../../src/engine/foundation/gameplay/inventory-item.ts" );
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { decodeStorageList } = await import( "../../src/engine/foundation/gameplay/storage-room.ts" );

// A dress-kind reference: the equipment band (flags & 0x60 === 0x20).
const DRESS = 7001, FLAGS = 0x2c;

/*
================
rentalBody

The equipment body of the dress: plus, variance, durability, no magic
options, then the rent deadline leg.
================
*/
function rentalBody( deadline ) {
	const body = new Uint8Array( 26 ), view = new DataView( body.buffer );
	view.setUint32( 0, DRESS, true );
	body[4] = 3;
	view.setBigUint64( 5, 123456789n, true );
	view.setUint32( 13, 900, true );
	body[17] = 0;
	view.setBigUint64( 18, BigInt( deadline ), true );
	return body;
}

test("a rental kind's body carries its deadline; the same bytes without the mark stop early", () => {
	const refs = new Map( [ [ DRESS, FLAGS ] ] ), body = rentalBody( 1790000000000 );
	const rented = decodeInventoryItem( body, 0, refs, new Map(), new Set( [ DRESS ] ) );
	assert.equal( rented.next, body.length );
	assert.equal( rented.item?.rentExpiresUnixMs, 1790000000000 );
	// Not taught: the deadline bytes are foreign and the caller refuses
	// the longer body (atomic, like any unknown later-kind body).
	const plain = decodeInventoryItem( body, 0, refs );
	assert.equal( plain.next, 18 );
	assert.equal( plain.item?.rentExpiresUnixMs, undefined );
});

test("a permanent dress body keeps its exact length beside a rented one", () => {
	const refs = new Map( [ [ DRESS, FLAGS ], [ 7002, FLAGS ] ] );
	const rented = decodeInventoryItem( rentalBody( 1790000000000 ), 0, refs, new Map(), new Set( [ DRESS ] ) );
	const plainBody = rentalBody( 1790000000000 ).slice( 0, 18 );
	const v = new DataView( plainBody.buffer );
	v.setUint32( 0, 7002, true );
	const permanent = decodeInventoryItem( plainBody, 0, refs, new Map(), new Set( [ DRESS ] ) );
	assert.equal( permanent.next, plainBody.length );
	assert.equal( permanent.item?.rentExpiresUnixMs, undefined );
	assert.equal( rented.next - permanent.next, 8 );
});

test("a warehouse list row carries the rental deadline to the storage room", () => {
	const list = new Uint8Array( 29 ), v = new DataView( list.buffer );
	list[0] = 30;
	list[1] = 1;
	list[2] = 2;
	list.set( rentalBody( 1790000000000 ), 3 );
	const room = decodeStorageList( list, new Map( [ [ DRESS, FLAGS ] ] ), new Map(), new Set( [ DRESS ] ) );
	assert.equal( room.items.length, 1 );
	assert.equal( room.items[0].rentExpiresUnixMs, 1790000000000 );
	assert.equal( room.items[0].slot, 2 );
});

test("the tooltip renders time remaining, then the over state", () => {
	const english = {
			UIIT_STT_RENT_REMNANTS_TIME: "Time Remains: %d %s %d %s",
			UIIT_STT_RENT_USED_TIME_OVER: "Time Period is over",
			PARAM_HOUR: "hour(s)",
			PARAM_MINUTE: "min(s)"
		},
		text = s => english[s] ?? s;
	const item = {
		slot: 0,
		refObjId: DRESS,
		name: "Rental dress",
		typeFlags: 0xeec,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		tooltip: { fields: { maxStack: 20 } },
		rentExpiresUnixMs: Date.now() + 90_000_000
	};
	const rows = itemTooltip( item, { masteries: [] }, text );
	assert.ok( rows.some( r => /^Time Remains: (24|25) hour\(s\) \d+ min\(s\)$/.test( r.value ) ) );
	const over = itemTooltip( { ...item, rentExpiresUnixMs: Date.now() - 1000 }, { masteries: [] }, text );
	assert.ok( over.some( r => r.value === "Time Period is over" ) );
	const plain = itemTooltip( { ...item, name: "Dress", rentExpiresUnixMs: undefined }, { masteries: [] }, text );
	assert.ok( !plain.some( r => r.value.includes( "Time Remains" ) ) );
});
