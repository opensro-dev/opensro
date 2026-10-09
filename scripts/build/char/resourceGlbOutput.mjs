import fs from "node:fs";
import path from "node:path";
import { normalizeAssetPath } from "../shared/assetPaths.mjs";
import { readJsonOrNullSync, writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { npcManifestModels } from "../shared/npcManifest.mjs";

/** Canonicalize one native BSR resource identity before it reaches an output map. */
export function normalizeBsrResourcePath( value ) {
	const normalized = normalizeAssetPath( value );
	if ( !normalized.startsWith( "res/" ) || !normalized.endsWith( ".bsr" ) ) {
		throw new Error( `Expected a res/.../*.bsr resource path, got "${value}"` );
	}
	if (
		normalized !== path.posix.normalize( normalized ) ||
		normalized.split( "/" ).some( ( segment ) => !segment || segment === "." || segment === ".." )
	) {
		throw new Error( `Unsafe BSR resource path "${value}"` );
	}
	return normalized;
}

/**
 * Preserve the complete native path below res/ inside a caller-owned public
 * namespace. Basenames are not resource identities: res/mob/europe/wolf.bsr
 * and res/mob/asiam/wolf.bsr must never address the same generated file.
 */
export function resourceGlbOutput( resourcePath, { namespace, publicAssetsRoot } ) {
	if ( !/^[a-z0-9][a-z0-9_-]*$/i.test( namespace ) ) {
		throw new Error( `Unsafe generated-asset namespace "${namespace}"` );
	}
	const sourcePath = normalizeBsrResourcePath( resourcePath );
	const relativePath = sourcePath.slice( "res/".length ).replace( /\.bsr$/i, ".glb" );
	const namespaceRoot = path.resolve( publicAssetsRoot, namespace );
	const diskPath = path.resolve( namespaceRoot, ...relativePath.split( "/" ) );
	if ( !diskPath.startsWith( `${namespaceRoot}${path.sep}` ) ) {
		throw new Error( `Generated GLB escaped ${namespaceRoot}: ${diskPath}` );
	}
	return {
		sourcePath,
		relativePath,
		publicPath: `/assets/${namespace}/${relativePath}`,
		diskPath
	};
}

/** Fail before writing when two distinct native resources claim one output. */
export function claimResourceOutput( ownersByOutput, sourcePath, publicPath ) {
	const owner = normalizeBsrResourcePath( sourcePath );
	const outputKey = String( publicPath ).replaceAll( "\\", "/" ).toLowerCase();
	const priorOwner = ownersByOutput.get( outputKey );
	if ( priorOwner && priorOwner !== owner ) {
		throw new Error(
			`Generated GLB output collision at ${publicPath}: ${priorOwner} and ${owner}`
		);
	}
	ownersByOutput.set( outputKey, owner );
}

/** Read only the generated GLB identities needed for a safe manifest diff. */
export function readPreviousResourceGlbPaths( manifestPath ) {
	const previousManifest = readJsonOrNullSync( manifestPath );
	// A normalized manifest (npc v9) keeps the GLB on its resources; the join
	// returns any other manifest's models unchanged.
	return Object.values( npcManifestModels( previousManifest ) )
		.flatMap( ( model ) => [ model?.glb, ...Object.values( model?.materialVariants ?? {} ) ] )
		.filter( Boolean );
}

/**
 * Remove only files named by the previous manifest and no longer named by
 * the replacement. The namespace containment check prevents a corrupt old
 * manifest from widening deletion scope.
 */
export function removeSupersededResourceOutputs( {
	previousPublicPaths,
	currentPublicPaths,
	namespace,
	publicAssetsRoot
} ) {
	const namespacePublicPrefix = `/assets/${namespace}/`;
	const namespaceRoot = path.resolve( publicAssetsRoot, namespace );
	const retained = new Set(
		currentPublicPaths.map( ( value ) => String( value ).replaceAll( "\\", "/" ).toLowerCase() )
	);
	const removed = [];

	for ( const value of new Set( previousPublicPaths ) ) {
		const publicPath = String( value ).replaceAll( "\\", "/" );
		if ( !publicPath.toLowerCase().startsWith( namespacePublicPrefix.toLowerCase() ) ) {
			throw new Error( `Previous manifest output is outside ${namespacePublicPrefix}: ${publicPath}` );
		}
		if ( retained.has( publicPath.toLowerCase() ) ) {
			continue;
		}
		const relativePath = publicPath.slice( namespacePublicPrefix.length );
		const diskPath = path.resolve( namespaceRoot, ...relativePath.split( "/" ) );
		if ( !diskPath.startsWith( `${namespaceRoot}${path.sep}` ) ) {
			throw new Error( `Previous manifest output escaped ${namespaceRoot}: ${publicPath}` );
		}
		if ( fs.existsSync( diskPath ) ) {
			fs.unlinkSync( diskPath );
			removed.push( publicPath );
		}
	}
	return removed;
}

/**
 * Publish one resource-model manifest, refresh its transport sidecars, then
 * remove only outputs explicitly retired by the manifest diff.
 */
export async function finalizeResourceGlbManifest( {
	manifestPath,
	manifest,
	previousPublicPaths,
	currentPublicPaths,
	namespace,
	publicAssetsRoot
} ) {
	fs.mkdirSync( path.dirname( manifestPath ), { recursive: true } );
	writeJsonIfChangedSync( manifestPath, manifest );
	await refreshPrecompressedSidecars( [ manifestPath ], { onlyWhenStale: true } );
	return removeSupersededResourceOutputs( {
		previousPublicPaths,
		currentPublicPaths,
		namespace,
		publicAssetsRoot
	} );
}
