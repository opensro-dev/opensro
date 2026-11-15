// Minified, write-if-changed JSON writers. Unchanged content keeps its file mtime, which is
// what lets the minify-, sidecar- and pack-caches skip work across rebuilds: an uncondition-
// ally rewritten JSON invalidates its gzip sidecar and its containing asset
// pack even when the bytes are identical. Minified output matches the form the JSON
// optimizer would rewrite the file to anyway.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { claimPublicFile } from "./publicationLedger.mjs";

/** Returns true when the file was written (content changed or file was missing). */
export async function writeJsonIfChanged( targetPath, value ) {
	const bytes = Buffer.from( JSON.stringify( value ), "utf8" );
	claimPublicFile( targetPath );
	try {
		if ( (await readFile( targetPath )).equals( bytes ) ) {
			return false;
		}
	} catch {
		// Missing or unreadable target: fall through to the write.
	}
	await mkdir( path.dirname( targetPath ), { recursive: true } );
	await writeFile( targetPath, bytes );
	return true;
}

/** Synchronous variant for the sync build steps. */
export function writeJsonIfChangedSync( targetPath, value ) {
	const bytes = Buffer.from( JSON.stringify( value ), "utf8" );
	claimPublicFile( targetPath );
	try {
		if ( readFileSync( targetPath ).equals( bytes ) ) {
			return false;
		}
	} catch {
		// Missing or unreadable target: fall through to the write.
	}
	mkdirSync( path.dirname( targetPath ), { recursive: true } );
	writeFileSync( targetPath, bytes );
	return true;
}

// Windows refuses a rename onto a file another process is renaming onto
// or holding at that instant (EPERM); a cache save retries briefly.
const STATE_RENAME_ATTEMPTS = 20;
const STATE_RENAME_BACKOFF_MS = 25;

/*
================
writeStateJson

Replace a build-state JSON file (a reuse cache, never a public asset) whole:
the bytes go to a temp name unique to this process, then rename over the
target. The caches live beside the shared generated tree (generatedRoot.mjs
buildStatePath), so two worktrees' builds or checks may save one at the same
time; an in-place write could interleave them into a file neither wrote.
Every reader sees one complete cache. When another writer keeps the target
busy through every attempt, its complete cache stands and this save is
dropped: a cache only ever saves work, so losing one save costs a re-hash.
Returns whether this save was published.
================
*/
export async function writeStateJson( targetPath, value ) {
	await mkdir( path.dirname( targetPath ), { recursive: true } );
	const temporaryPath = `${targetPath}.${process.pid}.${Math.random().toString( 16 ).slice( 2 )}.tmp`;
	try {
		await writeFile( temporaryPath, JSON.stringify( value ), "utf8" );
		for ( let attempt = 1; attempt <= STATE_RENAME_ATTEMPTS; attempt++ ) {
			try {
				await rename( temporaryPath, targetPath );
				return true;
			} catch ( error ) {
				if ( error?.code !== "EPERM" && error?.code !== "EACCES" ) throw error;
				await new Promise( resolve => setTimeout( resolve, STATE_RENAME_BACKOFF_MS * attempt ) );
			}
		}
		return false;
	} finally {
		await rm( temporaryPath, { force: true } );
	}
}

/** Parse JSON, returning undefined when the file is missing, unreadable, or invalid. */
export async function readJsonOrUndefined( filePath ) {
	try {
		return JSON.parse( await readFile( filePath, "utf8" ) );
	} catch {
		return undefined;
	}
}

/** Synchronous safe-read variant for synchronous build pipelines. */
export function readJsonOrUndefinedSync( filePath ) {
	try {
		return JSON.parse( readFileSync( filePath, "utf8" ) );
	} catch {
		return undefined;
	}
}

export async function readJsonOrNull( filePath ) {
	return (await readJsonOrUndefined( filePath )) ?? null;
}

export function readJsonOrNullSync( filePath ) {
	return readJsonOrUndefinedSync( filePath ) ?? null;
}
