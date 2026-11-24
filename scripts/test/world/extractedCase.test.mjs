/*
===========================================================================

extractedCase.test.mjs - opening data-named files on a case-sensitive disk

The extracted trees keep the folded form of the native names while the
game data keeps its authored case (#3962): dungeoninfo.txt names
Dungeon\wchina\Dunhwang_Cv.dof, the disk holds
dungeon/wchina/dunhwang_cv.dof. These tests pin the on-disk resolution
the build's shared openers apply, and the dungeon manifest built from
a table whose names disagree with the tree in case alone. Synthetic
trees only; no game data.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { buildDungeonResourceManifest } from "../../build/world/assets/buildDungeonResources.mjs";
import { notedExists, notedReadFile, resolveOnDiskPath } from "../../build/world/io.mjs";

/*
================
makeExtractedTree

A temp extracted root whose dungeon tree is spelled the way the
extractor writes it (folded), however the table names it.
================
*/
async function makeExtractedTree( t ) {
	const root = await mkdtemp( path.join( tmpdir(), "sro-extracted-case-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	await mkdir( path.join( root, "Data_extracted", "dungeon", "wchina" ), { recursive: true } );
	await mkdir( path.join( root, "Data_extracted", "prim" ), { recursive: true } );
	return root;
}

test("a data-named path resolves to the on-disk spelling", async ( t ) => {
	const root = await makeExtractedTree( t );
	const onDisk = path.join( root, "Data_extracted", "dungeon", "wchina", "dunhwang_cv.dof" );
	await writeFile( onDisk, "dof" );

	const resolved = await resolveOnDiskPath(
		path.join( root, "Data_extracted", "Dungeon", "wchina", "Dunhwang_Cv.dof" )
	);

	assert.equal( resolved, onDisk );
});

test("an exactly spelled path resolves to itself", async ( t ) => {
	const root = await makeExtractedTree( t );
	const onDisk = path.join( root, "Data_extracted", "dungeon", "wchina", "dunhwang_cv.dof" );
	await writeFile( onDisk, "dof" );

	assert.equal( await resolveOnDiskPath( onDisk ), onDisk );
});

test("a missing file keeps the caller's spelling", async ( t ) => {
	const root = await makeExtractedTree( t );
	const asked = path.join( root, "Data_extracted", "Dungeon", "wchina", "absent.dof" );

	assert.equal( await resolveOnDiskPath( asked ), asked );
});

test("case folding is ASCII only, like the native pk2 lookup", async ( t ) => {
	const root = await makeExtractedTree( t );
	// U+00C0 in the stored name, U+00E0 in the query: foldAssetCase folds
	// neither, so the names stay distinct (a Unicode fold would rewrite
	// CP949 bytes; see assetPaths.mjs).
	const onDisk = path.join( root, "Data_extracted", "prim", "Àname.bsr" );
	await writeFile( onDisk, "bsr" );
	const asked = path.join( root, "Data_extracted", "prim", "àname.bsr" );

	assert.equal( await resolveOnDiskPath( asked ), asked );
	assert.equal( await resolveOnDiskPath( onDisk ), onDisk );
});

test("the noted openers read through the case difference", async ( t ) => {
	const root = await makeExtractedTree( t );
	const onDisk = path.join( root, "Data_extracted", "dungeon", "wchina", "dunhwang_cv.dof" );
	await writeFile( onDisk, "dof-bytes" );
	const asked = path.join( root, "Data_extracted", "Dungeon", "wchina", "Dunhwang_Cv.dof" );

	assert.equal( await notedExists( asked ), true );
	assert.equal( (await notedReadFile( asked )).toString( "utf8" ), "dof-bytes" );
	assert.equal( await notedExists( path.join( root, "Data_extracted", "Dungeon", "absent.dof" ) ), false );
});

/*
================
minimalDof

The smallest JMXVDOF 0101 the manifest reader admits: the general
header, the bounding-box gap and a block section of zero blocks, which
must end exactly at the grid offset.
================
*/
function minimalDof() {
	const bytes = Buffer.alloc( 118 );
	bytes.write( "JMXVDOF 0101", 0, "ascii" );
	bytes.writeUInt32LE( 114, 12 ); // offsets[0]: block section
	bytes.writeUInt32LE( 118, 12 + 2 * 4 ); // offsets[2]: grid (end of blocks)
	bytes.writeUInt32LE( 66, 12 + 7 * 4 ); // offsets[7]: bounding boxes
	bytes.writeUInt32LE( 4, 48 );
	bytes.write( "test", 52, "ascii" );
	bytes.writeUInt16LE( 1, 64 ); // general region id
	return bytes;
}

test("the dungeon manifest reads a DOF the table names with authored case", async ( t ) => {
	const root = await makeExtractedTree( t );
	const dungeonDir = path.join( root, "Data_extracted", "dungeon" );
	await writeFile(
		path.join( dungeonDir, "dungeoninfo.txt" ),
		'1\t"Dungeon\\wchina\\Dunhwang_Cv.dof"\r\n'
	);
	await writeFile( path.join( dungeonDir, "wchina", "dunhwang_cv.dof" ), minimalDof() );
	const targetPath = path.join( root, "dungeon-resources.json" );

	const summary = await buildDungeonResourceManifest( {
		extractedRoot: root,
		targetPath,
		refreshSidecars: false
	} );
	const manifest = JSON.parse( await readFile( targetPath, "utf8" ) );

	assert.equal( summary.dungeonCount, 1 );
	assert.equal( summary.resourceCount, 1 );
	// The table's row keeps its authored spelling; the manifest records
	// the path it actually read, which is the folded on-disk spelling.
	assert.equal( manifest.entries[0].dofName, "Dungeon\\wchina\\Dunhwang_Cv.dof" );
	assert.equal( manifest.resources[0].normalizedName, "dungeon/wchina/dunhwang_cv.dof" );
	assert.equal(
		manifest.resources[0].sourcePath,
		"Data_extracted/dungeon/wchina/dunhwang_cv.dof"
	);
});
