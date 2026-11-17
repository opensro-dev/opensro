/*
===========================================================================

recall-appointment.ts - the recall point appointment

The 0x720D request a recall NPC dialog sends and the notice its 0xB20D
answer shows.

===========================================================================
*/

import type { WireFrame } from "@/engine/contracts/network";
import type { SystemNotice } from "./system-notices";

/*
================
recallAppointmentRequest

Retail 6971B0, dialog kind 5 / affirmative button 1.
================
*/
export function recallAppointmentRequest( gid: number ): WireFrame {
	if ( !Number.isInteger( gid ) || gid <= 0 || gid > 0xffffffff ) throw Error( "Invalid recall NPC identity" );
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, gid, true );
	return { opcode: 0x720d, payload };
}

/*
================
recallAppointmentNotice

75DA30: success publishes BOTH type-5 guide text and a notice; any other
result consumes a detail byte silently. Never infer appointment from a click.
================
*/
export function recallAppointmentNotice( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0xb20d ) return null;
	if ( p.length === 0 || p.length !== (p[0] === 1 ? 1 : 2) ) throw Error( "Invalid recall appointment response" );
	return p[0] === 1 ? { key: "UIIT_MSG_STATE_REBIRTH_POINT_APPOINT", value: 0, nativeType: 5, banner: true } : null;
}
