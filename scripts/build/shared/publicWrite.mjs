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

===========================================================================
*/
import { mkdirSync, writeFileSync } from "node:fs";
import { copyFile, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { claimPublicFile } from "./publicationLedger.mjs";

/*
================
copyIntoPublicTree

Copies a prepared file (a converted image, a sound, a launcher bitmap) to
its place in the public tree.
================
*/
export async function copyIntoPublicTree( source, target ) {
	await mkdir( path.dirname( target ), { recursive: true } );
	await copyFile( source, target );
	claimPublicFile( target );
}

/*
================
writeIntoPublicTree

Writes bytes a build step produced (a model, a VAT payload) to the public tree.
================
*/
export async function writeIntoPublicTree( target, bytes ) {
	await mkdir( path.dirname( target ), { recursive: true } );
	await writeFile( target, bytes );
	claimPublicFile( target );
}

/*
================
writeIntoPublicTreeSync

The same for the synchronous model compilers.
================
*/
export function writeIntoPublicTreeSync( target, bytes ) {
	mkdirSync( path.dirname( target ), { recursive: true } );
	writeFileSync( target, bytes );
	claimPublicFile( target );
}
