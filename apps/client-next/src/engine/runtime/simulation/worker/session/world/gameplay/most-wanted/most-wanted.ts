/*
===========================================================================

most-wanted.ts - the most-wanted state (port-only 0x3FBA, not native)

With the server's SRO_MOST_WANTED on, a streak holder's own client is
told the state the buff-board row renders after every counted kill, at
world entry and on a dress or undress. This owner decodes the latest
state; every rule (pacing, decay, bonuses) is the server's, so the row
only draws what arrives here. Nothing arrives when the rule is off.

Body, little-endian: u8 level, u8 kills into the level, u8 dressed
job, u8 kills per level, u64 bounty gold, u64 ms until the next decay
step (0 = none stamped). The decay is a duration the server measured,
never a clock time: the state is stamped with the frame clock at
receipt and counted down from there, so a skewed client clock cannot
age the row early.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { MostWantedState } from "@/engine/contracts/most-wanted";

export const OP_MOST_WANTED_STATE = 0x3fba;
// Mirrors the server's mostWantedMaxLevel (action/mostwanted.go).
const MAX_LEVEL = 10;
const STATE_BYTES = 20;

/*
================
createMostWanted
================
*/
export function createMostWanted() {
	let state: MostWantedState | undefined;
	return {
		/*
================
receive

Consumes a state frame; false for any other opcode. atMs is the
worker's frame clock, stamped onto the state for the countdown. A
malformed state is a protocol error, never a partial one.
================
		*/
		receive( frame: WireFrame, atMs: number ): boolean {
			if ( frame.opcode !== OP_MOST_WANTED_STATE ) return false;
			const p = frame.payload;
			if ( p.length !== STATE_BYTES ) throw Error( "Invalid most wanted state length" );
			const view = new DataView( p.buffer, p.byteOffset, p.byteLength );
			const level = p[0]!, kills = p[1]!, job = p[2]!, perLevel = p[3]!;
			if ( level > MAX_LEVEL || job > 3 || perLevel < 1 || kills > perLevel ) {
				throw Error( "Invalid most wanted state" );
			}
			state = {
				level,
				killsIntoLevel: kills,
				dressedJob: job,
				killsPerLevel: perLevel,
				bounty: Number( view.getBigUint64( 4, true ) ),
				decayInMs: Number( view.getBigUint64( 12, true ) ),
				receivedAtMs: atMs
			};
			return true;
		},
		current: () => state,
		/*
================
clear
================
		*/
		clear() {
			state = undefined;
		}
	};
}
