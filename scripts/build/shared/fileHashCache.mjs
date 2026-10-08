// Persistent (path -> sha256) cache keyed by file size + mtime, shared by the asset-pack
// and web-manifest builders so unchanged files are never re-read between runs. This is what
// makes pack/manifest rebuilds incremental: a no-change rerun goes from re-reading and
// re-hashing multiple GB to a stat() sweep.
//
// Caveat: a content change that preserves BOTH size and mtimeMs is invisible to the cache.
// None of this pipeline's writers do that (converters/builders always rewrite outputs with
// fresh mtimes). Set SRO_BUILD_HASH_CACHE=0 to bypass cache reads and force full re-hashing.

import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { sha256Hex } from "./hash.mjs";
import { readJsonOrUndefined } from "./jsonOut.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", "..", ".." );
const defaultCachePath = path.join( rebuildRoot, ".state", "file-hash-cache.json" );

const CACHE_FORMAT = "sro-file-hash-cache";
const CACHE_VERSION = 1;

export function fileHashCacheDisabled() {
	return process.env.SRO_BUILD_HASH_CACHE === "0";
}

export async function openFileHashCache( cachePath = defaultCachePath ) {
	let entries = Object.create( null );
	if ( !fileHashCacheDisabled() ) {
		const parsed = await readJsonOrUndefined( cachePath );
		if ( parsed?.format === CACHE_FORMAT && parsed?.version === CACHE_VERSION && parsed.entries ) {
			entries = parsed.entries;
		}
	}

	let dirty = false;

	function remember( absolutePath, fileStat, sha256 ) {
		entries[cacheKey( absolutePath )] = { size: fileStat.size, mtimeMs: fileStat.mtimeMs, sha256 };
		dirty = true;
	}

	return {
		/** sha256 of the file, re-reading it only when (size, mtimeMs) changed since last run. */
		async hashFile( absolutePath, knownStat ) {
			const fileStat = knownStat ?? (await stat( absolutePath ));
			const cached = entries[cacheKey( absolutePath )];
			if ( cached && cached.size === fileStat.size && cached.mtimeMs === fileStat.mtimeMs ) {
				return cached.sha256;
			}
			const bytes = await readFile( absolutePath );
			const sha256 = sha256Hex( bytes );
			remember( absolutePath, fileStat, sha256 );
			return sha256;
		},
		/**
		 * Cached sha256 iff (size, mtimeMs) match the entry exactly; undefined otherwise.
		 * Never reads file bytes, so callers can treat "undefined" as "re-hash yourself".
		 * Verification gates use this to let the cache CONFIRM an expected hash without
		 * ever letting a cache entry override what is actually on disk.
		 */
		peekFileHash( absolutePath, fileStat ) {
			const cached = entries[cacheKey( absolutePath )];
			if ( cached && cached.size === fileStat.size && cached.mtimeMs === fileStat.mtimeMs ) {
				return cached.sha256;
			}
			return undefined;
		},
		/** Record the hash of bytes the caller already read (avoids a second read). */
		noteFileBytes( absolutePath, fileStat, bytes ) {
			const sha256 = sha256Hex( bytes );
			remember( absolutePath, fileStat, sha256 );
			return sha256;
		},
		async save() {
			// SRO_BUILD_HASH_CACHE=0 means "do not TRUST the cache", never "erase it". Reads are
			// skipped above, so `entries` starts empty and holds only what this run happened to
			// touch; persisting that would replace a complete cache with a fragment. Observed on
			// 2026-07-25: one `SRO_BUILD_HASH_CACHE=0 pnpm test assets` truncated an 8.4 MB cache to
			// 3 entries, because a pack test builds a temp tree through the DEFAULT cache path.
			if ( fileHashCacheDisabled() ) return;
			if ( !dirty ) return;
			await mkdir( path.dirname( cachePath ), { recursive: true } );
			await writeFile(
				cachePath,
				JSON.stringify( { format: CACHE_FORMAT, version: CACHE_VERSION, entries } ),
				"utf8"
			);
			dirty = false;
		}
	};
}

function cacheKey( absolutePath ) {
	return path.resolve( absolutePath ).toLowerCase();
}
