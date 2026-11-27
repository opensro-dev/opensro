/*
===========================================================================

ui-action-census-gate.mjs - the action census, reusing a table whose inputs did not change

tools/ui-action-census.mjs re-drives every scene from a fresh fixture: it
is the joint-longest gate in the client check (8-26 min), and through
heavy.py every agent on the machine queues behind it. But the census is a
pure function of a small input set - ui.ts and the TypeScript closure it
imports, the panel registry and the gameplay modules the tool drives,
the tool's own files and the built asset tree - so a table built from
them stands until one of those moves.

The committed census is not one of them: it is only what the table is
judged against (census-verdict.mjs, milliseconds). Keyed into the build,
a census refresh commit (#4603) made r204n rebuild for 468 s a table
that could not have changed. So this wrapper:

  - digests the import closure of the census's roots (not a directory:
    an unrelated client change must not pay for the census, and a file
    ui.ts newly imports must start paying) into the build key;
  - reuses the table recorded for that key (checkStamps.findResult), or
    builds it (ui-action-census.mjs --out) and records it;
  - judges that table against the committed census on every run.

SRO_CHECK_FORCE=1 ignores recorded tables, so the gate builds afresh.
`--write` still goes to tools/ui-action-census.mjs directly.

===========================================================================
*/

import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { findResult, fingerprintOf, recordResult, snapshot } from "../../../scripts/checks/checkStamps.mjs";
import { censusVerdict } from "./census-verdict.mjs";
import { imports, project, resolve, root } from "./project.mjs";

export const ACTION_CENSUS_STAMP = "client-ui-action-census";

// The committed census every built table is judged against.
const CENSUS_PATH = path.join( root, "tests/runtime/ui-action-census.json" );

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

// The census's non-TypeScript build inputs, checkout-relative: the tool,
// its expanded-build rule and this gate, the source model both read
// through, the loader that strips types, the asset-root resolver, the
// stamp keeper itself, and the lockfile that pins the toolchain the model
// parses with. Not the committed census: the table is judged against it
// afresh every run (census-verdict.mjs).
export const ACTION_CENSUS_FILES = [
	"apps/client-next/tools/ui-action-census.mjs",
	"apps/client-next/tools/census-expanded.mjs",
	"apps/client-next/tools/replaced-windows.mjs",
	"scripts/lib/expandedBuild.mjs",
	"apps/client-next/tools/ui-action-census-gate.mjs",
	"apps/client-next/tools/project.mjs",
	"apps/client-next/tests/helpers/native-source-loader.mjs",
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
	"=ui-action-census-gate-2"
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
buildCensus

The census table, built by the tool in its own process (--out): the
table's text, or undefined when the build failed (its output already
went to the console).
================
*/
function buildCensus() {
	const directory = fs.mkdtempSync( path.join( os.tmpdir(), "sro-census-" ) );
	const out = path.join( directory, "ui-action-census.json" );
	return new Promise( ( resolvePromise ) => {
		const child = spawn( process.execPath, [ path.join( root, "tools/ui-action-census.mjs" ), "--out", out ], {
			cwd: root,
			stdio: "inherit",
			windowsHide: true
		} );
		const settle = code => {
			let text;
			if ( code === 0 ) {
				try {
					text = fs.readFileSync( out, "utf8" );
				} catch {
					text = undefined;
				}
			}
			fs.rmSync( directory, { recursive: true, force: true } );
			resolvePromise( text );
		};
		child.on( "error", () => settle( 1 ) );
		child.on( "close", code => settle( code ?? 1 ) );
	} );
}

/*
================
censusGate

The gate's decision, with its effects passed in: the table recorded for
key (cache.find) or a fresh build (build, then cache.record), judged
against the committed text. Returns { code, lines, built }.
================
*/
export async function censusGate( { key, committed, cache, build, verdict = censusVerdict } ) {
	let text = cache.find( key );
	const built = text === undefined;
	if ( built ) {
		text = await build();
		if ( text === undefined ) {
			return { code: 1, lines: [ "verify:ui-action-census: the census build failed" ], built };
		}
		cache.record( key, text );
	}
	const judged = verdict( committed, text );
	const source = built ? "built" : "reused (build inputs unchanged)";
	return { code: judged.ok ? 0 : 1, lines: [ `verify:ui-action-census: table ${source}`, ...judged.lines ], built };
}

/*
================
main

Key the build inputs, then let censusGate reuse or build and judge.
================
*/
async function main() {
	const model = project();
	const closure = actionCensusClosure( model.files );
	const key = actionCensusKey( snapshot( SNAPSHOT_PATHS ), actionCensusInputSet( closure ) );
	const committed = fs.existsSync( CENSUS_PATH ) ? fs.readFileSync( CENSUS_PATH, "utf8" ) : "";
	const result = await censusGate( {
		key,
		committed,
		cache: {
			find: k => findResult( ACTION_CENSUS_STAMP, k ),
			record: ( k, text ) => recordResult( ACTION_CENSUS_STAMP, k, text )
		},
		build: buildCensus
	} );
	for ( const line of result.lines ) (result.code === 0 ? console.log : console.error)( line );
	process.exitCode = result.code;
}

if ( process.argv[1] === import.meta.filename ) {
	main().catch( error => {
		console.error( error );
		process.exitCode = 1;
	} );
}
