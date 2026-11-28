/*
===========================================================================

cos-sale.test.mjs - the stable keeper's sale of the active transport

Pins cos-sale.ts against CIFMerchant_OnSlotTransfer (5B6CA0): the refusal
order before the sale box, the 0x71DC body the box sends (6FF980) and the
0xB1DC answer (7782E0). Also the presentation catalog's sale map, which
carries the row's CanSell and SellPrice.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const sale = await import( "../../src/engine/foundation/ui/cos-sale.ts" );
const cos = await import( "../../src/engine/foundation/ui/cos-command.ts" );

const DONKEY = { gid: 9, refObjId: 3910, band: 2, hp: 100, mp: 0, status: 0, dead: false, inventory: [] };
const DONKEY_REF = {
	icon: "",
	maxHp: 100,
	rideable: true,
	physicalDefence: 0,
	magicalDefence: 0,
	parry: 0,
	hit: 0,
	skills: [],
	sellPrice: 240
};
const STABLE = sale.COS_DEALER_CAPABILITY | 1;

function verdict( change ) {
	return sale.cosSaleVerdict( {
		capabilities: STABLE,
		riding: false,
		record: DONKEY,
		reference: DONKEY_REF,
		...change
	} );
}

test("an empty, sellable transport at a stable keeper opens the sale box at its SellPrice", () => {
	assert.deepEqual( verdict( {} ), { kind: "confirm", gid: 9, refObjId: 3910, price: 240 } );
});

test("5B6CA0 refuses in its own order", () => {
	const refuse = ( category, code ) => ({ kind: "refuse", category, code });
	// An NPC without the transport-dealer bit refuses first, whatever else holds.
	assert.deepEqual( verdict( { capabilities: 1, riding: true, record: undefined } ), refuse( 1, 0x45 ) );
	// Riding is checked before the record.
	assert.deepEqual( verdict( { riding: true, record: undefined } ), refuse( 0xc, 9 ) );
	assert.deepEqual( verdict( { record: undefined } ), refuse( 0xc, 0xa ) );
	// Goods aboard are checked before CanSell.
	assert.deepEqual(
		verdict( {
			record: { ...DONKEY, inventory: [ { slot: 0 } ] },
			reference: { ...DONKEY_REF, sellPrice: undefined }
		} ),
		refuse( 0xc, 0xb )
	);
	const { sellPrice, ...unsellable } = DONKEY_REF;
	assert.equal( sellPrice, 240 );
	assert.deepEqual( verdict( { reference: unsellable } ), refuse( 0xc, 0xa ) );
	assert.deepEqual( verdict( { reference: undefined } ), refuse( 0xc, 0xa ) );
});

test("the box sends 0x71DC [u32 target][u8 3] and reads 0xB1DC", () => {
	assert.deepEqual( [ ...sale.cosSaleRequest( 0x01020304 ) ], [ 4, 3, 2, 1, 3 ] );
	assert.deepEqual( sale.cosSaleResult( Uint8Array.of( 1, 3 ) ), { sold: true } );
	assert.deepEqual( sale.cosSaleResult( Uint8Array.of( 2, 0xb ) ), { sold: false, code: 0xb } );
	assert.throws( () => sale.cosSaleResult( Uint8Array.of( 1 ) ) );
	assert.throws( () => sale.cosSaleResult( Uint8Array.of( 3, 0 ) ) );
});

test("the catalog's sale map sets sellPrice only on the rows that can be sold", () => {
	const references = cos.decodeCosReferences( {
		format: "sro-cos-presentation",
		rows: { 3910: [ "", 100, true, 0, 0, 0, 0, [] ], 2191: [ "", 983, true, 0, 0, 0, 0, [] ] },
		sale: { 3910: 240 }
	} );
	assert.equal( references.get( 3910 ).sellPrice, 240 );
	assert.equal( "sellPrice" in references.get( 2191 ), false );
	assert.throws( () =>
		cos.decodeCosReferences( {
			format: "sro-cos-presentation",
			rows: { 3910: [ "", 100, true, 0, 0, 0, 0, [] ] },
			sale: { 3910: -1 }
		} )
	);
});
