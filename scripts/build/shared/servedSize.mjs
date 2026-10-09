/*
===========================================================================

servedSize.mjs - the bytes a player downloads, from the pack manifest

Packs are what the host serves for game data: a full download is every
pack, and the startup groups are what loads before the login screen. The
member compression lives inside the packs (SROPACK2), so a pack's manifest
bytes are its wire bytes. The ceiling is 80% of the original PK2 payload.

===========================================================================
*/

// ORIGINAL_PK2_BYTES is the retail v1.150 client's PK2 payload.
export const ORIGINAL_PK2_BYTES = 2_356_998_144;

// SERVED_CEILING_BYTES bounds a full download at 80% of the original.
export const SERVED_CEILING_BYTES = Math.floor( ORIGINAL_PK2_BYTES * 0.8 );

// STARTUP_LOAD is the load class fetched before the login screen.
const STARTUP_LOAD = "startup";

/*
================
servedSize

Totals per group and overall. A pack without a non-negative integer byte
count is a broken manifest, not a free download.
================
*/
export function servedSize( manifest ) {
	if ( !Array.isArray( manifest?.groups ) ) throw Error( "Pack manifest has no groups" );
	const groups = manifest.groups.map( group => {
		if ( typeof group.name !== "string" || typeof group.load !== "string" ) {
			throw Error( `Pack group ${group.name} has no name or load class` );
		}
		let bytes = 0;
		for ( const pack of group.packs ?? [] ) {
			if ( !Number.isSafeInteger( pack.bytes ) || pack.bytes < 0 ) {
				throw Error( `Pack ${pack.path} has no byte count` );
			}
			bytes += pack.bytes;
		}
		return { name: group.name, load: group.load, bytes, packs: group.packs?.length ?? 0 };
	} );
	const total = groups.reduce( ( sum, group ) => sum + group.bytes, 0 );
	const startup = groups.filter( group => group.load === STARTUP_LOAD ).reduce(
		( sum, group ) => sum + group.bytes,
		0
	);
	return { total, startup, groups };
}
