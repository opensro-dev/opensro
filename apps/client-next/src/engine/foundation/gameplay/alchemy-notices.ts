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

// 620DD0 refuses locally at six charges of the stone's own option.
const PROTECTION_STONE_MAX = 6;
const PROTECTION_STONE_NOTICE_CATEGORY = 0x16;

/*
================
protectionStone

The protection stone a codename names (620DD0 compares case-insensitively
against the item-data codename), with the option it charges and the
already-max notice code for that option.
================
*/
function protectionStone( stoneCodename: string ): { option: string; code: number; } | null {
	switch ( stoneCodename.toUpperCase() ) {
		case "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA":
			return { option: "MATTR_ATHANASIA", code: 0x65 };
		case "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK":
			return { option: "MATTR_LUCK", code: 0x64 };
		case "ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID":
			return { option: "MATTR_SOLID", code: 0x66 };
		default:
			return null;
	}
}

/*
================
alchemyProtectionStoneRefusal

The notice a Fuse click shows instead of sending, when the slotted
protection stone's option is already at six charges on the target
(620DD0, called on the click at 6210F0): the stone is named by its
codename, compared case-insensitively, and the target's charges are
read from its own option map. Without this the click reached the
server, whose (0x12, 0x22) cap answer is a different, generic string.
================
*/
export function alchemyProtectionStoneRefusal(
	stoneCodename: string | undefined,
	magic: readonly string[],
	references: ReadonlyMap<number, { optionName: string; }>
): SystemNotice | null {
	const stone = stoneCodename === undefined ? null : protectionStone( stoneCodename );
	if ( !stone ) return null;
	for ( const encoded of magic ) {
		const bits = BigInt( encoded );
		if (
			references.get( Number( bits & 0xffffn ) )?.optionName === stone.option &&
			Number( bits >> 32n ) === PROTECTION_STONE_MAX
		) return constantNativeNotice( PROTECTION_STONE_NOTICE_CATEGORY, stone.code );
	}
	return null;
}
