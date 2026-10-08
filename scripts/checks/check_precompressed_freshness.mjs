// Fail when a precompressed sidecar is OLDER than the asset it shadows.
//
// WHY THIS IS A GATE AND NOT A LINT. The packs hold the .json.gz sidecar of a
// JSON asset, not the JSON itself, and nothing compares their mtimes on the
// way in. So a stale sidecar ships OLD CONTENT to real users while every tool
// we would reach for to check the loose file reports the new content. The
// failure is invisible from the inside.
//
// It has already cost a wave: assets/anim/manifest.json gained its motion-0x26
// pickup clip entries on 2026-07-24 and its sidecars were left at 2026-07-08,
// so the browser fetched a manifest with no pick clip at all. The pickup
// animation fell back to a placeholder, and because the placeholder's length
// also drives the 0x2476 busy-motion gate, the player was locked out of input
// for the placeholder's duration after every pickup.
//
// Run:      node scripts/checks/check_precompressed_freshness.mjs
// Repair:   node scripts/checks/check_precompressed_freshness.mjs --fix

import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { refreshPrecompressedSidecars } from "../build/generatedManifestSidecars.mjs";
// Only the published sidecars ship; older .br/.zst copies are retired by the build.
import { PUBLISHED_SIDECAR_SUFFIXES } from "../build/shared/compressionUtils.mjs";
import { listFilesUnder, statFilesByPath } from "../build/webManifest.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;

/** Filesystems and build steps disagree by milliseconds; only flag real lag. */
export const PRECOMPRESSED_TOLERANCE_MS = 1000;

// The staleness rule itself, as a pure function over an already-collected stat map, so the
// standalone CLI below and the default-suite gate in
// scripts/test/assets/generatedAssetMembership.test.mjs share one definition instead of two copies
// that can drift. Callers supply the stats because collecting them is the expensive part and
// the test already has a sweep of the same tree in hand.
export function evaluatePrecompressedFreshness( filePaths, statsByPath, sidecarOnlyPaths = new Set() ) {
	const stale = new Map();
	const orphans = [];
	let scanned = 0;

	for ( const filePath of filePaths ) {
		const suffix = PUBLISHED_SIDECAR_SUFFIXES.find( ( candidate ) => filePath.endsWith( candidate ) );
		if ( !suffix ) continue;
		scanned += 1;

		const assetPath = filePath.slice( 0, -suffix.length );
		const assetStat = statsByPath.get( path.resolve( assetPath ) );
		if ( !assetStat ) {
			// Compact releases deliberately retain only each pack's zstd
			// representation. Those files are primary, manifest-owned payloads, not
			// transparent encodings shadowing a missing .bin file.
			if ( sidecarOnlyPaths.has( path.resolve( filePath ) ) ) continue;
			orphans.push( filePath );
			continue;
		}

		const sidecarStat = statsByPath.get( path.resolve( filePath ) );
		if ( !sidecarStat ) continue;
		if ( sidecarStat.mtimeMs + PRECOMPRESSED_TOLERANCE_MS >= assetStat.mtimeMs ) continue;

		const entry = stale.get( assetPath ) ?? {
			assetPath,
			assetMs: assetStat.mtimeMs,
			oldestSidecarMs: sidecarStat.mtimeMs,
			suffixes: []
		};
		entry.oldestSidecarMs = Math.min( entry.oldestSidecarMs, sidecarStat.mtimeMs );
		entry.suffixes.push( suffix );
		stale.set( assetPath, entry );
	}

	return { scanned, stale: [ ...stale.values() ], orphans };
}

/**
 * The only paths whose mtime this comparison reads: each sidecar and the asset it shadows.
 * Every asset carries three encodings, so this is ~32k of the ~45k files under public/.
 */
export function precompressedStatTargets( filePaths ) {
	const targets = new Set();
	for ( const filePath of filePaths ) {
		const suffix = PUBLISHED_SIDECAR_SUFFIXES.find( ( candidate ) => filePath.endsWith( candidate ) );
		if ( !suffix ) continue;
		targets.add( filePath );
		targets.add( filePath.slice( 0, -suffix.length ) );
	}
	return [ ...targets ];
}

// Was 3 synchronous syscalls per sidecar (existsSync + 2 statSync, ~72k blocking calls). That
// is why this only ever ran by hand and the staleness half of it was gated by nothing: it has
// no concurrency to hide per-call latency behind, so it collapses whenever another lane is
// using the disk. One concurrent sweep of just the paths being compared replaces it.
export async function findStalePrecompressedSidecars( root = publicRoot ) {
	const filePaths = await listFilesUnder( root );
	const statsByPath = await statFilesByPath( precompressedStatTargets( filePaths ) );
	const sidecarOnlyPaths = await readManifestOwnedPackSidecars( root );
	return evaluatePrecompressedFreshness( filePaths, statsByPath, sidecarOnlyPaths );
}

async function readManifestOwnedPackSidecars( root ) {
	const manifestPath = path.join( root, "assets", "packs", "manifest.json" );
	let manifest;
	try {
		manifest = JSON.parse( await readFile( manifestPath, "utf8" ) );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return new Set();
		throw new Error( `Cannot read asset-pack manifest while checking sidecars: ${manifestPath}`, { cause: error } );
	}
	const paths = new Set();
	for ( const group of manifest.groups ?? [] ) {
		for ( const pack of group.packs ?? [] ) {
			if ( typeof pack.zstdPath !== "string" ) continue;
			paths.add( path.resolve( root, pack.zstdPath.replace( /^\/+/, "" ) ) );
		}
	}
	return paths;
}

const invokedDirectly = process.argv[1] !== undefined &&
	import.meta.url === pathToFileURL( path.resolve( process.argv[1] ) ).href;

if ( invokedDirectly ) {
	const fix = process.argv.includes( "--fix" );
	let { scanned, stale, orphans } = await findStalePrecompressedSidecars();
	console.log( `scanned ${scanned} precompressed sidecar(s) under .generated/client-public` );

	if ( stale.length > 0 && fix ) {
		console.log( `\nrepairing ${stale.length} asset(s):` );
		const results = await refreshPrecompressedSidecars( stale.map( ( entry ) => entry.assetPath ) );
		for ( const result of results ) {
			const rel = path.relative( publicRoot, result.assetPath ).replaceAll( "\\", "/" );
			console.log(
				result.written ?
					`   ${rel}: ${result.sourceBytes}b -> ${
						result.written
							.map( ( w ) => `${w.suffix}=${w.bytes}b` )
							.join( " " )
					}` :
					`   ${rel}: skipped (${result.skipped})`
			);
		}
		({ stale, orphans } = await findStalePrecompressedSidecars());
	}

	for ( const entry of stale ) {
		const rel = path.relative( publicRoot, entry.assetPath ).replaceAll( "\\", "/" );
		const lagDays = ((entry.assetMs - entry.oldestSidecarMs) / 86_400_000).toFixed( 2 );
		console.log(
			`STALE ${rel} [${entry.suffixes.join( "," )}] - sidecar is ${lagDays} day(s) older than the asset`
		);
	}
	for ( const orphan of orphans ) {
		console.log( `ORPHAN ${path.relative( publicRoot, orphan ).replaceAll( "\\", "/" )} - no asset behind it` );
	}

	if ( stale.length === 0 && orphans.length === 0 ) {
		console.log( "== precompressed sidecar freshness: PASS ==" );
		process.exit( 0 );
	}
	console.log(
		`\n== precompressed sidecar freshness: FAIL == ${stale.length} stale, ${orphans.length} orphan.\n` +
			"Every browser sends Accept-Encoding: br, so these assets are being served STALE to real\n" +
			"users while the fresh bytes sit on disk. Repair with:\n" +
			"  node scripts/checks/check_precompressed_freshness.mjs --fix"
	);
	process.exit( 1 );
}
