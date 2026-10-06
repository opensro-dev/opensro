/*
===========================================================================

npc-service-shop.test.mjs - ordinary and special-trade shop wire contracts

Exercises the shipped inventory request producer and native acknowledgement
consumer together. The selected capability must survive both directions.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { npcInteractionMask } = await import( "../../src/engine/foundation/gameplay/npc-dialogue.ts" );

test("shop requests preserve the native special-trade capability", () => {
	for ( const capabilities of [ 1, 0x800, 0x801, 0x1801 ] ) {
		const sent = [], owner = createInventory( frame => sent.push( frame ) );
		owner.bootstrap( { inventorySlotCount: 109, equipmentSlotCount: 13 } );
		const request = owner.openShop( 17, 0, capabilities );
		const expected = capabilities & 0x800 ? 0x800 : 1;
		assert.equal( request.opcode, 0x7338 );
		assert.equal( new DataView( request.payload.buffer ).getUint32( 0, true ), 17 );
		assert.equal( new DataView( request.payload.buffer ).getUint32( 4, true ), expected );
		assert.deepEqual( sent, [ request ] );
		const ack = new Uint8Array( expected === 0x800 ? 6 : 5 );
		ack[0] = 1;
		new DataView( ack.buffer ).setUint32( 1, expected, true );
		assert.equal( npcInteractionMask( ack, capabilities ), expected | (capabilities & 0x1400) );
	}
});
