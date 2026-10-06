import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import * as zlib from "node:zlib";
import { CLIENT_PUBLIC_ROOT } from "./generatedRoot.mjs";

const defaultPublicRoot = CLIENT_PUBLIC_ROOT;
const indexCache = new Map();
const packCache = new Map();

/** Read the installed public-asset authority in either loose or compact form. */
export function readPublishedAssetBytesSync( publicPath, publicRoot = defaultPublicRoot ) {
	const normalized = normalizePublicPath( publicPath, publicRoot );
	const loosePath = resolveInsidePublic( publicRoot, normalized );
	if ( existsSync( loosePath ) ) return readFileSync( loosePath );

	return readPackedAssetBytesSync( normalized, publicRoot );
}

/** Verify the delivered pack even when a newer loose build shadows it. */
export function readPackedAssetBytesSync( publicPath, publicRoot = defaultPublicRoot ) {
	const normalized = normalizePublicPath( publicPath, publicRoot );
	const loosePath = resolveInsidePublic( publicRoot, normalized );

	const index = loadPackIndex( publicRoot );
	let entry = index.assetsByPath.get( normalized.toLowerCase() );
	let decodeGzip = false;
	if ( !entry && normalized.toLowerCase().endsWith( ".json" ) ) {
		entry = index.assetsByPath.get( `${normalized.toLowerCase()}.gz` );
		decodeGzip = Boolean( entry );
	}
	if ( !entry ) throw missingPublishedAsset( normalized, loosePath );

	const packKey = entry.packPath.toLowerCase();
	const pack = index.packsByPath.get( packKey );
	if ( !pack ) throw new Error( `Pack manifest has no descriptor for ${entry.packPath} (${normalized}).` );
	const loadedPack = readPackIdentity( publicRoot, pack, index.assetsByPackPath.get( packKey ) ?? [] );
	const headerEntry = loadedPack.entriesByPath.get( entry.path.toLowerCase() );
	if ( !headerEntry || !sameAssetDescriptor( headerEntry, entry ) ) {
		throw new Error( `Pack header does not match the manifest entry for ${entry.path}.` );
	}
	const start = loadedPack.dataStart + entry.offset;
	const end = start + entry.length;
	if ( start < loadedPack.dataStart || end > loadedPack.bytes.length ) {
		throw new Error( `Packed asset ${entry.path} exceeds ${pack.path}.` );
	}
	const assetBytes = loadedPack.bytes.subarray( start, end );
	const digest = createHash( "sha256" ).update( assetBytes ).digest( "hex" );
	if ( digest !== entry.sha256.toLowerCase() ) {
		throw new Error( `Packed asset SHA-256 mismatch: ${entry.path}.` );
	}
	return decodeGzip ? zlib.gunzipSync( assetBytes ) : assetBytes;
}

export async function readPublishedAssetBytes( publicPath, publicRoot = defaultPublicRoot ) {
	return readPublishedAssetBytesSync( publicPath, publicRoot );
}

export function readPublishedAssetTextSync( publicPath, publicRoot = defaultPublicRoot ) {
	return readPublishedAssetBytesSync( publicPath, publicRoot ).toString( "utf8" );
}

export async function readPublishedAssetText( publicPath, publicRoot = defaultPublicRoot ) {
	return readPublishedAssetTextSync( publicPath, publicRoot );
}

/** @returns {any} Parsed test data; callers assert or annotate the schema they consume. */
export function readPublishedAssetJsonSync( publicPath, publicRoot = defaultPublicRoot ) {
	return /** @type {any} */ (JSON.parse( readPublishedAssetTextSync( publicPath, publicRoot ) ));
}

/** @returns {Promise<any>} Parsed test data; callers assert or annotate the schema they consume. */
export async function readPublishedAssetJson( publicPath, publicRoot = defaultPublicRoot ) {
	return /** @type {any} */ (readPublishedAssetJsonSync( publicPath, publicRoot ));
}

export function publishedAssetExistsSync( publicPath, publicRoot = defaultPublicRoot ) {
	const normalized = normalizePublicPath( publicPath, publicRoot );
	if ( existsSync( resolveInsidePublic( publicRoot, normalized ) ) ) return true;
	let index;
	try {
		index = loadPackIndex( publicRoot );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return false;
		throw error;
	}
	return index.assetsByPath.has( normalized.toLowerCase() ) ||
		(normalized.toLowerCase().endsWith( ".json" ) && index.assetsByPath.has( `${normalized.toLowerCase()}.gz` ));
}

export function listPublishedAssetPathsSync( prefix, publicRoot = defaultPublicRoot ) {
	const normalizedPrefix = normalizePublicPath( prefix, publicRoot ).replace( /\/+$/, "/" ).toLowerCase();
	const values = new Set();
	for ( const asset of loadPackIndex( publicRoot ).manifest.assets ) {
		if ( asset.path.toLowerCase().startsWith( normalizedPrefix ) ) values.add( asset.path );
	}
	return [ ...values ].sort();
}

function loadPackIndex( publicRoot ) {
	const root = path.resolve( publicRoot );
	const manifestPath = path.join( root, "assets", "packs", "manifest.json" );
	const cached = indexCache.get( manifestPath );
	if ( cached ) return cached;
	const manifest = JSON.parse( readFileSync( manifestPath, "utf8" ) );
	if ( manifest.version !== 1 || !Array.isArray( manifest.groups ) || !Array.isArray( manifest.assets ) ) {
		throw new Error( `Invalid asset-pack manifest: ${manifestPath}.` );
	}
	const packs = manifest.groups.flatMap( ( group ) => group.packs ?? [] );
	const assetsByPackPath = new Map();
	for ( const asset of manifest.assets ) {
		const key = String( asset.packPath ).toLowerCase();
		const entries = assetsByPackPath.get( key ) ?? [];
		entries.push( asset );
		assetsByPackPath.set( key, entries );
	}
	const value = {
		manifest,
		assetsByPath: new Map( manifest.assets.map( ( asset ) => [ asset.path.toLowerCase(), asset ] ) ),
		assetsByPackPath,
		packsByPath: new Map( packs.map( ( pack ) => [ pack.path.toLowerCase(), pack ] ) )
	};
	if ( value.assetsByPath.size !== manifest.assets.length || value.packsByPath.size !== packs.length ) {
		throw new Error( `Asset-pack manifest contains duplicate asset or pack paths: ${manifestPath}.` );
	}
	indexCache.set( manifestPath, value );
	return value;
}

function readPackIdentity( publicRoot, pack, expectedAssets ) {
	const cacheKey = `${path.resolve( publicRoot )}\0${pack.path}`;
	const cached = packCache.get( cacheKey );
	if ( cached ) return cached;
	const identityPath = resolveInsidePublic( publicRoot, pack.path );
	let bytes;
	if ( existsSync( identityPath ) ) {
		bytes = readFileSync( identityPath );
	} else {
		if ( typeof zlib.zstdDecompressSync !== "function" || typeof pack.zstdPath !== "string" ) {
			throw new Error( `Cannot read compact pack ${pack.path}: zstd decompression is unavailable.` );
		}
		const compressedBytes = readFileSync( resolveInsidePublic( publicRoot, pack.zstdPath ) );
		if ( typeof pack.zstdBytes === "number" && compressedBytes.length !== pack.zstdBytes ) {
			throw new Error( `Compact pack byte-length mismatch: ${pack.zstdPath}.` );
		}
		bytes = zlib.zstdDecompressSync( compressedBytes );
	}
	const digest = createHash( "sha256" ).update( bytes ).digest( "hex" );
	if ( bytes.length !== pack.bytes || digest !== pack.sha256.toLowerCase() ) {
		throw new Error( `Pack identity mismatch: ${pack.path}.` );
	}
	const loaded = parsePackHeader( bytes, pack, expectedAssets );
	packCache.set( cacheKey, loaded );
	return loaded;
}

function parsePackHeader( bytes, pack, expectedAssets ) {
	const packPath = pack.path;
	if ( bytes.length < 12 || bytes.subarray( 0, 8 ).toString( "ascii" ) !== "SROPACK1" ) {
		throw new Error( `Invalid asset-pack header: ${packPath}.` );
	}
	const headerLength = bytes.readUInt32LE( 8 );
	const dataStart = 12 + headerLength;
	if ( dataStart > bytes.length ) throw new Error( `Invalid asset-pack table length: ${packPath}.` );
	const header = JSON.parse( bytes.subarray( 12, dataStart ).toString( "utf8" ) );
	if ( header?.format !== "sro-asset-pack" || header.version !== 1 || !Array.isArray( header.files ) ) {
		throw new Error( `Invalid asset-pack table: ${packPath}.` );
	}

	const entriesByPath = new Map();
	let previousEnd = 0;
	for ( const entry of header.files ) {
		const key = typeof entry?.path === "string" ? entry.path.toLowerCase() : "";
		if ( !key.startsWith( "/assets/" ) || entriesByPath.has( key ) ) {
			throw new Error( `Invalid or duplicate asset-pack table path in ${packPath}: ${entry?.path}.` );
		}
		if (
			!Number.isInteger( entry.offset ) || entry.offset < previousEnd || !Number.isInteger( entry.length ) ||
			entry.length < 0
		) {
			throw new Error( `Invalid or overlapping asset-pack range for ${entry.path} in ${packPath}.` );
		}
		if ( dataStart + entry.offset + entry.length > bytes.length || !/^[a-f0-9]{64}$/i.test( entry.sha256 ?? "" ) ) {
			throw new Error( `Asset-pack table range or digest is invalid for ${entry.path} in ${packPath}.` );
		}
		entriesByPath.set( key, entry );
		previousEnd = entry.offset + entry.length;
	}

	if ( header.files.length !== pack.assetCount || header.files.length !== expectedAssets.length ) {
		throw new Error( `Asset-pack table count mismatch: ${packPath}.` );
	}
	for ( const expected of expectedAssets ) {
		const actual = entriesByPath.get( expected.path.toLowerCase() );
		if ( !actual || !sameAssetDescriptor( actual, expected ) ) {
			throw new Error( `Asset-pack table does not match the manifest entry for ${expected.path}.` );
		}
	}
	return { bytes, dataStart, entriesByPath };
}

function sameAssetDescriptor( left, right ) {
	return left.path.toLowerCase() === right.path.toLowerCase() &&
		left.offset === right.offset &&
		left.length === right.length &&
		left.mime === right.mime &&
		left.sha256.toLowerCase() === right.sha256.toLowerCase();
}

function normalizePublicPath( value, publicRoot ) {
	const raw = String( value ).replaceAll( "\\", "/" );
	if ( path.isAbsolute( value ) ) {
		const relative = path.relative( path.resolve( publicRoot ), path.resolve( value ) );
		if ( !relative.startsWith( ".." ) && !path.isAbsolute( relative ) ) {
			return `/${relative.replaceAll( "\\", "/" )}`;
		}
		// Root-relative browser URLs ("/assets/...") are reported as absolute by
		// path.isAbsolute even though they are not host filesystem paths.
		if ( raw.startsWith( "/" ) && !raw.startsWith( "//" ) ) {
			return `/${raw.replace( /^\/+/, "" )}`.replace( /\/{2,}/g, "/" );
		}
		throw new Error( `Published asset path escapes ${publicRoot}: ${value}` );
	}
	return `/${raw.replace( /^\/+/, "" )}`.replace( /\/{2,}/g, "/" );
}

function resolveInsidePublic( publicRoot, publicPath ) {
	const root = path.resolve( publicRoot );
	const resolved = path.resolve( root, publicPath.replace( /^\/+/, "" ) );
	const relative = path.relative( root, resolved );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `Published asset path escapes ${root}: ${publicPath}` );
	}
	return resolved;
}

function missingPublishedAsset( publicPath, loosePath ) {
	return Object.assign(
		new Error( `Published asset is neither loose nor packed: ${publicPath} (${loosePath}).` ),
		{ code: "ENOENT" }
	);
}
