/*
===========================================================================

oldTabGate.test.mjs - planted violations for the old-tab gate

===========================================================================
*/

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { evaluateOldTab, oldTabFiles } from "../../checks/check_old_tab.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );

const ENCODER = "apps/server/internal/game/action/coscancel.go";
const DECODER = "apps/client-next/src/engine/runtime/simulation/worker/session/world/gameplay/inventory/gacha/gacha.ts";

function patchFor( file, ...lines ) {
	return `--- a/${file}\n+++ b/${file}\n@@ -1 +1 @@\n${lines.join( "\n" )}\n`;
}

test("an encoder line without a declaration fails; either declaration passes", () => {
	const patch = patchFor( ENCODER, "+\tw.U32(gid)" );
	assert.equal( evaluateOldTab( { patch, body: "" } ).ok, false );
	assert.equal( evaluateOldTab( { patch, body: "Notes.\n\nOld-tab: safe - checked live\n" } ).ok, true );
	assert.equal( evaluateOldTab( { patch, body: "Old-tab: bump" } ).ok, true );
});

test("a client decoder change without a declaration fails", () => {
	const patch = patchFor( DECODER, "-\tconst slot = v.getUint8( 1 );", "+\tconst slot = v.getUint16( 1 );" );
	const verdict = evaluateOldTab( { patch, body: "" } );
	assert.equal( verdict.ok, false );
	assert.deepEqual( verdict.wire, [ DECODER ] );
	assert.equal( evaluateOldTab( { patch, body: "Old-tab: safe - read order unchanged" } ).ok, true );
});

test("a visible-roots value change without a declaration fails", () => {
	const patch = patchFor(
		"apps/server/internal/game/action/itemuse.go",
		"+\t\tpet.Container = &domain.COSContainer{Capacity: cosRef.InventoryCapacity}"
	);
	const verdict = evaluateOldTab( { patch, body: "" } );
	assert.equal( verdict.ok, false );
	assert.deepEqual( verdict.visible, [ "apps/server/internal/game/action/itemuse.go" ] );
});

test("what no tab sees asks for nothing", () => {
	const patches = [
		patchFor( "apps/server/internal/game/action/y.go", "+\t// w.U8(1) is the old layout" ),
		patchFor( "apps/server/internal/game/action/y_test.go", "+\tw.U8(1)" ),
		patchFor( "apps/server/internal/store/z.go", "+\tn++" ),
		patchFor( "apps/client-next/src/engine/runtime/renderer/frame/frame.ts", "+\tconst a = 1;" ),
		patchFor( "apps/client-next/src/engine/runtime/simulation/worker/session/a.test.ts", "+\tconst a = 1;" )
	];
	for ( const patch of patches ) assert.equal( evaluateOldTab( { patch, body: "" } ).ok, true );
});

test("a removed encoder line and a deleted encoder file are wire", () => {
	const removed = patchFor( "apps/server/internal/data/catalogue/x.go", "-\tw.U8(kind)" );
	assert.deepEqual( oldTabFiles( removed ), { wire: [ "apps/server/internal/data/catalogue/x.go" ], visible: [] } );
	const deleted = "--- a/apps/server/internal/game/action/old.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-\tw.U8(kind)\n";
	assert.deepEqual( oldTabFiles( deleted ), { wire: [ "apps/server/internal/game/action/old.go" ], visible: [] } );
});

test("a file that gains an encoder line after a plain one is wire only", () => {
	const patch = patchFor( "apps/server/internal/game/action/y.go", "+\tn++", "+\tw.U8(1)" );
	assert.deepEqual( oldTabFiles( patch ), { wire: [ "apps/server/internal/game/action/y.go" ], visible: [] } );
});

test("the body gates read the pull request's current body, never the event payload", () => {
	// The payload carries the body the pull request had when the event
	// fired, so a re-run of a failed run judged the stale body and could
	// never pass after an edit (opensro#3113). Both gates must fetch the
	// live body through the API - which is what the pull-requests: read
	// grant is for - and hand it to the script as PR_BODY data.
	for ( const workflow of [ "old-tab.yml", "native-claims.yml" ] ) {
		const text = readFileSync( path.join( rebuildRoot, ".github", "workflows", workflow ), "utf8" );
		assert.equal( text.includes( "github.event.pull_request.body" ), false, `${workflow} reads the payload body` );
		assert.match( text, /pull-requests: read/, `${workflow} cannot read the body back` );
		assert.match(
			text,
			/gh api "repos\/\$\{\{ github\.repository \}\}\/pulls\/\$\{PR_NUMBER\}"/,
			`${workflow} never asks the API for the body`
		);
		assert.match( text, /echo "PR_BODY<</, `${workflow} does not hand the body over as PR_BODY` );
		assert.ok(
			text.indexOf( "Read the pull request's current body" ) < text.indexOf( "node scripts/checks/" ),
			`${workflow} reads the body only after the gate has run`
		);
	}
});

test("an em dash is not the audit's hyphen", () => {
	const patch = patchFor( DECODER, "+\tconst slot = v.getUint16( 1 );" );
	assert.equal( evaluateOldTab( { patch, body: "Old-tab: safe — checked live" } ).ok, false );
});
