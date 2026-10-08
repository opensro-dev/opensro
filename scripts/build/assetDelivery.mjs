/*
===========================================================================

assetDelivery.mjs - the delivery metadata of a pack index

Members travel compressed inside their packs (packFormat.mjs, SROPACK2), so
delivery has no second copy of any asset: the gzip transports and their
delivery.json catalog are retired (deliveryVersion 2). What remains is the
per-world animation source index: each animated-objects catalog row lists
the animation sources it names, keyed to its digest, so the client fetches
only the catalogs a scene needs. No region, character, feature or filename
allowlist decides membership.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { gunzipSync, zstdDecompress } from "node:zlib";
import { promisify } from "node:util";
import { decodeStoredMember, readPackPrefix, storedMemberBytes } from "./shared/packFormat.mjs";
import { publishAssetPackManifest, validatePackedFontAtlases } from "./assetPackPublication.mjs";

export const DELIVERY_VERSION = 2;
const ANIMATION_CATALOG = /^\/assets\/world\/[^/]+\/animated-objects\.json(?:\.gz)?$/i;
const MAX_PACK_BYTES = 64 << 20;
const decodePack = promisify( zstdDecompress );

/*
================
isAnimationCatalog
================
*/
export function isAnimationCatalog( publicPath ) {
	return ANIMATION_CATALOG.test( publicPath );
}

/*
================
prepareAssetDelivery

Writes each animation catalog's source index into the pack index rows and
stamps deliveryVersion. A member is read loose when present, else from its
pack (identity or compact copy), and verified against its row.
================
*/
export async function prepareAssetDelivery( index, root ) {
	const packs = new Map( index.groups.flatMap( g => g.packs ).map( p => [ p.path, p ] ) );
	let loadedPath, loadedBytes, dataStart, animationManifests = 0;
	const target = publicPath => {
		const result = path.resolve( root, "." + publicPath ), relative = path.relative( root, result );
		if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
			throw Error( "Delivery path escaped publication" );
		}
		return result;
	};

	/*
	================
	memberBytes
	================
	*/
	async function memberBytes( entry ) {
		const loose = await readFile( target( entry.path ) ).catch( error => {
			if ( error.code !== "ENOENT" ) throw error;
			return null;
		} );
		if ( loose ) return decodeStoredMember( loose, { ...entry, stored: undefined } );
		if ( loadedPath !== entry.packPath ) {
			const pack = packs.get( entry.packPath );
			if ( !pack ) throw Error( "Missing delivery pack: " + entry.packPath );
			loadedBytes = await readFile( target( pack.path ) ).catch( async error => {
				if ( error.code !== "ENOENT" ) throw error;
				return decodePack( await readFile( target( pack.zstdPath ?? pack.path + ".zst" ) ), {
					maxOutputLength: MAX_PACK_BYTES
				} );
			} );
			if ( loadedBytes.length !== pack.bytes ) throw Error( "Delivery pack length mismatch: " + pack.path );
			dataStart = readPackPrefix( loadedBytes, pack.path );
			loadedPath = entry.packPath;
		}
		return decodeStoredMember( storedMemberBytes( loadedBytes, dataStart, entry, entry.packPath ), entry );
	}

	const catalogs = index.assets.filter( entry => isAnimationCatalog( entry.path ) )
		.sort( ( a, b ) => a.packPath.localeCompare( b.packPath ) || a.offset - b.offset );
	for ( const entry of catalogs ) {
		const raw = await memberBytes( entry );
		const document = JSON.parse( (entry.path.endsWith( ".gz" ) ? gunzipSync( raw ) : raw).toString( "utf8" ) );
		if ( !document.objects || typeof document.objects !== "object" || Array.isArray( document.objects ) ) {
			throw Error( "Invalid animation catalog: " + entry.path );
		}
		entry.animationSources = Object.keys( document.objects ).sort();
		entry.animationDigest = entry.sha256;
		animationManifests++;
	}
	index.deliveryVersion = DELIVERY_VERSION;
	validateAssetDelivery( index );
	return { animationManifests };
}

/*
================
refreshAssetDelivery

Every publication that rebuilds the web manifest passes through this owner,
including sparse feature refreshes: the installed index gets its animation
source index, and is republished only when that changed it.
================
*/
export async function refreshAssetDelivery( root ) {
	const filename = path.join( root, "assets/packs/manifest.json" );
	let source;
	try {
		source = await readFile( filename, "utf8" );
	} catch ( error ) {
		if ( error.code === "ENOENT" ) return null;
		throw error;
	}
	const index = JSON.parse( source );
	await validatePackedFontAtlases( index, root );
	const report = await prepareAssetDelivery( index, root ), next = JSON.stringify( index );
	if ( next !== source ) {
		await publishAssetPackManifest( root, filename, Buffer.from( next ), { logLabel: "asset-delivery-index" } );
	}
	return report;
}

/*
================
validateAssetDelivery

Throws unless the index is delivery version 2, carries no transport, and
every world animation catalog carries its current source index.
================
*/
export function validateAssetDelivery( index ) {
	if ( index.deliveryVersion !== DELIVERY_VERSION ) {
		throw Error( "Asset delivery publication is missing or unsupported; run the owned publisher" );
	}
	for ( const entry of index.assets ) {
		if ( entry.transport !== undefined ) throw Error( "Retired delivery transport on " + entry.path );
		if (
			isAnimationCatalog( entry.path ) &&
			(!Array.isArray( entry.animationSources ) || entry.animationDigest !== entry.sha256)
		) throw Error( "Stale or missing animation source index: " + entry.path );
	}
}
