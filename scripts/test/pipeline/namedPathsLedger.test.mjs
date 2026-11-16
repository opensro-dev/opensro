/*
===========================================================================

namedPathsLedger.test.mjs - the named-paths ledger reads only new bytes

The ledger answers what a JSON file names from its content hash. These
tests pin that a remembered file is answered without reading it, that a
rewritten file is read again, that the answer survives a save, and that a
ledger written under another path pattern is ignored.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, utimes, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { namedPublicPaths, openNamedPathsLedger } from "../../build/shared/namedPathsLedger.mjs";

const OLD_SECONDS = 1_000_000_000;

/*
================
withLedgerFiles

A temp folder with one JSON file naming two public paths, and the ledger's
own state and hash cache paths.
================
*/
async function withLedgerFiles( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-named-paths-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const file = path.join( root, "index.json" );
	await writeFile( file, '{"a":"/assets/a.png","b":["/assets/b.glb","/assets/a.png"],"c":"/other"}' );
	await utimes( file, OLD_SECONDS, OLD_SECONDS );
	const options = {
		statePath: path.join( root, "ledger.json" ),
		hashCachePath: path.join( root, "hash-cache.json" )
	};
	return { root, file, options };
}

test("namedPublicPaths keeps each /assets path once, in the spelling written", () => {
	assert.deepEqual( namedPublicPaths( '["/assets/A.png","/assets/A.png","/x","/assets/b"]' ), [
		"/assets/A.png",
		"/assets/b"
	] );
});

test("a remembered file is answered by stat, and a save carries it to the next ledger", async ( t ) => {
	const { file, options } = await withLedgerFiles( t );
	const first = await openNamedPathsLedger( options );
	assert.deepEqual( await first.namedPaths( file ), [ "/assets/a.png", "/assets/b.glb" ] );
	await first.save();
	// Same size and time, other bytes: a remembered file is not read again.
	await writeFile( file, '{"a":"/assets/z.png","b":["/assets/y.glb","/assets/z.png"],"c":"/other"}' );
	await utimes( file, OLD_SECONDS, OLD_SECONDS );
	const second = await openNamedPathsLedger( options );
	assert.deepEqual( await second.namedPaths( file ), [ "/assets/a.png", "/assets/b.glb" ] );
});

test("a rewritten file is read again, and a missing one names nothing", async ( t ) => {
	const { root, file, options } = await withLedgerFiles( t );
	const ledger = await openNamedPathsLedger( options );
	await ledger.namedPaths( file );
	await writeFile( file, '{"a":"/assets/c.png"}' );
	assert.deepEqual( await ledger.namedPaths( file ), [ "/assets/c.png" ] );
	assert.equal( await ledger.namedPaths( path.join( root, "absent.json" ) ), undefined );
});

test("a ledger written under another path pattern is ignored", async ( t ) => {
	const { file, options } = await withLedgerFiles( t );
	const first = await openNamedPathsLedger( options );
	await first.namedPaths( file );
	await first.save();
	const saved = JSON.parse( await readFile( options.statePath, "utf8" ) );
	const [digest] = Object.keys( saved.named );
	await writeFile(
		options.statePath,
		JSON.stringify( { ...saved, pattern: "other", named: { [digest]: [ "/assets/stale" ] } } )
	);
	const second = await openNamedPathsLedger( options );
	assert.deepEqual( await second.namedPaths( file ), [ "/assets/a.png", "/assets/b.glb" ] );
});
