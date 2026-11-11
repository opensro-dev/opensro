/*
===========================================================================

trade-progress.ts - the caravan's distance to the next star (port-only 0x3FC0, not native)

Under SRO_TRADE_TIERS (#3163) the server publishes, with the star
answer (0x352D), the cargo's stars and the cargo gold still missing
from the next star, both from its own tier function, so the bag and
the specialty deal window can name the distance in stacks of the
goods in front of the player and never disagree with the server.
Nothing arrives with the flag off, and a client that predates the
frame drops the unknown opcode.

Body, little-endian: u8 stars, u32 gold missing from the next star.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const OP_TRADE_PROGRESS = 0x3fc0;

/*
================
TradeProgress

One published answer: the cargo's stars and the gold short of the
next one (0 at five stars).
================
*/
export interface TradeProgress {
	readonly stars: number;
	readonly gap: number;
}

/*
================
createTradeProgress
================
*/
export function createTradeProgress() {
	let progress: TradeProgress | null = null;
	return {
		/*
================
receive

Consumes a progress frame; false for any other opcode. A malformed
body is a protocol error, never a partial answer.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_TRADE_PROGRESS ) return false;
			const p = frame.payload;
			if ( p.length !== 5 ) throw new Error( "Invalid trade progress" );
			progress = {
				stars: p[0]!,
				gap: new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true )
			};
			return true;
		},
		state: () => progress,
		/*
================
clear
================
		*/
		clear() {
			progress = null;
		}
	};
}
