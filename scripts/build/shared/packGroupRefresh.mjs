/*
===========================================================================

packGroupRefresh.mjs - republish a few pack groups into the existing index

Every focused publisher ends the same way: rebuild the touched groups from
their loose files, merge them into the main index through the one merge
rule, prove the published files landed in the expected groups, publish the
index, soft-archive the packs the touched groups no longer use, and
refresh the web manifest and its sidecars. This is that sequence, once;
callers say only which groups change and how each is rebuilt.

The caller holds the generated-assets lock.

===========================================================================
*/
import { normalizePublicPath } from "./assetPaths.mjs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { archiveGeneratedArtifact } from "../artifacts/generatedArtifactArchive.mjs";
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "../assetPackPublication.mjs";
import { refreshGeneratedManifestSidecars } from "../generatedManifestSidecars.mjs";
import { patchAssetPackGroupFromLooseFiles } from "../sparseAssetPackGroupRefresh.mjs";
import { buildWebAssetManifest } from "../webManifest.mjs";
import { publicRoot } from "../world/paths.mjs";

export const PACKS_ROOT = path.join( publicRoot, "assets", "packs" );
export const PACK_INDEX_PATH = path.join( PACKS_ROOT, "manifest.json" );

// Iteration sidecars for the regenerated manifests are lossless but fast;
// the compact release step recompresses the retained manifests at maximum.
const FAST_MANIFEST_SIDECARS = { gzipLevel: 3 };

/**
 * @typedef {{
 *   groupName: string,
 *   files?: string[],
 *   rebuild?: ( previous: object, outputRoot: string ) => Promise<object>,
 *   startup?: boolean,
 *   sameMembership?: boolean
 * }} GroupDelta
 */

/*
================
timedStep

A step logger for one publisher: `[label] step: start / done (1.2s)`.
================
*/
export function timedStep( label ) {
	return async ( step, task ) => {
		const startedAt = performance.now();
		const seconds = () => ((performance.now() - startedAt) / 1000).toFixed( 1 );
		console.log( `[${label}] ${step}: start` );
		try {
			const result = await task();
			console.log( `[${label}] ${step}: done (${seconds()}s)` );
			return result;
		} catch ( error ) {
			console.error( `[${label}] ${step}: failed after ${seconds()}s` );
			throw error;
		}
	};
}

/*
================
requireGroup

The populated group of an index; startup=true also requires it to load at
startup, which the focused UI families depend on.
================
*/
export function requireGroup( index, groupName, { startup = false } = {} ) {
	const group = index.groups?.find( candidate => candidate.name === groupName );
	if ( !group || group.assetCount === 0 || group.packs?.length === 0 ) {
		throw new Error( `The main asset manifest has no populated ${groupName} group.` );
	}
	if ( startup && group.load !== "startup" ) {
		throw new Error( `${groupName} must be startup-resident; found load=${JSON.stringify( group.load )}.` );
	}
	return group;
}

/*
================
retireSupersededPacks

Soft-archives the packs a group held before a refresh and no longer holds.
Never deletes: a mistaken retirement is restored from temp/archives/.
================
*/
async function retireSupersededPacks( previousGroup, refreshedGroup, reason ) {
	const current = new Set(
		refreshedGroup.packs.flatMap( pack => [ pack.path, pack.zstdPath ].filter( Boolean ) ).map(
			normalizePublicPath
		)
	);
	for ( const pack of previousGroup?.packs ?? [] ) {
		for ( const publicPath of [ pack.path, pack.zstdPath ].filter( Boolean ).map( normalizePublicPath ) ) {
			if ( current.has( publicPath ) ) continue;
			await archiveGeneratedArtifact( resolvePackFile( publicPath ), { scopeRoot: publicRoot, reason } );
		}
	}
}

/*
================
resolvePackFile
================
*/
function resolvePackFile( publicPath ) {
	const resolved = path.resolve( publicRoot, publicPath.replace( /^\/+/, "" ) );
	const relative = path.relative( PACKS_ROOT, resolved );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `Refusing to retire a pack outside ${PACKS_ROOT}: ${resolved}` );
	}
	return resolved;
}

/*
================
refreshPackGroups

request.name names the publisher (incremental output folder, logs, archive
reason). Each delta rebuilds one group: from request files by default
(patchAssetPackGroupFromLooseFiles), or with its own rebuild(previous,
outputRoot). Every delta's files must end up in that group. sameMembership
refuses a refresh that adds or drops members. manifestSidecars overrides the
fast sidecar settings. Returns { updates, webManifest, totals }.
================
*/
export async function refreshPackGroups( request ) {
	const { name, deltas, timed = timedStep( `${name}-packs` ) } = request;
	const previous = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
	const outputRoot = path.join( PACKS_ROOT, "incremental", name );
	const updates = [];
	for ( const delta of deltas ) {
		// A refresh replaces a group the full build created; it never creates one.
		const previousGroup = requireGroup( previous, delta.groupName, { startup: Boolean( delta.startup ) } );
		const groupRoot = path.join( outputRoot, delta.groupName );
		const update = await timed(
			`${delta.groupName} refresh (${delta.files?.length ?? "authority"} files)`,
			() =>
				delta.rebuild ?
					delta.rebuild( previous, groupRoot ) :
					patchAssetPackGroupFromLooseFiles( {
						publicRoot,
						outputRoot: groupRoot,
						previousIndex: previous,
						groupName: delta.groupName,
						looseFiles: delta.files
					} )
		);
		if ( delta.sameMembership && update.assets.length !== previousGroup.assetCount ) {
			throw new Error(
				`${delta.groupName} refresh changed membership (${previousGroup.assetCount} -> ${update.assets.length}).`
			);
		}
		updates.push( { delta, previousGroup, update } );
	}

	const next = mergeAssetPackGroupUpdates(
		previous,
		updates.map( row => row.update ),
		deltas.map( delta => delta.groupName )
	);
	const assets = new Map( next.assets.map( asset => [ asset.path.toLowerCase(), asset ] ) );
	for ( const { delta } of updates ) {
		const missing = (delta.files ?? []).filter( file =>
			assets.get( file.toLowerCase() )?.group !== delta.groupName
		);
		if ( missing.length > 0 ) {
			throw new Error(
				`${name} publication closure: ${missing.length} file(s) missing from ${delta.groupName}: ` +
					missing.slice( 0, 8 ).join( ", " )
			);
		}
	}
	await mkdir( PACKS_ROOT, { recursive: true } );
	if ( next !== previous ) {
		await publishAssetPackManifest( publicRoot, PACK_INDEX_PATH, Buffer.from( JSON.stringify( next ) ), {
			logLabel: `${name}-packs`
		} );
	}
	for ( const { previousGroup, update } of updates ) {
		await retireSupersededPacks( previousGroup, update.groups[0], `superseded-${name}-pack` );
	}
	const webManifest = await timed( "web manifest refresh", buildWebAssetManifest );
	await timed( "manifest sidecar refresh", () =>
		refreshGeneratedManifestSidecars( {
			publicRoot,
			onlyWhenStale: true,
			...(request.manifestSidecars ?? FAST_MANIFEST_SIDECARS)
		} ) );
	const totals = updates.reduce( ( sum, { update } ) => ({
		built: sum.built + (update.builtPackCount ?? 0),
		reused: sum.reused + (update.reusedPackCount ?? 0),
		changed: sum.changed + (update.changedAssetCount ?? 0),
		hydrated: sum.hydrated + (update.hydratedAssetCount ?? 0)
	}), { built: 0, reused: 0, changed: 0, hydrated: 0 } );
	return { updates: updates.map( row => row.update ), webManifest, totals };
}
