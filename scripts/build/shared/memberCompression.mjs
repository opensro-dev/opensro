/*
===========================================================================

memberCompression.mjs - the stored form of one asset pack member

A pack member travels gzip-compressed inside its pack when that saves at
least a tenth of its bytes; otherwise it stays raw (PNG, MP3 and other
already-compressed media). gzip, not zstd or Brotli: every browser decodes
it natively, and the measured difference (1.677 GiB against 1.598 for zstd
and 1.547 for Brotli on 2026-10-09) did not justify a bundled decoder. The member's length and SHA-256 in the index
stay the decoded identity, so every reader verifies what it decodes.

Compression is the slow step of a pack build, so each verdict is cached
under .state/pack-member-gzip by the member's SHA-256: a hit is
decoded and re-hashed before it is reused, and a "raw" verdict is cached
too, so incompressible media is never tried twice.

===========================================================================
*/

import { createHash } from "node:crypto";
import { mkdir, readdir, readFile, rename, stat, unlink, utimes, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import zlib from "node:zlib";
import { promisify } from "node:util";
import { MEMBER_ENCODING_GZIP } from "./packFormat.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );
const DEFAULT_CACHE_ROOT = path.join( rebuildRoot, ".state", "pack-member-gzip" );

export const MEMBER_GZIP_LEVEL = 9;
// Keep compression only when the stored bytes are at most 90 % of the raw.
export const MEMBER_MAX_RATIO = 0.9;
const RAW_VERDICT = "raw";
// The cache key covers the parameters: a parameter change misses every entry.
const CACHE_VERSION = `gzip-l${MEMBER_GZIP_LEVEL}`;
const GZIP_OPTIONS = { level: MEMBER_GZIP_LEVEL };
// RFC 1952 header byte 9 names the compressing OS: zlib writes 10 on Windows
// and 3 on Linux, so the same member would pack to different bytes (and pack
// URLs) per build machine. 255 ("unknown") is valid and ignored by decoders.
const GZIP_OS_BYTE = 9, GZIP_OS_UNKNOWN = 255;
// Cache entries untouched this long are pruned by a full build.
const CACHE_RETENTION_MS = 30 * 24 * 3600 * 1000;

/*
================
portableGzip

The member's gzip stream with the build machine's OS byte normalised, so a
build is byte-identical on every OS (mtime is already 0 in Node's header).
================
*/
function portableGzip( encoded ) {
	encoded[GZIP_OS_BYTE] = GZIP_OS_UNKNOWN;
	return encoded;
}
const compress = promisify( zlib.gzip ), decompress = promisify( zlib.gunzip );

/*
================
encodeStoredMemberSync

The stored form of one member by the same rule as store(), without the
cache: for the few members a release tool rewrites.
================
*/
export function encodeStoredMemberSync( bytes ) {
	if ( bytes.length === 0 ) return { stored: bytes, encoding: null };
	const encoded = portableGzip( zlib.gzipSync( bytes, GZIP_OPTIONS ) );
	return encoded.length <= bytes.length * MEMBER_MAX_RATIO ?
		{ stored: encoded, encoding: MEMBER_ENCODING_GZIP } :
		{ stored: bytes, encoding: null };
}

/*
================
sha256

Hex digest of bytes.
================
*/
function sha256( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
cachePaths
================
*/
function cachePaths( root, digest ) {
	const folder = path.join( root, CACHE_VERSION, digest.slice( 0, 2 ) );
	return {
		folder,
		encoded: path.join( folder, digest + ".gz" ),
		raw: path.join( folder, digest + "." + RAW_VERDICT )
	};
}

/*
================
writeOnce

Write-then-rename, so a reader never sees a partial cache entry.
================
*/
async function writeOnce( filename, bytes ) {
	await mkdir( path.dirname( filename ), { recursive: true } );
	const temporary = `${filename}.${process.pid}.${Math.random().toString( 16 ).slice( 2 )}.tmp`;
	await writeFile( temporary, bytes );
	await rename( temporary, filename );
}

/*
================
touch

Marks a cache entry as used by this build.
================
*/
async function touch( filename ) {
	const now = new Date();
	await utimes( filename, now, now ).catch( () => {} );
}

/*
================
openMemberCompression

Returns store(bytes, digest), which resolves to { stored, encoding }:
encoding is "gzip" with the compressed bytes, or null with the raw bytes.
digest must be the SHA-256 of bytes; the caller already holds it.
================
*/
export function openMemberCompression( { cacheRoot = DEFAULT_CACHE_ROOT } = {} ) {
	let hits = 0, misses = 0;

	/*
	================
	cached
	================
	*/
	async function cached( bytes, digest ) {
		const paths = cachePaths( cacheRoot, digest );
		const encoded = await readFile( paths.encoded ).catch( () => null );
		if ( encoded ) {
			// A damaged or foreign entry is ignored and rebuilt, never served.
			const decoded = await decompress( encoded, { maxOutputLength: bytes.length } ).catch( () => null );
			if (
				decoded && decoded.length === bytes.length && sha256( decoded ) === digest &&
				encoded[GZIP_OS_BYTE] === GZIP_OS_UNKNOWN
			) {
				await touch( paths.encoded );
				return { stored: encoded, encoding: MEMBER_ENCODING_GZIP };
			}
		}
		const raw = await readFile( paths.raw ).catch( () => null );
		if ( raw && raw.toString( "utf8" ) === digest ) {
			await touch( paths.raw );
			return { stored: bytes, encoding: null };
		}
		return null;
	}

	/*
	================
	store
	================
	*/
	async function store( bytes, digest ) {
		if ( bytes.length === 0 ) return { stored: bytes, encoding: null };
		const hit = await cached( bytes, digest );
		if ( hit ) {
			hits++;
			return hit;
		}
		misses++;
		const encoded = portableGzip( await compress( bytes, GZIP_OPTIONS ) );
		const paths = cachePaths( cacheRoot, digest );
		if ( encoded.length <= bytes.length * MEMBER_MAX_RATIO ) {
			await writeOnce( paths.encoded, encoded );
			return { stored: encoded, encoding: MEMBER_ENCODING_GZIP };
		}
		await writeOnce( paths.raw, Buffer.from( digest, "utf8" ) );
		return { stored: bytes, encoding: null };
	}

	/*
	================
	prune

	Removes cache entries no build has used for CACHE_RETENTION_MS. A full
	build calls it; partial refreshes do not, since they touch only their own
	members.
	================
	*/
	async function prune( now = Date.now() ) {
		let removed = 0;
		const root = path.join( cacheRoot, CACHE_VERSION );
		for ( const shard of await readdir( root ).catch( () => [] ) ) {
			for ( const name of await readdir( path.join( root, shard ) ).catch( () => [] ) ) {
				const file = path.join( root, shard, name );
				const info = await stat( file ).catch( () => null );
				if ( info && now - info.mtimeMs > CACHE_RETENTION_MS ) {
					await unlink( file ).catch( () => {} );
					removed++;
				}
			}
		}
		return removed;
	}

	return { store, prune, stats: () => ({ hits, misses }) };
}
