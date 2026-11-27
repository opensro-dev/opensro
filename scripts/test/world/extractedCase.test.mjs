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
import { spawnSync } from "node:child_process";
import { mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { buildDungeonResourceManifest } from "../../build/world/assets/buildDungeonResources.mjs";
import {
	notedExists,
	notedReadFile,
	resolveOnDiskPath,
	resolveOnDiskPathSync,
	walkStart
} from "../../build/world/io.mjs";

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

test("the synchronous resolver takes the on-disk spelling", async ( t ) => {
	const root = await makeExtractedTree( t );
	const onDisk = path.join( root, "Data_extracted", "dungeon", "wchina", "C_Rudolph.BSK" );
	await writeFile( onDisk, "bsk" );

	const asked = path.join( root, "Data_extracted", "Dungeon", "wchina", "c_rudolph.bsk" );
	assert.equal( resolveOnDiskPathSync( asked ), onDisk );
	assert.equal( readFileSync( resolveOnDiskPathSync( asked ), "utf8" ), "bsk" );
	assert.equal( resolveOnDiskPathSync( onDisk ), onDisk );
});

test("the synchronous resolver keeps a missing file's spelling, then sees it created", async ( t ) => {
	const root = await makeExtractedTree( t );
	const asked = path.join( root, "Data_extracted", "Dungeon", "wchina", "absent.bms" );

	assert.equal( resolveOnDiskPathSync( asked ), asked );
	// The build creates files as it goes: a miss is re-listed once before
	// it is believed, so a file created after the first probe still resolves.
	await writeFile( path.join( root, "Data_extracted", "dungeon", "wchina", "absent.bms" ), "bms" );
	assert.equal(
		resolveOnDiskPathSync( asked ),
		path.join( root, "Data_extracted", "dungeon", "wchina", "absent.bms" )
	);
});

test("the synchronous resolver folds ASCII only, like the async one", async ( t ) => {
	const root = await makeExtractedTree( t );
	const onDisk = path.join( root, "Data_extracted", "prim", "Àname.bsr" );
	await writeFile( onDisk, "bsr" );
	const asked = path.join( root, "Data_extracted", "prim", "àname.bsr" );

	assert.equal( resolveOnDiskPathSync( asked ), asked );
	assert.equal( resolveOnDiskPathSync( onDisk ), onDisk );
});

test("the synchronous resolver answers from an injected listing", async ( t ) => {
	// A platform whose file system folds case cannot prove the resolution
	// from its own disk: the listing is the case-sensitive tree, served
	// by the test, and the resolver must answer with its spelling.
	const root = path.join( path.parse( process.cwd() ).root, "sro-virtual-case" );
	const extracted = path.join( root, "extracted" );
	const dataRoot = path.join( extracted, "Data_extracted" );
	const cos = path.join( dataRoot, "Res", "Cos" );
	const listings = new Map( [
		[ path.parse( process.cwd() ).root, [ "sro-virtual-case" ] ],
		[ root, [ "extracted" ] ],
		[ extracted, [ "Data_extracted" ] ],
		[ dataRoot, [ "Res" ] ],
		[ path.join( dataRoot, "Res" ), [ "Cos" ] ],
		[ cos, [ "C_Rudolph.BSK", "P_Goldpig01.BMS" ] ]
	] );
	const listDirectory = directory => {
		const names = listings.get( directory );
		if ( !names ) {
			const error = new Error( `ENOENT: no such directory ${directory}` );
			error.code = "ENOENT";
			throw error;
		}
		return names;
	};

	const asked = path.join( dataRoot, "res", "cos", "c_rudolph.bsk" );
	assert.equal( resolveOnDiskPathSync( asked, listDirectory ), path.join( cos, "C_Rudolph.BSK" ) );
	assert.equal(
		resolveOnDiskPathSync( path.join( dataRoot, "res", "cos", "absent.bms" ), listDirectory ),
		path.join( dataRoot, "res", "cos", "absent.bms" )
	);
});

/*
================
syntheticBsr

The smallest JMXVRES 0109 the character parser admits with one particle
modifier, the fixture shape of test/assets/bsrParticleModifiers.
================
*/
function syntheticBsr() {
	const bytes = Buffer.alloc( 1024 );
	bytes.write( "JMXVRES 0109" );
	let p = 0x80;
	bytes.writeUInt32LE( p, 0x0c + 6 * 4 );
	const u32 = value => {
		bytes.writeUInt32LE( value >>> 0, p );
		p += 4;
	};
	const str = value => {
		u32( value.length );
		p += bytes.write( value, p, "latin1" );
	};
	const vector = values => {
		for ( const v of values ) {
			bytes.writeFloatLE( v, p );
			p += 4;
		}
	};
	u32( 1 );
	u32( 2 );
	u32( -1 );
	str( "ambient" );
	u32( 1 );
	u32( 0x30000 );
	for ( let i = 1; i <= 6; i++ ) u32( i );
	bytes.set( [ 7, 8, 9, 10 ], p );
	p += 4;
	u32( 2 );
	for ( const optional of [ 0, 2 ] ) {
		u32( 11 );
		str( "system\\fixture.efp" );
		str( "Bip01 Head" );
		vector( [ 1.25, -2.5, 3.75 ] );
		u32( 12 );
		bytes.set( [ 13, 14, 15, optional ], p );
		p += 4;
		if ( optional ) vector( [ 4, 5, 6 ] );
	}
	u32( 0 );
	u32( 1 );
	u32( 4 );
	u32( 2 );
	u32( 1 );
	u32( 77 );
	u32( 99 );
	return bytes.subarray( 0, p );
}

test("the synchronous data readers find files the tree holds in authored case", async ( t ) => {
	// The readers root themselves at SRO_GAME_ROOT when their module
	// loads, so they run in a child pointed at the fixture tree. The
	// tree spells its directories and files the authored way
	// (Res/Cos/C_Rudolph.BSK); every query below uses the folded
	// spelling the game data carries.
	const root = await mkdtemp( path.join( tmpdir(), "sro-data-case-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	const dataDir = path.join( root, "extracted", "Data_extracted" );
	mkdirSync( path.join( dataDir, "Res", "Cos" ), { recursive: true } );
	mkdirSync( path.join( dataDir, "Res", "Char" ), { recursive: true } );
	writeFileSync( path.join( dataDir, "Res", "Cos", "C_Rudolph.BSR" ), syntheticBsr() );
	writeFileSync( path.join( dataDir, "Res", "Cos", "P_Goldpig01.BMS" ), "bms-bytes" );
	writeFileSync( path.join( dataDir, "Res", "Char", "Test_Warrior.BSR" ), "entity-bytes" );

	const moduleUrl = relative => JSON.stringify( new URL( relative, import.meta.url ).href );
	const program = `
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { dataAssetPathOnDisk } from ${moduleUrl( "../../build/shared/jmxAssetIO.mjs" )};
import { entitySource } from ${moduleUrl( "../../build/char/publishEntityBsrModifiers.mjs" )};
import { readEntityModel } from ${moduleUrl( "../../build/effects/buildEffectPrograms.mjs" )};

const dataDir = path.join( process.env.SRO_GAME_ROOT, "extracted", "Data_extracted" );
assert.equal(
	dataAssetPathOnDisk( "res/cos/c_rudolph.bsr" ),
	path.join( dataDir, "Res", "Cos", "C_Rudolph.BSR" )
);
const model = readEntityModel( "res/cos/c_rudolph.bsr" );
assert.equal( model.particleModifiers.length, 1 );
assert.equal( model.particleModifiers[0].entries[0].effectPath, "system\\\\fixture.efp" );
const entity = await entitySource( "res/char/test_warrior.bsr" );
assert.equal( entity.buffer.toString( "utf8" ), "entity-bytes" );
assert.equal( fs.existsSync( dataAssetPathOnDisk( "res/cos/p_goldpig01.bms" ) ), true );
assert.equal( fs.readFileSync( dataAssetPathOnDisk( "res/cos/p_goldpig01.bms" ), "utf8" ), "bms-bytes" );
assert.equal( fs.existsSync( dataAssetPathOnDisk( "res/cos/absent.bms" ) ), false );
console.log( "READERS-OK" );
`;
	const run = spawnSync( process.execPath, [ "--input-type=module", "--eval", program ], {
		env: { ...process.env, SRO_GAME_ROOT: root },
		encoding: "utf8"
	} );
	assert.equal( run.status, 0, run.stderr || run.stdout );
	assert.match( run.stdout, /READERS-OK/ );
});

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

test("a path under a trusted root is resolved from that root, never above it", async ( t ) => {
	const root = await makeExtractedTree( t );
	const onDisk = path.join( root, "Data_extracted", "dungeon", "wchina", "case.txt" );
	writeFileSync( onDisk, "c" );
	const listed = [];
	const list = directory => {
		listed.push( directory );
		return readdirSync( directory );
	};
	const asked = path.join( root, "Data_extracted", "Dungeon", "WChina", "Case.txt" );
	assert.equal( resolveOnDiskPathSync( asked, list, [ root ] ), onDisk );
	// Only the directories below the root: the root's parents are the
	// configuration's spelling, and a listing of them would key every
	// reader on whatever else lands beside the tree.
	assert.ok( listed.length > 0 );
	assert.ok(
		listed.every( directory => !path.relative( root, directory ).startsWith( ".." ) ),
		listed.join( "\n" )
	);
	assert.deepEqual(
		walkStart( path.join( path.dirname( root ), "outside.txt" ), [ root ] ).directory,
		path.parse( root ).root
	);
});
