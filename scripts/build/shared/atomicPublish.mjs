/*
===========================================================================

atomicPublish.mjs - replace a public file whole, or not at all

A tool's output reaches the public tree through a sibling temp file and a
rename, so a reader never sees half a file. Windows holds a file a reader
has open, so the rename retries and finally falls back to an in-place
write. Every publish claims its target (publicationLedger.mjs).

===========================================================================
*/
import { readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import path from "node:path";

import { describeHeldFile } from "./pythonRun.mjs";
import { claimPublicFile } from "./publicationLedger.mjs";

const WINDOWS_RENAME_ATTEMPTS = 30;

/*
================
publishFileFromTemp

Publish a tool-produced temporary file with stable-mtime handling and the
repository's Windows destination-lock retry/fallback policy.
================
*/
export async function publishFileFromTemp( temporaryPath, targetPath, options = {} ) {
	const bytes = await readFile( temporaryPath );
	if ( options.skipIfUnchanged !== false ) {
		try {
			if ( (await readFile( targetPath )).equals( bytes ) ) {
				await rm( temporaryPath, { force: true } );
				claimPublicFile( targetPath );
				return false;
			}
		} catch {
			// Missing or unreadable target: fall through to the publish.
		}
	}

	for ( let attempt = 1; attempt <= WINDOWS_RENAME_ATTEMPTS; attempt += 1 ) {
		try {
			await rename( temporaryPath, targetPath );
			claimPublicFile( targetPath );
			return true;
		} catch ( error ) {
			if ( error.code !== "EPERM" ) {
				throw decorateHeldFileError( error, targetPath );
			}
			await new Promise( ( resolveDelay ) =>
				setTimeout( resolveDelay, Math.min( 2000, 200 * attempt ) + Math.random() * 200 )
			);
		}
	}

	const logLabel = options.logLabel ?? "resources";
	console.warn(
		`[${logLabel}] ${path.basename( targetPath )} stayed open in another process through every ` +
			`rename attempt; publishing with an in-place write instead.`
	);
	try {
		await writeFile( targetPath, bytes );
	} catch ( error ) {
		throw decorateHeldFileError( error, targetPath );
	}
	await rm( temporaryPath, { force: true } );
	claimPublicFile( targetPath );
	return true;
}

/*
================
publishBytesAtomically

Write bytes to a sibling temp file before publishing them. A target that
already holds the bytes is left alone (no temp file, mtime kept), so its
sidecars and the stat fingerprints stay fresh; skipIfUnchanged: false
forces the write.
================
*/
export async function publishBytesAtomically( targetPath, bytes, options = {} ) {
	if ( options.skipIfUnchanged !== false && (await holdsBytes( targetPath, bytes )) ) {
		claimPublicFile( targetPath );
		return false;
	}
	const temporaryPath = options.temporaryPath ?? `${targetPath}.tmp`;
	await writeFile( temporaryPath, bytes );
	return publishFileFromTemp( temporaryPath, targetPath, { ...options, skipIfUnchanged: false } );
}

/*
================
holdsBytes

Whether targetPath holds exactly bytes; a missing target does not.
================
*/
async function holdsBytes( targetPath, bytes ) {
	try {
		if ( (await stat( targetPath )).size !== Buffer.byteLength( bytes ) ) return false;
		return (await readFile( targetPath )).equals( Buffer.isBuffer( bytes ) ? bytes : Buffer.from( bytes ) );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return false;
		throw error;
	}
}

/*
================
decorateHeldFileError
================
*/
/**
 * @param {NodeJS.ErrnoException} error
 * @param {string} targetPath
 * @returns {Error}
 */
function decorateHeldFileError( error, targetPath ) {
	if ( error.code !== "EPERM" && error.code !== "EACCES" && error.code !== "EBUSY" ) {
		return error;
	}
	return new Error(
		`${error.code} publishing '${targetPath}': ${describeHeldFile( targetPath )}. Original: ${error.message}`,
		{ cause: error }
	);
}
