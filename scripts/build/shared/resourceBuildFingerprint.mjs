/*
===========================================================================

resourceBuildFingerprint.mjs - skip a no-op resource build

A stat-level fingerprint over everything the SRO resource build reads and
writes, so a no-op rebuild can be skipped outright instead of re-verifying
~100k files through the full pipeline (~100s even when nothing changed).

The fingerprint is (path, size, mtimeMs) over:
- <game root>/extracted            every Media_extracted input the build parses
- <rebuild>/.generated/intermediate converted images + manifests feeding the copy steps
- <rebuild>/scripts/build (+entry) the pipeline code itself
- <rebuild>/.generated/client-public       the published browser projection
- external source/codegen files read or written by character asset builders
plus the env knobs that change what the build produces. It is recorded only
AFTER a successful build, so it always describes real on-disk state; any
external touch (content, timestamp, add, delete) misses and forces a full
build. Per-root hashes are persisted with the aggregate so an unexpected
miss identifies the mutated ownership boundary instead of leaving one opaque
~120k-file digest. False rebuilds are therefore possible (a pure touch),
silent stale skips are not - short of an actor rewriting bytes while faking
the old mtime, which no build tool here does.

===========================================================================
*/
import { createHash } from "node:crypto";
import { mkdir, readdir, rename, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { readJsonOrUndefined } from "./jsonOut.mjs";
import { extractedRoot, gameRoot, generatedRoot, publicRoot, rebuildRoot } from "../world/paths.mjs";

const serverSourceRoot = path.resolve(
	process.env.SRO_SERVER_SOURCE_ROOT ??
		path.join( rebuildRoot, "apps", "server" )
);
const statePath = path.join( rebuildRoot, ".state", "resource-build-fingerprint.json" );

const FINGERPRINT_VERSION = 6;
const DIRECTORY_CONCURRENCY = 32;

/** Env vars that alter the build's outputs; a change must invalidate the skip. */
const ENV_KNOBS = [
	"SRO_SERVER_SOURCE_ROOT",
	"SRO_ALLOW_DEV_OUTDOOR_ROUTING",
	"SRO_SKIP_TEXTURE_CONVERT",
	"SRO_RESOURCE_BUILD_LANES",
	"SRO_ASSET_ENCODINGS",
	"SRO_ASSET_COMPRESSION_JOBS",
	"SRO_ASSET_PACKS_NO_CACHE",
	"SRO_BUILD_HASH_CACHE"
];

const FINGERPRINT_ROOTS = [
	{ label: "extracted", absolutePath: extractedRoot },
	{ label: "client-executable", absolutePath: path.join( gameRoot, "SRO_Client.exe" ) },
	{ label: "server-go-mod", absolutePath: path.join( serverSourceRoot, "go.mod" ) },
	{ label: "server-go-sum", absolutePath: path.join( serverSourceRoot, "go.sum" ) },
	{
		label: "server-roster-exporter",
		absolutePath: path.join( serverSourceRoot, "cmd", "tools", "sro-evidence" )
	},
	{
		label: "server-roster-policy",
		absolutePath: path.join( serverSourceRoot, "internal", "game", "world" )
	},
	{ label: "rebuild-assets", absolutePath: path.join( generatedRoot, "intermediate" ) },
	{ label: "build-scripts", absolutePath: path.join( rebuildRoot, "scripts", "build" ) },
	{ label: "build-entry", absolutePath: path.join( rebuildRoot, "scripts", "build_sro_resources.mjs" ) },
	{ label: "outdoor-entry", absolutePath: path.join( rebuildRoot, "scripts", "build_outdoor_world_resources.mjs" ) },
	{ label: "lock-helper", absolutePath: path.join( rebuildRoot, "scripts", "rebuildLock.mjs" ) },
	{ label: "public", absolutePath: publicRoot }
];

/*
================
computeResourceBuildFingerprint
================
*/
export async function computeResourceBuildFingerprint() {
	const startedAt = performance.now();
	const lines = [];
	const roots = [];

	for ( const root of FINGERPRINT_ROOTS ) {
		const rootLines = [];
		await collectRoot( root, rootLines );
		rootLines.sort();
		lines.push( ...rootLines );
		roots.push( {
			label: root.label,
			hash: hashFingerprintLines( rootLines ),
			fileCount: rootLines.length
		} );
	}

	lines.sort();

	const hash = createHash( "sha256" );
	hash.update( `v${FINGERPRINT_VERSION}\n` );
	for ( const knob of ENV_KNOBS ) {
		hash.update( `env ${knob}=${process.env[knob] ?? ""}\n` );
	}
	for ( const line of lines ) {
		hash.update( line );
		hash.update( "\n" );
	}

	return {
		hash: hash.digest( "hex" ),
		fileCount: lines.length,
		roots,
		elapsedMs: Math.round( performance.now() - startedAt )
	};
}

/*
================
readRecordedFingerprint
================
*/
export async function readRecordedFingerprint() {
	const recorded = await readJsonOrUndefined( statePath );
	if ( recorded?.version === FINGERPRINT_VERSION && typeof recorded.hash === "string" ) {
		return recorded;
	}
	return null;
}

/*
================
writeRecordedFingerprint
================
*/
export async function writeRecordedFingerprint( fingerprint ) {
	await mkdir( path.dirname( statePath ), { recursive: true } );
	const payload = `${
		JSON.stringify(
			{
				comment: "Stat fingerprint of the SRO resource build's inputs and outputs, written after a " +
					"successful build by scripts/build_sro_resources.mjs. When the current tree matches, " +
					"the build is skipped; delete this file or set SRO_FORCE_RESOURCE_BUILD=1 to force.",
				version: FINGERPRINT_VERSION,
				hash: fingerprint.hash,
				fileCount: fingerprint.fileCount,
				roots: fingerprint.roots,
				recordedAt: new Date().toISOString()
			},
			null,
			2
		)
	}\n`;
	const temporaryPath = `${statePath}.tmp`;
	await writeFile( temporaryPath, payload, "utf8" );
	await rename( temporaryPath, statePath );
}

/*
================
hashFingerprintLines
================
*/
function hashFingerprintLines( lines ) {
	const hash = createHash( "sha256" );
	for ( const line of lines ) {
		hash.update( line );
		hash.update( "\n" );
	}
	return hash.digest( "hex" );
}

/*
================
collectRoot
================
*/
async function collectRoot( root, lines ) {
	let rootStat;
	try {
		rootStat = await stat( root.absolutePath );
	} catch {
		// An absent root is itself part of the state (e.g. no Media_extracted
		// checkout): record the absence so its appearance invalidates the skip.
		lines.push( `${root.label}|<absent>` );
		return;
	}

	if ( rootStat.isFile() ) {
		lines.push( `${root.label}|${rootStat.size}|${rootStat.mtimeMs}` );
		return;
	}

	await walkDirectory( root.label, root.absolutePath, root.absolutePath, lines );
}

/*
================
walkDirectory

Parallel breadth-first walk: directories are read DIRECTORY_CONCURRENCY at a
time and every file is stat()ed. On Windows each stat is a slow syscall, so the
fan-out is what keeps ~120k files in the low seconds instead of half a minute.
================
*/
async function walkDirectory( label, rootPath, startPath, lines ) {
	const pending = [ startPath ];

	while ( pending.length > 0 ) {
		const batch = pending.splice( 0, DIRECTORY_CONCURRENCY );
		const results = await Promise.all(
			batch.map( async ( directory ) => {
				const entries = await readdir( directory, { withFileTypes: true } ).catch( () => [] );
				const subdirectories = [];
				const files = [];
				for ( const entry of entries ) {
					if ( entry.isSymbolicLink() ) {
						continue;
					}
					const absolutePath = path.join( directory, entry.name );
					if ( entry.isDirectory() ) {
						subdirectories.push( absolutePath );
					} else if ( entry.isFile() ) {
						files.push( absolutePath );
					}
				}
				const statted = await Promise.all(
					files.map( async ( filePath ) => {
						try {
							const fileStat = await stat( filePath );
							const relativePath = path.relative( rootPath, filePath ).split( path.sep ).join( "/" );
							return `${label}/${relativePath}|${fileStat.size}|${fileStat.mtimeMs}`;
						} catch {
							return null;
						}
					} )
				);
				return { subdirectories, statted };
			} )
		);

		for ( const result of results ) {
			pending.push( ...result.subdirectories );
			for ( const line of result.statted ) {
				if ( line !== null ) {
					lines.push( line );
				}
			}
		}
	}
}
