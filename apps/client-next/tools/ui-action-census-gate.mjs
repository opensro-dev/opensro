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
that could not have changed. Nor is the whole asset tree: keyed on its
pack manifest, any asset build reran every scene (r204n: 582 s), while
each scene reads a few published files. So this wrapper:

  - digests the import closure of the census's roots (not a directory:
    an unrelated client change must not pay for the census, and a file
    ui.ts newly imports must start paying) into the code key;
  - keeps, for that key (checkStamps.findResult), every scene's census
    with the digest of each published file it requested, an absent one
    included;
  - rebuilds (ui-action-census.mjs --parts) only the scenes one of whose
    files moved, all of them when no record holds the key, and records
    the mix;
  - judges the assembled table against the committed census on every run.

SRO_CHECK_FORCE=1 ignores recorded tables, so the gate builds afresh.
`--write` still goes to tools/ui-action-census.mjs directly.

===========================================================================
*/

import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { findResult, fingerprintOf, recordResult, snapshot } from "../../../scripts/checks/checkStamps.mjs";
import { MISSING_DIGEST } from "../../../scripts/build/graph/action.mjs";
import { openFileHashCache } from "../../../scripts/build/shared/fileHashCache.mjs";
import { buildStatePath, CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import { pngSizeDigest } from "./census-reads.mjs";
import { censusTable, censusText, censusVerdict } from "./census-verdict.mjs";
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
	"apps/client-next/tools/census-verdict.mjs",
	"apps/client-next/tools/census-reads.mjs",
	"apps/client-next/tools/census-expanded.mjs",
	"apps/client-next/tools/censusWorkers.mjs",
	"apps/client-next/tools/replaced-windows.mjs",
	"scripts/lib/expandedBuild.mjs",
	"apps/client-next/tools/ui-action-census-gate.mjs",
	"apps/client-next/tools/project.mjs",
	"apps/client-next/tests/helpers/native-source-loader.mjs",
	"scripts/checks/checkStamps.mjs",
	"scripts/lib/generatedRoot.mjs",
	"scripts/build/shared/assetPaths.mjs",
	"scripts/build/world/paths.mjs",
	"pnpm-lock.yaml"
];

// The trees the stamp snapshot reads: the closure lives under src/, the
// remaining inputs are the files above.
const SNAPSHOT_PATHS = [ "apps/client-next/src", ...ACTION_CENSUS_FILES ];

// Non-path key parts: the Node that strips the types and this gate's
// format salt. The published files are keyed per scene, by what it read.
export const ACTION_CENSUS_KEYS = [ "@node,--version", "=ui-action-census-gate-5" ];
// The gate's own (path -> sha256) cache of the published files the scenes
// read: one owner per cache file.
const HASH_CACHE_PATH = buildStatePath( "census-file-hash-cache.json" );

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
buildParts

The named scenes (all when names is undefined) built by the tool in its
own process (--parts): { names, scenes: { name: { census, reads } } },
or undefined when the build failed (its output already went to the
console).
================
*/
function buildParts( names ) {
	const directory = fs.mkdtempSync( path.join( os.tmpdir(), "sro-census-" ) );
	const out = path.join( directory, "parts.json" );
	const argv = [ path.join( root, "tools/ui-action-census.mjs" ), "--parts", out ];
	if ( names ) argv.push( "--scenes", names.join( "," ) );
	return new Promise( ( resolvePromise ) => {
		const child = spawn( process.execPath, argv, { cwd: root, stdio: "inherit", windowsHide: true } );
		const settle = code => {
			let parts;
			if ( code === 0 ) {
				try {
					parts = JSON.parse( fs.readFileSync( out, "utf8" ) );
				} catch {
					parts = undefined;
				}
			}
			fs.rmSync( directory, { recursive: true, force: true } );
			resolvePromise( parts );
		};
		child.on( "error", () => settle( 1 ) );
		child.on( "close", code => settle( code ?? 1 ) );
	} );
}

/*
================
movedScenes

The recorded scenes one of whose read files no longer has its recorded
digest, each with the first moved file: [[name, file]].
================
*/
async function movedScenes( record, digest ) {
	const moved = [];
	for ( const name of record.names ) {
		for ( const [file, recorded] of record.scenes[name].reads ) {
			if ( (await digest( file )) !== recorded ) {
				moved.push( [ name, file ] );
				break;
			}
		}
	}
	return moved;
}

/*
================
censusGate

The gate's decision, with its effects passed in: the scenes recorded for
key (cache.find) are kept while every file each read holds its digest;
the rest are built (build( names ), all when nothing is recorded), and
the mix is recorded (cache.record) and judged against the committed
text. Returns { code, lines, built } with built the rebuilt scene names.
================
*/
export async function censusGate( { key, committed, cache, build, digest, verdict = censusVerdict } ) {
	const found = cache.find( key );
	const record = found === undefined ? undefined : JSON.parse( found );
	const moved = record ? await movedScenes( record, digest ) : [];
	const built = record ? moved.map( ( [name] ) => name ) : undefined;
	let names = record?.names, scenes = record?.scenes;
	if ( !record || built.length > 0 ) {
		const parts = await build( built );
		if ( parts === undefined ) {
			return { code: 1, lines: [ "verify:ui-action-census: the census build failed" ], built: built ?? [] };
		}
		names = parts.names;
		scenes = { ...scenes };
		for ( const [name, part] of Object.entries( parts.scenes ) ) {
			const reads = [];
			for ( const file of part.reads ) reads.push( [ file, await digest( file ) ] );
			scenes[name] = { census: part.census, reads };
		}
		cache.record( key, JSON.stringify( { names, scenes } ) );
	}
	const judged = verdict( committed, censusText( censusTable( names, scenes ) ) );
	const source = !record ?
		`built (${names.length} scenes)` :
		moved.length === 0 ?
		"reused (build inputs unchanged)" :
		`rebuilt ${moved.length} of ${names.length} scenes (${moved.map( ( [n, f] ) => `${n}: ${f}` ).join( ", " )})`;
	return {
		code: judged.ok ? 0 : 1,
		lines: [ `verify:ui-action-census: table ${source}`, ...judged.lines ],
		built: built ?? names
	};
}

/*
================
publishedDigest

A digest function over the published tree: what the fixture sees of a
file (a PNG's size, census-reads.mjs; any other file's sha256 through
the gate's hash cache), MISSING_DIGEST for one the fixture could not
read (the census treats any read failure as absent).
================
*/
function publishedDigest( hashes ) {
	return async file => {
		try {
			if ( file.endsWith( ".png" ) ) return pngSizeDigest( CLIENT_PUBLIC_ROOT + file );
			return await hashes.hashFile( CLIENT_PUBLIC_ROOT + file );
		} catch {
			return MISSING_DIGEST;
		}
	};
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
	const hashes = await openFileHashCache( HASH_CACHE_PATH );
	const result = await censusGate( {
		key,
		committed,
		cache: {
			find: k => findResult( ACTION_CENSUS_STAMP, k ),
			record: ( k, text ) => recordResult( ACTION_CENSUS_STAMP, k, text )
		},
		build: buildParts,
		digest: publishedDigest( hashes )
	} );
	await hashes.save();
	for ( const line of result.lines ) (result.code === 0 ? console.log : console.error)( line );
	process.exitCode = result.code;
}

if ( process.argv[1] === import.meta.filename ) {
	main().catch( error => {
		console.error( error );
		process.exitCode = 1;
	} );
}
