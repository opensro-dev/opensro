/*
===========================================================================

codeStamp.test.mjs - a reuse stamp follows the code that fills the cache

The stamp must change when any module in the entry's import closure (or a
Python helper it names) changes bytes, ignore prose in comments, and round
trip through the generated tree.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";

const temporaryRoot = await mkdtemp( path.join( os.tmpdir(), "sro-code-stamp-" ) );
process.env.SRO_GENERATED_ROOT = path.join( temporaryRoot, "generated" );
const { codeClosure, codeHash, stampIsCurrent, writeStamp } = await import( "../../build/shared/codeStamp.mjs" );

test.after( () => rm( temporaryRoot, { recursive: true, force: true } ) );

/*
================
writeModules

A small module graph: entry imports a helper (static and dynamic), the
helper names a Python encoder, and a comment names a file that does not
exist.
================
*/
async function writeModules( directory ) {
	await mkdir( path.join( directory, "lib" ), { recursive: true } );
	await writeFile(
		path.join( directory, "entry.mjs" ),
		[
			'// see "./missing.mjs" for history',
			'/* import "./also-missing.mjs" */',
			'import { helper } from "./lib/helper.mjs";',
			'const late = await import( "./lib/late.mjs" );',
			"export const value = helper + late.value;"
		].join( "\n" )
	);
	await writeFile(
		path.join( directory, "lib", "helper.mjs" ),
		'export const helper = new URL( "encode.py", import.meta.url ) && 1;\n'
	);
	await writeFile( path.join( directory, "lib", "late.mjs" ), "export const value = 2;\n" );
	await writeFile( path.join( directory, "lib", "encode.py" ), "print(1)\n" );
}

test("the closure follows relative imports and named Python helpers, not comments", async () => {
	const directory = path.join( temporaryRoot, "closure" );
	await writeModules( directory );
	const closure = await codeClosure( path.join( directory, "entry.mjs" ) );
	assert.deepEqual(
		closure.map( ( file ) => path.relative( directory, file ).split( path.sep ).join( "/" ) ).sort(),
		[ "entry.mjs", "lib/encode.py", "lib/helper.mjs", "lib/late.mjs" ]
	);
});

test("a byte change anywhere in the closure changes the hash", async () => {
	const directory = path.join( temporaryRoot, "hash" );
	await writeModules( directory );
	const entryUrl = pathToFileURL( path.join( directory, "entry.mjs" ) ).href;
	const before = await codeHash( entryUrl );
	assert.equal( await codeHash( entryUrl ), before );
	await writeFile( path.join( directory, "lib", "encode.py" ), "print(2)\n" );
	assert.notEqual( await codeHash( entryUrl ), before );
});

test("a stamp is current only for the hash it recorded", async () => {
	assert.equal( await stampIsCurrent( "example", "a" ), false );
	await writeStamp( "example", "a" );
	assert.equal( await stampIsCurrent( "example", "a" ), true );
	assert.equal( await stampIsCurrent( "example", "b" ), false );
});
