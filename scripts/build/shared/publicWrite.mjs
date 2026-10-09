/*
===========================================================================

publicWrite.mjs - every write into the public tree, claimed

client-public/assets is what players download, so every file in it must be
something a current build step produced. These helpers are the one way a
build step puts a file there: each writes (or copies) the file and claims
it for the open publication (publicationLedger.mjs). A step that keeps an
existing output without rewriting it claims that output with
claimPublicFile. A file a manifest names that no step claimed is a
local-only leftover the ledger refuses, because a fresh clone would not
have it.

Atomic replacement lives in atomicPublish.mjs and JSON in jsonOut.mjs; both
claim too.

A target that already holds the exact bytes is not rewritten: an untouched
output keeps its mtime, so the stat fingerprints and sidecar freshness
checks after it stay hits, and a warm build stops rewriting thousands of
identical images and models.

===========================================================================
*/
import { mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { copyFile, mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { claimPublicFile } from "./publicationLedger.mjs";

/*
================
asBuffer
================
*/
function asBuffer( bytes ) {
	return Buffer.from( bytes.buffer, bytes.byteOffset, bytes.byteLength );
}

/*
================
sizeOf

The file's size, or -1 when it does not exist.
================
*/
async function sizeOf( file ) {
	try {
		return (await stat( file )).size;
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return -1;
		throw error;
	}
}

/*
================
sizeOfSync
================
*/
function sizeOfSync( file ) {
	try {
		return statSync( file ).size;
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return -1;
		throw error;
	}
}

/*
================
holdsBytes

Whether target already holds exactly bytes. The size check rejects most
changes without reading the file.
================
*/
async function holdsBytes( target, bytes ) {
	if ( (await sizeOf( target )) !== bytes.byteLength ) return false;
	return (await readFile( target )).equals( asBuffer( bytes ) );
}

/*
================
copyIntoPublicTree

Copies a prepared file (a converted image, a sound, a launcher bitmap) to
its place in the public tree.
================
*/
export async function copyIntoPublicTree( source, target ) {
	const size = await sizeOf( source );
	if ( size < 0 || size !== (await sizeOf( target )) || !(await holdsBytes( target, await readFile( source ) )) ) {
		await mkdir( path.dirname( target ), { recursive: true } );
		await copyFile( source, target );
	}
	claimPublicFile( target );
}

/*
================
writeIntoPublicTree

Writes bytes a build step produced (a model, a VAT payload) to the public tree.
================
*/
export async function writeIntoPublicTree( target, bytes ) {
	if ( !(await holdsBytes( target, bytes )) ) {
		await mkdir( path.dirname( target ), { recursive: true } );
		await writeFile( target, bytes );
	}
	claimPublicFile( target );
}

/*
================
writeIntoPublicTreeSync

The same for the synchronous model compilers.
================
*/
export function writeIntoPublicTreeSync( target, bytes ) {
	const same = sizeOfSync( target ) === bytes.byteLength && readFileSync( target ).equals( asBuffer( bytes ) );
	if ( !same ) {
		mkdirSync( path.dirname( target ), { recursive: true } );
		writeFileSync( target, bytes );
	}
	claimPublicFile( target );
}
