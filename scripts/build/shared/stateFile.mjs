/*
===========================================================================

stateFile.mjs - replace a build-state file whole

The build's reuse caches and stamps (file hash cache, lane memo, minify
cache, code stamps, the resource-build fingerprint) live beside the shared
generated tree (generatedRoot.mjs buildStatePath), where two worktrees may
save the same file at once. Every one of them is written here, through a
temp name and a rename, so no reader ever sees a half-written or
interleaved file. Public files are a different contract (a claimed
target, an in-place fallback): they go through atomicPublish.mjs.

This module imports nothing from the build, so the lowest owners
(codeStamp.mjs) can use it.

===========================================================================
*/
import { mkdir, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";

// Windows refuses a rename onto a file another process is renaming onto
// or holding at that instant (EPERM); a cache save retries briefly.
const STATE_RENAME_ATTEMPTS = 20;
const STATE_RENAME_BACKOFF_MS = 25;

/*
================
writeStateJson

writeStateFile for a JSON value, minified.
================
*/
export async function writeStateJson( targetPath, value ) {
	return writeStateFile( targetPath, JSON.stringify( value ) );
}

/*
================
writeStateFile

Replace a build-state file (a reuse cache or a stamp, never a public asset:
those go through atomicPublish.mjs) whole. The bytes go to a temp name
unique to this writer, then rename over the target. The caches live beside
the shared generated tree (generatedRoot.mjs buildStatePath), so two
worktrees' builds or checks may save one at the same time; an in-place
write could interleave them into a file neither wrote. Every reader sees
one complete file. When another writer keeps the target busy through every
attempt, its complete file stands and this save is dropped: state only
ever saves work, so losing one save costs a rebuild or a re-hash. Returns
whether this save was published.
================
*/
export async function writeStateFile( targetPath, bytes ) {
	await mkdir( path.dirname( targetPath ), { recursive: true } );
	const temporaryPath = `${targetPath}.${process.pid}.${Math.random().toString( 16 ).slice( 2 )}.tmp`;
	try {
		await writeFile( temporaryPath, bytes );
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
