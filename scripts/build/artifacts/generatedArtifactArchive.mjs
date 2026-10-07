/*
===========================================================================

generatedArtifactArchive.mjs - preserve superseded publication files

Stage an exclusive archive copy before removing a live file. Worktrees and
shared generated assets may reside on different volumes.

===========================================================================
*/
import * as filesystem from "node:fs/promises";
import { constants } from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";
import { rebuildRoot } from "../world/paths.mjs";

const DEFAULT_ARCHIVE_ROOT = path.join( rebuildRoot, "temp", "archives", "generated-artifacts" );
const VERIFY_CHUNK_BYTES = 1024 * 1024;

/*
================
fileDigest

Bound verification memory even when a retired pack is several gigabytes.
================
*/
async function fileDigest( filename, files ) {
	const handle = await files.open( filename, "r" );
	try {
		const hash = createHash( "sha256" );
		const buffer = Buffer.allocUnsafe( VERIFY_CHUNK_BYTES );
		for ( ;; ) {
			const { bytesRead } = await handle.read( buffer, 0, buffer.length, null );
			if ( !bytesRead ) return hash.digest( "hex" );
			hash.update( buffer.subarray( 0, bytesRead ) );
		}
	} finally {
		await handle.close();
	}
}

/*
================
sameFile
================
*/
function sameFile( before, after ) {
	return before.dev === after.dev && before.ino === after.ino && before.size === after.size &&
		before.mtimeMs === after.mtimeMs;
}

/*
================
archiveGeneratedArtifact

Exclusive links avoid rename's overwrite behavior. Across devices, verify
an exclusive staged copy first. Publish provenance before deleting the source;
any failed operation leaves the live source available for a later retry.
================
*/
export async function archiveGeneratedArtifact( sourcePath, options = {} ) {
	const files = { ...filesystem, ...options.files };
	const source = path.resolve( sourcePath );
	const scopeRoot = path.resolve( options.scopeRoot ?? rebuildRoot );
	const archiveRoot = path.resolve( options.archiveRoot ?? DEFAULT_ARCHIVE_ROOT );
	const relative = path.relative( scopeRoot, source );
	if ( !relative || relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `generated artifact archive source escapes ${scopeRoot}: ${source}` );
	}
	const sourceStat = await files.stat( source ).catch( error => {
		if ( error.code === "ENOENT" ) return undefined;
		throw error;
	} );
	if ( !sourceStat ) return undefined;
	if ( !sourceStat.isFile() ) throw new Error( `generated artifact archive source is not a file: ${source}` );
	const now = new Date();
	const day = [
		String( now.getFullYear() ).padStart( 4, "0" ),
		String( now.getMonth() + 1 ).padStart( 2, "0" ),
		String( now.getDate() ).padStart( 2, "0" )
	].join( "-" );
	const reason = normalizeReason( options.reason ?? "superseded" );
	const baseDestination = path.join( archiveRoot, day, reason, relative );
	await files.mkdir( path.dirname( baseDestination ), { recursive: true } );
	const staging = await files.mkdtemp( path.join( path.dirname( baseDestination ), ".archive-" ) );
	const staged = path.join( staging, "payload" );
	try {
		try {
			await files.link( source, staged );
		} catch ( error ) {
			if ( error.code !== "EXDEV" ) throw error;
			await files.copyFile( source, staged, constants.COPYFILE_EXCL );
			if ( await fileDigest( source, files ) !== await fileDigest( staged, files ) ) {
				throw new Error( `generated artifact archive copy verification failed: ${source}` );
			}
		}
		if ( !sameFile( sourceStat, await files.stat( source ) ) ) {
			throw new Error( `generated artifact archive source changed: ${source}` );
		}
		for ( let collision = 0;; collision++ ) {
			const destination = collision ? `${baseDestination}.archive-${collision}` : baseDestination;
			// An existing provenance record reserves its name even without a payload.
			if ( await pathExists( `${destination}.archive.json`, files ) ) continue;
			try {
				await files.link( staged, destination );
			} catch ( error ) {
				if ( error.code === "EEXIST" ) continue;
				throw error;
			}
			const record = {
				format: "sro-generated-artifact-archive-record",
				version: 1,
				archivedAt: now.toISOString(),
				reason,
				originalPath: relative.replaceAll( "\\", "/" ),
				archivedPath: path.relative( archiveRoot, destination ).replaceAll( "\\", "/" ),
				bytes: sourceStat.size
			};
			try {
				await files.writeFile( `${destination}.archive.json`, `${JSON.stringify( record, null, 2 )}\n`, {
					flag: "wx"
				} );
			} catch ( error ) {
				await files.unlink( destination );
				if ( error.code === "EEXIST" ) continue;
				throw error;
			}
			if ( !sameFile( sourceStat, await files.stat( source ) ) ) {
				throw new Error( `generated artifact archive source changed: ${source}` );
			}
			await files.unlink( source );
			return { destination, record };
		}
	} finally {
		await files.unlink( staged ).catch( error => {
			if ( error.code !== "ENOENT" ) throw error;
		} );
		await files.rmdir( staging );
	}
}

/*
================
normalizeReason
================
*/
function normalizeReason( value ) {
	const normalized = String( value ).trim().toLowerCase().replace( /[^a-z0-9]+/gu, "-" ).replace( /^-+|-+$/gu, "" );
	if ( !normalized ) throw new Error( "generated artifact archive reason is empty" );
	return normalized;
}

/*
================
pathExists
================
*/
async function pathExists( filename, files ) {
	return files.stat( filename ).then( () => true, error => {
		if ( error.code === "ENOENT" ) return false;
		throw error;
	} );
}
