/*
===========================================================================

scriptReach.test.mjs - a step's key follows the Node code it can reach

A variant step keys the Node modules behind its scripts by what it can
reach (scriptReach.mjs). The key must stand when a shared module gains a
declaration the step never names (#4596's new constant and helpers re-ran
17 steps), and must move whenever reached code changes: an entry, a
function it calls through any import or re-export, a statement that runs
at load, or anything behind a namespace or dynamic import.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { scriptKeys } from "../../build/shared/scriptReach.mjs";

const temporaryRoot = await mkdtemp( path.join( os.tmpdir(), "sro-script-reach-" ) );

test.after( () => rm( temporaryRoot, { recursive: true, force: true } ) );

// The base tree: an entry calls one helper of a shared module, which also
// holds a helper and a constant nobody calls, and imports a writer module
// the entry never reaches.
const BASE = {
	"entry.mjs": [
		'import { used } from "./shared.mjs";',
		'import { relayed } from "./barrel.mjs";',
		"console.log( used( 1 ) + relayed() );"
	].join( "\n" ),
	"shared.mjs": [
		'import { write } from "./writer.mjs";',
		"const STEP = 2;",
		"export function used( value ) {",
		"\treturn value * STEP;",
		"}",
		"export function unused() {",
		"\treturn write( 'x' );",
		"}"
	].join( "\n" ),
	"writer.mjs": [ "const PATH = 'out';", "export function write( text ) {", "\treturn PATH + text;", "}" ].join(
		"\n"
	),
	"barrel.mjs": 'export { relayed } from "./relay.mjs";',
	"relay.mjs": [ "export function relayed() {", "\treturn 3;", "}" ].join( "\n" )
};

let tree = 0;

/*
================
keysOf

The script keys of one tree: BASE with edits applied (a file's text, or
null to leave it out), entry.mjs as the step's one entry. Each call
writes a fresh directory, so trees never share files.
================
*/
async function keysOf( edits = {} ) {
	const root = path.join( temporaryRoot, `tree-${tree++}` );
	await mkdir( root, { recursive: true } );
	const files = [];
	for ( const [name, text] of Object.entries( { ...BASE, ...edits } ) ) {
		if ( text === null ) continue;
		const file = path.join( root, name );
		await writeFile( file, text );
		files.push( file );
	}
	const keys = await scriptKeys( files, [ path.join( root, "entry.mjs" ) ] );
	return Object.fromEntries(
		Object.entries( keys ).map( ( [file, key] ) => [ path.basename( file ), key && key.digest ] )
	);
}

test("a declaration no reached code names leaves the key standing", async () => {
	const base = await keysOf();
	const grown = await keysOf( {
		"shared.mjs": BASE["shared.mjs"] +
			"\nexport const OUTDOOR_WORLD_AREA = 'outdoor';\nexport function more() {\n\treturn 1;\n}"
	} );
	assert.deepEqual( grown, base );
	// The writer is reached only through unused(): it leaves the key.
	assert.equal( base["writer.mjs"], null );
	assert.ok( base["shared.mjs"] );
});

test("a new import of a module the step never calls leaves the key standing", async () => {
	const base = await keysOf();
	const grown = await keysOf( {
		"shared.mjs": 'import { other } from "./other.mjs";\n' + BASE["shared.mjs"] +
			"\nexport function viaOther() {\n\treturn other();\n}",
		"other.mjs": "const SIZE = 4;\nexport function other() {\n\treturn SIZE;\n}"
	} );
	assert.equal( grown["other.mjs"], null );
	delete grown["other.mjs"];
	assert.deepEqual( grown, base );
});

test("a reached function, a constant it reads, or the entry changes the key", async () => {
	const base = await keysOf();
	const body = await keysOf( { "shared.mjs": BASE["shared.mjs"].replace( "value * STEP", "value * STEP + 1" ) } );
	assert.notEqual( body["shared.mjs"], base["shared.mjs"] );
	const constant = await keysOf( { "shared.mjs": BASE["shared.mjs"].replace( "STEP = 2", "STEP = 3" ) } );
	assert.notEqual( constant["shared.mjs"], base["shared.mjs"] );
	const entry = await keysOf( { "entry.mjs": BASE["entry.mjs"] + "\nconsole.log( 'more' );" } );
	assert.notEqual( entry["entry.mjs"], base["entry.mjs"] );
});

test("a function reached through a re-export is keyed", async () => {
	const base = await keysOf();
	assert.ok( base["relay.mjs"] );
	const changed = await keysOf( { "relay.mjs": BASE["relay.mjs"].replace( "return 3", "return 4" ) } );
	assert.notEqual( changed["relay.mjs"], base["relay.mjs"] );
});

test("a statement that runs at load is keyed even when nothing names its module", async () => {
	const base = await keysOf();
	const effect = await keysOf( { "writer.mjs": BASE["writer.mjs"] + "\nglobalThis.loaded = true;" } );
	assert.ok( effect["writer.mjs"], "the loaded module's statement is reached" );
	assert.notEqual( effect["writer.mjs"], base["writer.mjs"] );
});

test("a namespace import or a dynamic import reaches its module whole", async () => {
	const namespace = {
		"entry.mjs": 'import * as shared from "./shared.mjs";\nconsole.log( shared.used( 1 ) );'
	};
	const base = await keysOf( namespace );
	const grown = await keysOf( {
		...namespace,
		"shared.mjs": BASE["shared.mjs"] + "\nexport function more() {\n\treturn 1;\n}"
	} );
	assert.notEqual( grown["shared.mjs"], base["shared.mjs"] );
	const dynamic = {
		"shared.mjs": BASE["shared.mjs"] + "\nexport async function late() {\n\treturn import( './relay.mjs' );\n}"
	};
	const before = await keysOf( dynamic );
	const after = await keysOf( {
		"shared.mjs": dynamic["shared.mjs"] + "\nexport function more() {\n\treturn 1;\n}"
	} );
	assert.notEqual( after["shared.mjs"], before["shared.mjs"] );
});
