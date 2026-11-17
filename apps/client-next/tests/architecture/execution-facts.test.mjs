/*
===========================================================================

execution-facts.test.mjs - per-file facts are the same whoever extracts them

The execution graph's checker facts are cached per file and extracted in
worker threads (tools/execution-facts.mjs). That is only sound when a
file's facts do not depend on which other files one program was asked
about, and when a file's key changes exactly when its type closure does.
These tests hold both on a closed fixture project.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { executionFlow } from "../../tools/execution-flow.mjs";
import { extractFacts, factKeys, readProject } from "../../tools/execution-facts.mjs";

const TSCONFIG = {
	compilerOptions: {
		strict: true,
		target: "ES2022",
		// No DOM: every program the tests build would parse lib.dom.d.ts again,
		// most of this file's time on the two-core CI runner.
		lib: [ "ES2022" ],
		types: [],
		module: "ESNext",
		moduleResolution: "Bundler",
		noEmit: true
	},
	include: [ "src/**/*.ts" ]
};

/*
================
project

A scratch project: two resource owners with a dispose each, a loop that
disposes both through one union-typed variable (the UI's shape), and a
file that imports neither.
================
*/
function project( t ) {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-execution-facts-" ) );
	t.after( () => fs.rmSync( dir, { recursive: true, force: true } ) );
	const write = ( name, text ) => {
		const file = path.join( dir, name );
		fs.mkdirSync( path.dirname( file ), { recursive: true } );
		fs.writeFileSync( file, text );
	};
	write( "tsconfig.json", JSON.stringify( TSCONFIG ) );
	write( "src/engine/ownership.json", JSON.stringify( { bootstrap: "src/main.ts", workers: {} } ) );
	write(
		"src/a.ts",
		"export function createA() {\n\treturn { dispose() { return 1; } };\n}\n"
	);
	write(
		"src/b.ts",
		"export function createB() {\n\treturn { dispose() { return 'b'; }, extra: 2 };\n}\n"
	);
	write(
		"src/main.ts",
		'import { createA } from "./a";\nimport { createB } from "./b";\n' +
			"export function shutdown() {\n\tfor ( const owner of [ createA(), createB() ] ) owner.dispose();\n}\n"
	);
	write( "src/other.ts", "export function other() {\n\treturn 3;\n}\n" );
	return dir;
}

test("a call through a union reaches every member's function", ( t ) => {
	const dir = project( t );
	const graph = executionFlow( dir );
	const call = graph.calls.find( c => c.expression === "owner.dispose" );
	assert.ok( call, "the loop's dispose call is in the graph" );
	assert.deepEqual( call.targets, [
		"src/a.ts#module/createA/dispose",
		"src/b.ts#module/createB/dispose"
	] );
});

test("a file's facts do not depend on what else one program extracts", ( t ) => {
	const dir = project( t );
	const { sources } = readProject( dir );
	const main = sources.find( file => file.endsWith( "/main.ts" ) );
	const together = extractFacts( dir, sources );
	const alone = extractFacts( dir, [ main ] );
	const reversed = extractFacts( dir, [ ...sources ].reverse() );
	const ofMain = list => list.find( facts => facts.file === "src/main.ts" );
	assert.deepEqual( alone[0], ofMain( together ) );
	assert.deepEqual( ofMain( reversed ), ofMain( together ) );
});

test("a key changes with its type closure and only with it", ( t ) => {
	const dir = project( t );
	const keyOf = name => {
		const project_ = readProject( dir );
		const keys = factKeys( dir, project_ );
		return keys.get( project_.sources.find( file => file.endsWith( `/${name}` ) ) );
	};
	const main = keyOf( "main.ts" ), other = keyOf( "other.ts" );
	fs.appendFileSync( path.join( dir, "src/b.ts" ), "// edited\n" );
	assert.notEqual( keyOf( "main.ts" ), main, "main imports b: its facts are re-extracted" );
	assert.equal( keyOf( "other.ts" ), other, "other imports nothing: its facts are kept" );
});

test("a global augmentation changes the key of a file that never imports it", ( t ) => {
	// telemetry.ts's declare global adds window.sroDebug for every file; a
	// reader outside the augmenting module's import closure must not keep
	// facts extracted against the old declaration.
	const dir = project( t );
	fs.writeFileSync(
		path.join( dir, "src/augment.ts" ),
		"export {};\ndeclare global {\n\tvar sroFixture: number | undefined;\n}\n"
	);
	fs.writeFileSync(
		path.join( dir, "src/reader.ts" ),
		"export function read() {\n\treturn globalThis.sroFixture;\n}\n"
	);
	const keyOf = name => {
		const project_ = readProject( dir );
		return factKeys( dir, project_ ).get( project_.sources.find( file => file.endsWith( `/${name}` ) ) );
	};
	const reader = keyOf( "reader.ts" );
	fs.writeFileSync(
		path.join( dir, "src/augment.ts" ),
		"export {};\ndeclare global {\n\tvar sroFixture: string | undefined;\n}\n"
	);
	assert.notEqual( keyOf( "reader.ts" ), reader, "the augmentation's change reaches the reader's key" );
});
