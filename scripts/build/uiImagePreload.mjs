/*
===========================================================================

uiImagePreload.mjs - publish the full build's eligible UI image inventory

Only currently claimed images enter the preload document. Focused ownership
uses the same pure eligibility rule without replacing this document from a
partial loose projection.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { stat } from "node:fs/promises";
import path from "node:path";
import { toPublicPath as toPublicAssetPath } from "./shared/assetPaths.mjs";
import { listFiles } from "./shared/fsUtils.mjs";
import { writeJsonIfChanged } from "./shared/jsonOut.mjs";
import { currentClaims, isClaimed, isPublicationOpen } from "./shared/publicationLedger.mjs";
import { INTERACTIVE_IMAGE_PATTERN, uiImagePreloadReason } from "./shared/uiImageEligibility.mjs";

const publicRoot = CLIENT_PUBLIC_ROOT;
const assetsRoot = path.join( publicRoot, "assets" );
const imageRoot = path.join( assetsRoot, "images" );
const preloadManifestPath = path.join( assetsRoot, "ui", "preload-images.json" );

/*
================
buildUiImagePreloadManifest
================
*/
export async function buildUiImagePreloadManifest( options = {} ) {
	const root = options.imageRoot ?? imageRoot;
	const targetPath = options.targetPath ?? preloadManifestPath;
	const rootPublic = options.publicRoot ?? derivePublicRootFromImageRoot( root );
	// Inside a build, only images a current owner produced (currentClaims, the
	// set the audit uses): a stale PNG an older pipeline or a retired family
	// left behind would otherwise become a manifest dependency, and the audit
	// would refuse the build instead of archiving the file.
	const claimed = options.claimed ?? (isPublicationOpen() ? await currentClaims() : null);
	const images = await collectUiPreloadImages( root, rootPublic, claimed );

	// No timestamp: the same images give the same bytes on every machine, so a
	// fresh clone packs exactly what this one does (and nothing re-downloads).
	const manifest = {
		format: "sro-image-preload-manifest",
		version: 1,
		images
	};

	await writeJsonIfChanged( targetPath, manifest );

	return {
		publicPath: "/assets/ui/preload-images.json",
		outputPath: targetPath,
		imageCount: images.length,
		totalBytes: images.reduce( ( sum, image ) => sum + image.bytes, 0 ),
		images
	};
}

/*
================
collectUiPreloadImages
================
*/
async function collectUiPreloadImages( root, rootPublic, claimed ) {
	const current = ( filePath ) => !claimed || isClaimed( toPublicAssetPath( filePath, rootPublic ), claimed );
	const pngFiles = (await listFiles( root, { extensions: [ ".png" ] } )).filter( current );
	const inventory = new Set( pngFiles.map( file => toPublicAssetPath( file, rootPublic ).toLowerCase() ) );
	const images = [];
	for ( const file of pngFiles ) {
		const publicPath = toPublicAssetPath( file, rootPublic );
		const reason = uiImagePreloadReason( publicPath, inventory );
		if ( reason ) images.push( { path: publicPath, bytes: (await stat( file )).size, reason } );
	}
	return images.sort( comparePreloadImages );
}

/*
================
derivePublicRootFromImageRoot
================
*/
function derivePublicRootFromImageRoot( root ) {
	const resolvedRoot = path.resolve( root );
	const marker = `${path.sep}assets${path.sep}images`;
	const markerIndex = resolvedRoot.toLowerCase().lastIndexOf( marker.toLowerCase() );
	if ( markerIndex >= 0 ) {
		return resolvedRoot.slice( 0, markerIndex );
	}

	return publicRoot;
}

/*
================
comparePreloadImages
================
*/
function comparePreloadImages( left, right ) {
	const leftBase = preloadSortBase( left.path );
	const rightBase = preloadSortBase( right.path );
	const baseOrder = leftBase.localeCompare( rightBase );
	if ( baseOrder !== 0 ) {
		return baseOrder;
	}

	return preloadStateOrder( left.path ) - preloadStateOrder( right.path );
}

/*
================
preloadSortBase
================
*/
function preloadSortBase( publicPath ) {
	return publicPath.replace( INTERACTIVE_IMAGE_PATTERN, ".png" );
}

/*
================
preloadStateOrder
================
*/
function preloadStateOrder( publicPath ) {
	const match = /_(focus|press|disable)\.png$/i.exec( publicPath );
	if ( !match ) return 0;
	if ( match[1].toLowerCase() === "focus" ) return 1;
	if ( match[1].toLowerCase() === "press" ) return 2;
	return 3;
}
