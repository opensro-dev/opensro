/*
===========================================================================

unusedExportGuard.test.mjs - an unused export in a shared module reruns nothing

The guard over the real pipeline, not a fixture: #4596 added one constant
and two helpers no step called to world/constants.mjs and world/io.mjs,
and r204n reran 17 variant steps (about 1000 s) and three asset lanes on
an otherwise unchanged tree, because the code keys took shared modules
whole. Every runnable build script (isMainScript) is keyed here as the
resource build and the release graph key it (codeStamp.mjs codeHash);
then every module their closures share gains an export nothing reads,
and no key may move. A new script, lane or shared module is covered the
day it lands.

The closures are copied once into a temporary tree, so the checkout is
never written.

===========================================================================
*/
import assert from "node:assert/strict";
import { appendFile, cp, mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { codeClosure, codeHash } from "../../build/shared/codeStamp.mjs";
import { listFiles } from "../../build/shared/fsUtils.mjs";

const repository = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../.." );
const BUILD_ROOT = path.join( repository, "scripts", "build" );
const RUNNABLE = /\bisMainScript\s*\(/;
const UNUSED_EXPORT =
	"\nexport const GUARD_UNUSED_EXPORT = 1;\nexport function guardUnusedHelper() {\n\treturn 1;\n}\n";

/*
================
runnableScripts

Every build script that runs as a program: the entries the resource build,
its lanes and the release graph key.
================
*/
async function runnableScripts() {
	const scripts = [];
	for ( const file of await listFiles( BUILD_ROOT, { extensions: [ ".mjs" ] } ) ) {
		if ( file.endsWith( path.join( "shared", "fsUtils.mjs" ) ) ) continue;
		if ( RUNNABLE.test( await readFile( file, "utf8" ) ) ) scripts.push( file );
	}
	return scripts.sort();
}

test( "an unused export in every shared module moves no build script's code key", { timeout: 300000 }, async () => {
	const entries = await runnableScripts();
	assert.ok( entries.length >= 10, `found only ${entries.length} runnable build scripts` );
	const temporary = await mkdtemp( path.join( os.tmpdir(), "sro-unused-export-" ) );
	try {
		const inside = file => path.join( temporary, path.relative( repository, file ) );
		const shared = new Set();
		for ( const entry of entries ) {
			for ( const file of await codeClosure( entry, repository ) ) {
				await cp( file, inside( file ) );
				if ( file.endsWith( ".mjs" ) ) shared.add( file );
			}
		}
		// An entry is reached whole by its own key; it counts as shared code
		// only for the other scripts, so entries are left as they are.
		for ( const entry of entries ) shared.delete( entry );
		const before = {};
		for ( const entry of entries ) before[entry] = await codeHash( inside( entry ), temporary );
		for ( const file of shared ) await appendFile( inside( file ), UNUSED_EXPORT );
		const moved = [];
		for ( const entry of entries ) {
			if ( await codeHash( inside( entry ), temporary ) !== before[entry] ) {
				moved.push( path.relative( repository, entry ) );
			}
		}
		assert.ok( shared.size > 0 );
		assert.deepEqual( moved, [], `an unused export moved these keys: ${moved.join( ", " )}` );
	} finally {
		await rm( temporary, { recursive: true, force: true } );
	}
} );
