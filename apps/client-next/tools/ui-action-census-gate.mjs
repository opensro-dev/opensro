/*
===========================================================================

ui-action-census-gate.mjs - the action census, reusing a pass whose inputs did not change

tools/ui-action-census.mjs re-drives every scene from a fresh fixture: it
is the joint-longest gate in the client check (8-26 min), and through
heavy.py every agent on the machine queues behind it. But the census is a
pure function of a small input set - ui.ts and the TypeScript closure it
imports, the panel registry and the gameplay modules the tool drives,
the tool's own files, the committed census it diffs against, and the
built asset tree - so a pass stands until one of those moves.

This wrapper owns that judgement, with the stamps checkStamps.mjs keeps:

  - the input key digests the import closure of the census's roots (not
    a directory: an unrelated client change must not pay for the census,
    and a file ui.ts newly imports must start paying);
  - a recorded pass with the same key reports "up to date" and exits 0;
  - otherwise the census runs for real, and only a pass records a stamp.

SRO_CHECK_FORCE=1 ignores the stamps (checkStamps.findPass), and a failed
run records nothing, so the gate can never skip inputs it has not seen
pass. `--write` still goes to tools/ui-action-census.mjs directly.

===========================================================================
*/

import { spawn } from "node:child_process";
import path from "node:path";

import { findPass, fingerprintOf, recordPass, snapshot } from "../../../scripts/checks/checkStamps.mjs";
import { imports, project, resolve, root } from "./project.mjs";

export const ACTION_CENSUS_STAMP = "client-ui-action-census";

// The TypeScript roots the census executes: ui.ts itself, the panel
// registry (foundation/ui/panels.ts) and the gameplay modules the tool
// imports beside createUi. Model paths (src-relative), as project() keys.
export const ACTION_CENSUS_ROOTS = [
	"src/engine/runtime/ui/ui.ts",
	"src/engine/foundation/ui/panels.ts",
	"src/engine/foundation/gameplay/stall.ts",
	"src/engine/foundation/gameplay/fortress.ts",
	"src/engine/foundation/gameplay/fortress-production.ts"
];

// The census's non-TypeScript inputs, checkout-relative: the tool and
// this gate, the source model both read through, the loader that strips
// types, the committed census the run diffs against, the asset-root
// resolver, the stamp keeper itself, and the lockfile that pins the
// toolchain the model parses with.
export const ACTION_CENSUS_FILES = [
	"apps/client-next/tools/ui-action-census.mjs",
	"apps/client-next/tools/ui-action-census-gate.mjs",
	"apps/client-next/tools/project.mjs",
	"apps/client-next/tests/helpers/native-source-loader.mjs",
	"apps/client-next/tests/runtime/ui-action-census.json",
	"scripts/checks/checkStamps.mjs",
	"scripts/lib/generatedRoot.mjs",
	"pnpm-lock.yaml"
];

// The trees the stamp snapshot reads: the closure lives under src/, the
// remaining inputs are the files above.
const SNAPSHOT_PATHS = [ "apps/client-next/src", ...ACTION_CENSUS_FILES ];

// Non-path key parts: the Node that strips the types, the built asset
// tree the scenes read (its pack manifest stands for the whole tree, as
// in the client check's own key), and this gate's format salt.
export const ACTION_CENSUS_KEYS = [
	"@node,--version",
	"!.generated/client-public/assets/packs/manifest.json",
	"=ui-action-census-gate-1"
];

const CLIENT_PREFIX = "apps/client-next/";

/*
================
actionCensusClosure

The TypeScript files the census can observe: the roots plus everything
they transitively import, resolved the bundler's way (project.mjs). A
root the model does not hold is a hard error - a key computed over a
missing root would silently stop watching ui.ts, and the gate would
start skipping on faith.
================
*/
export function actionCensusClosure( files ) {
	for ( const rootPath of ACTION_CENSUS_ROOTS ) {
		if ( !files.has( rootPath ) ) {
			throw new Error( `action census root ${rootPath} is not in the source model` );
		}
	}
	const seen = new Set();
	const queue = [ ...ACTION_CENSUS_ROOTS ];
	while ( queue.length > 0 ) {
		const file = queue.pop();
		if ( seen.has( file ) ) continue;
		seen.add( file );
		for ( const edge of imports( file, files.get( file ) ).edges ) {
			const target = resolve( { files }, file, edge.specifier );
			if ( target && !seen.has( target ) ) queue.push( target );
		}
	}
	return seen;
}

/*
================
actionCensusInputSet

Every checkout-relative path the stamp watches: the closure's files plus
the tool's own inputs. Anything outside this set cannot move a census
row, so its edits leave the stamp standing.
================
*/
export function actionCensusInputSet( closure ) {
	const selected = new Set( ACTION_CENSUS_FILES );
	for ( const file of closure ) selected.add( CLIENT_PREFIX + file );
	return selected;
}

/*
================
actionCensusKey

The stamp key over a checkStamps snapshot: only the watched inputs
digest, with the toolchain, asset-tree and salt keys.
================
*/
export function actionCensusKey( entries, inputSet ) {
	return fingerprintOf( entries, file => inputSet.has( file ), ACTION_CENSUS_KEYS );
}

/*
================
runCensus

The census itself, exactly as the unwrapped gate ran it.
================
*/
function runCensus() {
	return new Promise( ( resolvePromise ) => {
		const child = spawn( process.execPath, [ path.join( root, "tools/ui-action-census.mjs" ) ], {
			cwd: root,
			stdio: "inherit",
			windowsHide: true
		} );
		child.on( "error", () => resolvePromise( 1 ) );
		child.on( "close", code => resolvePromise( code ?? 1 ) );
	} );
}

/*
================
main

Skip on a recorded pass over the same inputs; otherwise run the census
and stamp the pass.
================
*/
async function main() {
	const model = project();
	const closure = actionCensusClosure( model.files );
	const key = actionCensusKey( snapshot( SNAPSHOT_PATHS ), actionCensusInputSet( closure ) );
	const stamp = findPass( ACTION_CENSUS_STAMP, key );
	if ( stamp ) {
		console.log(
			`verify:ui-action-census: up to date (inputs unchanged since the pass at ${stamp.passedAt})`
		);
		return;
	}
	const code = await runCensus();
	if ( code !== 0 ) {
		process.exitCode = code;
		return;
	}
	recordPass( ACTION_CENSUS_STAMP, key );
	console.log( "verify:ui-action-census: PASS (stamped; unchanged inputs will skip)" );
}

if ( process.argv[1] === import.meta.filename ) {
	main().catch( error => {
		console.error( error );
		process.exitCode = 1;
	} );
}
