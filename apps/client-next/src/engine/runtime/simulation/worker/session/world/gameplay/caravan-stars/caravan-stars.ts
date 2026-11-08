/*
===========================================================================

caravan-stars.ts - the 5-star caravan roster (port-only 0x3FBF, not native)

Under SRO_TRADE_TIERS (#2958) the server pushes the division's 5-star
caravan transport gids every two seconds, and the transport's nameplate
shows its star (overhead-layout.ts). Nothing arrives with the flag off,
and a client that predates the frame drops the unknown opcode. 0x3FBB
stays the pet-crow visual's (entities.ts): the two frames must never
share an opcode, because each owner throws on the other's body.

Body, little-endian: u16 count, then count x { u32 gid }.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const OP_CARAVAN_STARS = 0x3fbf;
const MAX_CARAVANS = 1024;

/*
================
createCaravanStars
================
*/
export function createCaravanStars() {
	let gids: readonly number[] = [];
	return {
		/*
================
receive

Consumes a roster frame; false for any other opcode. A malformed roster
is a protocol error, never a partial list.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_CARAVAN_STARS ) return false;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( p.length < 2 ) throw new Error( "Invalid caravan stars" );
			const count = v.getUint16( 0, true );
			if ( count > MAX_CARAVANS || p.length !== 2 + count * 4 ) {
				throw new Error( "Invalid caravan star count" );
			}
			const next: number[] = [];
			for ( let i = 0; i < count; i++ ) {
				next.push( v.getUint32( 2 + i * 4, true ) );
			}
			gids = next;
			return true;
		},
		gids: () => gids,
		/*
================
clear
================
		*/
		clear() {
			gids = [];
		}
	};
}
