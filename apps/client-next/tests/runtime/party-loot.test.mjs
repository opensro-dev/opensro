/*
===========================================================================

party-loot.test.mjs - the party loot notice (0x317D)

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import "../helpers/native-source-loader.mjs";

const { partyLootNotice } = await import( "../../src/engine/foundation/gameplay/party-loot.ts" );

// Expendable (class 0xC, category 0x60); gold is group 0x280.
const GOLD = 0xc | 0x60 | 0x280, POTION = 0xc | 0x60 | 0x080, BLADE = 0x0c00;
const items = new Map( [ [ 1, { typeFlags: GOLD, name: "Gold" } ], [ 2, { typeFlags: POTION, name: "HP Potion" } ], [
	3,
	{ typeFlags: BLADE, name: "Blade" }
] ] );
const context = {
	item: id => items.get( id ),
	memberName: gid => gid === 77 ? "Friend" : gid === 5 ? "Me" : undefined,
	localGid: 5
};

/*
================
frame

[u32 recipient][u32 ref] then the amount bytes.
================
*/
function frame( recipient, ref, amount ) {
	const p = new Uint8Array( 8 + amount.length ), v = new DataView( p.buffer );
	v.setUint32( 0, recipient, true );
	v.setUint32( 4, ref, true );
	p.set( amount, 8 );
	return p;
}

test("a shared gold heap prints the distributed amount and its recipient", () => {
	const notice = partyLootNotice( frame( 77, 1, [ 0x10, 0x27, 0, 0 ] ), context );
	assert.equal( notice.key, "UIIT_MSG_PARTYGET_GOLD" );
	assert.deepEqual( notice.arguments, [ "10000", "Friend" ] );
	assert.equal( notice.nativeType, 4 );
});

test("a shared stack names the member, or reads as gained on the recipient's own client", () => {
	const other = partyLootNotice( frame( 77, 2, [ 3, 0 ] ), context );
	assert.equal( other.key, "UIIT_MSG_PARTYGET_ITEM_EXPENDABLE" );
	assert.deepEqual( other.arguments, [ "HP Potion", "3", "Friend" ] );
	const own = partyLootNotice( frame( 5, 2, [ 3, 0 ] ), context );
	assert.equal( own.key, "UIIT_MSG_STATE_GET_ITEM_EXPENDABLE" );
	assert.equal( own.nativeType, 1 );
});

test("equipment carries a one-byte amount and an unknown item is refused", () => {
	assert.deepEqual( partyLootNotice( frame( 77, 3, [ 1 ] ), context ).arguments, [ "Blade", "1", "Friend" ] );
	assert.throws( () => partyLootNotice( frame( 77, 3, [ 1, 0 ] ), context ), /length/ );
	assert.throws( () => partyLootNotice( frame( 77, 9, [ 1 ] ), context ), /unknown item/ );
});
