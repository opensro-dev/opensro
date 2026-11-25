/*
===========================================================================

renderPortraits.test.mjs - the catalogue's portrait stills are kept by key

A still depends only on the served viewer and its pack index
(renderPortraits.mjs portraitKey): a pass under the same key copies the
kept stills and launches no browser; a new key draws again and drops the
old stills; a failed draw is never kept (opensro #4337).

===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { portraitKey, renderPortraits } from "../../build/catalogue/renderPortraits.mjs";

const BASE = "http://127.0.0.1:1/";

/*
================
served

A fake viewer: the bytes it serves per path.
================
*/
function served( files ) {
	return async ( _base, name ) => Buffer.from( files[name] );
}

/*
================
fakeLaunch

A browser whose page draws monster <id> as the PNG bytes "png <id>", and
fails the ids in failing. Counts its launches.
================
*/
function fakeLaunch( failing = new Set() ) {
	const launch = async () => {
		launch.count++;
		let loaded = 0;
		const page = {
			goto: async url => {
				loaded = Number( new URL( url ).searchParams.get( "monster" ) );
			},
			evaluate: async ( _fn, { id } ) => {
				const which = id || loaded;
				if ( failing.has( which ) ) return { reason: "render" };
				return { png: "data:image/png;base64," + Buffer.from( "png " + which ).toString( "base64" ) };
			}
		};
		return { browser: { newPage: async () => page, close: async () => {} }, page };
	};
	launch.count = 0;
	return launch;
}

/*
================
scratch
================
*/
function scratch( t ) {
	const root = mkdtempSync( path.join( tmpdir(), "portraits-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	return root;
}

const MONSTERS = [ { id: 7, grade: "normal" }, { id: 3, grade: "unique" } ];
const SITE = { "viewer.html": "<script src=main-1.js>", "assets/packs/manifest.json": '{"packs":1}' };

test("the key moves with the viewer page, the pack index and the size, and only with them", async () => {
	const key = await portraitKey( { viewerBase: BASE, size: 384, fetchBytes: served( SITE ) } );
	assert.equal( await portraitKey( { viewerBase: BASE, size: 384, fetchBytes: served( SITE ) } ), key );
	for (
		const changed of [
			{ size: 384, files: { ...SITE, "viewer.html": "<script src=main-2.js>" } },
			{ size: 384, files: { ...SITE, "assets/packs/manifest.json": '{"packs":2}' } },
			{ size: 256, files: SITE }
		]
	) {
		assert.notEqual(
			await portraitKey( { viewerBase: BASE, size: changed.size, fetchBytes: served( changed.files ) } ),
			key
		);
	}
});

test("a pass under a kept key copies the stills and launches no browser", async t => {
	const root = scratch( t ), cacheDir = path.join( root, "cache" ), launch = fakeLaunch();
	const pass = workDir =>
		renderPortraits( {
			viewerBase: BASE,
			monsters: MONSTERS,
			workDir: path.join( root, workDir ),
			cacheDir,
			pages: 1,
			log: () => {},
			fetchBytes: served( SITE ),
			launch
		} );
	const first = await pass( "one" );
	assert.equal( launch.count, 1 );
	assert.deepEqual( [ ...first.drawn.keys() ].sort(), [ 3, 7 ] );
	const second = await pass( "two" );
	assert.equal( launch.count, 1 );
	assert.equal( readFileSync( second.drawn.get( 7 ), "utf8" ), "png 7" );
	assert.equal( path.dirname( second.drawn.get( 3 ) ), path.join( root, "two" ) );
});

test("a new key draws again and drops the old stills; a failed draw is not kept", async t => {
	const root = scratch( t ), cacheDir = path.join( root, "cache" );
	const pass = ( workDir, files, launch ) =>
		renderPortraits( {
			viewerBase: BASE,
			monsters: MONSTERS,
			workDir: path.join( root, workDir ),
			cacheDir,
			pages: 1,
			log: () => {},
			fetchBytes: served( files ),
			launch
		} );
	const failing = fakeLaunch( new Set( [ 7 ] ) );
	const first = await pass( "one", SITE, failing );
	assert.deepEqual( first.failed.map( f => f.id ), [ 7 ] );
	const retry = fakeLaunch();
	const second = await pass( "two", SITE, retry );
	assert.equal( retry.count, 1 );
	assert.deepEqual( [ ...second.drawn.keys() ].sort(), [ 3, 7 ] );
	const moved = { ...SITE, "assets/packs/manifest.json": '{"packs":2}' }, redraw = fakeLaunch();
	await pass( "three", moved, redraw );
	assert.equal( redraw.count, 1 );
	const keys = readdirSync( cacheDir );
	assert.equal( keys.length, 1 );
	assert.equal( keys[0], await portraitKey( { viewerBase: BASE, size: 384, fetchBytes: served( moved ) } ) );
	assert.ok( existsSync( path.join( cacheDir, keys[0], "7.png" ) ) );
});
