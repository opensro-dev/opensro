/*
===========================================================================

assetPackPublication.mjs - the one boundary for publishing a pack index

Validates a complete pack index (structure, and every packed font atlas
against its own descriptor), publishes it atomically, and, for the main
index, retires the pack outputs it no longer uses. Also holds the single
merge rule focused publishers use to replace their groups.

===========================================================================
*/

import { ASSET_SCHEMA } from "./assetSchema.mjs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { gunzipSync, zstdDecompressSync } from "node:zlib";
import { publishBytesAtomically } from "./shared/atomicPublish.mjs";
import { validateAssetPackIndex } from "./assetPackIndexValidation.mjs";
import { collectPackGarbage } from "./assetPackGarbage.mjs";

const hash = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );

// One publication boundary for complete pack indexes. A valid hash for each
// file does NOT prove a valid atlas: coordinates and pixels must be a pair.
// Read candidate PACK bytes, never loose files (which may be a newer build).
// Discover descriptors in the font namespace; image identity comes from the
// descriptor, so adding a font does not require another filename allowlist.
export async function validatePackedFontAtlases( index, publicRoot, { partial = false } = {} ) {
	const assets = new Map( index.assets.map( a => [ a.path, a ] ) );
	const packs = new Map( index.groups.flatMap( g => g.packs ).map( p => [ p.path, p ] ) );
	const cache = new Map();
	const resolve = publicPath => {
		const result = path.resolve( publicRoot, "." + publicPath ), relative = path.relative( publicRoot, result );
		if ( !publicPath.startsWith( "/" ) || relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
			throw Error( "Font publication path escaped root" );
		}
		return result;
	};
	async function member( entry ) {
		let bytes = cache.get( entry.packPath );
		if ( !bytes ) {
			const pack = packs.get( entry.packPath );
			if ( !pack ) throw Error( "Missing font pack: " + entry.packPath );
			try {
				bytes = await readFile( resolve( pack.path ) );
			} catch ( error ) {
				if ( error.code !== "ENOENT" ) throw error;
				bytes = zstdDecompressSync( await readFile( resolve( pack.zstdPath ?? pack.path + ".zst" ) ), {
					maxOutputLength: 64 << 20
				} );
			}
			if (
				bytes.length !== pack.bytes || hash( bytes ) !== pack.sha256 ||
				bytes.subarray( 0, 8 ).toString() !== "SROPACK1"
			) throw Error( "Font pack integrity mismatch: " + pack.path );
			cache.set( entry.packPath, bytes );
		}
		const start = 12 + bytes.readUInt32LE( 8 ) + entry.offset,
			result = bytes.subarray( start, start + entry.length );
		if ( result.length !== entry.length || hash( result ) !== entry.sha256 ) {
			throw Error( "Font member integrity mismatch: " + entry.path );
		}
		return result;
	}
	for ( const entry of index.assets ) {
		if ( !/^\/assets\/fonts\/[^/]+\.json(?:\.gz)?$/i.test( entry.path ) ) continue;
		const raw = await member( entry ),
			atlas = JSON.parse( (entry.path.endsWith( ".gz" ) ? gunzipSync( raw ) : raw).toString( "utf8" ) );
		if ( !atlas.fonts || !atlas.image || !atlas.atlasWidth || !atlas.atlasHeight ) continue;
		const image = assets.get( atlas.image );
		if ( !image ) {
			if ( partial ) continue;
			throw Error( "Font atlas image missing from candidate: " + atlas.image );
		}
		const png = await member( image );
		if (
			png.length < 24 || png.subarray( 0, 8 ).toString( "hex" ) !== "89504e470d0a1a0a" ||
			png.readUInt32BE( 16 ) !== atlas.atlasWidth || png.readUInt32BE( 20 ) !== atlas.atlasHeight
		) throw Error( "Font atlas dimensions disagree with packed image: " + entry.path );
		// Legacy descriptors can still be audited by dimensions. Newly generated
		// atlases bind the exact PNG digest, including same-size glyph rearrangements.
		if ( atlas.imageSha256 !== undefined && atlas.imageSha256 !== image.sha256 ) {
			throw Error( "Font atlas image digest mismatch: " + entry.path );
		}
	}
}

// Fail closed: a merged index the client would reject (duplicate owner, count
// drift, missing pack) must never replace the live one. On 2026-09-23 a
// focused equipment publish appended equipment-models over a full build whose
// game-models already held the same GLBs; the client refused the whole index
// and every asset request failed.
export async function publishAssetPackManifest( publicRoot, filename, bytes, options = {} ) {
	const index = JSON.parse( bytes.toString( "utf8" ) );
	validateAssetPackIndex( index );
	const mainIndex = path.resolve( filename ) === path.resolve( publicRoot, "assets", "packs", "manifest.json" );
	// The live index names the data's format; clients refuse any other.
	if ( mainIndex && index.assetSchema !== ASSET_SCHEMA ) {
		throw Error(
			`Asset pack index declares asset schema ${index.assetSchema}; this pipeline writes ${ASSET_SCHEMA}`
		);
	}
	await validatePackedFontAtlases( index, publicRoot );
	const published = await publishBytesAtomically( filename, bytes, options );
	// Focused publishers write each refresh into fresh slot folders; once the
	// main index is live, retire the packs it no longer uses so they never pile up.
	if ( mainIndex ) {
		await collectPackGarbage( { publicRoot, apply: true, index } );
	}
	return published;
}

/**
 * The one merge for focused publishers: replace `replacedGroups` wholesale
 * with the groups in `updates`, keep every other group, and never splice
 * rows out of a group it does not rebuild. Removing rows from an untouched
 * pack would break its entry count, so cross-group ownership is decided up
 * front by assetPackOwnership.mjs and enforced at publication by
 * validateAssetPackIndex.
 */
export function mergeAssetPackGroupUpdates(
	previous,
	updates,
	replacedGroups = updates.flatMap( update => update.groups.map( group => group.name ) )
) {
	// A focused publisher writes today's formats; merged into data of another
	// schema it would publish a mix no client reads. Rebuild everything instead.
	if ( previous.assetSchema !== ASSET_SCHEMA ) {
		throw Error(
			`Published data is asset schema ${previous.assetSchema}; this pipeline writes ${ASSET_SCHEMA}. ` +
				"Run the full asset build."
		);
	}
	const replaced = new Set( replacedGroups );
	for ( const update of updates ) {
		for ( const group of update.groups ) {
			if ( !replaced.has( group.name ) ) {
				throw Error( `Asset pack update ${group.name} is not declared as replaced` );
			}
		}
	}
	return {
		...previous,
		generatedAt: new Date().toISOString(),
		groups: [
			...previous.groups.filter( group => !replaced.has( group.name ) ),
			...updates.flatMap( update => update.groups )
		].sort( ( a, b ) => a.name.localeCompare( b.name ) ),
		assets: [
			...previous.assets.filter( asset => !replaced.has( asset.group ) ),
			...updates.flatMap( update => update.assets )
		].sort( ( a, b ) => a.path.localeCompare( b.path ) )
	};
}
