/*
===========================================================================

cachedFiles.mjs - catalogue files written once, then linked

The catalogue writes about 10,500 tooltip files, nearly all the same
bytes from one release to the next, into a tree that is removed and
rebuilt every time. The release then tars that tree, and on the release
PC the first read of a freshly written file cost 5.8 ms against 0.12 ms
for a file the scanner had already seen: about 60 s of every catalogue
step for the tooltips alone (opensro #4337).

So each file is written once into a content cache, named by the sha256
of its bytes, and the target is a hard link to that entry: an unchanged
file is the same file as last release. Where a link is refused (another
volume, the file system's link limit) the bytes are written to the target
directly. webp_pool.py places its encodes the same way, in Python.

An entry is written through a temporary name and a rename, and never
written in place, so a killed build leaves no partial entry and a target
linked to an entry keeps its bytes.

===========================================================================
*/
import { createHash } from "node:crypto";
import { existsSync, linkSync, mkdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";

// The errors that mean "no link here" rather than a fault: another
// volume, a file system without hard links, NTFS's 1,024 links per file.
const LINK_REFUSED = new Set( [ "EXDEV", "EPERM", "ENOTSUP", "EMLINK" ] );
// Entries fan out by the first two hex digits of their name.
const FAN_OUT_DIGITS = 2;

/*
================
cachedFileWriter

A writeFileSync(target, data) for one cache directory: the entry for the
data's bytes, written when absent, then linked at target.
================
*/
export function cachedFileWriter( cacheDir, link = linkSync ) {
	return function writeCached( target, data ) {
		const bytes = Buffer.from( data );
		const key = createHash( "sha256" ).update( bytes ).digest( "hex" );
		const entry = path.join( cacheDir, key.slice( 0, FAN_OUT_DIGITS ), key );
		if ( !existsSync( entry ) ) {
			mkdirSync( path.dirname( entry ), { recursive: true } );
			const temporary = `${entry}.${process.pid}.tmp`;
			writeFileSync( temporary, bytes );
			renameSync( temporary, entry );
		}
		// Removed first: writing onto a target that is a link to an older
		// entry would write through into the cache.
		rmSync( target, { force: true } );
		try {
			link( entry, target );
		} catch ( error ) {
			if ( !LINK_REFUSED.has( error.code ) ) throw error;
			writeFileSync( target, bytes );
		}
	};
}
