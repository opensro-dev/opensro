/*
===========================================================================

cachedFiles.test.mjs - catalogue files are cache entries, linked

cachedFileWriter must give every target the written bytes, make targets
with the same bytes one cache entry, never write through an existing
link into the cache, and fall back to plain bytes where a link is
refused.

===========================================================================
*/
import assert from "node:assert/strict";
import { linkSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { cachedFileWriter } from "../../build/catalogue/cachedFiles.mjs";

/*
================
scratch

A temp directory removed after the test.
================
*/
function scratch( t ) {
	const root = mkdtempSync( path.join( tmpdir(), "sro-cached-files-" ) );
	t.after( () => rmSync( root, { recursive: true, force: true } ) );
	return root;
}

/*
================
sameFile
================
*/
function sameFile( a, b ) {
	const left = statSync( a, { bigint: true } ), right = statSync( b, { bigint: true } );
	return left.ino === right.ino && left.dev === right.dev;
}

test("targets with the same bytes are one cache entry", t => {
	const root = scratch( t ), cache = path.join( root, "cache" );
	const write = cachedFileWriter( cache );
	write( path.join( root, "a.json" ), '{"id":1}\n' );
	write( path.join( root, "b.json" ), '{"id":1}\n' );
	write( path.join( root, "c.json" ), '{"id":2}\n' );
	assert.equal( readFileSync( path.join( root, "a.json" ), "utf8" ), '{"id":1}\n' );
	assert.equal( readFileSync( path.join( root, "c.json" ), "utf8" ), '{"id":2}\n' );
	assert.ok( sameFile( path.join( root, "a.json" ), path.join( root, "b.json" ) ) );
	assert.ok( !sameFile( path.join( root, "a.json" ), path.join( root, "c.json" ) ) );
	const entries = readdirSync( cache, { recursive: true } ).filter( name =>
		statSync( path.join( cache, name ) ).isFile()
	);
	assert.equal( entries.length, 2, entries.join( "\n" ) );
});

test("rewriting a linked target leaves the cache entry's bytes", t => {
	const root = scratch( t ), cache = path.join( root, "cache" ), target = path.join( root, "a.json" );
	const write = cachedFileWriter( cache );
	write( target, "old\n" );
	const other = path.join( root, "other.json" );
	write( other, "old\n" );
	write( target, "new\n" );
	assert.equal( readFileSync( target, "utf8" ), "new\n" );
	assert.equal( readFileSync( other, "utf8" ), "old\n" );
});

test("a refused link writes the bytes to the target", t => {
	const root = scratch( t ), target = path.join( root, "a.json" );
	const refuse = () => {
		throw Object.assign( Error( "cross-device link" ), { code: "EXDEV" } );
	};
	cachedFileWriter( path.join( root, "cache" ), refuse )( target, "bytes\n" );
	assert.equal( readFileSync( target, "utf8" ), "bytes\n" );
	assert.equal( statSync( target ).nlink, 1 );
});

test("a link fault that is not a refusal is not hidden", t => {
	const root = scratch( t );
	const broken = () => {
		throw Object.assign( Error( "no such directory" ), { code: "ENOENT" } );
	};
	assert.throws(
		() => cachedFileWriter( path.join( root, "cache" ), broken )( path.join( root, "a.json" ), "x" ),
		/no such directory/
	);
});

test("an existing plain file at the target is replaced", t => {
	const root = scratch( t ), target = path.join( root, "a.json" ), keep = path.join( root, "keep.json" );
	writeFileSync( keep, "kept\n" );
	linkSync( keep, target );
	cachedFileWriter( path.join( root, "cache" ) )( target, "fresh\n" );
	assert.equal( readFileSync( keep, "utf8" ), "kept\n" );
	assert.equal( readFileSync( target, "utf8" ), "fresh\n" );
});
