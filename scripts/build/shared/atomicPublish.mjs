import { readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";

import { describeHeldFile } from "./pythonRun.mjs";
import { claimPublicFile } from "./publicationLedger.mjs";

const WINDOWS_RENAME_ATTEMPTS = 30;

/**
 * Publish a tool-produced temporary file with stable-mtime handling and the
 * repository's Windows destination-lock retry/fallback policy.
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

/** Write bytes to a sibling temp file before publishing them. */
export async function publishBytesAtomically( targetPath, bytes, options = {} ) {
	const temporaryPath = options.temporaryPath ?? `${targetPath}.tmp`;
	await writeFile( temporaryPath, bytes );
	return publishFileFromTemp( temporaryPath, targetPath, {
		...options,
		skipIfUnchanged: options.skipIfUnchanged ?? false
	} );
}

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
