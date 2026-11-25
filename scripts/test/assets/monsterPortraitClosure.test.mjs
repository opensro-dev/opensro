/*
===========================================================================

monsterPortraitClosure.test.mjs - the portrait key follows the viewer's code

The cached stills are keyed by the renderer fingerprint. With a recorded
closure (the client files the viewer loaded on its last complete draw) the
key moves when one of those files changes and holds when any other client
file does: r204m's six UI files redrew all 1,102 stills while the key
hashed the whole engine tree. A partial draw can only add to a trusted
closure, never shrink it or start one.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import {
	monsterPortraitRendererFingerprint,
	nextRendererClosure,
	readRendererClosure,
	viewerClosure
} from "../../build/char/buildMonsterPortraitAssets.mjs";

const EFFECT_INDEX = JSON.stringify( { effects: [], nativeUnavailable: { effects: [] }, meshes: [] } );

/*
================
readAsset

Every viewer manifest as a fixed byte string; the effect index is empty.
================
*/
async function readAsset( publicPath ) {
	return Buffer.from( publicPath.endsWith( "programs/index.json" ) ? EFFECT_INDEX : `asset ${publicPath}` );
}

/*
================
clientTree

A fake client: the viewer's entry and page, one file the viewer loads and
one it never does.
================
*/
async function clientTree( t ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "portrait-closure-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const client = path.join( root, "apps", "client-next" );
	await mkdir( path.join( client, "src", "engine", "render" ), { recursive: true } );
	await mkdir( path.join( client, "src", "engine", "ui" ), { recursive: true } );
	await writeFile( path.join( client, "src", "bootstrap.ts" ), "boot" );
	await writeFile( path.join( client, "viewer.html" ), "<html>" );
	await writeFile( path.join( client, "src", "engine", "render", "draw.ts" ), "draw v1" );
	await writeFile( path.join( client, "src", "engine", "ui", "hud.ts" ), "hud v1" );
	await writeFile( path.join( root, "pnpm-lock.yaml" ), "lock v1" );
	return { root, client };
}

const MANIFEST = { models: {} };

test("with a closure, a client file the viewer never loaded keeps the key", async t => {
	const { client } = await clientTree( t );
	const closure = [ "src/engine/render/draw.ts" ];
	const key = () => monsterPortraitRendererFingerprint( MANIFEST, readAsset, { client, closure } );
	const before = await key();
	await writeFile( path.join( client, "src", "engine", "ui", "hud.ts" ), "hud v2" );
	assert.equal( await key(), before );
});

test("with a closure, a change to a loaded file or the lockfile moves the key", async t => {
	const { root, client } = await clientTree( t );
	const closure = [ "src/engine/render/draw.ts" ];
	const key = () => monsterPortraitRendererFingerprint( MANIFEST, readAsset, { client, closure } );
	const first = await key();
	await writeFile( path.join( client, "src", "engine", "render", "draw.ts" ), "draw v2" );
	const second = await key();
	assert.notEqual( second, first );
	await writeFile( path.join( root, "pnpm-lock.yaml" ), "lock v2" );
	assert.notEqual( await key(), second );
});

test("a deleted closure file moves the key instead of failing the build", async t => {
	const { client } = await clientTree( t );
	const closure = [ "src/engine/render/draw.ts" ];
	const key = () => monsterPortraitRendererFingerprint( MANIFEST, readAsset, { client, closure } );
	const before = await key();
	await rm( path.join( client, "src", "engine", "render", "draw.ts" ) );
	assert.notEqual( await key(), before );
});

test("without a closure, any engine file moves the key, as before", async t => {
	const { client } = await clientTree( t );
	const key = () => monsterPortraitRendererFingerprint( MANIFEST, readAsset, { client } );
	const before = await key();
	await writeFile( path.join( client, "src", "engine", "ui", "hud.ts" ), "hud v2" );
	assert.notEqual( await key(), before );
});

test("the viewer's closure keeps client files only, without dependencies", () => {
	const client = path.join( "C:", "repo", "apps", "client-next" );
	const files = viewerClosure( [
		path.join( client, "src", "engine", "render", "draw.ts" ),
		path.join( client, "src", "bootstrap.ts" ),
		path.join( client, "node_modules", ".vite", "deps", "chunk.js" ),
		path.join( "C:", "repo", "scripts", "lib", "probe.mjs" ),
		path.join( client, "src", "engine", "render", "draw.ts" )
	], client );
	assert.deepEqual( files, [ "src/bootstrap.ts", "src/engine/render/draw.ts" ] );
});

test("a complete draw replaces the closure; a partial one only adds to a trusted one", () => {
	assert.deepEqual( nextRendererClosure( [ "a.ts", "b.ts" ], [ "c.ts" ], true ), [ "c.ts" ] );
	assert.deepEqual( nextRendererClosure( [ "b.ts" ], [ "a.ts", "b.ts" ], false ), [ "a.ts", "b.ts" ] );
	assert.equal( nextRendererClosure( null, [ "a.ts" ], false ), null, "a partial draw never starts a closure" );
});

test("a recorded closure that is not ours, or escapes the client, reads as none", async t => {
	const root = await mkdtemp( path.join( os.tmpdir(), "portrait-closure-record-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const file = path.join( root, "closure.json" );
	assert.equal( await readRendererClosure( file ), null, "missing" );
	await writeFile( file, JSON.stringify( { format: "other", files: [ "a.ts" ] } ) );
	assert.equal( await readRendererClosure( file ), null, "foreign format" );
	await writeFile( file, JSON.stringify( { format: "sro-portrait-renderer-closure", files: [ "../x.ts" ] } ) );
	assert.equal( await readRendererClosure( file ), null, "escapes the client" );
	await writeFile( file, JSON.stringify( { format: "sro-portrait-renderer-closure", files: [ "src/a.ts" ] } ) );
	assert.deepEqual( await readRendererClosure( file ), [ "src/a.ts" ] );
});
