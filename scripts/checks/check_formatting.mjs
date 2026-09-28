/*
===========================================================================

Formatting Ratchet Gate

dprint (dprint.json) owns the layout of every JavaScript and TypeScript file
in its includes, configured to the id Software house style: tabs, padded
parentheses, same-line braces. A file must be formatted unless it is listed
in format-baseline.txt, the ledger of files written before the formatter was
adopted. The ledger only shrinks:

- an unformatted file that is not in the ledger fails the gate;
- a ledger entry whose file is now formatted fails the gate, so the entry is
  removed in the same change that formatted it;
- a ledger entry whose file no longer exists fails the gate.

A file dprint cannot parse (for example one that is not valid UTF-8) counts
as unformatted.

`--update` rewrites the ledger to the entries that are still unformatted. It
never adds a file.

Format one file with `pnpm exec dprint fmt <path>`.

===========================================================================
*/

import { spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );
const baselinePath = path.join( rebuildRoot, "scripts", "checks", "format-baseline.txt" );
const dprintLauncher = path.join( rebuildRoot, "node_modules", "dprint", "bin.cjs" );
const BASELINE_HEADER = [
	"# Files written before dprint was adopted. This ledger may only shrink:",
	"# format a file, then delete its line. See scripts/checks/check_formatting.mjs."
];

/*
================
toRelativePath
================
*/
function toRelativePath( file ) {
	return path.relative( rebuildRoot, path.resolve( rebuildRoot, file ) ).split( path.sep ).join( "/" );
}

/*
================
unformattedFiles

Returns the set of files dprint would reformat or cannot format. Any other
failure (a configuration problem, a missing plugin) throws.
================
*/
function unformattedFiles() {
	const result = spawnSync(
		process.execPath,
		[ dprintLauncher, "check", "--list-different" ],
		{ windowsHide: true, cwd: rebuildRoot, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 }
	);
	if ( result.error ) {
		throw result.error;
	}

	const files = new Set();
	for ( const line of result.stdout.split( /\r?\n/ ) ) {
		if ( line.trim().length > 0 ) {
			files.add( toRelativePath( line.trim() ) );
		}
	}

	const unexplained = [];
	for ( const line of result.stderr.split( /\r?\n/ ) ) {
		const failed = /^Error formatting (.+?)\. Message: /.exec( line );
		if ( failed ) {
			files.add( toRelativePath( failed[1] ) );
		} else if ( line.trim().length > 0 && !/^(Compiling |Had \d+ error)/.test( line ) ) {
			unexplained.push( line );
		}
	}

	if ( unexplained.length > 0 ) {
		throw new Error( `dprint failed:\n${unexplained.join( "\n" )}` );
	}
	if ( result.status !== 0 && files.size === 0 ) {
		throw new Error( `dprint exited ${result.status} without naming a file:\n${result.stderr}` );
	}
	return files;
}

/*
================
readBaseline
================
*/
function readBaseline() {
	return readFileSync( baselinePath, "utf8" )
		.split( /\r?\n/ )
		.map( ( line ) => line.trim() )
		.filter( ( line ) => line.length > 0 && !line.startsWith( "#" ) );
}

/*
================
writeBaseline
================
*/
function writeBaseline( entries ) {
	const sorted = [ ...entries ].sort();
	writeFileSync( baselinePath, `${[ ...BASELINE_HEADER, ...sorted ].join( "\n" )}\n` );
}

/*
================
listProblem
================
*/
function listProblem( files, message ) {
	return `${files.length} ${message}:\n${files.map( ( file ) => `\t${file}` ).join( "\n" )}`;
}

/*
================
main
================
*/
function main() {
	const update = process.argv.includes( "--update" );
	const unformatted = unformattedFiles();
	const baseline = readBaseline();
	const listed = new Set( baseline );

	const unlisted = [ ...unformatted ].filter( ( file ) => !listed.has( file ) ).sort();
	const missing = baseline.filter( ( file ) => !existsSync( path.join( rebuildRoot, file ) ) );
	const formatted = baseline.filter( ( file ) =>
		existsSync( path.join( rebuildRoot, file ) ) && !unformatted.has( file )
	);

	if ( update ) {
		const kept = baseline.filter( ( file ) => unformatted.has( file ) );
		writeBaseline( kept );
		process.stdout.write(
			`format-baseline: ${baseline.length - kept.length} entries removed, ${kept.length} remain\n`
		);
	}

	const problems = [];
	if ( unlisted.length > 0 ) {
		problems.push( listProblem( unlisted, "file(s) are not formatted; run `pnpm exec dprint fmt <path>`" ) );
	}
	if ( !update && formatted.length > 0 ) {
		problems.push( listProblem(
			formatted,
			"baseline entr(ies) are already formatted; run `node scripts/checks/check_formatting.mjs --update`"
		) );
	}
	if ( !update && missing.length > 0 ) {
		problems.push( listProblem(
			missing,
			"baseline entr(ies) name missing files; run `node scripts/checks/check_formatting.mjs --update`"
		) );
	}

	if ( problems.length > 0 ) {
		process.stderr.write( `${problems.join( "\n\n" )}\n` );
		process.exit( 1 );
	}
	process.stdout.write(
		`format: ${unformatted.size} file(s) remain in the baseline ledger; no new unformatted files\n`
	);
}

main();
