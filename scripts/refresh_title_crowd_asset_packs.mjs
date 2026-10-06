/*
===========================================================================

refresh_title_crowd_asset_packs.mjs - republish the title-crowd VAT packs

Repacks the title-crowd VAT artifacts sparsely and reconciles game-data from
its loose authority (the roster), then archives the packs they superseded.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "./lib/generatedRoot.mjs";
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "./build/assetPackPublication.mjs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { archiveGeneratedArtifact } from "./build/artifacts/generatedArtifactArchive.mjs";
import { reconcileAssetPackGroupFromLooseAuthority } from "./build/assetPackGroupAuthority.mjs";
import { listPublicAssetFiles } from "./build/assetPacks.mjs";
import { refreshGeneratedManifestSidecars } from "./build/generatedManifestSidecars.mjs";
import { patchAssetPackGroupFromLooseFiles } from "./build/sparseAssetPackGroupRefresh.mjs";
import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const packsRoot = path.join( publicRoot, "assets", "packs" );
const packManifestPath = path.join( packsRoot, "manifest.json" );
const refreshRoot = path.join( packsRoot, "incremental", "title-crowd" );
const TITLE_CROWD_VAT_GROUP = "title-crowd-vat";
const GAME_DATA_GROUP = "game-data";

await withGeneratedAssetsLock( "title-crowd asset-pack refresh", async () => {
	const vatFiles = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/char/vat" ],
		extensions: [ ".bin", ".json" ]
	} );
	if ( vatFiles.length === 0 ) throw new Error( "No published title-crowd VAT artifacts were found." );

	const deltas = [
		{ groupName: TITLE_CROWD_VAT_GROUP, looseFiles: vatFiles, reconcileAuthority: false },
		{ groupName: GAME_DATA_GROUP, looseFiles: [ "/assets/char/roster.json.gz" ], reconcileAuthority: true }
	];
	const previous = JSON.parse( await readFile( packManifestPath, "utf8" ) );
	const refreshedByGroup = new Map();

	for ( const delta of deltas ) {
		const previousGroup = requirePopulatedGroup( previous, delta.groupName );
		const refreshed = await timed(
			delta.reconcileAuthority ?
				`authoritative ${delta.groupName} reconciliation` :
				`sparse ${delta.groupName} refresh`,
			() =>
				(delta.reconcileAuthority ?
					reconcileAssetPackGroupFromLooseAuthority :
					patchAssetPackGroupFromLooseFiles)( {
						publicRoot,
						outputRoot: path.join( refreshRoot, delta.groupName ),
						previousIndex: previous,
						groupName: delta.groupName,
						...(delta.reconcileAuthority ? {} : { looseFiles: delta.looseFiles })
					} )
		);
		if ( refreshed.assets.length !== previousGroup.assetCount ) {
			throw new Error(
				`${delta.groupName} sparse refresh changed membership ` +
					`(${previousGroup.assetCount} -> ${refreshed.assets.length}).`
			);
		}
		refreshedByGroup.set( delta.groupName, refreshed );
	}

	const merged = mergeAssetPackGroupUpdates( previous, [ ...refreshedByGroup.values() ] );
	const changed = merged !== previous;

	validateClosure( merged, deltas );
	await mkdir( packsRoot, { recursive: true } );
	if ( changed ) {
		await publishAssetPackManifest( publicRoot, packManifestPath, Buffer.from( JSON.stringify( merged ), "utf8" ), {
			logLabel: "title-crowd-pack-overlay"
		} );
	}

	for ( const [groupName, refreshed] of refreshedByGroup ) {
		await retireReplacedPacks( requirePopulatedGroup( previous, groupName ), refreshed.groups[0] );
	}

	const webManifest = await timed( "web manifest refresh", buildWebAssetManifest );
	await timed( "manifest sidecar refresh", () =>
		refreshGeneratedManifestSidecars( {
			publicRoot,
			onlyWhenStale: true,
			brotliQuality: 4,
			gzipLevel: 3,
			zstdLevel: 3
		} ) );

	const totals = [ ...refreshedByGroup.values() ].reduce( ( sum, refreshed ) => ({
		built: sum.built + refreshed.builtPackCount,
		reused: sum.reused + refreshed.reusedPackCount,
		changed: sum.changed + refreshed.changedAssetCount,
		hydrated: sum.hydrated + refreshed.hydratedAssetCount
	}), { built: 0, reused: 0, changed: 0, hydrated: 0 } );
	console.log(
		`Title-crowd closure OK: ${totals.built} pack(s) rebuilt, ${totals.reused} reused, ` +
			`${totals.changed} asset delta(s), ${totals.hydrated} member(s) hydrated; ` +
			`${refreshedByGroup.get( GAME_DATA_GROUP ).authorityFileCount} game-data authorities checked, ` +
			`${webManifest.files.length} web assets.`
	);
} );

/*
================
requirePopulatedGroup
================
*/
function requirePopulatedGroup( index, groupName ) {
	const group = index.groups?.find( candidate => candidate.name === groupName );
	if ( !group || group.assetCount === 0 || group.packs?.length === 0 ) {
		throw new Error( `The main asset manifest has no populated ${groupName} group.` );
	}
	return group;
}

/*
================
validateClosure
================
*/
function validateClosure( index, deltas ) {
	const assets = new Map( index.assets.map( asset => [ asset.path.toLowerCase(), asset ] ) );
	for ( const delta of deltas ) {
		for ( const publicPath of delta.looseFiles ) {
			const asset = assets.get( publicPath.toLowerCase() );
			if ( !asset || asset.group !== delta.groupName ) {
				throw new Error( `${publicPath} is absent from the ${delta.groupName} pack group.` );
			}
		}
	}
}

/*
================
retireReplacedPacks
================
*/
async function retireReplacedPacks( previousGroup, refreshedGroup ) {
	const currentPaths = new Set(
		refreshedGroup.packs.flatMap( pack =>
			[ pack.path, pack.zstdPath ]
				.filter( Boolean )
				.map( normalizePublicPath )
		)
	);
	for ( const pack of previousGroup.packs ) {
		for ( const publicPath of [ pack.path, pack.zstdPath ].filter( Boolean ) ) {
			if ( currentPaths.has( normalizePublicPath( publicPath ) ) ) continue;
			await archiveGeneratedArtifact( resolvePackFile( publicPath ), {
				scopeRoot: publicRoot,
				reason: "superseded-title-crowd-pack"
			} );
		}
	}
}

/*
================
resolvePackFile
================
*/
function resolvePackFile( publicPath ) {
	const resolved = path.resolve( publicRoot, normalizePublicPath( publicPath ).replace( /^\/+/, "" ) );
	const relative = path.relative( packsRoot, resolved );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `Refusing to retire pack outside ${packsRoot}: ${resolved}` );
	}
	return resolved;
}

/*
================
normalizePublicPath
================
*/
function normalizePublicPath( value ) {
	return `/${String( value ).replaceAll( "\\", "/" ).replace( /^\/+/, "" )}`.replace( /\/{2,}/g, "/" );
}

/*
================
timed
================
*/
async function timed( label, task ) {
	const startedAt = performance.now();
	console.log( `[title-crowd-refresh] ${label}: start` );
	try {
		const result = await task();
		console.log(
			`[title-crowd-refresh] ${label}: done (${((performance.now() - startedAt) / 1000).toFixed( 1 )}s)`
		);
		return result;
	} catch ( error ) {
		console.error(
			`[title-crowd-refresh] ${label}: failed after ${((performance.now() - startedAt) / 1000).toFixed( 1 )}s`
		);
		throw error;
	}
}
