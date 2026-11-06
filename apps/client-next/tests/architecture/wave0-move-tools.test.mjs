/*
===========================================================================

wave0-move-tools.test.mjs - the wave-0 move gates reject each escape

The #2534 decision record's three checkable promises, one seeded
violation each: a move that changes a token is not an identity, a
package that is not on the list is not allowed, and a scene whose
allocations rose unnamed is not a certificate. The identity and
dependency cases run over small fixture texts and fixture projects, so
an edit under src/ does not rerun them; the real tree is checked by
the gates themselves. The allocation cases exercise the gate's
comparison over rows, which is where its decision lives.

===========================================================================
*/

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { normaliseAdded, normaliseRemoved, proveMove, readCensusFields } from "../../tools/move-identity.mjs";
import { verifyDependencies } from "../../tools/verify-dependencies.mjs";
import { compareSceneRows } from "../../tools/perf/bench/ui-allocation-gauge.mjs";

// ============================================================================
// Identity proof
// ============================================================================

test("a verbatim move is token-identical, comments and layout aside", () => {
	const removed = "function drawStall( x ) {\n\t// authored rect\n\treturn rect( x, 1 );\n}\n";
	const added = "function drawStall( x ) { return rect( x, 1 ); }";
	const proof = proveMove( removed, added, [] );
	assert.equal( proof.identical, true );
	assert.deepEqual( proof.removedResidue, [] );
	assert.deepEqual( proof.addedResidue, [] );
});

test("the census rewrite is the one allowed difference, in both directions", () => {
	const removed = "if ( stallOpen ) {\n\tstallOpen = pick( stallGrid );\n}";
	const added = "if ( state.stallOpen ) {\n\tstate.stallOpen = pick( state.stallGrid );\n}";
	assert.deepEqual(
		normaliseRemoved( removed, [ "stallOpen", "stallGrid" ] ),
		normaliseAdded( added, [ "stallOpen", "stallGrid" ] )
	);
	assert.equal( proveMove( removed, added, [ "stallOpen", "stallGrid" ] ).identical, true );
});

test("a verbatim move that binds a census name still proves identical", () => {
	// The block's own binding shadows the closure field: the spelling is
	// the local everywhere in the block, so no rewrite may touch it.
	const arrow = "const hot = rows.filter( stallOpen => stallOpen > 2 );";
	assert.equal( proveMove( arrow, arrow, [ "stallOpen" ] ).identical, true );
	const declared = "function draw( stallGrid ) { let stallOpen = stallGrid; return stallOpen; }";
	assert.equal( proveMove( declared, declared, [ "stallOpen", "stallGrid" ] ).identical, true );
	const destructured = "const { stallGrid } = state; return stallGrid;";
	assert.equal( proveMove( destructured, destructured, [ "stallGrid" ] ).identical, true );
});

test("a bound census name is not rewritten, but other fields still are", () => {
	const removed = "const hot = rows.filter( stallOpen => stallOpen > 2 );\nreturn stallGrid;";
	const added = "const hot = rows.filter( stallOpen => stallOpen > 2 );\nreturn state.stallGrid;";
	assert.equal( proveMove( removed, added, [ "stallOpen", "stallGrid" ] ).identical, true );
	// Rewriting the shadowed read itself is a real change, not a move.
	const rewritten = "const hot = rows.filter( stallOpen => state.stallOpen > 2 );\nreturn state.stallGrid;";
	assert.equal( proveMove( removed, rewritten, [ "stallOpen", "stallGrid" ] ).identical, false );
});

test("a rewrite of a field the census does not list is residue, not a move", () => {
	const proof = proveMove( "return stallGrid;", "return state.stallGrid;", [] );
	assert.equal( proof.identical, false );
	assert.deepEqual( proof.removedResidue, [] );
	assert.deepEqual( proof.addedResidue, [ "state", "." ] );
});

test("a changed token inside the moved block is reported on both sides", () => {
	const proof = proveMove( "return rect( x, 1 );", "return rect( x, 2 );", [] );
	assert.equal( proof.identical, false );
	assert.deepEqual( proof.removedResidue, [ "1" ] );
	assert.deepEqual( proof.addedResidue, [ "2" ] );
});

test("a member read and a property name are not closure fields", () => {
	// holder.stallGrid is another object's field, and { stallGrid: v }
	// names a property: neither is the census field, so neither side
	// may be rewritten and the two texts must already agree.
	const text = "return holder.stallGrid + pair( { stallGrid: 1 } );";
	assert.deepEqual( normaliseRemoved( text, [ "stallGrid" ] ), normaliseRemoved( text, [] ) );
});

test("an optional-chain member read is not a closure field", () => {
	// holder?.stallGrid reads another object's field through an
	// optional chain: ?. is member position exactly like ., so the
	// rewrite must leave the spelling alone and a verbatim move
	// containing it must still prove identical.
	const text = "return holder?.stallGrid;";
	assert.deepEqual( normaliseRemoved( text, [ "stallGrid" ] ), normaliseRemoved( text, [] ) );
	assert.equal( proveMove( text, text, [ "stallGrid" ] ).identical, true );
});

test("a census file the tool cannot read is an error, not silence", () => {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-census-" ) );
	try {
		const bad = path.join( dir, "census.json" );
		fs.writeFileSync( bad, JSON.stringify( { rows: [] } ) );
		assert.throws( () => readCensusFields( bad ), /no field list/ );
		const good = path.join( dir, "good.json" );
		fs.writeFileSync( good, JSON.stringify( [ { name: "stallGrid", owner: "stall" }, "stallOpen" ] ) );
		assert.deepEqual( readCensusFields( good ), [ "stallGrid", "stallOpen" ] );
	} finally {
		fs.rmSync( dir, { recursive: true, force: true } );
	}
});

// ============================================================================
// No-new-dependency gate
// ============================================================================

const DEPENDENCY_SOURCES = {
	"src/engine/ownership.json": JSON.stringify( {
		version: 1,
		root: "src/engine/runtime/runtime.ts",
		bootstrap: "src/bootstrap.ts",
		modules: {},
		internals: {},
		workers: {},
		contracts: {}
	} ),
	"src/bootstrap.ts": "export {};\n",
	"src/engine/runtime/runtime.ts": "export {};\n",
	"src/engine/runtime/ui/ui.ts": "export function createUi() { return 1; }\n"
};

/*
================
withDependencyFixture

Writes a minimal client tree (manifest, allowlist, the gate's fixture
sources) plus one edit, and hands the directory to fn.
================
*/
function withDependencyFixture( edit, fn ) {
	const dir = fs.mkdtempSync( path.join( os.tmpdir(), "sro-dependencies-" ) );
	const write = ( name, text ) => {
		fs.mkdirSync( path.dirname( path.join( dir, name ) ), { recursive: true } );
		fs.writeFileSync( path.join( dir, name ), text );
	};
	try {
		write(
			"package.json",
			JSON.stringify( { name: "@sro/client-next", devDependencies: { typescript: "catalog:" } } )
		);
		write(
			"tools/dependency-allowlist.json",
			JSON.stringify( { packages: [ "typescript" ] } )
		);
		for ( const [name, text] of Object.entries( DEPENDENCY_SOURCES ) ) write( name, text );
		if ( edit ) write( edit.file, edit.text );
		return fn( dir );
	} finally {
		fs.rmSync( dir, { recursive: true, force: true } );
	}
}

test("the dependency gate passes a tree that only carries listed packages", () => {
	withDependencyFixture( null, dir => assert.deepEqual( verifyDependencies( dir ), [] ) );
});

test("a new devDependency is refused before any file imports it", () => {
	withDependencyFixture(
		{
			file: "package.json",
			text: JSON.stringify( { devDependencies: { typescript: "catalog:", zustand: "^5" } } )
		},
		dir => {
			const issues = verifyDependencies( dir );
			assert.equal( issues.length, 1 );
			assert.match( issues[0], /zustand/ );
		}
	);
});

test("a window module importing an unlisted package is refused", () => {
	withDependencyFixture(
		{
			file: "src/engine/runtime/ui/stall.ts",
			text: 'import { createStore } from "zustand";\nexport { createStore };\n'
		},
		dir => {
			const issues = verifyDependencies( dir );
			assert.equal( issues.some( issue => issue.includes( "stall.ts" ) && issue.includes( "zustand" ) ), true );
		}
	);
});

test("relative, alias and builtin imports in a window module are not packages", () => {
	withDependencyFixture(
		{
			file: "src/engine/runtime/ui/stall.ts",
			text: 'import path from "node:path";\nimport { createUi } from "./ui";\nexport { path, createUi };\n'
		},
		dir => assert.deepEqual( verifyDependencies( dir ), [] )
	);
});

// ============================================================================
// Allocation gauge comparison
// ============================================================================

test("allocations within the slack pass, and a fall always passes", () => {
	const baseline = { "world-hud": { bytesPerFrame: 10000 } };
	assert.deepEqual( compareSceneRows( baseline, { "world-hud": { bytesPerFrame: 10500 } } ), [] );
	assert.deepEqual( compareSceneRows( baseline, { "world-hud": { bytesPerFrame: 2000 } } ), [] );
});

test("an unnamed rise past the slack fails and names the scene", () => {
	const failures = compareSceneRows(
		{ "world-hud": { bytesPerFrame: 10000 } },
		{ "world-hud": { bytesPerFrame: 30000 } }
	);
	assert.equal( failures.length, 1 );
	assert.match( failures[0], /world-hud/ );
});

test("a rise the baseline names is the PR naming its allocation", () => {
	const baseline = {
		"world-hud": { bytesPerFrame: 10000, named: "one retained quad pool per stall window (#2521)" }
	};
	assert.deepEqual( compareSceneRows( baseline, { "world-hud": { bytesPerFrame: 30000 } } ), [] );
});

test("a scene missing on either side fails", () => {
	assert.equal( compareSceneRows( { "world-hud": { bytesPerFrame: 1 } }, {} ).length, 1 );
	assert.equal( compareSceneRows( {}, { "world-hud": { bytesPerFrame: 1 } } ).length, 1 );
});
