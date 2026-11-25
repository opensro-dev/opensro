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
const { decodeCosRecord } = await import( "../../src/engine/foundation/gameplay/cos-record.ts" );
const { cosGrantResult } = await import( "../../src/engine/foundation/gameplay/cos-container.ts" );

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

test("a permanent row of a rental kind reads its zero leg as no deadline", () => {
	// The r204l outage: a permanent Halloween avatar's body now carries
	// the leg as 0; it must decode to the full length and not read as an
	// expired rental.
	const refs = new Map( [ [ DRESS, FLAGS ] ] ), body = rentalBody( 0 );
	const permanent = decodeInventoryItem( body, 0, refs, new Map(), new Set( [ DRESS ] ) );
	assert.equal( permanent.next, body.length );
	assert.equal( permanent.item?.rentExpiresUnixMs, undefined );
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

/*
================
cosRecordWith

An owned COS record (830EC0, the layout packet-variants.test.mjs pins)
whose bag holds the given slots, each a dress body.
================
*/
function cosRecordWith( rows ) {
	const head = new Uint8Array( 18 ), v = new DataView( head.buffer );
	v.setUint32( 0, 7, true );
	v.setUint32( 4, 102, true );
	v.setUint32( 8, 100, true );
	v.setUint32( 12, 50, true );
	head[16] = 28;
	head[17] = rows.length;
	const parts = [ head ];
	for ( const [ slot, body ] of rows ) parts.push( Uint8Array.of( slot ), body );
	parts.push( new Uint8Array( 4 ) );
	const out = new Uint8Array( parts.reduce( ( n, part ) => n + part.length, 0 ) );
	let o = 0;
	for ( const part of parts ) {
		out.set( part, o );
		o += part.length;
	}
	return out;
}

test("a pet bag's rental-kind rows read their leg, permanent or rented", () => {
	// Every row of a rental kind carries the leg (server wire rentrefs.go):
	// a pet holding permanent avatars was refused at entry with
	// "Duplicate COS inventory slot" when the bag read them without it.
	const objRefs = new Map( [ [ 102, 0x11c6 ] ] ), refs = new Map( [ [ DRESS, FLAGS ] ] );
	const packet = cosRecordWith( [ [ 0, rentalBody( 0 ) ], [ 1, rentalBody( 1790000000000 ) ] ] );
	const record = decodeCosRecord( packet, objRefs, refs, new Set( [ DRESS ] ) );
	assert.deepEqual( record?.inventory?.map( row => [ row.slot, row.rentExpiresUnixMs ] ), [
		[ 0, undefined ],
		[ 1, 1790000000000 ]
	] );
	assert.throws( () => decodeCosRecord( packet, objRefs, refs ) );
	// A grant into the bag (0xB06D type 0x15) reads the leg too.
	const grant = new Uint8Array( 7 + 26 ), g = new DataView( grant.buffer );
	grant[0] = 1;
	grant[1] = 0x15;
	g.setUint32( 2, 7, true );
	grant[6] = 2;
	grant.set( rentalBody( 0 ), 7 );
	const granted = cosGrantResult( record, grant, refs, new Set( [ DRESS ] ) );
	assert.deepEqual( granted.inventory?.map( row => row.slot ), [ 0, 1, 2 ] );
	assert.throws( () => cosGrantResult( record, grant, refs ) );
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
