// Minified, write-if-changed JSON writers. Unchanged content keeps its file mtime, which is
// what lets the minify-, sidecar- and pack-caches skip work across rebuilds: an uncondition-
// ally rewritten JSON invalidates its gzip sidecar and its containing asset
// pack even when the bytes are identical. Minified output matches the form the JSON
// optimizer would rewrite the file to anyway.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { mkdir, readFile, writeFile } from "node:fs/promises";
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
