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
(0xB053) refusal, or null when the answer is not a refusal. A Gori
exchange answer (0xB1BE) with flag 2 always shows the fixed category-1
notice 2. Purely the native mapper: 0xB5BD and 0xB06D route identically
for every byte (the port-only rename scroll's lines ride on top in
gameplay.ts, #1689).
================
*/
export function inventoryNotice(
	opcode: number,
	payload: Uint8Array,
	country?: number,
	itemMallOpen = false
): SystemNotice | null {
	// 766A42..766A79: B053 sends every non-1 result to category 1. The
	// refusal body is [2][u8 code]; the notice rides on payload[1]. The
	// success body stays [1][u8]. 766A90: a 0xB1BE answer whose flag is
	// 2 shows the fixed category-1 notice 2, whatever its second byte
	// holds; any other exchange answer is silent here.
	// Ordinary inventory/item-use acknowledgements recognize result 2 only.
	if ( opcode === 0xb053 ) {
		if ( payload[0] === 1 ) {
			if ( payload.length !== 2 ) throw Error( "Invalid Magic Pang result" );
			return null;
		}
		if ( payload.length !== 2 ) throw Error( "Invalid Magic Pang result" );
		const refused = resolveNativeNotice( 1, payload[1]!, { country, itemMallOpen } );
		return refused.kind === "notice" ? refused.notice : null;
	}
	if ( opcode === 0xb1be ) {
		if ( payload.length !== 2 ) throw Error( "Invalid Magic Pang exchange result" );
		if ( payload[0] !== 2 ) return null;
		const refused = resolveNativeNotice( 1, 2, { country, itemMallOpen } );
		return refused.kind === "notice" ? refused.notice : null;
	}
	if ( (opcode !== 0xb06d && opcode !== 0xb5bd) || payload[0] !== 2 ) return null;
	if ( payload.length !== 2 ) throw Error( "Invalid inventory rejection" );
	const result = resolveNativeNotice( 1, payload[1]!, { country, itemMallOpen } );
	return result.kind === "notice" ? result.notice : null;
}
