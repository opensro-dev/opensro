/*
===========================================================================

assetDelivery.mjs - lossless transport products for the published packs

Derives, from the admitted pack publication alone, a gzip transport copy of
every large record and GLB, the per-world animation source index, and the
delivery.json catalog the client validates both against. An asset's digest
is the invalidation key for both products; bytes are never quantized, and
no region, character, feature or filename allowlist decides membership.

===========================================================================
*/
import { readFile, stat, mkdir } from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { gzip, gunzipSync, zstdDecompress } from "node:zlib";
import { promisify } from "node:util";
import { publishBytesAtomically } from "./shared/atomicPublish.mjs";
import { publishAssetPackManifest, validatePackedFontAtlases } from "./assetPackPublication.mjs";
import { refreshPrecompressedSidecars } from "./generatedManifestSidecars.mjs";
const compress = promisify( gzip ), decodePack = promisify( zstdDecompress );
/*
================
hash
================
*/
function hash( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
prepareAssetDelivery

Builds the transport copies and the delivery catalog for one pack index.
A partial index owns partial metadata beside itself. Identical metadata is
not rewritten, and the global catalog's precompressed sidecars are
refreshed in the same pass (see generatedManifestSidecars.mjs for why).
================
*/
export async function prepareAssetDelivery(
	index,
	root,
	metadataPath = path.join( root, "assets/packs/delivery.json" )
) {
	// A partial index owns partial metadata beside itself. It must never replace
	// the global delivery catalog before the incremental index has been merged.
	const relative = path.relative( root, metadataPath );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw Error( "Delivery metadata escaped publication" );
	}
	let previousRows = [];
	try {
		const previous = JSON.parse( await readFile( metadataPath, "utf8" ) );
		if ( previous.version === 1 ) previousRows = previous.assets;
	} catch ( error ) {
		if ( error.code !== "ENOENT" ) throw error;
	}
	const previousMetadata = new Map( previousRows.map( e => [ e.path, e ] ) ), metadata = [];
	const packs = new Map( index.groups.flatMap( g => g.packs ).map( p => [ p.path, p ] ) );
	let loadedPath, loadedBytes, dataStart;
	const target = publicPath => {
		const result = path.resolve( root, "." + publicPath ), relative = path.relative( root, result );
		if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
			throw Error( "Delivery path escaped publication" );
		}
		return result;
	};
	async function bytes( entry ) {
		let value;
		try {
			value = await readFile( target( entry.path ) );
		} catch ( error ) {
			if ( error.code !== "ENOENT" ) throw error;
			if ( loadedPath !== entry.packPath ) {
				const pack = packs.get( entry.packPath );
				if ( !pack ) throw Error( "Missing delivery pack" );
				try {
					loadedBytes = await readFile( target( pack.path ) );
				} catch ( error ) {
					if ( error.code !== "ENOENT" ) throw error;
					loadedBytes = await decodePack( await readFile( target( pack.zstdPath ?? pack.path + ".zst" ) ), {
						maxOutputLength: 64 << 20
					} );
				}
				if (
					loadedBytes.length !== pack.bytes || hash( loadedBytes ) !== pack.sha256 ||
					loadedBytes.subarray( 0, 8 ).toString() !== "SROPACK1"
				) throw Error( "Delivery pack integrity mismatch" );
				dataStart = 12 + loadedBytes.readUInt32LE( 8 );
				loadedPath = entry.packPath;
			}
			value = loadedBytes.subarray( dataStart + entry.offset, dataStart + entry.offset + entry.length );
		}
		if ( value.length !== entry.length || hash( value ) !== entry.sha256 ) {
			throw Error( "Delivery source integrity mismatch: " + entry.path );
		}
		return value;
	}
	let compressedBytes = 0, identityBytes = 0, compressedMembers = 0, animationManifests = 0;
	const folder = target( "/assets/packs/transport" );
	await mkdir( folder, { recursive: true } );
	for (
		const entry of [ ...index.assets ].sort( ( a, b ) =>
			a.packPath.localeCompare( b.packPath ) || a.offset - b.offset
		)
	) {
		const animation = /^\/assets\/world\/[^/]+\/animated-objects\.json(?:\.gz)?$/i.test( entry.path );
		let raw;
		if ( animation ) {
			raw = await bytes( entry );
			const document = JSON.parse( (entry.path.endsWith( ".gz" ) ? gunzipSync( raw ) : raw).toString( "utf8" ) );
			if ( !document.objects || typeof document.objects !== "object" || Array.isArray( document.objects ) ) {
				throw Error( "Invalid animation catalog: " + entry.path );
			}
			entry.animationSources = Object.keys( document.objects ).sort();
			entry.animationDigest = entry.sha256;
			animationManifests++;
		}
		// Small records keep existing request batching; already-compressed records
		// keep identity when gzip would save less than ten percent.
		if ( entry.length < 65536 && !/\.glb$/i.test( entry.path ) ) continue;
		let sourceStat;
		try {
			const s = await stat( target( entry.path ) );
			sourceStat = { size: s.size, mtimeMs: s.mtimeMs, ctimeMs: s.ctimeMs };
		} catch ( error ) {
			if ( error.code !== "ENOENT" ) throw error;
		}
		const previous = entry.delivery ?? previousMetadata.get( entry.path );
		delete entry.delivery;
		let transport;
		if ( previous?.sourceSha256 === entry.sha256 && previous.version === 1 ) {
			if ( previous.transport ?? entry.transport ) {
				const t = previous.transport ?? entry.transport;
				try {
					const value = await readFile( target( t.path ) );
					if ( value.length === t.length && hash( value ) === t.sha256 ) transport = t;
				} catch ( error ) {
					if ( error.code !== "ENOENT" ) throw error;
				}
			} else {
				delete entry.transport;
				metadata.push( { ...previous, path: entry.path } );
				continue;
			}
		}
		if ( !transport ) {
			raw ??= await bytes( entry );
			const encoded = await compress( raw, { level: 6 } );
			if ( encoded.length <= raw.length * 0.9 ) {
				const digest = hash( encoded ), publicPath = "/assets/packs/transport/" + digest + ".gz";
				await publishBytesAtomically( target( publicPath ), encoded, { logLabel: "asset-delivery" } );
				transport = { path: publicPath, length: encoded.length, sha256: digest, encoding: "gzip" };
			}
		}
		metadata.push( {
			path: entry.path,
			version: 1,
			sourceSha256: entry.sha256,
			...(transport ? { transport } : {}),
			...(sourceStat ? { sourceStat } : {})
		} );
		if ( transport ) {
			entry.transport = transport;
			compressedMembers++;
			identityBytes += entry.length;
			compressedBytes += transport.length;
		} else delete entry.transport;
	}
	index.deliveryVersion = 1;
	validateAssetDelivery( index, { version: 1, assets: metadata } );
	metadata.sort( ( a, b ) => a.path.localeCompare( b.path ) );
	await publishBytesAtomically( metadataPath, Buffer.from( JSON.stringify( { version: 1, assets: metadata } ) ), {
		logLabel: "asset-delivery-metadata",
		skipIfUnchanged: true
	} );
	if ( path.resolve( metadataPath ) === path.resolve( root, "assets/packs/delivery.json" ) ) {
		await refreshPrecompressedSidecars( [ metadataPath ], { onlyWhenStale: true } );
	}
	if ( Buffer.byteLength( JSON.stringify( index ) ) > (16 << 20) ) {
		throw Error( "Delivery manifest exceeds browser admission budget" );
	}
	return { compressedMembers, identityBytes, compressedBytes, animationManifests };
}

/*
================
refreshAssetDelivery

Every publication that rebuilds the web manifest passes through this owner,
including sparse feature refreshes. Old indexes are upgraded automatically.
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

Throws unless every large record and GLB has current delivery metadata and
every world animation catalog carries its source index.
================
*/
export function validateAssetDelivery( index, metadata ) {
	if ( index.deliveryVersion !== 1 || metadata.version !== 1 ) {
		throw Error( "Asset delivery publication is missing or unsupported; run the owned publisher" );
	}
	const rows = new Map( metadata.assets.map( e => [ e.path, e ] ) );
	for ( const entry of index.assets ) {
		if ( entry.length >= 65536 || /\.glb$/i.test( entry.path ) ) {
			const row = rows.get( entry.path );
			if (
				!row || row.sourceSha256 !== entry.sha256 ||
				JSON.stringify( row.transport ) !== JSON.stringify( entry.transport )
			) throw Error( "Stale or missing generated delivery: " + entry.path );
		}
		if (
			/^\/assets\/world\/[^/]+\/animated-objects\.json(?:\.gz)?$/i.test( entry.path ) &&
			(!Array.isArray( entry.animationSources ) || entry.animationDigest !== entry.sha256)
		) throw Error( "Stale or missing animation source index: " + entry.path );
	}
}
