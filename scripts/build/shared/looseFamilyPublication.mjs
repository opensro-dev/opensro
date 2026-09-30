/*
===========================================================================

looseFamilyPublication.mjs - publish a small family of loose files into packs

Focused publishers (footprints, overlays, quick status, quickslots, return
scrolls) write a handful of loose files into the public tree and must then
pack them without disturbing anything else. This owner does that one way:
each file joins the group that already holds it (or the family's default
group), only those groups are patched, the index is merged through
mergeAssetPackGroupUpdates (the one merge, with its schema guard), every
file must be owned exactly once, and the web manifest follows.

The caller holds the generated-assets lock.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "../assetPackPublication.mjs";
import { refreshGeneratedManifestSidecars } from "../generatedManifestSidecars.mjs";
import { patchAssetPackGroupFromLooseFiles } from "../sparseAssetPackGroupRefresh.mjs";
import { buildWebAssetManifest } from "../webManifest.mjs";
import { publicRoot } from "../world/paths.mjs";

/*
================
publishLooseFamily

family.name names the incremental pack folder and the log label;
family.files are public paths already written loose; family.defaultGroup
(a group name, or a function of the path) places files no group holds yet.
Without a default every file must already have an owner. Returns the
patched group updates so a caller can report what was rebuilt.
================
*/
export async function publishLooseFamily( family ) {
	const manifestPath = path.join( publicRoot, "assets", "packs", "manifest.json" );
	const previous = JSON.parse( await readFile( manifestPath, "utf8" ) );
	const groupOf = typeof family.defaultGroup === "function" ? family.defaultGroup : () => family.defaultGroup;
	const owners = new Map( previous.assets.map( row => [ row.path, row.group ] ) );
	const deltas = new Map();
	for ( const file of new Set( family.files ) ) {
		const group = owners.get( file ) ?? groupOf( file );
		if ( !group ) throw new Error( `${family.name}: ${file} has no asset-pack owner` );
		const rows = deltas.get( group ) ?? [];
		rows.push( file );
		deltas.set( group, rows );
	}
	const updates = [];
	for ( const [groupName, looseFiles] of deltas ) {
		console.log( `[${family.name}-packs] Updating ${groupName} (${looseFiles.length} files).` );
		updates.push(
			await patchAssetPackGroupFromLooseFiles( {
				publicRoot,
				previousIndex: previous,
				groupName,
				looseFiles,
				outputRoot: path.join( publicRoot, "assets", "packs", "incremental", family.name, groupName )
			} )
		);
	}
	const next = mergeAssetPackGroupUpdates( previous, updates, [ ...deltas.keys() ] );
	for ( const file of family.files ) {
		if ( next.assets.filter( row => row.path === file ).length !== 1 ) {
			throw new Error( `${family.name} publication closure: ${file}` );
		}
	}
	if ( next !== previous ) {
		await publishAssetPackManifest( publicRoot, manifestPath, Buffer.from( JSON.stringify( next ) ), {
			logLabel: `${family.name}-packs`
		} );
	}
	await buildWebAssetManifest();
	await refreshGeneratedManifestSidecars( { publicRoot, onlyWhenStale: true } );
	return updates;
}
