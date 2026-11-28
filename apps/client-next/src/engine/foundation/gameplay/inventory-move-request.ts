/*
===========================================================================

inventory-move-request.ts - the native 0x706D bag/equipment slot move

The one encoder of a plain slot move (movement type 0, client
ItemMoveRequest_Serialize 0x697f7c): the inventory owner sends it for a
drag, and the crowd bench's peers send it to take clothes off and on.
Equipment sockets are slots 0..12, the bag starts at 13.

===========================================================================
*/

import { ITEM_MOVE_REQUEST_OPCODE } from "@/engine/contracts/opcodes";

const MOVE_INVENTORY = 0;

/*
================
inventoryMoveRequest

[u8 0][u8 source][u8 destination][u16 quantity]; the caller has checked
the slots and the quantity.
================
*/
export function inventoryMoveRequest( source: number, destination: number, quantity: number ) {
	const p = new Uint8Array( 5 );
	p[0] = MOVE_INVENTORY;
	p[1] = source;
	p[2] = destination;
	new DataView( p.buffer ).setUint16( 3, quantity, true );
	return { opcode: ITEM_MOVE_REQUEST_OPCODE, payload: p };
}
