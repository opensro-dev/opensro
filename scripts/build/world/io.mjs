import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { readdirSync } from "node:fs";
import path from "node:path";
import { noteRead } from "../graph/reads.mjs";
import { foldAssetCase } from "../shared/assetPaths.mjs";
import { pathExists } from "../shared/fsUtils.mjs";
import { writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { publicRoot } from "./paths.mjs";

/**
 * Write minified JSON, skipping the write when the target already holds identical bytes.
 * Minified is the file's final form anyway (optimizePublicJsonAssets rewrites pretty JSON
 * in place), and the unchanged-skip keeps mtimes stable so the minify/sidecar/pack caches
 * hold across rebuilds - re-writing the multi-MB region JSONs every run forced minutes of
 * sidecar recompression for byte-identical content.
 */
export const writeJson = writeJsonIfChanged;

export async function writePublicFile( publicPath, bytes ) {
	const targetPath = path.join( publicRoot, publicPath.replace( /^\/+/, "" ) );
	await mkdir( path.dirname( targetPath ), { recursive: true } );
	await writeFile( targetPath, bytes );
	claimPublicFile( targetPath );
}

export const exists = pathExists;

// ============================================================================

// One directory listing per directory per process, shared as a promise so
// concurrent opens of one tree fill it once.
const directoryIndexes = new Map();

/*
================
directoryIndex

The real entry names of one directory: the names themselves, and each
name under its folded form (foldAssetCase). Null when the directory
cannot be listed at all. A refresh replaces the cached listing: the
build creates directories as it goes, so a listing taken before a
sibling appeared must not answer for it.
================
*/
async function directoryIndex( directory, refresh = false ) {
	let index = refresh ? undefined : directoryIndexes.get( directory );
	if ( !index ) {
		index = (async () => {
			try {
				const names = new Set();
				const byFolded = new Map();
				for ( const name of await readdir( directory ) ) {
					names.add( name );
					const folded = foldAssetCase( name );
					if ( !byFolded.has( folded ) ) {
						byFolded.set( folded, name );
					}
				}
				return { names, byFolded };
			} catch {
				return null;
			}
		})();
		directoryIndexes.set( directory, index );
	}
	return index;
}

/*
================
directoryEntry

One segment's on-disk spelling in a directory index: the segment
itself on an exact match, else its folded match, else undefined.
================
*/
function directoryEntry( index, segment ) {
	if ( !index ) {
		return undefined;
	}
	if ( index.names.has( segment ) ) {
		return segment;
	}
	return index.byFolded.get( foldAssetCase( segment ) );
}

/*
================
resolveOnDiskPath

The path as the disk spells it. The extracted trees keep the folded
form of the native names, while the game data that names a file keeps
its authored case (`Dungeon\wchina\Dunhwang_Cv.dof` in dungeoninfo.txt,
disk `dungeon/wchina/dunhwang_cv.dof`), so a literal open of a
data-named file only agrees with the disk on a case-insensitive file
system - `assets build full` stopped with ENOENT on Linux (#3962). A
segment that exactly matches a real entry keeps the caller's spelling;
a segment that only differs by ASCII case is replaced with the real
entry (the native client's own pk2 lookup folds the same way); a
segment with no folded match - a genuinely missing file - also keeps
the caller's spelling, so a missing file still fails naming the path
the caller asked for. Build outputs are opened by the exact spelling
they were written with, which resolves to the caller's path unchanged.
================
*/
export async function resolveOnDiskPath( file ) {
	const parsed = path.parse( file );
	const segments = file.slice( parsed.root.length ).split( /[\\/]+/ ).filter( Boolean );
	let directory = parsed.root;
	for ( const segment of segments ) {
		// A miss re-lists the directory once before it is believed: the
		// segment may be a directory or file the build just created
		// under a parent an earlier open already listed.
		let real = directoryEntry( await directoryIndex( directory ), segment );
		if ( real === undefined ) {
			real = directoryEntry( await directoryIndex( directory, true ), segment );
		}
		if ( real === undefined ) {
			return file;
		}
		directory = path.join( directory, real );
	}
	return directory;
}

// One directory listing per directory per process for the synchronous
// resolver, with the same refresh-on-miss rule as the async cache: the
// build creates directories as it goes, so a listing taken before a
// sibling appeared must not answer for it.
const syncDirectoryIndexes = new Map();

/*
================
directoryIndexSync

The synchronous twin of directoryIndex, over an injectable listing so
a test can serve a case-sensitive directory on a platform whose own
file system folds case. The listing is cached per directory; a null
index (the directory cannot be listed) is cached too.
================
*/
function directoryIndexSync( directory, listDirectory, refresh = false ) {
	if ( !refresh && syncDirectoryIndexes.has( directory ) ) {
		return syncDirectoryIndexes.get( directory );
	}
	let index = null;
	try {
		const names = new Set();
		const byFolded = new Map();
		for ( const name of listDirectory( directory ) ) {
			names.add( name );
			const folded = foldAssetCase( name );
			if ( !byFolded.has( folded ) ) {
				byFolded.set( folded, name );
			}
		}
		index = { names, byFolded };
	} catch {
		index = null;
	}
	syncDirectoryIndexes.set( directory, index );
	return index;
}

/*
================
resolveOnDiskPathSync

resolveOnDiskPath for the readers that cannot await (the character
and effect readers open synchronously): the same rule - an exact
segment keeps the caller's spelling, a case-only difference takes the
real entry, a genuinely missing segment keeps the caller's spelling so
the failure names the path that was asked for. One rule in two
runtimes, so a build can never disagree with itself about the
spelling of a file (#4468).
================
*/
export function resolveOnDiskPathSync( file, listDirectory = readdirSync ) {
	const parsed = path.parse( file );
	const segments = file.slice( parsed.root.length ).split( /[\\/]+/ ).filter( Boolean );
	let directory = parsed.root;
	for ( const segment of segments ) {
		let real = directoryEntry( directoryIndexSync( directory, listDirectory ), segment );
		if ( real === undefined ) {
			real = directoryEntry( directoryIndexSync( directory, listDirectory, true ), segment );
		}
		if ( real === undefined ) {
			return file;
		}
		directory = path.join( directory, real );
	}
	return directory;
}

/*
================
notedExists

pathExists that tells a tracked step (graph/reads.mjs) it probed the
file: a step that chose a source because another was missing depends on
that absence. Outside a tracked step it is pathExists. The probe runs
against the on-disk spelling (resolveOnDiskPath), so a data-named
source is found on a case-sensitive file system too; the read is
recorded under the path that was actually probed.
================
*/
export async function notedExists( file ) {
	const resolved = await resolveOnDiskPath( file );
	noteRead( resolved );
	return pathExists( resolved );
}

/*
================
notedReadFile

readFile that tells a tracked step it read the file, against the
on-disk spelling (resolveOnDiskPath); the read is recorded under the
path that was actually read.
================
*/
export async function notedReadFile( file, ...rest ) {
	const resolved = await resolveOnDiskPath( file );
	noteRead( resolved );
	return readFile( resolved, ...rest );
}
