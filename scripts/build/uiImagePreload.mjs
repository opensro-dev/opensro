import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { toPublicPath as toPublicAssetPath } from "./shared/assetPaths.mjs";
import { listFiles, pathExists as exists } from "./shared/fsUtils.mjs";
import { writeJsonIfChanged } from "./shared/jsonOut.mjs";
import { isClaimed, isPublicationOpen, readClaims } from "./shared/publicationLedger.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const assetsRoot = path.join( publicRoot, "assets" );
const imageRoot = path.join( assetsRoot, "images" );
const preloadManifestPath = path.join( assetsRoot, "ui", "preload-images.json" );

const INTERACTIVE_IMAGE_PATTERN = /_(focus|press|disable)\.png$/i;
const NATIVE_INTERFACE_PATH_SEGMENT = "/media_extracted/interface/";
const REASON_PRIORITY = {
	"interactive-state": 3,
	"native-interface": 2,
	"interactive-normal": 1
};

export async function buildUiImagePreloadManifest( options = {} ) {
	const root = options.imageRoot ?? imageRoot;
	const targetPath = options.targetPath ?? preloadManifestPath;
	const rootPublic = options.publicRoot ?? derivePublicRootFromImageRoot( root );
	// Inside a build, only images a current step produced: a stale PNG an older
	// pipeline left behind would otherwise become a manifest dependency, and
	// the ledger audit would refuse the build instead of archiving the file.
	const claimed = options.claimed ?? (isPublicationOpen() ? (await readClaims()).claimed : null);
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

async function collectUiPreloadImages( root, rootPublic, claimed ) {
	const current = ( filePath ) => !claimed || isClaimed( toPublicAssetPath( filePath, rootPublic ), claimed );
	const pngFiles = (await listFiles( root, { extensions: [ ".png" ] } )).filter( current );
	const stateFiles = pngFiles.filter( ( filePath ) => INTERACTIVE_IMAGE_PATTERN.test( filePath ) );
	const byPath = new Map();

	for ( const pngFile of pngFiles ) {
		if ( isNativeInterfaceImage( pngFile ) ) {
			await addImage( byPath, pngFile, rootPublic, "native-interface" );
		}
	}

	for ( const stateFile of stateFiles ) {
		await addImage( byPath, stateFile, rootPublic, "interactive-state" );

		const normalFile = stateFile.replace( INTERACTIVE_IMAGE_PATTERN, ".png" );
		if ( await exists( normalFile ) && current( normalFile ) ) {
			await addImage( byPath, normalFile, rootPublic, "interactive-normal" );
		}
	}

	return [ ...byPath.values() ].sort( comparePreloadImages );
}

async function addImage( byPath, filePath, rootPublic, reason ) {
	const publicPath = toPublicAssetPath( filePath, rootPublic );
	const existing = byPath.get( publicPath );
	if ( existing ) {
		if ( REASON_PRIORITY[reason] > REASON_PRIORITY[existing.reason] ) {
			existing.reason = reason;
		}
		return;
	}

	byPath.set( publicPath, {
		path: publicPath,
		bytes: (await stat( filePath )).size,
		reason
	} );
}

function isNativeInterfaceImage( filePath ) {
	return filePath.replaceAll( "\\", "/" ).toLowerCase().includes( NATIVE_INTERFACE_PATH_SEGMENT );
}

function derivePublicRootFromImageRoot( root ) {
	const resolvedRoot = path.resolve( root );
	const marker = `${path.sep}assets${path.sep}images`;
	const markerIndex = resolvedRoot.toLowerCase().lastIndexOf( marker.toLowerCase() );
	if ( markerIndex >= 0 ) {
		return resolvedRoot.slice( 0, markerIndex );
	}

	return publicRoot;
}

function comparePreloadImages( left, right ) {
	const leftBase = preloadSortBase( left.path );
	const rightBase = preloadSortBase( right.path );
	const baseOrder = leftBase.localeCompare( rightBase );
	if ( baseOrder !== 0 ) {
		return baseOrder;
	}

	return preloadStateOrder( left.path ) - preloadStateOrder( right.path );
}

function preloadSortBase( publicPath ) {
	return publicPath.replace( INTERACTIVE_IMAGE_PATTERN, ".png" );
}

function preloadStateOrder( publicPath ) {
	const match = /_(focus|press|disable)\.png$/i.exec( publicPath );
	if ( !match ) return 0;
	if ( match[1].toLowerCase() === "focus" ) return 1;
	if ( match[1].toLowerCase() === "press" ) return 2;
	return 3;
}
