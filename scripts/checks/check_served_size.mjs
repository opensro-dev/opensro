/*
===========================================================================

check_served_size.mjs - a full download stays under 80% of the original

Measures the packs exactly as the host serves them (servedSize.mjs): each
manifest pack must exist on disk at its recorded size, and the total must
stay under the ceiling. Prints the full download, the startup share and
every group, largest first, so a size regression names its owner.

===========================================================================
*/
import { readFile, stat } from "node:fs/promises";
import path from "node:path";

import { SERVED_CEILING_BYTES, ORIGINAL_PK2_BYTES, servedSize } from "../build/shared/servedSize.mjs";
import { publicAssetsRoot } from "../build/world/paths.mjs";

const manifestPath = path.join( publicAssetsRoot, "packs", "manifest.json" );
const manifest = JSON.parse( await readFile( manifestPath, "utf8" ) );

for ( const group of manifest.groups ?? [] ) {
	for ( const pack of group.packs ?? [] ) {
		const packPath = path.join( publicAssetsRoot, "..", pack.path );
		const packStats = await stat( packPath ).catch( () => undefined );
		if ( !packStats?.isFile() || packStats.size !== pack.bytes ) {
			fail( `Pack ${pack.path} is missing or not ${pack.bytes} bytes on disk` );
		}
	}
}

const size = servedSize( manifest );
console.log( `Full download: ${formatBytes( size.total )} (${percentOfOriginal( size.total )} of the original PK2s)` );
console.log( `Before login (startup groups): ${formatBytes( size.startup )}` );
for ( const group of [ ...size.groups ].sort( ( a, b ) => b.bytes - a.bytes ) ) {
	console.log(
		`  ${group.name.padEnd( 20 )} ${group.load.padEnd( 8 )} ${formatBytes( group.bytes ).padStart( 10 )}`
	);
}
if ( size.total > SERVED_CEILING_BYTES ) {
	fail( `Full download ${formatBytes( size.total )} exceeds the ceiling ${formatBytes( SERVED_CEILING_BYTES )}` );
}

/*
================
percentOfOriginal
================
*/
function percentOfOriginal( bytes ) {
	return ((bytes / ORIGINAL_PK2_BYTES) * 100).toFixed( 1 ) + "%";
}

/*
================
formatBytes
================
*/
function formatBytes( bytes ) {
	return (bytes / 1024 / 1024).toFixed( 1 ) + " MiB";
}

/*
================
fail
================
*/
function fail( message ) {
	console.error( message );
	process.exit( 1 );
}
