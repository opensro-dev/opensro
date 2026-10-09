/*
===========================================================================

resourceBuildFingerprint.mjs - skip a no-op resource build

A stat-level fingerprint over everything the SRO resource build reads and
writes, so a no-op rebuild can be skipped outright instead of re-verifying
~100k files through the full pipeline (~100s even when nothing changed).

The fingerprint is (path, size, mtimeMs) over:
- <game root>/extracted            every Media_extracted input the build parses
- <rebuild>/.generated/intermediate converted images + manifests feeding the copy steps
- <rebuild>/scripts/build (+entry) the pipeline code itself, plus every file
  outside it the entries can run (codeStamp.mjs codeClosure: the converter,
  sro_paths.py, scripts/lib), which a change there once skipped past
- SRO_ASSET_PACK_BASELINE's file  the layout baseline a release builds against
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
import { codeClosure } from "./codeStamp.mjs";
import { readJsonOrUndefined } from "./jsonOut.mjs";
import { extractedRoot, gameRoot, generatedRoot, publicRoot, rebuildRoot } from "../world/paths.mjs";

const serverSourceRoot = path.resolve(
	process.env.SRO_SERVER_SOURCE_ROOT ??
		path.join( rebuildRoot, "apps", "server" )
);
const statePath = path.join( rebuildRoot, ".state", "resource-build-fingerprint.json" );

// v7: the code closure and the pack-layout baseline joined the roots.
const FINGERPRINT_VERSION = 7;
const DIRECTORY_CONCURRENCY = 32;

/** Env vars that alter the build's outputs; a change must invalidate the skip. */
export const ENV_KNOBS = [
	"SRO_SERVER_SOURCE_ROOT",
	"SRO_ALLOW_DEV_OUTDOOR_ROUTING",
	"SRO_SKIP_TEXTURE_CONVERT",
	"SRO_ASSET_PACKS_NO_CACHE",
	"SRO_BUILD_HASH_CACHE",
	// The release builds against the live layout (RELEASE.md); an up-to-date
	// local tree must not skip that and keep its own layout.
	"SRO_ASSET_PACK_BASELINE"
];

// The entries whose source closure the fingerprint covers beyond scripts/build.
const CODE_ENTRIES = [
	path.join( rebuildRoot, "scripts", "build_sro_resources.mjs" ),
	path.join( rebuildRoot, "scripts", "build_outdoor_world_resources.mjs" ),
	path.join( rebuildRoot, "scripts", "convert_images.py" )
];

const FINGERPRINT_ROOTS = [
	{ label: "extracted", absolutePath: extractedRoot },
	{ label: "client-executable", absolutePath: path.join( gameRoot, "SRO_Client.exe" ) },
	// The effects builder checks the archive itself against its evidence.
	{ label: "particle-archive", absolutePath: path.join( gameRoot, "Particles.pk2" ) },
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

// The retail and converted inputs every lane may read (laneMemo.mjs): not
// the code, not the public tree, and not the server sources, which only
// the NPC lane's roster export runs (serverRosterRoots).
const DATA_ROOT_LABELS = new Set( [ "extracted", "client-executable", "particle-archive", "rebuild-assets" ] );

/*
================
hashRoots

The stat hash (path, size, mtime) over the given roots, absent ones included.
================
*/
export async function hashRoots( roots ) {
	const hash = createHash( "sha256" );
	for ( const root of roots ) {
		const rootLines = [];
		await collectRoot( root, rootLines );
		rootLines.sort();
		hash.update( `${root.label}\n${hashFingerprintLines( rootLines )}\n` );
	}
	return hash.digest( "hex" );
}

/*
================
dataRootsHash

The stat hash of each data root, by label, so a lane that runs can say
which root moved.
================
*/
export async function dataRootsHash() {
	const hashes = {};
	for ( const root of FINGERPRINT_ROOTS.filter( root => DATA_ROOT_LABELS.has( root.label ) ) ) {
		hashes[root.label] = await hashRoots( [ root ] );
	}
	return hashes;
}

/*
================
serverRosterRoots

The server sources the roster exporter (sro-evidence) compiles from: the
whole internal tree, since the roster joins enterworld, monster and more.
================
*/
export function serverRosterRoots() {
	return [
		{ label: "server-go-mod", absolutePath: path.join( serverSourceRoot, "go.mod" ) },
		{ label: "server-go-sum", absolutePath: path.join( serverSourceRoot, "go.sum" ) },
		{ label: "server-cmd", absolutePath: path.join( serverSourceRoot, "cmd" ) },
		{ label: "server-internal", absolutePath: path.join( serverSourceRoot, "internal" ) }
	];
}

/*
================
computeResourceBuildFingerprint
================
*/
export async function computeResourceBuildFingerprint() {
	const startedAt = performance.now();
	const lines = [];
	const roots = [];

	for ( const root of [ ...FINGERPRINT_ROOTS, ...(await dynamicRoots()) ] ) {
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
dynamicRoots

The code closure of the build entries (each file its own root, so a miss
names it) and the pack-layout baseline when one is set.
================
*/
async function dynamicRoots() {
	const files = new Set();
	for ( const entry of CODE_ENTRIES ) {
		for ( const file of await codeClosure( entry ) ) files.add( file );
	}
	const roots = [ ...files ].sort().map( ( absolutePath ) => ({
		label: `code:${path.relative( rebuildRoot, absolutePath ).split( path.sep ).join( "/" )}`,
		absolutePath
	}) );
	const baseline = process.env.SRO_ASSET_PACK_BASELINE?.trim();
	if ( baseline ) roots.push( { label: "pack-baseline", absolutePath: path.resolve( baseline ) } );
	return roots;
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
