/*
===========================================================================

most-wanted.ts - the local player's Most Wanted state (port-only 0x3FB2)

#1849, not native. While the server's SRO_MOST_WANTED rule is on, the
streak holder receives its own wanted state at world entry, on every
counted kill, when the job suit goes on over a saved streak, and once
more cleared when the streak ends. This owner decodes the latest state
for the buff board; nothing arrives when the rule is off.

Body, little-endian: u8 level, u8 killsIntoLevel, u8 killsPerLevel,
u8 job (1 trader, 2 thief, 3 hunter), u8 damagePercent.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { MostWantedState } from "@/engine/contracts/most-wanted";

export const OP_MOST_WANTED_STATE = 0x3fb2;
const STATE_BYTES = 5;
const MAX_LEVEL = 10;
const MAX_JOB = 3;
const MAX_DAMAGE_PERCENT = 3;

/*
================
createMostWantedState
================
*/
export function createMostWantedState() {
	let state: MostWantedState | null = null;
	return {
		/*
================
receive

Consumes a wanted-state frame; false for any other opcode. A cleared
streak (level 0, or no dressed job) publishes null, so the row is gone.
A malformed state is a protocol error, never a partial row.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_MOST_WANTED_STATE ) return false;
			const p = frame.payload;
			if ( p.length !== STATE_BYTES ) throw new Error( "Invalid most wanted state" );
			const level = p[0]!, killsIntoLevel = p[1]!, killsPerLevel = p[2]!, job = p[3]!, damagePercent = p[4]!;
			if (
				level > MAX_LEVEL || job > MAX_JOB || killsPerLevel === 0 ||
				killsIntoLevel > killsPerLevel || damagePercent > MAX_DAMAGE_PERCENT
			) {
				throw new Error( "Invalid most wanted state" );
			}
			state = level === 0 || job === 0 ?
				null :
				{ level, killsIntoLevel, killsPerLevel, job, damagePercent };
			return true;
		},
		current: () => state,
		/*
================
clear
================
		*/
		clear() {
			state = null;
		}
	};
}
