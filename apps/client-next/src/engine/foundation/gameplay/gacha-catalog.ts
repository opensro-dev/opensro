/*
===========================================================================

gacha-catalog.ts - Magic Pang selected prize rows and ticket classification

Owns the twelve visible set-1 prize entries for NPC 9251 (gachanpcmap.txt:
visible set 1, lottery set 2) and the item-type mask for Magic Pang tickets.
Port-only, not native: when the server runs with SRO_GACHA_POOL_V2=on (the
default in apps/server/internal/game/item/gacha/catalog.go), entries 1..6
serve the degree-9 Seal of Star weapons and shield (4146, 4038, 4110, 4074,
4182, 4218); when the server runs with SRO_GACHA_POOL_V2=off, entries 1..6
fall back to the retail v1.150 gachaitemset.txt set (4140, 4035, 4104, 4071,
4176, 4215).

===========================================================================
*/

export interface GachaCatalogPrize {
	readonly entry: number;
	readonly refObjId: number;
	readonly quantity: number;
	readonly nameSymbol: string;
}

const GACHA_TICKET_TYPE_MASK = 0xfffe;
const GACHA_TICKET_TYPE_FLAGS = 0x0f6c;

const GACHA_POOL_V1_FIRST_REF = 4140;
const GACHA_POOL_V2_FIRST_REF = 4146;

/*
================
gachaPrizes

Returns the twelve set-1 Magic Pang prize definitions. Defaults to Pool V2
(SRO_GACHA_POOL_V2=on) and falls back to the retail v1.150 pool when passed
false or a server reference map that published the v1.150 reward set without
the Pool V2 set.
================
*/
export function gachaPrizes( poolV2OrRefs: boolean | ReadonlyMap<number, unknown> = true ): GachaCatalogPrize[] {
	const names: Readonly<Record<number, string>> = {
		3783: "SN_ITEM_MALL_RESURRECTION_100P_SCROLL",
		3884: "SN_ITEM_MALL_GOLD_TIME_SERVICE_TICKET_4W",
		4035: "SN_ITEM_CH_SWORD_08_A_RARE",
		4038: "SN_ITEM_CH_SWORD_09_A_RARE",
		4071: "SN_ITEM_CH_BLADE_08_A_RARE",
		4074: "SN_ITEM_CH_BLADE_09_A_RARE",
		4104: "SN_ITEM_CH_SPEAR_07_A_RARE",
		4110: "SN_ITEM_CH_SPEAR_09_A_RARE",
		4140: "SN_ITEM_CH_TBLADE_07_A_RARE",
		4146: "SN_ITEM_CH_TBLADE_09_A_RARE",
		4176: "SN_ITEM_CH_BOW_07_A_RARE",
		4182: "SN_ITEM_CH_BOW_09_A_RARE",
		4215: "SN_ITEM_CH_SHIELD_08_A_RARE",
		4218: "SN_ITEM_CH_SHIELD_09_A_RARE",
		5912: "SN_ITEM_MALL_HP_SUPERSET_5_BAG",
		5913: "SN_ITEM_MALL_MP_SUPERSET_5_BAG",
		6855: "SN_ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09",
		6891: "SN_ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09"
	};
	const v1WeaponEntries: readonly (readonly [number, number, number])[] = [
		[ 1, 4140, 1 ],
		[ 2, 4035, 1 ],
		[ 3, 4104, 1 ],
		[ 4, 4071, 1 ],
		[ 5, 4176, 1 ],
		[ 6, 4215, 1 ]
	];
	const v2WeaponEntries: readonly (readonly [number, number, number])[] = [
		[ 1, 4146, 1 ],
		[ 2, 4038, 1 ],
		[ 3, 4110, 1 ],
		[ 4, 4074, 1 ],
		[ 5, 4182, 1 ],
		[ 6, 4218, 1 ]
	];
	const sharedConsumableEntries: readonly (readonly [number, number, number])[] = [
		[ 7, 6891, 1 ],
		[ 8, 6855, 1 ],
		[ 9, 3884, 1 ],
		[ 10, 5912, 1000 ],
		[ 11, 5913, 1000 ],
		[ 12, 3783, 11 ]
	];
	const usePoolV2 = typeof poolV2OrRefs === "boolean" ?
		poolV2OrRefs :
		poolV2OrRefs.has( GACHA_POOL_V2_FIRST_REF ) || !poolV2OrRefs.has( GACHA_POOL_V1_FIRST_REF );
	const weaponEntries = usePoolV2 ? v2WeaponEntries : v1WeaponEntries;

	return [ ...weaponEntries, ...sharedConsumableEntries ].map( ( [entry, refObjId, quantity] ) => ({
		entry,
		refObjId,
		quantity,
		nameSymbol: names[refObjId]!
	}) );
}

/*
================
isGachaTicket

Matches the 3.3.13.14 Magic Pang ticket type word (0x0f6c; bit 0 is ignored
in native type-flag comparisons).
================
*/
export function isGachaTicket( flags: number ): boolean {
	return (flags & GACHA_TICKET_TYPE_MASK) === GACHA_TICKET_TYPE_FLAGS;
}
