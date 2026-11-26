/*
===========================================================================

resourceBuildFingerprint.mjs - skip a no-op resource build

A stat-level fingerprint over everything the SRO resource build reads and
writes, so a no-op rebuild can be skipped outright instead of re-verifying
~100k files through the full pipeline (~100s even when nothing changed).

The fingerprint is (path, size, mtimeMs) over:
- <game root>/extracted            every Media_extracted input the build parses
- <rebuild>/.generated/intermediate's image conversion (images/ and its
  manifest, discovery and failure files) feeding the copy steps
- <rebuild>/scripts/build (+entry) the pipeline code itself, plus every file
  outside it the entries can run (codeStamp.mjs codeClosure: the converter,
  sro_paths.py, scripts/lib), which a change there once skipped past
- SRO_ASSET_PACK_BASELINE's file  the layout baseline a release builds against
- <rebuild>/.generated/client-public       the published browser projection
- external source/codegen files read or written by character asset builders
- what the build reads of the Go server: the sro-evidence exports' bytes
  (serverExportsKey), the overlay data folders and the pet design
plus the env knobs that change what the build produces. It is recorded only
AFTER a successful build, so it always describes real on-disk state; any
external touch (content, timestamp, add, delete) misses and forces a full
build. Per-root hashes (and, for a directory root, per-folder hashes two
levels down) are persisted with the aggregate, and a miss prints what moved
(movedRoots): "fingerprint miss over 243133 files" alone left a release's
seven-minute rebuild unexplained (#4337). False rebuilds are therefore possible (a pure touch),
silent stale skips are not - short of an actor rewriting bytes while faking
the old mtime, which no build tool here does.

A root marked source (the checkout's own files: scripts, server sources,
survival art) is hashed by content, (path, size, sha256), not by mtime:
a checkout's mtimes say when it was checked out, so every fresh release
worktree moved them and reran every memoized lane on content that had not
changed (release 67b727e6c: all seven, "data survival-art changed"). The
hashes come from a file hash cache of their own, so a stat sweep vouches
for an unchanged file.

===========================================================================
*/
import { createHash } from "node:crypto";
import { readdir, stat } from "node:fs/promises";
import path from "node:path";
import { buildStatePath } from "../../lib/generatedRoot.mjs";
import { serverExportsKey } from "../char/npcModelRoster.mjs";
import { codeClosure } from "./codeStamp.mjs";
import { openFileHashCache } from "./fileHashCache.mjs";
import { crossCityTeleportEnabled } from "./crossCityTeleport.mjs";
import { skytempleEnabled } from "./skytemple.mjs";
import { janganTombTeleportEnabled } from "./janganTombTeleport.mjs";
import { resolveVsroRoot, VSRO_ROOT_ENV } from "./laterClientRoot.mjs";
import { muhanEnabled } from "./muhan.mjs";
import { qinshiTombEnabled } from "./qinshiTomb.mjs";
import { readJsonOrUndefined } from "./jsonOut.mjs";
import { writeStateFile } from "./stateFile.mjs";
import { extractedRoot, gameRoot, generatedRoot, publicRoot, rebuildRoot } from "../world/paths.mjs";

const serverSourceRoot = path.resolve(
	process.env.SRO_SERVER_SOURCE_ROOT ??
		path.join( rebuildRoot, "apps", "server" )
);
// Beside the generated tree it describes, so a fresh worktree that reads the
// shared tree finds the last build's fingerprint (generatedRoot.mjs).
const statePath = buildStatePath( "resource-build-fingerprint.json" );

// v12: the server exports' bytes replace the Go sources behind them (#4337).
// v11: source roots hash by content; rebuild-assets is the image conversion only.
// v10: the effective tomb posture joined the knobs (#2024 follow-up).
// v9: the pet design joined; it moved from scripts/build into the server (#1631).
// v8: the Qin-Shi tomb overlay knob and the enterworld roots joined.
// v7: the code closure and the pack-layout baseline joined the roots.
const FINGERPRINT_VERSION = 12;
const DIRECTORY_CONCURRENCY = 32;
// How deep a directory root's recorded folder hashes go: two levels name
// "public/assets/images" rather than "public", without one row per file.
const MOVED_FOLDER_DEPTH = 2;
// How many moved roots a miss names before it counts the rest.
const MOVED_ROOTS_SHOWN = 12;
// The content hashes of the source roots: their own cache, so the lanes
// keying themselves at once never race the pack builder's.
const SOURCE_HASH_CACHE_PATH = buildStatePath( "source-hash-cache.json" );
// What the lanes read from .generated/intermediate: the image conversion
// (convert_images.py's OUTPUT_ROOT, MANIFEST_PATH, DISCOVERY_CACHE_PATH and
// failure list). The texture-variant, block-texture and terrain-tile
// caches there belong to steps no lane memo covers, and each keys itself
// by its own digests; counting them made a build that refilled one miss
// every lane on the next.
const IMAGE_CONVERSION_PARTS = [
	"images",
	"image-manifest.csv",
	"image-discovery.json",
	"image-conversion-failures.txt"
];

/** Env vars that alter the build's outputs; a change must invalidate the skip. */
export const ENV_KNOBS = [
	"SRO_SERVER_SOURCE_ROOT",
	// Port-only, not native: the server's Qin-Shi tomb overlays join the
	// client bake (shared/qinshiTomb.mjs).
	"SRO_QINSHI_TOMB",
	// Port-only, not native: the server's Muhan teleport overlays join
	// the teleport bake (shared/muhan.mjs, #3314).
	"SRO_MUHAN",
	// Port-only, not native: the Jangan gate's tomb floor links join the
	// teleport bake (shared/janganTombTeleport.mjs).
	"SRO_JANGAN_TOMB_TELEPORT",
	// Port-only, not native: the direct town-to-town gate links join the
	// teleport bake (shared/crossCityTeleport.mjs).
	"SRO_CROSS_CITY_TELEPORT",
	// Port-only, not native: the Sky Temple fields leave the teleport
	// bake while the switch is off (shared/skytemple.mjs).
	"SRO_SKYTEMPLE",
	"SRO_SOOK_SURVIVAL_ARENA",
	"SRO_ALLOW_DEV_OUTDOOR_ROUTING",
	"SRO_SKIP_TEXTURE_CONVERT",
	"SRO_ASSET_PACKS_NO_CACHE",
	"SRO_BUILD_HASH_CACHE",
	// The release builds against the live layout (docs/operations/RELEASE.md); an up-to-date
	// local tree must not skip that and keep its own layout.
	"SRO_ASSET_PACK_BASELINE",
	// The later client's dungeon floor pages join the build from the pinned
	// archive this names (#2956); setting or moving it must not skip.
	"SRO_VSRO_ROOT"
];

/*
================
BAKE_SWITCH_POSTURES

The bake switches whose deployed posture joins the knob lines: each
switch's env name and its enabled reader. A switch joins the fingerprint
only through this list. Its default stays in the shard settings registry
(shardsettings.go, the one owner), never copied here (#3779).
================
*/
export const BAKE_SWITCH_POSTURES = [
	{ env: "SRO_QINSHI_TOMB", enabled: qinshiTombEnabled },
	{ env: "SRO_MUHAN", enabled: muhanEnabled },
	{ env: "SRO_JANGAN_TOMB_TELEPORT", enabled: janganTombTeleportEnabled },
	{ env: "SRO_CROSS_CITY_TELEPORT", enabled: crossCityTeleportEnabled },
	{ env: "SRO_SKYTEMPLE", enabled: skytempleEnabled }
];

/*
================
buildKnobLines

The knob lines both the whole-build fingerprint and every lane memo
hash: each raw ENV_KNOBS value, then the postures the bake derives
from more than its env. The tomb bake follows the shard's deployed
posture when SRO_QINSHI_TOMB is unset (shared/qinshiTomb.mjs), read
from the release profile and the shard registry, neither of which is a
fingerprint root; hashing only the raw env would let a profile or
registry flip skip the bake and ship the other posture's assets, the
#2024 split in reverse.
================
*/
export function buildKnobLines() {
	const lines = ENV_KNOBS.map( ( name ) => `env ${name}=${process.env[name] ?? ""}` );
	for ( const { env, enabled } of BAKE_SWITCH_POSTURES ) {
		lines.push( `posture ${env}=${enabled()}` );
	}
	// The later client the variant stamp names stands in for an unset
	// SRO_VSRO_ROOT (laterClientRoot.mjs): finding one must not skip. A
	// shell that names the root is hashed above already; the line joins
	// only then, so trees that set it keep their memos.
	const later = resolveVsroRoot();
	if ( later.source !== VSRO_ROOT_ENV && later.root ) lines.push( `posture SRO_VSRO_ROOT=${later.root}` );
	return lines;
}

// The entries whose source closure the fingerprint covers beyond scripts/build.
const CODE_ENTRIES = [
	path.join( rebuildRoot, "scripts", "build_sro_resources.mjs" ),
	path.join( rebuildRoot, "scripts", "build_outdoor_world_resources.mjs" ),
	path.join( rebuildRoot, "scripts", "convert_images.py" )
];

const FINGERPRINT_ROOTS = [
	{
		label: "survival-art",
		absolutePath: path.join( rebuildRoot, "docs", "features", "assets", "survival" ),
		source: true
	},
	{
		label: "pet-art",
		absolutePath: path.join( rebuildRoot, "docs", "pet-expansion", "assets" ),
		source: true
	},
	{ label: "extracted", absolutePath: extractedRoot },
	{ label: "client-executable", absolutePath: path.join( gameRoot, "SRO_Client.exe" ) },
	// The effects builder checks the archive itself against its evidence.
	{ label: "particle-archive", absolutePath: path.join( gameRoot, "Particles.pk2" ) },
	{
		// The sro-evidence exports (rosters, unique nests) as bytes, not the
		// Go sources behind them: keyed on all of internal/game/world, any
		// server change there rebuilt every step, though no export moved
		// (r204m2: about seven minutes, #4337).
		label: "server-exports",
		key: "serverExports"
	},
	{
		// The monster overlay TSV the character bake reads with SRO_QINSHI_TOMB on.
		label: "server-monster-data",
		absolutePath: path.join( serverSourceRoot, "internal", "game", "world", "monster", "data" ),
		source: true
	},
	{
		// The skill overlay TSV the skill bake reads with SRO_QINSHI_TOMB on.
		label: "server-enterworld",
		absolutePath: path.join( serverSourceRoot, "internal", "game", "enterworld", "data" ),
		source: true
	},
	{
		// The teleportlink overlay TSV the teleport bake reads with SRO_QINSHI_TOMB on.
		label: "server-action-data",
		absolutePath: path.join( serverSourceRoot, "internal", "game", "action", "data" ),
		source: true
	},
	{
		// The pet design the pet gear rows read (portItemRows.mjs).
		label: "server-pet-design",
		absolutePath: path.join( serverSourceRoot, "internal", "game", "companion", "petdesign.json" ),
		source: true
	},
	{
		label: "rebuild-assets",
		absolutePath: path.join( generatedRoot, "intermediate" ),
		parts: IMAGE_CONVERSION_PARTS
	},
	{ label: "build-scripts", absolutePath: path.join( rebuildRoot, "scripts", "build" ), source: true },
	{
		label: "build-entry",
		absolutePath: path.join( rebuildRoot, "scripts", "build_sro_resources.mjs" ),
		source: true
	},
	{
		label: "outdoor-entry",
		absolutePath: path.join( rebuildRoot, "scripts", "build_outdoor_world_resources.mjs" ),
		source: true
	},
	{ label: "lock-helper", absolutePath: path.join( rebuildRoot, "scripts", "rebuildLock.mjs" ), source: true },
	{ label: "public", absolutePath: publicRoot }
];

// The retail and converted inputs every lane may read (laneMemo.mjs): not
// the code, not the public tree, and not the server sources, which only
// the NPC lane's roster export runs (serverRosterRoots).
const DATA_ROOT_LABELS = new Set( [
	"extracted",
	"client-executable",
	"particle-archive",
	"rebuild-assets",
	"survival-art",
	"pet-art"
] );

/*
================
hashRoots

The hash over the given roots, absent ones included: (path, size, mtime)
per file, or (path, size, sha256) for a source root. hashes is the
source hash cache (tests pass their own); by default the build state's.
================
*/
export async function hashRoots( roots, hashes = undefined ) {
	const hash = createHash( "sha256" );
	hashes ??= await sourceHashes();
	for ( const root of roots ) {
		const rootLines = [];
		await collectRoot( root, rootLines, hashes );
		rootLines.sort();
		hash.update( `${root.label}\n${hashFingerprintLines( rootLines )}\n` );
	}
	await hashes.save();
	return hash.digest( "hex" );
}

/*
================
folderHashes

A directory root's sorted fingerprint lines ("<label>/<path>|size|stamp"),
hashed per folder or file depth levels inside it (one by default), as
"<label>/<name>"; a file above that depth is its own entry.
================
*/
export function folderHashes( label, lines, depth = 1 ) {
	const folders = new Map(), prefix = label + "/";
	for ( const line of lines ) {
		if ( !line.startsWith( prefix ) ) continue;
		const name = line.slice( prefix.length ).split( "|", 1 )[0].split( "/" ).slice( 0, depth ).join( "/" );
		if ( !folders.has( name ) ) folders.set( name, [] );
		folders.get( name ).push( line );
	}
	return Object.fromEntries(
		[ ...folders ].map( ( [name, rows] ) => [ prefix + name, hashFingerprintLines( rows ) ] )
	);
}

/*
================
dataRootsHash

The stat hash of each data root, by label, and of each top folder (or
file) inside a directory root, as "<label>/<name>", so a lane that runs
says which folder moved: "data extracted changed" alone hid that the
asset build's own optional-teleport import rewrote one file under
extracted/ on every run, rerunning all three lanes (#4337).
================
*/
export async function dataRootsHash() {
	const hashes = {}, files = await sourceHashes();
	for ( const root of FINGERPRINT_ROOTS.filter( root => DATA_ROOT_LABELS.has( root.label ) ) ) {
		// One walk: the root's hash as hashRoots( [ root ] ) gives it.
		const lines = [];
		await collectRoot( root, lines, files );
		lines.sort();
		hashes[root.label] = createHash( "sha256" ).update( `${root.label}\n${hashFingerprintLines( lines )}\n` )
			.digest( "hex" );
		Object.assign( hashes, folderHashes( root.label, lines ) );
	}
	await files.save();
	return hashes;
}

/*
================
computeResourceBuildFingerprint

keys names the functions behind the key roots (a root hashed from a
function's output rather than from files); tests pass their own so no
Go tool runs.
================
*/
export async function computeResourceBuildFingerprint( keys = { serverExports: serverExportsKey } ) {
	const startedAt = performance.now();
	const lines = [];
	const roots = [];
	const hashes = await sourceHashes();

	for ( const root of [ ...FINGERPRINT_ROOTS, ...(await dynamicRoots()) ] ) {
		const rootLines = [];
		if ( root.key ) rootLines.push( `${root.label}|key|${await keys[root.key]()}` );
		else await collectRoot( root, rootLines, hashes );
		rootLines.sort();
		lines.push( ...rootLines );
		const folders = folderHashes( root.label, rootLines, MOVED_FOLDER_DEPTH );
		roots.push( {
			label: root.label,
			hash: hashFingerprintLines( rootLines ),
			fileCount: rootLines.length,
			...(Object.keys( folders ).length > 0 ? { folders } : {})
		} );
	}

	await hashes.save();
	lines.sort();

	const hash = createHash( "sha256" );
	hash.update( `v${FINGERPRINT_VERSION}\n` );
	const knobLines = buildKnobLines();
	for ( const line of knobLines ) {
		hash.update( `${line}\n` );
	}
	for ( const line of lines ) {
		hash.update( line );
		hash.update( "\n" );
	}

	return {
		hash: hash.digest( "hex" ),
		fileCount: lines.length,
		knobs: hashFingerprintLines( knobLines ),
		roots,
		elapsedMs: Math.round( performance.now() - startedAt )
	};
}

/*
================
movedRoots

What a miss moved, against the recorded fingerprint: the build settings
(the env knobs and postures), each root that changed, appeared or went,
and for a changed directory root the recorded folders that moved instead
of the whole root. A record written before a field existed simply names
less (no knobs, the root without its folders); the aggregate hash is
unchanged, so such a record still skips a no-op build.
================
*/
export function movedRoots( recorded, current ) {
	const moved = [];
	if ( recorded.knobs !== undefined && recorded.knobs !== current.knobs ) moved.push( "build settings" );
	const before = new Map( (recorded.roots ?? []).map( ( root ) => [ root.label, root ] ) );
	for ( const root of current.roots ) {
		const old = before.get( root.label );
		before.delete( root.label );
		if ( old === undefined ) {
			moved.push( `${root.label} (new)` );
			continue;
		}
		if ( old.hash === root.hash ) continue;
		const folders = movedFolders( old.folders, root.folders );
		moved.push( ...(folders.length > 0 ? folders : [ root.label ]) );
	}
	for ( const label of before.keys() ) moved.push( `${label} (gone)` );
	return moved;
}

/*
================
movedFolders

The folder names whose hash differs between two recorded folder maps,
either side's extra names included; none when either map is missing.
================
*/
function movedFolders( before, after ) {
	if ( before === undefined || after === undefined ) return [];
	const names = new Set( [ ...Object.keys( before ), ...Object.keys( after ) ] );
	return [ ...names ].filter( ( name ) => before[name] !== after[name] ).sort();
}

/*
================
describeMovedRoots

movedRoots as one log phrase, the first MOVED_ROOTS_SHOWN named and the
rest counted.
================
*/
export function describeMovedRoots( moved ) {
	if ( moved.length === 0 ) return "nothing recorded moved";
	const shown = moved.slice( 0, MOVED_ROOTS_SHOWN ).join( ", " );
	const rest = moved.length - MOVED_ROOTS_SHOWN;
	return rest > 0 ? `${shown} and ${rest} more` : shown;
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
		absolutePath,
		source: true
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
	const payload = `${
		JSON.stringify(
			{
				comment: "Stat fingerprint of the SRO resource build's inputs and outputs, written after a " +
					"successful build by scripts/build_sro_resources.mjs. When the current tree matches, " +
					"the build is skipped; delete this file or set SRO_FORCE_RESOURCE_BUILD=1 to force.",
				version: FINGERPRINT_VERSION,
				hash: fingerprint.hash,
				fileCount: fingerprint.fileCount,
				knobs: fingerprint.knobs,
				roots: fingerprint.roots,
				recordedAt: new Date().toISOString()
			},
			null,
			2
		)
	}\n`;
	await writeStateFile( statePath, payload );
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
sourceHashes

The source roots' hash cache, opened once per process and shared by every
caller (the lanes key themselves concurrently; each save writes the whole
in-memory set atomically).
================
*/
let sourceHashCache;
function sourceHashes() {
	sourceHashCache ??= openFileHashCache( SOURCE_HASH_CACHE_PATH );
	return sourceHashCache;
}

/*
================
fileLine

One file's line: its size and mtime, or its size and content hash for a
source root.
================
*/
async function fileLine( name, filePath, fileStat, source, hashes ) {
	const stamp = source ? await hashes.hashFile( filePath, fileStat ) : fileStat.mtimeMs;
	return `${name}|${fileStat.size}|${stamp}`;
}

/*
================
collectRoot

A root's lines; a root with parts collects only those entries below it,
each named under the root's label.
================
*/
async function collectRoot( root, lines, hashes ) {
	if ( root.parts ) {
		for ( const part of root.parts ) {
			const entry = { label: `${root.label}/${part}`, absolutePath: path.join( root.absolutePath, part ) };
			await collectRoot( { ...entry, source: root.source }, lines, hashes );
		}
		return;
	}
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
		lines.push( await fileLine( root.label, root.absolutePath, rootStat, root.source, hashes ) );
		return;
	}

	await walkDirectory( root, lines, hashes );
}

/*
================
walkDirectory

Parallel breadth-first walk: directories are read DIRECTORY_CONCURRENCY at a
time and every file is stat()ed. On Windows each stat is a slow syscall, so the
fan-out is what keeps ~120k files in the low seconds instead of half a minute.
================
*/
async function walkDirectory( root, lines, hashes ) {
	const rootPath = root.absolutePath;
	const pending = [ rootPath ];

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
							return await fileLine(
								`${root.label}/${relativePath}`,
								filePath,
								fileStat,
								root.source,
								hashes
							);
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
