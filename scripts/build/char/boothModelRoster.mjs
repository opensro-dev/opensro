/*
===========================================================================

boothModelRoster.mjs - native default and item-selected stall resources

Use the item catalog's existing model resolution. A booth is a secondary
CCObjAnimation resource, independent of its owner's action state 15.

===========================================================================
*/

import { loadEquipmentRecords } from "./equipmentVisualRecords.mjs";
import { normalizeBsrResourcePath } from "./resourceGlbOutput.mjs";

export const DEFAULT_BOOTH_MODELS = [
	"res/item/china/item/cj_store.bsr",
	"res/item/europe/item/euro_streetstall01.bsr"
];

/*
================
collectBoothModelRoster

86A880 loads the selected booth as its own CCObjAnimation. Default paths
come from the native loader; item variants use their resolved item model.
================
*/
export function collectBoothModelRoster( itemRecords ) {
	const resources = new Map( DEFAULT_BOOTH_MODELS.map( bsrPath => [ bsrPath, {
		bsrPath,
		kind: "booth",
		isMob: false,
		fields: { requiredBy: [] },
		requiredStates: []
	} ] ) );
	for ( const row of itemRecords.values() ) {
		if ( !row.code.startsWith( "ITEM_MALL_BOOTH_" ) ) continue;
		if ( !row.resolvedModel ) throw new Error( `[npc] ${row.code}: booth has no item model` );
		const bsrPath = normalizeBsrResourcePath( row.resolvedModel );
		const resource = resources.get( bsrPath ) ?? {
			bsrPath,
			kind: "booth",
			isMob: false,
			fields: { requiredBy: [] },
			requiredStates: []
		};
		resource.fields.requiredBy.push( row.code );
		resources.set( bsrPath, resource );
	}
	return [ ...resources.values() ].sort( ( a, b ) => a.bsrPath.localeCompare( b.bsrPath ) );
}

/*
================
loadBoothModelRoster
================
*/
export function loadBoothModelRoster( textdataRoot ) {
	return collectBoothModelRoster( loadEquipmentRecords( textdataRoot ) );
}
