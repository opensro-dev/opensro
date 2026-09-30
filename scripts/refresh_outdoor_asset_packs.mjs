/*
===========================================================================

refresh_outdoor_asset_packs.mjs - republish the outdoor world packs

Repacks the outdoor-world group through its content-addressed pack cache
(a full rebuild when the loose projection is complete, else a sparse patch)
and archives the packs it superseded.

===========================================================================
*/
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "./build/assetPackPublication.mjs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { IMAGE_ASSET_EXTENSIONS, isImageAsset } from "./build/assetPackGroups.mjs";
import { buildAssetPacks, listPublicAssetFiles } from "./build/assetPacks.mjs";
import { refreshGeneratedManifestSidecars } from "./build/generatedManifestSidecars.mjs";
import { optimizeJsonAssets } from "./build/jsonAssetCompression.mjs";
import { patchAssetPackGroupFromLooseFiles } from "./build/sparseAssetPackGroupRefresh.mjs";
import { publishBytesAtomically } from "./build/shared/atomicPublish.mjs";
import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";
import { archiveGeneratedArtifact } from "./build/artifacts/generatedArtifactArchive.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, ".." );
const publicRoot = path.join( rebuildRoot, ".generated", "client-public" );
const assetsRoot = path.join( publicRoot, "assets" );
const packsRoot = path.join( assetsRoot, "packs" );
// This sub-index is a real content-addressed build cache. It deliberately
// survives refreshes and compact releases; unchanged packs may point either
// at the ordinary pack root or at this cache directory.
const outdoorPackCacheRoot = path.join( packsRoot, "outdoor" );
const outdoorPackCacheManifestPath = path.join( outdoorPackCacheRoot, "manifest.json" );
const packManifestPath = path.join( packsRoot, "manifest.json" );
const OUTDOOR_PREFIX = "/assets/world/outdoor/";
const OUTDOOR_GROUP = "outdoor-world";

await withGeneratedAssetsLock( "outdoor asset-pack refresh", async () => {
	// Only gzip is consumed by fetchJson's pack path. Keep this focused refresh
	// cheap; a full production resource build still emits every encoding.
	const compression = await timed( "JSON sidecar freshness", () =>
		optimizeJsonAssets( {
			root: path.join( assetsRoot, "world", "outdoor" ),
			publicRoot,
			encodings: [ "gzip" ]
		} ) );
	const previous = JSON.parse( await readFile( packManifestPath, "utf8" ) );
	await timed( "pack-cache seed", () => seedOutdoorPackCache( previous ) );
	const outdoorFiles = await timed( "outdoor file enumeration", collectOutdoorPackFiles );
	const previousOutdoor = requireOutdoorGroup( previous );
	const outdoorFileSet = new Set( outdoorFiles );
	const previousOutdoorAssets = previous.assets.filter( ( asset ) => asset.group === OUTDOOR_GROUP );
	const completeLooseProjection = previousOutdoorAssets.every( ( asset ) => outdoorFileSet.has( asset.path ) );
	const refreshed = await timed(
		completeLooseProjection ? "content-hash pack refresh" : "sparse compact-state pack refresh",
		() =>
			completeLooseProjection ?
				buildAssetPacks( {
					publicRoot,
					outputRoot: outdoorPackCacheRoot,
					targetBytes: previous.targetPackBytes,
					groups: [
						{
							name: OUTDOOR_GROUP,
							load: previousOutdoor.load ?? "manual",
							targetBytes: previousOutdoor.targetBytes ?? 8 * 1024 * 1024,
							files: outdoorFiles
						}
					]
				} ) :
				patchAssetPackGroupFromLooseFiles( {
					publicRoot,
					outputRoot: outdoorPackCacheRoot,
					previousIndex: previous,
					groupName: OUTDOOR_GROUP,
					looseFiles: outdoorFiles
				} )
	);

	const merged = mergeAssetPackGroupUpdates( previous, [ refreshed ] );
	const changed = merged !== previous;

	await mkdir( packsRoot, { recursive: true } );
	if ( changed ) {
		await publishAssetPackManifest( publicRoot, packManifestPath, Buffer.from( JSON.stringify( merged ), "utf8" ), {
			logLabel: "outdoor-pack-overlay"
		} );
	}
	await timed( "superseded pack cleanup", () => removeSupersededPackFiles( previous, refreshed.groups ) );
	const manifest = await timed( "web manifest refresh", buildWebAssetManifest );
	await timed( "manifest sidecar refresh", () =>
		refreshGeneratedManifestSidecars( {
			publicRoot,
			onlyWhenStale: true,
			// Iteration sidecars are lossless but intentionally fast. The compact
			// release step recompresses the retained manifests at maximum settings.
			brotliQuality: 4,
			gzipLevel: 3,
			zstdLevel: 3
		} ) );

	console.log(
		`Refreshed ${outdoorFiles.length} outdoor files: ` +
			`${refreshed.builtPackCount} pack(s) rebuilt, ${refreshed.reusedPackCount} reused; ` +
			`${refreshed.changedAssetCount ?? 0} sparse delta(s), ${refreshed.hydratedAssetCount ?? 0} hydrated; ` +
			`${compression.compressionJobs} gzip job(s), ${manifest.files.length} web assets.`
	);
} );

/*
================
timed
================
*/
async function timed( label, task ) {
	const startedAt = performance.now();
	console.log( `[outdoor-refresh] ${label}: start` );
	try {
		const result = await task();
		console.log( `[outdoor-refresh] ${label}: done (${((performance.now() - startedAt) / 1000).toFixed( 1 )}s)` );
		return result;
	} catch ( error ) {
		console.error(
			`[outdoor-refresh] ${label}: failed after ${((performance.now() - startedAt) / 1000).toFixed( 1 )}s`
		);
		throw error;
	}
}

/*
================
seedOutdoorPackCache
================
*/
async function seedOutdoorPackCache( previous ) {
	const existing = await readFile( outdoorPackCacheManifestPath, "utf8" )
		.then( JSON.parse )
		.catch( () => undefined );
	if ( existing?.format === "sro-asset-pack-index" && existing.version === 1 ) {
		return;
	}

	const group = requireOutdoorGroup( previous );
	const seed = {
		format: "sro-asset-pack-index",
		version: 1,
		generatedAt: previous.generatedAt,
		targetPackBytes: previous.targetPackBytes,
		groups: [ structuredClone( group ) ],
		assets: previous.assets
			.filter( ( asset ) => asset.group === OUTDOOR_GROUP )
			.map( ( asset ) => structuredClone( asset ) )
			.sort( ( left, right ) => left.path.localeCompare( right.path ) )
	};
	await mkdir( outdoorPackCacheRoot, { recursive: true } );
	await publishBytesAtomically(
		outdoorPackCacheManifestPath,
		Buffer.from( JSON.stringify( seed ), "utf8" ),
		{ logLabel: "outdoor-pack-cache-seed" }
	);
}

/*
================
requireOutdoorGroup
================
*/
function requireOutdoorGroup( index ) {
	const group = index.groups?.find( ( candidate ) => candidate.name === OUTDOOR_GROUP );
	if ( !group || group.assetCount === 0 || group.packs?.length === 0 ) {
		throw new Error(
			"The main asset manifest has no populated outdoor-world group. Run `pnpm assets build world-outdoor` once before refreshing packs."
		);
	}
	return group;
}

/*
================
collectOutdoorPackFiles
================
*/
async function collectOutdoorPackFiles() {
	const files = await listPublicAssetFiles( {
		publicRoot,
		roots: [ OUTDOOR_PREFIX ],
		extensions: [ ".json", ".gz", ...IMAGE_ASSET_EXTENSIONS ]
	} );
	const compressedJson = files.filter( ( publicPath ) => publicPath.endsWith( ".json.gz" ) );
	const compressedSources = new Set(
		compressedJson.map( ( publicPath ) => publicPath.slice( 0, -".gz".length ) )
	);
	const rawJson = files.filter(
		( publicPath ) => publicPath.endsWith( ".json" ) && !compressedSources.has( publicPath )
	);
	const images = files.filter( isImageAsset );
	return [ ...new Set( [ ...compressedJson, ...rawJson, ...images ] ) ].sort();
}

/*
================
removeSupersededPackFiles
================
*/
async function removeSupersededPackFiles( previous, refreshedGroups ) {
	const currentPackPaths = new Set(
		refreshedGroups.flatMap( ( group ) =>
			group.packs.flatMap( ( pack ) => [ pack.path, pack.zstdPath ].filter( Boolean ).map( normalizePublicPath ) )
		)
	);
	for ( const group of previous.groups.filter( ( candidate ) => candidate.name === OUTDOOR_GROUP ) ) {
		for ( const pack of group.packs ) {
			for ( const publicPath of [ pack.path, pack.zstdPath ].filter( Boolean ) ) {
				if ( currentPackPaths.has( normalizePublicPath( publicPath ) ) ) {
					continue;
				}
				const targetPath = resolvePublicFile( publicPath );
				const relative = path.relative( packsRoot, targetPath );
				if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
					throw new Error( `Refusing to remove pack outside ${packsRoot}: ${targetPath}` );
				}
				await archiveGeneratedArtifact( targetPath, {
					scopeRoot: publicRoot,
					reason: "superseded-outdoor-pack"
				} );
			}
		}
	}
}

/*
================
resolvePublicFile
================
*/
function resolvePublicFile( publicPath ) {
	const normalized = normalizePublicPath( publicPath );
	const resolved = path.resolve( publicRoot, normalized.replace( /^\/+/, "" ) );
	const publicPrefix = `${path.resolve( publicRoot )}${path.sep}`;
	if ( !resolved.startsWith( publicPrefix ) ) {
		throw new Error( `Invalid generated public path ${publicPath}` );
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
