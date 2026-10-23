/*
===========================================================================

gold-group.test.mjs - gold heaps and the shop tokens that share their group

Group 0x280 (TID 3.3.5) is one wire width but two meanings: gold heaps
(TID4 0) and the later shop tokens (TID4 1, ITEM_ETC_SD_TOKEN_*), which
the native gold predicate excludes. No game data is read.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { decodeGroundItem, isGoldGroupType, isGoldHeapType } = await import(
	"../../src/engine/foundation/gameplay/ground-item.ts"
);
const { groundItemName } = await import( "../../src/engine/foundation/ui/ground-item-label.ts" );

const GOLD = 0xc | 0x60 | 0x280, SD_TOKEN = 0xc | 0x60 | 0x280 | 0x800, POTION = 0xc | 0x60 | 0x080;

test("the whole group carries the u32 amount; only a heap is gold", () => {
	assert.equal( isGoldGroupType( GOLD ), true );
	assert.equal( isGoldGroupType( SD_TOKEN ), true );
	assert.equal( isGoldGroupType( POTION ), false );
	assert.equal( isGoldHeapType( GOLD ), true );
	assert.equal( isGoldHeapType( SD_TOKEN ), false );
});

/*
================
groundSpawn

A ground-item spawn row (CIItem_ParseSpawnPacket 86E4E0) for a gold-group
item: refObjId, the group's u32 amount, gid, region, position, heading, no
owner and no tint.
================
*/
function groundSpawn( refObjId, amount ) {
	const p = new Uint8Array( 4 + 4 + 4 + 2 + 12 + 2 + 1 + 1 ), v = new DataView( p.buffer );
	v.setUint32( 0, refObjId, true );
	v.setUint32( 4, amount, true );
	v.setUint32( 8, 900, true );
	v.setUint16( 12, 25000, true );
	return p;
}

test("a dropped token is labelled by its name, a heap by its amount", () => {
	const heap = decodeGroundItem( groundSpawn( 1, 1500 ), GOLD, false, "Gold" );
	const token = decodeGroundItem( groundSpawn( 42864, 0 ), SD_TOKEN, false, "Silver Coin" );
	assert.equal( groundItemName( heap, "Gold" ), "1500 Gold" );
	assert.equal( groundItemName( token, "Gold" ), "Silver Coin" );
});
