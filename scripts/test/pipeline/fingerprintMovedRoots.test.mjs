/*
===========================================================================

fingerprintMovedRoots.test.mjs - a fingerprint miss names what moved

movedRoots compares a recorded fingerprint with the current one: the build
settings, each changed, new or gone root, and for a directory root the
folders that moved. Records are written by hand here (no tree is walked),
including one from before the folder and knob fields existed, which must
still name the root that moved.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { describeMovedRoots, folderHashes, movedRoots } from "../../build/shared/resourceBuildFingerprint.mjs";

/*
================
record

A fingerprint record with the given knobs hash and roots.
================
*/
function record( knobs, roots ) {
	return { hash: "h", fileCount: 0, knobs, roots };
}

test("an unchanged fingerprint names nothing", () => {
	const roots = [ { label: "public", hash: "a", folders: { "public/assets/images": "1" } } ];
	assert.deepEqual( movedRoots( record( "k", roots ), record( "k", roots ) ), [] );
	assert.equal( describeMovedRoots( [] ), "nothing recorded moved" );
});

test("a changed directory root names the folders that moved, not the whole root", () => {
	const before = record( "k", [
		{ label: "public", hash: "a", folders: { "public/assets/images": "1", "public/assets/world": "2" } },
		{ label: "code:scripts/x.mjs", hash: "c" }
	] );
	const after = record( "k", [
		{
			label: "public",
			hash: "b",
			folders: { "public/assets/images": "1", "public/assets/world": "3", "public/assets/new": "4" }
		},
		{ label: "code:scripts/x.mjs", hash: "c" }
	] );
	assert.deepEqual( movedRoots( before, after ), [ "public/assets/new", "public/assets/world" ] );
});

test("settings, new roots and gone roots are named", () => {
	const before = record( "k1", [ { label: "extracted", hash: "a" }, { label: "pack-baseline", hash: "p" } ] );
	const after = record( "k2", [ { label: "extracted", hash: "a" }, { label: "server-exports", hash: "s" } ] );
	assert.deepEqual( movedRoots( before, after ), [
		"build settings",
		"server-exports (new)",
		"pack-baseline (gone)"
	] );
});

test("a record from before the folder and knob fields still names the root", () => {
	const before = { hash: "h", fileCount: 0, roots: [ { label: "public", hash: "a" } ] };
	const after = record( "k", [ { label: "public", hash: "b", folders: { "public/assets": "1" } } ] );
	assert.deepEqual( movedRoots( before, after ), [ "public" ] );
});

test("a long list is cut to the first twelve and the rest counted", () => {
	const moved = Array.from( { length: 15 }, ( _, index ) => `code:f${index}.mjs` );
	const phrase = describeMovedRoots( moved );
	assert.ok( phrase.startsWith( "code:f0.mjs, code:f1.mjs" ) );
	assert.ok( phrase.endsWith( "code:f11.mjs and 3 more" ) );
});

test("folder hashes two levels down split a root by its second folder", () => {
	const lines = [
		"public/assets/images/a.png|1|1",
		"public/assets/world/r.json|1|1",
		"public/index.html|1|1"
	];
	assert.deepEqual( Object.keys( folderHashes( "public", lines, 2 ) ).sort(), [
		"public/assets/images",
		"public/assets/world",
		"public/index.html"
	] );
	// The default stays one level, as the lane memo's data roots record it.
	assert.deepEqual( Object.keys( folderHashes( "public", lines ) ).sort(), [ "public/assets", "public/index.html" ] );
	const changed = [ ...lines.slice( 0, 1 ), "public/assets/world/r.json|2|1", lines[2] ];
	const before = folderHashes( "public", lines, 2 ), after = folderHashes( "public", changed, 2 );
	assert.equal( after["public/assets/images"], before["public/assets/images"] );
	assert.notEqual( after["public/assets/world"], before["public/assets/world"] );
});
