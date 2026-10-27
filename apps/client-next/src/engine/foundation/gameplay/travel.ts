/*
===========================================================================

travel.ts - the native travel packets: travel modes, reset regions and the
0x7495 teleport requests

Pure codecs over wire frames. travelMode reads which loading screen a
frame starts, resetTravelRegion the region a reset names, and gateRequest
writes the talk window's teleport requests in their native widths
(NetClient_Send7495 6FEF10: a u32 target for type 2, a u8 for 3 and 5,
none for type 1).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { WorldTravel } from "@/engine/contracts/world";

/*
================
isReturnScroll

The type word of a return scroll.
================
*/
export function isReturnScroll( word: number ) {
	return !(word & 2) && (word & 0x1c) === 12 && (word & 0x60) === 0x60 && (word & 0x780) === 0x180 &&
		[ 1, 3 ].includes( word >>> 11 );
}

/*
================
travelMode

777B60: only the local life-state death branch sets rebirth mode.
755E40: successful return-scroll casting chooses mode 2.
================
*/
export function travelMode( frame: WireFrame, localGid: number, itemCooldownMs = 0 ): WorldTravel["mode"] | null {
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( frame.opcode === 0x3122 ) {
		if ( p.length !== 6 ) throw Error( "Invalid travel character state" );
		return localGid !== 0 && v.getUint32( 0, true ) === localGid && p[4] === 0 && p[5] === 2 ? 1 : null;
	}
	if ( frame.opcode === 0xb5bd && p[0] === 1 ) {
		if ( p.length !== 6 ) throw Error( "Invalid travel item reply" );
		const word = v.getUint16( 4, true );
		if ( isReturnScroll( word ) ) return 2;
		if (
			!(word & 2) && (word & 0x1c) === 12 && (word & 0x60) === 0x60 && (word & 0x780) === 0x680 &&
			(word >>> 11) === 9 && itemCooldownMs === 0
		) return 6;
	}
	return null;
}

/*
================
resetTravelRegion
================
*/
export function resetTravelRegion( frame: WireFrame ): number | null {
	if ( frame.opcode !== 0x3369 && frame.opcode !== 0x366a ) return null;
	if ( frame.payload.length !== 2 ) throw Error( "Invalid reset region packet" );
	return new DataView( frame.payload.buffer, frame.payload.byteOffset, 2 ).getUint16( 0, true );
}

// Type 1 is row 0x10's return point (no target), 2 a destination, 5 the
// reverse return's point; travel-instance is 5DA1B0 action 0x2A, type 3.
export type GateCommand = {
	readonly kind: "travel-gate";
	readonly gid: number;
	readonly type: 1 | 2 | 5;
	readonly target: number;
} | { readonly kind: "travel-instance"; readonly gid: number; readonly target: number; };

/*
================
gateRequest

One 0x7495 request: [u32 gid][u8 type], then the type's target. Type 1
carries none, so its target must be 0.
================
*/
export function gateRequest( command: GateCommand ): WireFrame {
	const type = command.kind === "travel-instance" ? 3 : command.type;
	const targetMax = type === 2 ? 0xffffffff : type === 1 ? 0 : 255;
	if (
		!Number.isInteger( command.gid ) || command.gid < 1 || command.gid > 0xffffffff ||
		![ 1, 2, 3, 5 ].includes( type ) || !Number.isInteger( command.target ) || command.target < 0 ||
		command.target > targetMax
	) throw Error( "Invalid native travel target" );
	const payload = new Uint8Array( type === 2 ? 9 : type === 1 ? 5 : 6 ), v = new DataView( payload.buffer );
	v.setUint32( 0, command.gid, true );
	payload[4] = type;
	if ( type === 2 ) v.setUint32( 5, command.target, true );
	else if ( type !== 1 ) payload[5] = command.target;
	return { opcode: 0x7495, payload };
}
