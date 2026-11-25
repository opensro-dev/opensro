/*
===========================================================================

publishedPortraits.test.mjs - the catalogue's stills come from the published family

The catalogue no longer draws monsters (publishedPortraits.mjs): it reads
the asset build's content-addressed family from the served release. A
listed monster gets its still; an unlisted one, or a still whose bytes do
not match its name, is a named failure; a still shared by monsters is
fetched once; a manifest of another format is refused.

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { PORTRAIT_PREFIX, publishedPortraits } from "../../build/catalogue/publishedPortraits.mjs";

const BASE = "http://127.0.0.1:1/";

/*
================
still

A published still: its bytes and the path the family names it by.
================
*/
function still( text ) {
	const bytes = Buffer.from( text );
	return {
		bytes,
		published: "/" + PORTRAIT_PREFIX + createHash( "sha256" ).update( bytes ).digest( "hex" ) + ".png"
	};
}

/*
================
site

A served release: the manifest's rows and the files, counting fetches.
================
*/
function site( rows, files, format = "sro-hunting-portraits" ) {
	const served = new Map( files.map( ( { bytes, published } ) => [ published.slice( 1 ), bytes ] ) );
	served.set( PORTRAIT_PREFIX + "manifest.json", Buffer.from( JSON.stringify( { format, version: 1, rows } ) ) );
	const fetchBytes = async ( _base, name ) => {
		fetchBytes.count[name] = (fetchBytes.count[name] ?? 0) + 1;
		if ( !served.has( name ) ) throw Error( "404 " + name );
		return served.get( name );
	};
	fetchBytes.count = {};
	return fetchBytes;
}

/*
================
scratch
================
*/
function scratch( t ) {
	const root = mkdtempSync( path.join( tmpdir(), "published-portraits-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	return root;
}

test("each listed monster gets its published still; a shared still is fetched once", async t => {
	const tiger = still( "png tiger" ), wolf = still( "png wolf" );
	const fetchBytes = site( [ [ 1, tiger.published ], [ 2, wolf.published ], [ 3, wolf.published ] ], [
		tiger,
		wolf
	] );
	const workDir = scratch( t );
	const { drawn, failed } = await publishedPortraits( {
		viewerBase: BASE,
		monsters: [ { id: 1 }, { id: 2 }, { id: 3 } ],
		workDir,
		fetchBytes
	} );
	assert.deepEqual( failed, [] );
	assert.deepEqual( [ ...drawn.keys() ], [ 1, 2, 3 ] );
	assert.equal( readFileSync( drawn.get( 3 ), "utf8" ), "png wolf" );
	assert.equal( fetchBytes.count[wolf.published.slice( 1 )], 1 );
});

test("an unlisted monster and a still that does not match its name are named failures", async t => {
	const good = still( "png good" ), bad = still( "png bad" );
	const tampered = { bytes: Buffer.from( "png other" ), published: bad.published };
	const fetchBytes = site( [ [ 1, good.published ], [ 2, bad.published ] ], [ good, tampered ] );
	const { drawn, failed } = await publishedPortraits( {
		viewerBase: BASE,
		monsters: [ { id: 1 }, { id: 2 }, { id: 9 } ],
		workDir: scratch( t ),
		fetchBytes
	} );
	assert.deepEqual( [ ...drawn.keys() ], [ 1 ] );
	assert.deepEqual( failed, [ { id: 2, reason: "mismatch" }, { id: 9, reason: "unpublished" } ] );
});

test("a manifest of another format is refused", async t => {
	await assert.rejects(
		publishedPortraits( {
			viewerBase: BASE,
			monsters: [ { id: 1 } ],
			workDir: scratch( t ),
			fetchBytes: site( [], [], "something-else" )
		} ),
		/manifest is something-else/
	);
});
