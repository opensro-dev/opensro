/*
===========================================================================
pet-gear.ts - ordinary and sealed pet reference identities and white stats
===========================================================================
*/
import { PET_DESIGN } from "./pet-design-data";

/*
================
petGear

Seals occupy the tens digit of the slot's reserved reference range.
================
*/
export function petGear( ref = 0 ) {
	const seal = Math.floor( ref % 100 / 10 );
	if ( seal > 3 ) return undefined;
	const base = ref - seal * 10;
	for ( const degree of PET_DESIGN.gear ) {
		const item = degree.items.find( gear => gear.id === base );
		if ( !item ) continue;
		const scale = 1 + seal * .5;
		return {
			degree,
			seal,
			item: {
				...item,
				baseStats: Object.fromEntries(
					Object.entries( item.baseStats ).map(
						( [key, value] ) => [ key, value * scale ]
					)
				) as Record<keyof typeof item.baseStats, number>
			}
		};
	}
	return undefined;
}
