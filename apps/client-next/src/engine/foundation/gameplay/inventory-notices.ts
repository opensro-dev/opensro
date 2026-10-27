/*
===========================================================================

inventory-notices.ts - inventory and item-use refusals as notices

75A3D0 / 755E40 -> 689420 category 1; packet boundaries stay here.

===========================================================================
*/
import { resolveNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

/*
================
inventoryNotice

The notice for an inventory (0xB06D), item-use (0xB5BD) or Magic Pang
(0xB053) refusal, or null when the answer is not a refusal. Purely the
native mapper: 0xB5BD and 0xB06D route identically for every byte (the
port-only rename scroll's lines ride on top in gameplay.ts, #1689).
================
*/
export function inventoryNotice(
	opcode: number,
	payload: Uint8Array,
	country?: number,
	itemMallOpen = false
): SystemNotice | null {
	// 766A42..766A79: B053 sends every non-1 result to category 1.
	// Ordinary inventory/item-use acknowledgements recognize result 2 only.
	if ( opcode === 0xb053 ) {
		if ( payload.length !== 2 ) throw Error( "Invalid Magic Pang result" );
		if ( payload[0] === 1 ) return null;
	} else if ( (opcode !== 0xb06d && opcode !== 0xb5bd) || payload[0] !== 2 ) return null;
	if ( payload.length !== 2 ) throw Error( "Invalid inventory rejection" );
	const result = resolveNativeNotice( 1, payload[1]!, { country, itemMallOpen } );
	return result.kind === "notice" ? result.notice : null;
}
