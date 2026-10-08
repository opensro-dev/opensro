/*
===========================================================================

generatedManifestSidecars.mjs - precompressed sidecars for generated manifests

Writes the .gz copy of a generated manifest from its current bytes (the one
published sidecar, PUBLISHED_SIDECAR_SUFFIXES).

Freshness is not cosmetic. A precompressed sidecar is served in place of
its asset without comparing mtimes, so one older than its asset silently
replaces it for every real user while curl and Node's fs still see the
fresh bytes, which makes the bug look impossible. It happened (when the dev
middleware still served .br): assets/anim/manifest.json gained its motion-0x26
pickup entries on 2026-07-24, its sidecars stayed at 2026-07-08, and every
browser fetched a manifest with no pick clip for sixteen days; the placeholder
length also drove the 0x2476 busy-motion gate, so it surfaced as a
multi-second input lockout after picking an item up.

So whatever writes a generated manifest refreshes its sidecars in the same
pass.

===========================================================================
*/
import { readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { compressGzipSync } from "./shared/compressionUtils.mjs";
import { publicRoot } from "./world/paths.mjs";

// Shared compression helpers keep these byte-comparable with the bulk optimizer.
const ENCODINGS = [
	{
		suffix: ".gz",
		compress: ( bytes, options ) => compressGzipSync( bytes, { level: options.gzipLevel } )
	}
];

// The loose manifests every publication regenerates, relative to the public root.
const GENERATED_MANIFESTS = [
	[ "assets", "packs", "manifest.json" ],
	[ "assets", "packs", "delivery.json" ],
	[ "assets", "manifest.json" ]
];

/*
================
mtimeMs

The file's modification time, or null when it does not exist.
================
*/
async function mtimeMs( filePath ) {
	try {
		return (await stat( filePath )).mtimeMs;
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return null;
		throw error;
	}
}

/*
================
refreshPrecompressedSidecars

Rewrites the .gz sidecar of each asset from its current bytes and
returns one record per asset. `onlyWhenStale` skips assets whose sidecars
are all at least as new as the asset, which makes this cheap enough to call
unconditionally at the end of a build step. Options: onlyWhenStale,
gzipLevel.
================
*/
export async function refreshPrecompressedSidecars( assetPaths, options = {} ) {
	const { onlyWhenStale = false, gzipLevel } = options;
	const results = [];
	for ( const assetPath of assetPaths ) {
		const assetMs = await mtimeMs( assetPath );
		if ( assetMs === null ) {
			results.push( { assetPath, skipped: "missing" } );
			continue;
		}
		if ( onlyWhenStale ) {
			const sidecarTimes = await Promise.all(
				ENCODINGS.map( ( { suffix } ) => mtimeMs( `${assetPath}${suffix}` ) )
			);
			if ( sidecarTimes.every( ( ms ) => ms !== null && ms >= assetMs ) ) {
				results.push( { assetPath, skipped: "fresh" } );
				continue;
			}
		}
		const bytes = await readFile( assetPath );
		const written = [];
		for ( const { suffix, compress } of ENCODINGS ) {
			const compressed = compress( bytes, { gzipLevel } );
			await writeFile( `${assetPath}${suffix}`, compressed );
			written.push( { suffix, bytes: compressed.byteLength } );
		}
		results.push( { assetPath, sourceBytes: bytes.byteLength, written } );
	}
	return results;
}

/*
================
refreshGeneratedManifestSidecars

Refreshes the sidecars of every generated loose manifest. Options as
refreshPrecompressedSidecars, plus publicRoot for fixture trees.
================
*/
export async function refreshGeneratedManifestSidecars( options = {} ) {
	const root = options.publicRoot ?? publicRoot;
	await refreshPrecompressedSidecars(
		GENERATED_MANIFESTS.map( ( parts ) => path.join( root, ...parts ) ),
		options
	);
}
