import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { refreshAssetDelivery } from "./assetDelivery.mjs";
import { existsSync } from "node:fs";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isPrecompressedAssetPath } from "./jsonAssetCompression.mjs";
import { toPublicPath } from "./shared/assetPaths.mjs";
import { mapWithConcurrency } from "./shared/asyncUtils.mjs";
import { fileHashCacheDisabled, openFileHashCache } from "./shared/fileHashCache.mjs";
import { listFiles } from "./shared/fsUtils.mjs";
import { sha256Hex } from "./shared/hash.mjs";
import { readJsonOrUndefined } from "./shared/jsonOut.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const assetsRoot = path.join( publicRoot, "assets" );
const launcherManifestPath = path.join( assetsRoot, "launcher", "manifest.json" );
const webManifestPath = path.join( assetsRoot, "manifest.json" );

/** Read-only verification sweeps stat wide: cheap syscalls, but slow one-at-a-time on Windows. */
const STAT_CONCURRENCY = 128;
/** Reads stay narrow: a hash-cache miss reads the whole file, and packs run to tens of MiB. */
const HASH_CONCURRENCY = 8;

/** Every file under .generated/client-public/assets, registerable or not. */
export async function listAssetTreeFiles() {
	return listFiles( assetsRoot );
}

/** Every file under `root`, recursively. Shared with the sidecar freshness gate. */
export async function listFilesUnder( root ) {
	return listFiles( root );
}

export const webAssetManifestPath = webManifestPath;
export const publicAssetRoot = publicRoot;

// The one definition of "the web asset manifest registers this file". Two files in the
// tree are deliberately not entries: the manifest itself (it cannot carry its own hash)
// and precompressed sidecars (derived bytes: the .json.gz the packs hold, and older
// .br/.zst copies the full build retires). Exported so the membership
// guard in scripts/test/assets/generatedAssetMembership.test.mjs tests the tree against this
// rule rather than a second copy of it that can drift.
export function isRegisterableAssetFile( filePath ) {
	if ( /[\\/]assets[\\/]packs[\\/]transport[\\/][a-f0-9]{64}\.gz$/.test( filePath ) ) return true;
	if ( path.resolve( filePath ) === path.resolve( webManifestPath ) ) {
		return false;
	}
	if ( !isPrecompressedAssetPath( filePath ) ) {
		return true;
	}

	// In the compact profile a pack's .bin.zst is the installed artifact, not a
	// derived duplicate beside an identity .bin. Register that physical file so
	// launcher/update manifests describe the bytes that actually ship.
	return /[.]bin[.]zst$/i.test( filePath ) && !existsSync( filePath.slice( 0, -".zst".length ) );
}

export async function listRegisterableAssetFiles() {
	return (await listAssetTreeFiles()).filter( isRegisterableAssetFile );
}

export async function buildWebAssetManifest() {
	await refreshAssetDelivery( publicRoot );
	const [candidates, launcherManifest] = await Promise.all( [
		listRegisterableAssetFiles(),
		readLauncherManifest()
	] );
	// Hashing every public asset (multiple GB including the packs) dominated this step;
	// the shared size+mtime hash cache reduces a no-change rerun to a stat() sweep.
	const hashCache = await openFileHashCache();

	// Stat wide (cheap syscalls, slow on Windows), then hash narrow: on a warm cache the
	// hash phase does no I/O at all; on a cold cache it reads at most 8 files at a time.
	const statted = await mapWithConcurrency( candidates, 64, async ( filePath ) => ({
		filePath,
		fileStat: await stat( filePath )
	}) );
	const manifestFiles = await mapWithConcurrency( statted, HASH_CONCURRENCY, async ( { filePath, fileStat } ) => ({
		path: toPublicAssetPath( filePath ),
		size: fileStat.size,
		sha256: await hashCache.hashFile( filePath, fileStat )
	}) );
	await hashCache.save();

	manifestFiles.sort( ( left, right ) => left.path.localeCompare( right.path ) );

	const version = launcherManifest.version?.value ?? launcherManifest.version?.displayText ?? "unknown";
	const manifestHash = computeWebManifestHash( version, manifestFiles );

	// manifestHash covers version + files, so an unchanged hash means the manifest content is
	// identical; keeping the existing file (and its generatedAt/mtime) lets the JSON sidecar
	// cache skip recompressing this multi-MB file on every no-op rebuild.
	const existing = await readExistingWebManifest();
	if ( existing?.manifestHash === manifestHash ) {
		return existing;
	}

	const manifest = {
		version,
		generatedAt: new Date().toISOString(),
		manifestHash,
		files: manifestFiles
	};

	await mkdir( path.dirname( webManifestPath ), { recursive: true } );
	await writeFile( webManifestPath, JSON.stringify( manifest ), "utf8" );
	return manifest;
}

// The one definition of the manifest's self-hash. buildWebAssetManifest() writes it and the
// content guard in scripts/test/assets/generatedAssetMembership.test.mjs recomputes it, so the
// manifest cannot be hand-edited to agree with bytes it never described.
export function computeWebManifestHash( version, files ) {
	return sha256Hex( JSON.stringify( { version, files } ) );
}

export async function readWebAssetManifest() {
	return JSON.parse( await readFile( webManifestPath, "utf8" ) );
}

/** (resolved path -> {size, mtimeMs}) for `filePaths`; absent when the file could not be statted. */
export async function statFilesByPath( filePaths, concurrency = STAT_CONCURRENCY ) {
	const stats = await mapWithConcurrency( filePaths, concurrency, async ( filePath ) => {
		try {
			const { size, mtimeMs } = await stat( filePath );
			return { size, mtimeMs };
		} catch {
			return undefined;
		}
	} );

	const byPath = new Map();
	for ( const [index, filePath] of filePaths.entries() ) {
		if ( stats[index] ) byPath.set( path.resolve( filePath ), stats[index] );
	}
	return byPath;
}

// Content drift: the manifest records size+sha256 for every entry, and until this existed
// nothing compared them to the bytes on disk. Membership guards cannot see it -- a registered
// file whose bytes change keeps its path and stays green -- and we shipped exactly that defect
// (stale precompressed sidecars serving outdated bytes) on 2026-07-25.
//
// COST AND WHAT IT COSTS YOU IN COVERAGE. `size` is compared against a fresh stat and is
// therefore unconditional: no cache can hide a length-changing edit. `sha256` goes through the
// shared size+mtime hash cache, so an unchanged tree is a stat sweep (~1.1s for 20,651 files)
// rather than a multi-GB read (~23s). The cache re-reads any file whose size OR mtime moved, so
// every ordinary modification is caught; a content change that preserves BOTH is not. Set
// SRO_BUILD_HASH_CACHE=0 to bypass the cache and re-hash every byte.
//
// Deliberately does not call hashCache.save(): a gate must not mutate the shared build cache
// that other lanes' builds are concurrently reading.
/**
 * @param {{
 *   manifest?: { files: { path: string, size: number, sha256?: string }[] },
 *   stats?: Map<string, { size: number, mtimeMs: number }>
 * }} [options]
 */
export async function verifyWebAssetManifestContent( { manifest, stats } = {} ) {
	const webManifest = manifest ?? (await readWebAssetManifest());
	const entries = webManifest.files.map( ( entry ) => ({ entry, filePath: path.join( publicRoot, entry.path ) }) );

	const statsByPath = stats ?? (await statFilesByPath( entries.map( ( { filePath } ) => filePath ) ));
	const hashCache = await openFileHashCache();

	const missing = [];
	const sizeMismatches = [];
	const resolved = [];

	for ( const { entry, filePath } of entries ) {
		const fileStat = statsByPath.get( path.resolve( filePath ) );
		if ( !fileStat ) {
			missing.push( entry.path );
			continue;
		}
		if ( fileStat.size !== entry.size ) {
			sizeMismatches.push( `${entry.path}: manifest ${entry.size} bytes, on disk ${fileStat.size}` );
			continue;
		}
		resolved.push( { entry, filePath, fileStat } );
	}

	// Narrow concurrency: a cache miss reads the whole file, and the packs run to tens of MiB.
	const hashMismatches = [];
	await mapWithConcurrency( resolved, HASH_CONCURRENCY, async ( { entry, filePath, fileStat } ) => {
		const sha256 = await hashCache.hashFile( filePath, fileStat );
		if ( sha256 !== entry.sha256 ) {
			hashMismatches.push( `${entry.path}: manifest ${entry.sha256}, on disk ${sha256}` );
		}
	} );

	return {
		verified: resolved.length,
		missing: missing.sort(),
		sizeMismatches: sizeMismatches.sort(),
		hashMismatches: hashMismatches.sort(),
		fullyRehashed: fileHashCacheDisabled()
	};
}

async function readExistingWebManifest() {
	const parsed = await readJsonOrUndefined( webManifestPath );
	if ( parsed && typeof parsed.manifestHash === "string" && Array.isArray( parsed.files ) ) {
		return parsed;
	}
	return undefined;
}

async function readLauncherManifest() {
	return (
		(await readJsonOrUndefined( launcherManifestPath )) ?? {
			version: {
				value: "unknown",
				displayText: "ver ?"
			}
		}
	);
}

export function toPublicAssetPath( filePath ) {
	return toPublicPath( filePath, publicRoot, { leadingSlash: false } );
}
