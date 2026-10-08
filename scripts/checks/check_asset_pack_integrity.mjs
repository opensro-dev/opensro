// Asset pack integrity gate: validates the generated manifest.json against the pack
// files on disk (schema, sizes, headers, per-asset offsets/lengths/mimes, SHA-256
// values, zstd sidecars).
//
// Byte-hashing is the expensive part (~GBs of packs), so it can be short-circuited
// through a persistent stat-keyed hash cache (scripts/build/shared/fileHashCache.mjs),
// kept in a check-owned file (.state/asset-integrity-hash-cache.json) so this gate
// never mutates the shared build cache that concurrent build lanes read.
//
// Safety model - the cache can only CONFIRM, never overrule:
//   - A cached digest is consulted only when the file's (size, mtimeMs) match the
//     entry exactly, and it is trusted only when it EQUALS the manifest's expected
//     digest. Any disagreement (or cache miss) falls through to a full re-read and
//     re-hash of the real bytes, so a corrupted file can never be masked by a stale
//     or wrong cache entry.
//   - Entries are written from bytes this check actually read and hashed, and the
//     cache file is saved only after the whole check passed.
//   - Structural checks (manifest schema, file sizes, pack headers, per-asset
//     offsets/lengths/mimes/digest agreement, zstd sidecar presence and size) run
//     unconditionally on every run; only re-hashing of unchanged bytes is skipped.
//   - Known caveat (same as the build cache): a tamper that preserves BOTH size and
//     mtimeMs is invisible to the stat check. SRO_ASSET_INTEGRITY_NO_CACHE=1 forces
//     a full re-hash of every byte (SRO_BUILD_HASH_CACHE=0 has the same effect).
//
// Env knobs:
//   SRO_ASSET_INTEGRITY_NO_CACHE=1   ignore the cache entirely (no reads, no writes).
//   SRO_ASSET_INTEGRITY_CACHE_PATH   override the cache file location (tests use this
//                                    so temp trees never touch the production cache).

import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import {
	ASSET_PACK_HEADER_FORMAT,
	ASSET_PACK_MAGIC,
	ASSET_PACK_VERSION,
	decodeStoredMember,
	sameStoredForm,
	storedLength,
	validStoredForm
} from "../build/shared/packFormat.mjs";
import { createHash } from "node:crypto";
import { open, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import * as zlib from "node:zlib";
import { fileHashCacheDisabled, openFileHashCache } from "../build/shared/fileHashCache.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const defaultPublicRoot = CLIENT_PUBLIC_ROOT;
const manifestPath = path.resolve(
	process.argv[2] ?? path.join( defaultPublicRoot, "assets", "packs", "manifest.json" )
);
// The manifest always lives at <publicRoot>/assets/packs/manifest.json, so the public
// root is derived from it; an explicitly passed manifest (tests, temp trees) then
// resolves its /assets/... paths inside its own tree instead of the production one.
const publicRoot = path.resolve( path.dirname( manifestPath ), "..", ".." );
const skipZstd = process.argv.includes( "--skip-zstd" );

const cacheDisabledBy = process.env.SRO_ASSET_INTEGRITY_NO_CACHE === "1" ?
	"SRO_ASSET_INTEGRITY_NO_CACHE=1" :
	fileHashCacheDisabled() ?
	"SRO_BUILD_HASH_CACHE=0" :
	null;
const cachePath = process.env.SRO_ASSET_INTEGRITY_CACHE_PATH ?
	path.resolve( process.env.SRO_ASSET_INTEGRITY_CACHE_PATH ) :
	path.join( rebuildRoot, ".state", "asset-integrity-hash-cache.json" );
const hashCache = cacheDisabledBy ? null : await openFileHashCache( cachePath );

const manifest = JSON.parse( await readFile( manifestPath, "utf8" ) );
const packByPath = new Map();
const packGroupByPath = new Map();
const assetsByPackPath = new Map();
const groupStats = new Map();

assertEqual( manifest.format, "sro-asset-pack-index", "manifest format" );
assertEqual( manifest.version, ASSET_PACK_VERSION, "manifest version" );
assertArray( manifest.groups, "manifest groups" );
assertArray( manifest.assets, "manifest assets" );

for ( const group of manifest.groups ) {
	assertString( group.name, "group name" );
	assertArray( group.packs, `group ${group.name} packs` );
	if ( groupStats.has( group.name ) ) {
		fail( `duplicate group ${group.name}` );
	}
	groupStats.set( group.name, {
		expectedAssets: integer( group.assetCount, `group ${group.name} assetCount` ),
		expectedBytes: integer( group.totalBytes, `group ${group.name} totalBytes` ),
		packAssets: 0,
		actualAssets: 0,
		actualBytes: 0
	} );

	for ( const pack of group.packs ) {
		const packPath = normalizePublicPath( pack.path, "pack path" );
		if ( packByPath.has( packPath ) ) {
			fail( `duplicate pack ${packPath}` );
		}
		const normalizedPack = {
			...pack,
			path: packPath,
			bytes: integer( pack.bytes, `pack ${packPath} bytes` ),
			assetCount: integer( pack.assetCount, `pack ${packPath} assetCount` ),
			sha256: sha256Digest( pack.sha256, `pack ${packPath} sha256` )
		};
		if ( pack.zstdPath !== undefined ) {
			normalizedPack.zstdPath = normalizePublicPath( pack.zstdPath, `pack ${packPath} zstdPath` );
			normalizedPack.zstdBytes = integer( pack.zstdBytes, `pack ${packPath} zstdBytes` );
			assertEqual( normalizedPack.zstdPath, `${packPath}.zst`, `pack ${packPath} zstdPath` );
		}
		packByPath.set( packPath, normalizedPack );
		packGroupByPath.set( packPath, group.name );
		assetsByPackPath.set( packPath, [] );
		groupStats.get( group.name ).packAssets += normalizedPack.assetCount;
	}
}

const seenAssets = new Set();
for ( const asset of manifest.assets ) {
	const assetPath = normalizePublicPath( asset.path, "asset path" );
	const key = assetPath.toLowerCase();
	if ( seenAssets.has( key ) ) {
		fail( `duplicate asset ${assetPath}` );
	}
	seenAssets.add( key );

	const packPath = normalizePublicPath( asset.packPath, `asset ${assetPath} packPath` );
	const pack = packByPath.get( packPath );
	if ( !pack ) {
		fail( `asset ${assetPath} references missing pack ${packPath}` );
	}
	assertEqual( asset.group, packGroupByPath.get( packPath ), `asset ${assetPath} group owner` );

	const normalizedAsset = {
		...asset,
		path: assetPath,
		packPath,
		offset: integer( asset.offset, `asset ${assetPath} offset` ),
		length: integer( asset.length, `asset ${assetPath} length` ),
		mime: assertString( asset.mime, `asset ${assetPath} mime` ),
		sha256: sha256Digest( asset.sha256, `asset ${assetPath} sha256` )
	};
	if ( !validStoredForm( normalizedAsset ) ) fail( `asset ${assetPath} has an invalid stored form` );
	assetsByPackPath.get( packPath ).push( normalizedAsset );

	const stats = groupStats.get( asset.group );
	if ( !stats ) {
		fail( `asset ${assetPath} references missing group ${asset.group}` );
	}
	stats.actualAssets += 1;
	stats.actualBytes += normalizedAsset.length;
}

for ( const [groupName, stats] of groupStats ) {
	assertEqual( stats.packAssets, stats.expectedAssets, `group ${groupName} pack asset total` );
	assertEqual( stats.actualAssets, stats.expectedAssets, `group ${groupName} manifest asset total` );
	assertEqual( stats.actualBytes, stats.expectedBytes, `group ${groupName} byte total` );
}

let checkedPacks = 0;
let checkedAssets = 0;
let checkedBytes = 0;
let checkedZstd = 0;
let hashedPacks = 0;
let statMatchedPacks = 0;
let hashedZstd = 0;
let statMatchedZstd = 0;
let sidecarOnlyPacks = 0;

for ( const [packPath, pack] of packByPath ) {
	const absolutePackPath = resolvePublicPath( packPath );
	const packStats = await stat( absolutePackPath ).catch( ( error ) => {
		if ( error?.code === "ENOENT" ) return undefined;
		throw error;
	} );
	if ( packStats ) {
		assertEqual( packStats.size, pack.bytes, `pack ${packPath} file size` );
	}

	// The cached digest counts only if it matches the manifest exactly; anything else
	// (miss, stale entry, disagreement) re-reads and re-hashes the real bytes.
	const cachedPackSha = packStats ? hashCache?.peekFileHash( absolutePackPath, packStats ) : undefined;
	let packBuffer = null;
	let headerBytes;
	let sidecarOnlyZstdStats;
	let sidecarOnlyZstdValidated = false;
	if ( !packStats ) {
		if ( skipZstd || !pack.zstdPath ) {
			fail( `pack ${packPath} has neither an identity file nor an enabled zstd representation` );
		}
		if ( typeof zlib.zstdDecompressSync !== "function" ) {
			fail( `pack ${packPath} is zstd-only but this Node runtime cannot decompress zstd` );
		}

		const zstdAbsolutePath = resolvePublicPath( pack.zstdPath );
		sidecarOnlyZstdStats = await stat( zstdAbsolutePath );
		assertEqual( sidecarOnlyZstdStats.size, pack.zstdBytes, `pack ${pack.zstdPath} file size` );
		const zstdBuffer = await readFile( zstdAbsolutePath );
		packBuffer = zlib.zstdDecompressSync( zstdBuffer );
		assertEqual( packBuffer.byteLength, pack.bytes, `pack ${packPath} decompressed byte length` );
		const actualSha = sha256Hex( packBuffer );
		assertEqual( actualSha, pack.sha256, `pack ${packPath} decompressed SHA-256` );
		hashCache?.noteFileBytes( `${zstdAbsolutePath}#decompressed`, sidecarOnlyZstdStats, packBuffer );
		headerBytes = packBuffer;
		sidecarOnlyZstdValidated = true;
		sidecarOnlyPacks += 1;
		hashedPacks += 1;
	} else if ( cachedPackSha === pack.sha256 ) {
		headerBytes = await readPackHeaderPrefix( absolutePackPath, packStats.size, packPath );
		statMatchedPacks += 1;
	} else {
		packBuffer = await readFile( absolutePackPath );
		const actualSha = hashCache ?
			hashCache.noteFileBytes( absolutePackPath, packStats, packBuffer ) :
			sha256Hex( packBuffer );
		assertEqual( actualSha, pack.sha256, `pack ${packPath} SHA-256` );
		headerBytes = packBuffer;
		hashedPacks += 1;
	}

	const header = parsePackHeader( headerBytes, pack.bytes, packPath );
	const expectedAssets = assetsByPackPath.get( packPath ) ?? [];
	assertEqual( header.files.length, pack.assetCount, `pack ${packPath} header asset count` );
	assertEqual( expectedAssets.length, pack.assetCount, `pack ${packPath} manifest asset count` );

	const headerByPath = new Map( header.files.map( ( entry ) => [ entry.path.toLowerCase(), entry ] ) );
	for ( const asset of expectedAssets ) {
		const headerEntry = headerByPath.get( asset.path.toLowerCase() );
		if ( !headerEntry ) {
			fail( `pack ${packPath} header is missing ${asset.path}` );
		}
		assertEqual( headerEntry.offset, asset.offset, `pack ${packPath} ${asset.path} offset` );
		assertEqual( headerEntry.length, asset.length, `pack ${packPath} ${asset.path} length` );
		assertEqual( headerEntry.mime, asset.mime, `pack ${packPath} ${asset.path} MIME type` );
		assertEqual( headerEntry.sha256, asset.sha256, `pack ${packPath} ${asset.path} header SHA-256` );
		if ( !sameStoredForm( headerEntry, asset ) ) {
			fail( `pack ${packPath} ${asset.path} stored form differs from the manifest` );
		}

		const start = header.dataStart + asset.offset;
		const end = start + storedLength( asset );
		if ( end > pack.bytes ) {
			fail( `pack ${packPath} ${asset.path} extends past pack bytes` );
		}
		// Slice digests are recomputed only when the whole pack was re-hashed. On the
		// cache-confirmed path the pack bytes are proven identical to the manifest's
		// whole-pack digest, and the header/manifest digest agreement above still runs.
		if ( packBuffer ) {
			try {
				decodeStoredMember( packBuffer.subarray( start, end ), asset );
			} catch ( error ) {
				fail( `pack ${packPath} ${asset.path} member: ${error.message}` );
			}
		}
		checkedAssets += 1;
	}

	if ( !skipZstd && pack.zstdPath ) {
		const zstdAbsolutePath = resolvePublicPath( pack.zstdPath );
		const zstdStats = sidecarOnlyZstdStats ?? (await stat( zstdAbsolutePath ));
		assertEqual( zstdStats.size, pack.zstdBytes, `pack ${pack.zstdPath} file size` );
		if ( sidecarOnlyZstdValidated ) {
			hashedZstd += 1;
		} else if ( typeof zlib.zstdDecompressSync === "function" ) {
			// The manifest pins no digest for the raw sidecar bytes; what matters is that
			// they decompress to the pack. Cache that DECOMPRESSED digest under a synthetic
			// key (stat-keyed to the sidecar file) in the check-owned cache file.
			const decompressedCacheKey = `${zstdAbsolutePath}#decompressed`;
			const cachedDecompressedSha = hashCache?.peekFileHash( decompressedCacheKey, zstdStats );
			if ( cachedDecompressedSha === pack.sha256 ) {
				statMatchedZstd += 1;
			} else {
				const zstdBuffer = await readFile( zstdAbsolutePath );
				const decompressed = zlib.zstdDecompressSync( zstdBuffer );
				const decompressedSha = hashCache ?
					hashCache.noteFileBytes( decompressedCacheKey, zstdStats, decompressed ) :
					sha256Hex( decompressed );
				assertEqual( decompressedSha, pack.sha256, `pack ${pack.zstdPath} decompressed SHA-256` );
				hashedZstd += 1;
			}
		}
		checkedZstd += 1;
	}

	checkedPacks += 1;
	checkedBytes += pack.bytes;
}

// Persist only after every check passed; a failing run must stay conservative.
await hashCache?.save();

console.log(
	`Asset pack integrity OK: ${checkedPacks} packs, ${checkedAssets} assets, ${formatBytes( checkedBytes )}, ` +
		`${checkedZstd} zstd sidecars, ${sidecarOnlyPacks} sidecar-only packs.`
);
console.log(
	hashCache ?
		`Pack SHA-256: ${hashedPacks} hashed, ${statMatchedPacks} stat-matched from cache. ` +
		`Zstd sidecars: ${hashedZstd} hashed, ${statMatchedZstd} stat-matched from cache.` :
		`Hash cache disabled (${cacheDisabledBy}): ${hashedPacks} packs and ${hashedZstd} zstd sidecars fully re-hashed.`
);

/*
Read just the 12-byte magic/length prefix plus the JSON header, so the cache-confirmed
path still parses and cross-checks the header without reading GBs of asset data.
*/
async function readPackHeaderPrefix( absolutePackPath, packBytes, packPath ) {
	const handle = await open( absolutePackPath, "r" );
	try {
		const fixed = Buffer.alloc( 12 );
		const fixedRead = await handle.read( fixed, 0, 12, 0 );
		if ( fixedRead.bytesRead < 12 ) {
			fail( `pack ${packPath} is too small` );
		}
		const headerLength = fixed.readUInt32LE( 8 );
		if ( headerLength <= 0 || 12 + headerLength > packBytes ) {
			fail( `pack ${packPath} has invalid header length` );
		}
		const prefix = Buffer.alloc( 12 + headerLength );
		fixed.copy( prefix, 0 );
		const headerRead = await handle.read( prefix, 12, headerLength, 12 );
		if ( headerRead.bytesRead < headerLength ) {
			fail( `pack ${packPath} has invalid header length` );
		}
		return prefix;
	} finally {
		await handle.close();
	}
}

function parsePackHeader( buffer, packBytes, packPath ) {
	if ( buffer.byteLength < 12 ) {
		fail( `pack ${packPath} is too small` );
	}
	assertEqual( buffer.subarray( 0, 8 ).toString( "ascii" ), ASSET_PACK_MAGIC, `pack ${packPath} magic` );
	const headerLength = buffer.readUInt32LE( 8 );
	const dataStart = 12 + headerLength;
	if ( headerLength <= 0 || dataStart > packBytes || dataStart > buffer.byteLength ) {
		fail( `pack ${packPath} has invalid header length` );
	}
	const header = JSON.parse( buffer.subarray( 12, dataStart ).toString( "utf8" ) );
	assertEqual( header.format, ASSET_PACK_HEADER_FORMAT, `pack ${packPath} header format` );
	assertEqual( header.version, ASSET_PACK_VERSION, `pack ${packPath} header version` );
	assertArray( header.files, `pack ${packPath} header files` );
	const seen = new Set();
	const files = header.files.map( ( entry, index ) => {
		const assetPath = normalizePublicPath( entry.path, `pack ${packPath} header file ${index}` );
		const key = assetPath.toLowerCase();
		if ( seen.has( key ) ) {
			fail( `pack ${packPath} header repeats ${assetPath}` );
		}
		seen.add( key );
		return {
			path: assetPath,
			offset: integer( entry.offset, `pack ${packPath} ${assetPath} offset` ),
			length: integer( entry.length, `pack ${packPath} ${assetPath} length` ),
			mime: assertString( entry.mime, `pack ${packPath} ${assetPath} mime` ),
			sha256: sha256Digest( entry.sha256, `pack ${packPath} ${assetPath} sha256` ),
			...(entry.stored === undefined ? {} : { stored: entry.stored })
		};
	} );
	for ( const file of files ) {
		if ( !validStoredForm( file ) ) fail( `pack ${packPath} ${file.path} has an invalid stored form` );
	}
	files.sort( ( left, right ) => left.offset - right.offset );
	let previousEnd = 0;
	for ( const file of files ) {
		if ( file.offset < previousEnd ) {
			fail( `pack ${packPath} header has overlapping range for ${file.path}` );
		}
		previousEnd = file.offset + storedLength( file );
	}
	return { dataStart, files };
}

function resolvePublicPath( publicPath ) {
	const absolutePath = path.resolve( publicRoot, publicPath.replace( /^\/+/, "" ) );
	const relative = path.relative( publicRoot, absolutePath );
	if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		fail( `public path escapes public root: ${publicPath}` );
	}
	return absolutePath;
}

function normalizePublicPath( value, label ) {
	const normalized = `/${assertString( value, label ).replaceAll( "\\", "/" ).replace( /^\/+/, "" )}`.replace(
		/\/{2,}/g,
		"/"
	);
	if ( !normalized.startsWith( "/assets/" ) ) {
		fail( `${label} must be a public /assets path: ${value}` );
	}
	return normalized;
}

function assertArray( value, label ) {
	if ( !Array.isArray( value ) ) {
		fail( `${label} must be an array` );
	}
}

function assertString( value, label ) {
	if ( typeof value !== "string" || value.trim().length === 0 ) {
		fail( `${label} must be a non-empty string` );
	}
	return value.trim();
}

function integer( value, label ) {
	if ( !Number.isInteger( value ) || value < 0 ) {
		fail( `${label} must be a non-negative integer` );
	}
	return value;
}

function sha256Digest( value, label ) {
	const digest = assertString( value, label ).toLowerCase();
	if ( !/^[a-f0-9]{64}$/.test( digest ) ) {
		fail( `${label} must be a SHA-256 hex digest` );
	}
	return digest;
}

function sha256Hex( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

function assertEqual( actual, expected, label ) {
	if ( actual !== expected ) {
		fail( `${label}: expected ${expected}, got ${actual}` );
	}
}

function formatBytes( bytes ) {
	return `${(bytes / (1024 * 1024)).toFixed( 1 )} MiB`;
}

function fail( message ) {
	throw new Error( `Asset pack integrity failed: ${message}` );
}
