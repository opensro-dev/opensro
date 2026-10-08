/*
===========================================================================

codeStamp.test.mjs - a reuse stamp follows the code that fills the cache

The stamp must change when any file the entry can run changes bytes: a
module in its import closure, a Python file a module names however the path
is assembled, or a repository module that Python file imports. Prose in
comments is ignored, and a Python name the stamp cannot resolve is an error
rather than a silent gap.

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
const { codeClosure, codeHash, invalidateStamp, stampIsCurrent, writeStamp } = await import(
	"../../build/shared/codeStamp.mjs"
);

test.after( () => rm( temporaryRoot, { recursive: true, force: true } ) );

/*
================
writeRepository

A small repository: an entry module imports a helper (static and dynamic);
the helper names one Python file beside it and one by path.join pieces in
another root; that script imports a module from scripts/ through sys.path.
A comment names a file that does not exist.
================
*/
async function writeRepository( root ) {
	const files = {
		"scripts/build/entry.mjs": [
			'// see "./missing.mjs" and "gone.py" for history',
			'/* import "./also-missing.mjs" */',
			'import { helper } from "./lib/helper.mjs";',
			'const late = await import( "./lib/late.mjs" );',
			"export const value = helper + late.value;"
		].join( "\n" ),
		"scripts/build/lib/helper.mjs": [
			'export const encoder = new URL( "encode.py", import.meta.url );',
			'export const helper = [ "apps", "client-next", "tools", "project.py" ].join( "/" );'
		].join( "\n" ),
		"scripts/build/lib/late.mjs": "export const value = 2;\n",
		"scripts/build/lib/encode.py": "import os\nprint(1)\n",
		"apps/client-next/tools/project.py": "import sys\nfrom paths import ROOT\nprint(ROOT)\n",
		"scripts/paths.py": "ROOT = 1\n"
	};
	for ( const [name, text] of Object.entries( files ) ) {
		await mkdir( path.dirname( path.join( root, name ) ), { recursive: true } );
		await writeFile( path.join( root, name ), text );
	}
}

test("the closure covers imports, named Python files and the modules they import, not comments", async () => {
	const root = path.join( temporaryRoot, "closure" );
	await writeRepository( root );
	const closure = await codeClosure( path.join( root, "scripts/build/entry.mjs" ), root );
	assert.deepEqual(
		closure.map( ( file ) => path.relative( root, file ).split( path.sep ).join( "/" ) ).sort(),
		[
			"apps/client-next/tools/project.py",
			"scripts/build/entry.mjs",
			"scripts/build/lib/encode.py",
			"scripts/build/lib/helper.mjs",
			"scripts/build/lib/late.mjs",
			"scripts/paths.py"
		]
	);
});

test("a byte change anywhere in the closure changes the hash", async () => {
	const root = path.join( temporaryRoot, "hash" );
	await writeRepository( root );
	const entryUrl = pathToFileURL( path.join( root, "scripts/build/entry.mjs" ) ).href;
	const before = await codeHash( entryUrl, root );
	assert.equal( await codeHash( entryUrl, root ), before );
	await writeFile( path.join( root, "scripts/paths.py" ), "ROOT = 2\n" );
	assert.notEqual( await codeHash( entryUrl, root ), before );
});

test("a Python name the stamp cannot resolve fails instead of leaving a gap", async () => {
	const root = path.join( temporaryRoot, "unresolved" );
	await writeRepository( root );
	await writeFile( path.join( root, "scripts/build/lib/late.mjs" ), 'export const value = "renamed_away.py";\n' );
	await assert.rejects(
		codeClosure( path.join( root, "scripts/build/entry.mjs" ), root ),
		/renamed_away\.py.*matches 0/
	);
});

test("a stamp is current only for the hash it recorded", async () => {
	assert.equal( await stampIsCurrent( "example", "a" ), false );
	await writeStamp( "example", "a" );
	assert.equal( await stampIsCurrent( "example", "a" ), true );
	assert.equal( await stampIsCurrent( "example", "b" ), false );
});

test("a run under other code drops the old stamp before it writes", async () => {
	await writeStamp( "mixed", "revision-a" );
	// Revision B starts writing; if it fails, nothing may still vouch for A.
	await invalidateStamp( "mixed" );
	assert.equal( await stampIsCurrent( "mixed", "revision-a" ), false );
	await invalidateStamp( "mixed" );
});
