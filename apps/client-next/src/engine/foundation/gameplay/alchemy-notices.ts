/*
===========================================================================

alchemy-notices.ts - what a refused reinforcement tells the player

The reinforce answers (0xB373 elixir, 0xB651 attribute and magic stones)
report a refusal as [2, code]. The native handlers print it as a category
0x12 notice (689420): 7500F0 for every 0xB373 code, 7503D0 for every
0xB651 code but 0x23, the failed roll the alchemy window itself shows
(62B0B0 with flag 0x80, AlchemyOutcome).

===========================================================================
*/
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

const ALCHEMY_NOTICE_CATEGORY = 0x12;
const ALCHEMY_REFUSED = 2;
// 7503D0's code that the window shows as a failed roll, not as a notice.
const MAGIC_STONE_FAILED_ROLL = 0x23;

/*
================
alchemyNotice

The notice a refused reinforcement answer shows, or null for any other
frame. The answer's own length is checked by its inventory owner.
================
*/
export function alchemyNotice( opcode: number, payload: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0xb373 && opcode !== 0xb651 ) return null;
	if ( payload.length !== 2 || payload[0] !== ALCHEMY_REFUSED ) return null;
	const code = payload[1]!;
	if ( opcode === 0xb651 && code === MAGIC_STONE_FAILED_ROLL ) return null;
	return constantNativeNotice( ALCHEMY_NOTICE_CATEGORY, code );
}
