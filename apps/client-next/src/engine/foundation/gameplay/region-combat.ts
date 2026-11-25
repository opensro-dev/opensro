/*
===========================================================================

region-combat.ts - which regions allow player combat, for the client

The lookup over _RefRegion.IsBattleField that the server's
RegionPlayerCombat answers. The ranges are generated into
region-combat-ranges.ts by scripts/build/import_region_combat.py, from
the same parse as the server table, so the two cannot drift
(scripts/test/python/test_region_combat_parity.py). The SRO Macro town
gate (006861c0 / 00dade90) treats a known non-battlefield region as
in town.

===========================================================================
*/

import { REGION_COMBAT_RANGES } from "./region-combat-ranges";

/*
================
regionAllowsPlayerCombat

Whether the region allows player combat. undefined when the id is
absent from the catalog (server RegionPlayerCombat known=false).
================
*/
export function regionAllowsPlayerCombat( regionId: number ): boolean | undefined {
	if ( !Number.isInteger( regionId ) || regionId < 0 || regionId > 0xffff ) return undefined;
	let lo = 0, hi = REGION_COMBAT_RANGES.length - 1;
	while ( lo <= hi ) {
		const mid = (lo + hi) >> 1;
		const row = REGION_COMBAT_RANGES[mid]!;
		if ( regionId < row.first ) hi = mid - 1;
		else if ( regionId > row.last ) lo = mid + 1;
		else return row.allowed;
	}
	return undefined;
}

/*
================
macroRegionIsTown

006861c0 / 00dade90: a known region that refuses player combat is town
(IsBattleField clear). Unknown regions stay non-town so the macro is
not disabled by a missing catalog row.
================
*/
export function macroRegionIsTown( regionId: number ): boolean {
	return regionAllowsPlayerCombat( regionId ) === false;
}
