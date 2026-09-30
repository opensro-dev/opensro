/*
===========================================================================

refresh_world_map_asset_packs.mjs - republish the world-map image closure

Converts and publishes every image the world-map tables and layout reach,
repacks game-images sparsely and removes the packs it superseded.

===========================================================================
*/
import { mergeAssetPackGroupUpdates, publishAssetPackManifest } from "./build/assetPackPublication.mjs";
import { mkdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { refreshGeneratedManifestSidecars } from "./build/generatedManifestSidecars.mjs";
import { patchAssetPackGroupFromLooseFiles } from "./build/sparseAssetPackGroupRefresh.mjs";
import { buildWorldMapImageResources, imagePublicPath } from "./build/shared/cifResources.mjs";
import { runConvertImages } from "./build/shared/convertImagesRunner.mjs";
import { collectWorldMapImageReferences } from "./build/shared/worldMapImageReferences.mjs";
import { buildWebAssetManifest } from "./build/webManifest.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, ".." );
const publicRoot = path.join( rebuildRoot, ".generated", "client-public" );
const packsRoot = path.join( publicRoot, "assets", "packs" );
const packManifestPath = path.join( packsRoot, "manifest.json" );
const refreshRoot = path.join( packsRoot, "incremental", "game-images" );
const GROUP_NAME = "game-images";

await withGeneratedAssetsLock( "world-map asset-pack refresh", async () => {
	const closure = await timed( "dependency closure", collectWorldMapImageReferences );
	const conversion = await timed( "targeted image conversion", () => runConvertImages( closure.references ) );
	if ( conversion.status !== 0 ) {
		throw new Error( `World-map image conversion failed with exit status ${conversion.status}.` );
	}

	const published = await timed( "strict publication", () => buildWorldMapImageResources() );
	const looseFiles = published.references.map( imagePublicPath );
	const previous = JSON.parse( await readFile( packManifestPath, "utf8" ) );
	const previousGroup = requireStartupGroup( previous, GROUP_NAME );
	const refreshed = await timed( "sparse pack refresh", () =>
		patchAssetPackGroupFromLooseFiles( {
			publicRoot,
			outputRoot: refreshRoot,
			previousIndex: previous,
			groupName: GROUP_NAME,
			looseFiles
		} ) );
	const merged = mergeAssetPackGroupUpdates( previous, [ refreshed ] );
	const changed = merged !== previous;

	validateClosure( merged, looseFiles );
	await mkdir( packsRoot, { recursive: true } );
	if ( changed ) {
		await publishAssetPackManifest( publicRoot, packManifestPath, Buffer.from( JSON.stringify( merged ), "utf8" ), {
			logLabel: "world-map-pack-overlay"
		} );
	}
	await timed( "superseded pack cleanup", () => removeSupersededPackFiles( previousGroup, refreshed.groups[0] ) );
	const webManifest = await timed( "web manifest refresh", buildWebAssetManifest );
	await timed( "manifest sidecar refresh", () =>
		refreshGeneratedManifestSidecars( {
			publicRoot,
			onlyWhenStale: true,
			brotliQuality: 4,
			gzipLevel: 3,
			zstdLevel: 3
		} ) );

	console.log(
		`World-map closure OK: ${closure.tileReferences.length} tiles, ` +
			`${closure.mapPageReferences.length} pages, ${closure.overlayReferences.length} overlays; ` +
			`${refreshed.builtPackCount} pack(s) rebuilt, ${refreshed.reusedPackCount} reused, ` +
			`${refreshed.changedAssetCount} asset delta(s), ${webManifest.files.length} web assets.`
	);
} );

/*
================
requireStartupGroup
================
*/
function requireStartupGroup( index, groupName ) {
	const group = index.groups?.find( ( candidate ) => candidate.name === groupName );
	if ( !group || group.assetCount === 0 || group.packs?.length === 0 ) {
		throw new Error( `The main asset manifest has no populated ${groupName} group.` );
	}
	if ( group.load !== "startup" ) {
		throw new Error( `${groupName} must be startup-resident; found load=${JSON.stringify( group.load )}.` );
	}
	return group;
}

/*
================
validateClosure
================
*/
function validateClosure( index, publicPaths ) {
	const assets = new Map( index.assets.map( ( asset ) => [ asset.path.toLowerCase(), asset ] ) );
	const missing = [];
	for ( const publicPath of publicPaths ) {
		const asset = assets.get( publicPath.toLowerCase() );
		if ( !asset || asset.group !== GROUP_NAME ) missing.push( publicPath );
	}
	if ( missing.length > 0 ) {
		throw new Error(
			`World-map pack closure is incomplete (${missing.length} missing): ${missing.slice( 0, 8 ).join( ", " )}`
		);
	}
}

/*
================
removeSupersededPackFiles
================
*/
async function removeSupersededPackFiles( previousGroup, refreshedGroup ) {
	const currentPaths = new Set(
		refreshedGroup.packs.flatMap( ( pack ) =>
			[ pack.path, pack.zstdPath ].filter( Boolean ).map( normalizePublicPath )
		)
	);
	for ( const pack of previousGroup.packs ) {
		for ( const publicPath of [ pack.path, pack.zstdPath ].filter( Boolean ) ) {
			if ( currentPaths.has( normalizePublicPath( publicPath ) ) ) continue;
			const targetPath = resolvePackFile( publicPath );
			await rm( targetPath, { force: true } );
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
		throw new Error( `Refusing to remove pack outside ${packsRoot}: ${resolved}` );
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
	console.log( `[world-map-refresh] ${label}: start` );
	try {
		const result = await task();
		console.log( `[world-map-refresh] ${label}: done (${((performance.now() - startedAt) / 1000).toFixed( 1 )}s)` );
		return result;
	} catch ( error ) {
		console.error(
			`[world-map-refresh] ${label}: failed after ${((performance.now() - startedAt) / 1000).toFixed( 1 )}s`
		);
		throw error;
	}
}
