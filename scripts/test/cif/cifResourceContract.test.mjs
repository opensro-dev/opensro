/*
===========================================================================

cifResourceContract.test.mjs - the CIF define registry, preprocessor and layouts

The active config/define.txt defines, the #ifdef preprocessor's line and
directive rules, and that every shipped resinfo source is published as a
layout parsed from the same live macro branch.

===========================================================================
*/
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { applyCifPreprocessor, loadCifDefines, parseCifDefines } from "../../build/shared/cifPreprocessor.mjs";
import { discoverCifLayouts } from "../../build/shared/cifLayoutCatalog.mjs";
import { parseCifLayout } from "../../build/shared/cifResources.mjs";
import { publicRoot, readText, resinfoDir } from "../../build/shared/resourceIo.mjs";

const layoutOutputDir = path.join( publicRoot, "assets", "cif", "layouts" );

test("CIF define registry follows active config/define.txt lines", async () => {
	const defines = await loadCifDefines();

	assert.equal( defines.EUROPE_SYSTEM, true );
	assert.equal( defines.RENEWAL_SHOP_SYSTEM, true );
	assert.equal( defines.APPLY_MENTOR_SYSTEM, true );
	assert.equal( defines.APPLY_AVATAR_SYSTEM, true );
	assert.equal( defines.APPLY_SIEGE_FORTRESS_SYSTEM, true );
	assert.equal( defines.APPLY_UI_4TH, true );
	assert.equal( defines.HISTORY_FOR_BUYING_SILK, undefined );
	assert.equal( parseCifDefines( "//DISABLED\n ENABLED // comment\n" ).ENABLED, true );
});

test("CIF preprocessor preserves lines and repairs malformed directive ownership", () => {
	const source = "first\n#ifdef LIVE\nactive\n#else\ninactive\n#endif\nlast";
	assert.equal(
		applyCifPreprocessor( source, { LIVE: true } ).split( "\n" ).length,
		source.split( "\n" ).length
	);
	assert.match( applyCifPreprocessor( source, { LIVE: true } ), /active/ );
	assert.doesNotMatch( applyCifPreprocessor( source, { LIVE: true } ), /\ninactive\n/ );
});

test("CIF preprocessor reads a directive by its keyword, whatever follows it", () => {
	// A later client's layouts write "#endif // KEY" and tabs (2026-11-07):
	// the exact-line compare took them for content and failed the build.
	const source = [ "#ifdef\tLIVE // note", "shown", "#else // other", "hidden", "#endif // LIVE", "after" ].join(
		"\n"
	);
	const output = applyCifPreprocessor( source, { LIVE: true }, () => assert.fail( "no warning expected" ) );
	assert.deepEqual( output.split( "\n" ), [ "", "shown", "", "", "", "after" ] );
	const negated = [ "#ifndef OFF", "kept", "#else", "dropped", "#endif" ].join( "\n" );
	assert.deepEqual( applyCifPreprocessor( negated, {} ).split( "\n" ), [ "", "kept", "", "", "" ] );
});

test("CIF preprocessor warns and repairs a broken structure instead of failing the build", () => {
	const warnings = [];
	const warn = message => warnings.push( message );
	// An #ifdef left open: closed at the end of the file, lines kept.
	const open = [ "before", "#ifdef CONTENT_CHARGING_SYSTEM", "charged", "after" ].join( "\n" );
	assert.deepEqual( applyCifPreprocessor( open, {}, warn ).split( "\n" ), [ "before", "", "", "" ] );
	assert.match( warnings.at( -1 ), /CONTENT_CHARGING_SYSTEM opened at line 2 has no #endif/ );
	// A stray #else or #endif, and a second #else, are ignored.
	const stray = [ "#else", "x", "#endif", "y" ].join( "\n" );
	assert.deepEqual( applyCifPreprocessor( stray, {}, warn ).split( "\n" ), [ "", "x", "", "y" ] );
	const twice = [ "#ifdef A", "a", "#else", "b", "#else", "c", "#endif" ].join( "\n" );
	assert.deepEqual( applyCifPreprocessor( twice, {}, warn ).split( "\n" ), [ "", "", "", "b", "", "c", "" ] );
	assert.equal( warnings.length, 4 );
});

test("every shipped resinfo source has a published layout", async () => {
	const [sourceFiles, publishedFiles] = await Promise.all( [
		discoverCifLayouts( resinfoDir ),
		readdir( layoutOutputDir )
	] );
	const jsonFiles = publishedFiles
		.filter( ( name ) => name.endsWith( ".json" ) )
		.sort( ( left, right ) => left.localeCompare( right ) );
	const expectedJsonFiles = sourceFiles.map( ( name ) => name.replace( /\.txt$/i, ".json" ) );

	assert.equal( sourceFiles.length, 231 );
	assert.deepEqual( jsonFiles, expectedJsonFiles );
});

// cif-class-catalog.json was a one-off reverse-engineering export; no build
// step produces it, so the layout is checked against a fresh parse instead.
test("the generated layout parses the same live macro branch as its source", async () => {
	const [raw, defines, generatedSource] = await Promise.all( [
		readText( path.join( resinfoDir, "ginterface.txt" ) ),
		loadCifDefines(),
		readFile( path.join( layoutOutputDir, "ginterface.json" ), "utf8" )
	] );
	const parsed = parseCifLayout(
		applyCifPreprocessor( raw, defines ),
		path.join( resinfoDir, "ginterface.txt" )
	);
	const generated = JSON.parse( generatedSource.replace( /^\uFEFF/, "" ) );

	assert.deepEqual(
		generated.sections.map( ( section ) => section.name ),
		parsed.sections.map( ( section ) => section.name )
	);
});
