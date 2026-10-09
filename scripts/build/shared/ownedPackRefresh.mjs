/*
===========================================================================

ownedPackRefresh.mjs - refresh files without changing their published owners

Focused producers can span several groups. Existing manifest ownership wins
over a fallback for new files, using the same case-folded identity as client
admission. The caller holds the generated-assets lock.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import { normalizePublicAssetPath } from "./assetPaths.mjs";
import { PACK_INDEX_PATH, refreshPackGroups } from "./packGroupRefresh.mjs";

/*
================
refreshOwnedPackFiles

Preserve each group's load policy. startup requests additionally require
every touched group to be startup-resident. Without defaultGroup, a new
path is refused instead of guessing its ownership.
================
*/
export async function refreshOwnedPackFiles( request ) {
	const previous = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
	const groupOf = typeof request.defaultGroup === "function" ? request.defaultGroup : () => request.defaultGroup;
	const owners = new Map( previous.assets.map( row => [ row.path.toLowerCase(), row.group ] ) );
	const files = new Map();
	for ( const value of request.files ) {
		const file = normalizePublicAssetPath( value );
		files.set( file.toLowerCase(), file );
	}
	const deltas = new Map();
	const incoming = [ ...files.values() ];
	for ( const [key, file] of files ) {
		const group = owners.get( key ) ?? groupOf( file, previous, incoming );
		if ( !group ) throw new Error( `${request.name}: ${file} has no asset-pack owner` );
		const rows = deltas.get( group ) ?? [];
		rows.push( file );
		deltas.set( group, rows );
	}
	return refreshPackGroups( {
		name: request.name,
		timed: request.timed,
		manifestSidecars: request.manifestSidecars,
		deltas: [ ...deltas ].map( ( [groupName, files] ) => ({ groupName, files, startup: request.startup }) )
	} );
}
