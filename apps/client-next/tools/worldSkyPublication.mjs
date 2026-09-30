/*
===========================================================================

worldSkyPublication.mjs - shared pieces of the world sky publishers

publish-flares.mjs and publish-star-rng.mjs both rewrite the sky block of
every published world file and repack those files. They share the world
root, the sidecar levels and the rule for which representations to repack.

===========================================================================
*/
import path from "node:path";
import { publicRoot } from "../../../scripts/build/world/paths.mjs";

export const WORLD_ROOT = path.join( publicRoot, "assets", "world" );

// Lighter sidecar levels: the world files are large and rebuilt often.
export const SIDECAR_LEVELS = { brotliQuality: 4, gzipLevel: 3, zstdLevel: 3 };

/*
================
packedWorldFiles

The public paths to repack for rewritten world files: each representation
(plain or .gz) the index already packs. A representation no group holds is
left loose, as the full build left it.
================
*/
export function packedWorldFiles( index, files ) {
	const packed = new Set( index.assets.map( row => row.path ) );
	return files.flatMap( file => {
		const logical = "/" + path.relative( publicRoot, file ).replaceAll( "\\", "/" );
		return [ logical, logical + ".gz" ].filter( candidate => packed.has( candidate ) );
	} );
}
