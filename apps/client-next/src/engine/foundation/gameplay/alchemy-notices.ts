/*
===========================================================================

alchemy-notices.ts - what a refused alchemy answer tells the player

The reinforce answers (0xB373 elixir, 0xB651 attribute and magic stones)
report a refusal as [2, code]. The native handlers print it as a category
0x12 notice (689420): 7500F0 for every 0xB373 code, 7503D0 for every
0xB651 code but 0x23, the failed roll the alchemy window itself shows
(62B0B0 with flag 0x80, AlchemyOutcome).

The compound answers (0xB16F making a stone from a tablet, 0xB549
dissolving one) refuse the same way, and their native handler prints the
code as a category 0x17 notice. A compound refusal the client stored
but never showed left the Fuse button looking dead (BR-261022-1735-82BB).

The Reinforce page refuses a drop locally with the same category 0x12
table (62B6D0 calls CGInterface_ShowSystemNotification(0x12, code)), so
its refusals come from here too (reinforcePageNotice).

===========================================================================
*/
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";
import {
	ALCHEMY_STEP_RESULT_OPCODE,
	ALCHEMY_STONE_RESULT_OPCODE,
	COMPOUND_RESULT_OPCODE,
	DISSOLVE_RESULT_OPCODE,
	REINFORCE_RESULT_OPCODE
} from "@/engine/contracts/opcodes";

const ALCHEMY_NOTICE_CATEGORY = 0x12;
const COMPOUND_NOTICE_CATEGORY = 0x17;
const ALCHEMY_REFUSED = 2;
// 7503D0's code that the window shows as a failed roll, not as a notice.
const MAGIC_STONE_FAILED_ROLL = 0x23;
// 62B6D0's local refusals on the empty Reinforce page: anything but
// reinforceable equipment (62B801), and a fortress hammer (62B7C8).
export const REINFORCE_LOAD_EQUIP_FIRST = 0x15;
export const REINFORCE_TARGET_CANT_BE_REINFORCED = 0x0f;

/*
================
alchemyNotice

The notice a refused alchemy answer shows, or null for any other frame.
The answer's own length is checked by its inventory owner.
================
*/
export function alchemyNotice( opcode: number, payload: Uint8Array ): SystemNotice | null {
	// 0x3359 (a timed batch step's answer) shares 0xB16F's handler 74FF50.
	if (
		opcode === COMPOUND_RESULT_OPCODE || opcode === ALCHEMY_STEP_RESULT_OPCODE || opcode === DISSOLVE_RESULT_OPCODE
	) {
		if ( payload.length !== 2 || payload[0] !== ALCHEMY_REFUSED ) return null;
		return constantNativeNotice( COMPOUND_NOTICE_CATEGORY, payload[1]! );
	}
	if ( opcode !== REINFORCE_RESULT_OPCODE && opcode !== ALCHEMY_STONE_RESULT_OPCODE ) return null;
	if ( payload.length !== 2 || payload[0] !== ALCHEMY_REFUSED ) return null;
	const code = payload[1]!;
	if ( opcode === ALCHEMY_STONE_RESULT_OPCODE && code === MAGIC_STONE_FAILED_ROLL ) return null;
	return constantNativeNotice( ALCHEMY_NOTICE_CATEGORY, code );
}

/*
================
reinforcePageNotice

The notice the Reinforce page raises when it refuses a drop itself; no
packet is sent. code is one of the REINFORCE_ constants above.
================
*/
export function reinforcePageNotice( code: number ): SystemNotice | null {
	return constantNativeNotice( ALCHEMY_NOTICE_CATEGORY, code );
}
