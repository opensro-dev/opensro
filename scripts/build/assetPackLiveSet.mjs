/*
===========================================================================

assetPackLiveSet.mjs - which files under assets/packs/ the published index uses

The published pack index (assets/packs/manifest.json) is the single source of
truth for delivery. A file under the packs root is live when the index or
its precompressed variant names it: the index itself and every pack with
its compact zstd copy. Members travel compressed inside their packs
(SROPACK2), so retired gzip transports and delivery.json are not live.
Everything else under the packs root is a superseded build output.

Both the full pack build (assetPacks.mjs) and the pack garbage collector
(gc_asset_packs.mjs) use this one rule, so they cannot disagree about what
may be retired.

===========================================================================
*/

import { containedPublicFile } from "./shared/assetPaths.mjs";
import path from "node:path";

// The index and its published sidecar (PUBLISHED_SIDECAR_SUFFIXES).
const INDEX_SIDECARS = [ "", ".gz" ];

/*
================
livePackFiles

Returns the lower-cased absolute paths of every live file for a published
index. `indexPath` is the index file; `publicRoot` resolves the public
asset paths the index names.
================
*/
export function livePackFiles( publicRoot, indexPath, index ) {
	const live = new Set();
	const add = ( filename ) => live.add( path.resolve( filename ).toLowerCase() );
	for ( const suffix of INDEX_SIDECARS ) {
		add( `${indexPath}${suffix}` );
	}
	for ( const group of index.groups ?? [] ) {
		for ( const pack of group.packs ?? [] ) {
			add( containedPublicFile( publicRoot, pack.path ) );
			if ( pack.zstdPath ) {
				add( containedPublicFile( publicRoot, pack.zstdPath ) );
			}
		}
	}
	return live;
}
