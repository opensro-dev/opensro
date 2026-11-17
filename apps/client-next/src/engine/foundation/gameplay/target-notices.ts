/*
===========================================================================

target-notices.ts - selection refusals as notices

The 0xB45A selection refusal; it bypasses the 689420 dispatcher.

===========================================================================
*/

import type { SystemNotice } from "./system-notices";

/*
================
targetNotice

764C60 / 7651F1: selection refusals bypass the 689420 dispatcher.
v1.150 reads one error byte; only error 7 writes the type-5 guide log.
================
*/
export function targetNotice( opcode: number, payload: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0xb45a || payload[0] !== 2 ) return null;
	if ( payload.length !== 2 ) throw Error( "Invalid target rejection" );
	return payload[1] === 7 ? { key: "UIIT_MSG_STRGERR_CANT_SWAP_JOBITEM", value: 0, nativeType: 5 } : null;
}
