/*
===========================================================================

item-seal.ts - which items are sealed, and which seal they carry

One owner for the seal rule the tooltip, the slot shine, the shop refusal and
the drop notices all read. The item data's rarity column says it: 2 is a
sealed item, 6 a sealed set item (`_SET_A_RARE` / `_SET_B_RARE`). Plain (0)
and set (3) items are not sealed. The item class column gives the tier:
(class - 1) % 3 is Star, Moon, Sun, and the degree 11 classes (31 and up)
name the first tier Seal of Nova.

===========================================================================
*/

export const RARITY_SEALED = 2;
export const RARITY_SEALED_SET = 6;
export const SEAL_TIER_COUNT = 3;
// The first item class of degree 11: its first seal tier is "Nova", not "Star".
export const NOVA_FIRST_CLASS = 31;

const SEAL_SYMBOLS = [ "PARAM_RARE_FIRST", "PARAM_RARE_SECOND", "PARAM_RARE_THIRD" ] as const;
export const SEAL_SYMBOL_NOVA = "PARAM_RARE_FIRST2";

/*
================
itemRarityIsSealed

Whether a rarity column value marks a sealed item.
================
*/
export function itemRarityIsSealed( rarity: number | undefined ): boolean {
	return rarity === RARITY_SEALED || rarity === RARITY_SEALED_SET;
}

/*
================
sealTier

The seal tier (0 Star, 1 Moon, 2 Sun) of a sealed item, or undefined when
the item is not sealed or has no class.
================
*/
export function sealTier( rarity: number | undefined, itemClass: number | undefined ): number | undefined {
	if ( !itemRarityIsSealed( rarity ) || !itemClass ) return undefined;
	return (itemClass - 1) % SEAL_TIER_COUNT;
}

/*
================
sealTextSymbol

The text symbol naming an item's seal, or undefined when it has none.
================
*/
export function sealTextSymbol( rarity: number | undefined, itemClass: number | undefined ): string | undefined {
	const tier = sealTier( rarity, itemClass );
	if ( tier === undefined ) return undefined;
	if ( tier === 0 && itemClass! >= NOVA_FIRST_CLASS ) return SEAL_SYMBOL_NOVA;
	return SEAL_SYMBOLS[tier];
}
