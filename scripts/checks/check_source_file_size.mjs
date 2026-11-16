/*
===========================================================================

Maintained Source File Size Gate

No maintained source file may exceed 1000 physical lines. This is a review smoke
alarm, not an architecture score. A file over the boundary must be separated by
a coherent owner, lifecycle, or responsibility family. Mechanical line-range
splitting does not satisfy it.

This gate is the single owner of the size policy for every language in apps/
and scripts/, the Go server included; there is no second list elsewhere.

size-baseline.txt holds two kinds of entry, both as `path # reason`:

- debt: most JavaScript and TypeScript predates the formatter and is
  minified, which hides its real size; these files exceed the limit once
  formatted and are waiting for a split;
- exemptions: a formatted file over the limit whose split would break one
  cohesive owner apart. The reason says why no responsibility boundary exists.

The ledger only shrinks:

- a file over the limit that is not in the ledger fails the gate;
- a ledger entry whose file no longer exists fails the gate;
- a ledger entry whose file is formatted (no longer in format-baseline.txt)
  and within the limit fails the gate, so the entry is removed in the same
  change that split the file.

`--update` removes the entries that no longer apply. It never adds a file.

===========================================================================
*/

import { readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { compareText } from "../build/shared/textOrder.mjs";

const MAX_SOURCE_LINES = 1000;
const LEDGER_HEADER_LINES = 3;
const rebuildRoot = path.resolve(
	path.dirname( fileURLToPath( import.meta.url ) ),
	"..",
	".."
);

const checksDirectory = path.join( rebuildRoot, "scripts", "checks" );
const sizeBaselinePath = path.join( checksDirectory, "size-baseline.txt" );
const formatBaselinePath = path.join( checksDirectory, "format-baseline.txt" );
// The asset pipeline's source lives in scripts/build, which the build-output
// rule below would otherwise skip.
const pipelineSourceDirectory = path.join( rebuildRoot, "scripts", "build" );
const SOURCE_ROOTS = [
	"apps",
	"scripts"
];

const SOURCE_EXTENSIONS = new Set( [
	".c",
	".cc",
	".cpp",
	".cjs",
	".css",
	".cts",
	".go",
	".h",
	".hpp",
	".inl",
	".js",
	".jsx",
	".mjs",
	".mts",
	".ps1",
	".psm1",
	".py",
	".scss",
	".sh",
	".ts",
	".tsx"
] );

// testdata and vendor hold fixtures and upstream code whose size is set by
// the data they preserve or by their owner, not by this repository.
const EXCLUDED_DIRECTORIES = new Set( [
	"__pycache__",
	"bin",
	"logs",
	"node_modules",
	"obj",
	"scratch",
	"temp",
	"testdata",
	"vendor"
] );

/*
================
isGeneratedDirectory
================
*/
function isGeneratedDirectory( name ) {
	return (
		EXCLUDED_DIRECTORIES.has( name ) ||
		name.startsWith( "." ) ||
		name.startsWith( "build" ) ||
		name.startsWith( "dist" )
	);
}

/*
================
physicalLineCount
================
*/
function physicalLineCount( text ) {
	if ( text.length === 0 ) {
		return 0;
	}

	const lines = text.split( /\r\n|\n|\r/ );
	return /(?:\r\n|\n|\r)$/.test( text ) ? lines.length - 1 : lines.length;
}

/*
================
collectSourceFiles
================
*/
function collectSourceFiles( directory, files ) {
	for ( const entry of readdirSync( directory, { withFileTypes: true } ) ) {
		const absolute = path.join( directory, entry.name );
		if ( entry.isDirectory() && isGeneratedDirectory( entry.name ) && absolute !== pipelineSourceDirectory ) {
			continue;
		}

		if ( entry.isDirectory() ) {
			collectSourceFiles( absolute, files );
			continue;
		}
		if ( !entry.isFile() || !SOURCE_EXTENSIONS.has( path.extname( entry.name ).toLowerCase() ) ) {
			continue;
		}
		if ( entry.name.endsWith( ".d.ts" ) ) {
			continue;
		}
		files.push( absolute );
	}
}

/*
================
readLedger

Returns the non-comment entries of a ledger file, keyed by path, with any
trailing `# reason` kept as the value.
================
*/
function readLedger( ledgerPath ) {
	const entries = new Map();

	for ( const line of readFileSync( ledgerPath, "utf8" ).split( /\r?\n/ ) ) {
		const trimmed = line.trim();
		if ( trimmed.length === 0 || trimmed.startsWith( "#" ) ) {
			continue;
		}
		const reasonStart = trimmed.indexOf( " #" );
		const file = reasonStart === -1 ? trimmed : trimmed.slice( 0, reasonStart ).trim();
		entries.set( file, reasonStart === -1 ? "" : trimmed.slice( reasonStart ) );
	}
	return entries;
}

/*
================
writeSizeLedger

Keeps the header comment and rewrites the surviving entries in sorted order.
================
*/
function writeSizeLedger( entries ) {
	const header = readFileSync( sizeBaselinePath, "utf8" ).split( /\r?\n/ ).slice( 0, LEDGER_HEADER_LINES );
	const lines = [ ...entries.keys() ].sort().map( ( file ) => `${file}${entries.get( file )}` );
	writeFileSync( sizeBaselinePath, `${[ ...header, ...lines ].join( "\n" )}\n` );
}

/*
================
main
================
*/
function main() {
	const update = process.argv.includes( "--update" );
	const ledger = readLedger( sizeBaselinePath );
	const unformatted = new Set( readLedger( formatBaselinePath ).keys() );
	const files = [];

	for ( const root of SOURCE_ROOTS ) {
		const absolute = path.resolve( rebuildRoot, root );
		if ( !statSync( absolute, { throwIfNoEntry: false } )?.isDirectory() ) {
			throw new Error( `maintained source root is missing: ${root}` );
		}
		collectSourceFiles( absolute, files );
	}

	const oversized = [];
	const lineCounts = new Map();
	let largest = { lines: 0, relativePath: "" };

	for ( const absolute of files ) {
		const lines = physicalLineCount( readFileSync( absolute, "utf8" ) );
		const relativePath = path.relative( rebuildRoot, absolute ).split( path.sep ).join( "/" );

		lineCounts.set( relativePath, lines );
		if ( lines > largest.lines ) {
			largest = { lines, relativePath };
		}
		if ( lines > MAX_SOURCE_LINES && !ledger.has( relativePath ) ) {
			oversized.push( { lines, relativePath } );
		}
	}

	// An entry is stale once its file is gone, or formatted and within the limit.
	const stale = [ ...ledger.keys() ].filter( ( file ) => {
		const lines = lineCounts.get( file );
		return lines === undefined || (!unformatted.has( file ) && lines <= MAX_SOURCE_LINES);
	} );
	if ( update ) {
		for ( const file of stale ) {
			ledger.delete( file );
		}
		writeSizeLedger( ledger );
		process.stdout.write( `source-size: removed ${stale.length} ledger entries\n` );
		return;
	}
	if ( stale.length > 0 ) {
		process.stderr.write(
			`${stale.length} size-baseline.txt entries no longer apply (missing, or formatted and within ` +
				`${MAX_SOURCE_LINES} lines):\n${stale.map( ( file ) => `\t${file}` ).join( "\n" )}\n` +
				"Run `node scripts/checks/check_source_file_size.mjs --update`.\n"
		);
		process.exit( 1 );
	}

	oversized.sort( ( left, right ) =>
		right.lines - left.lines || compareText( left.relativePath, right.relativePath )
	);
	if ( oversized.length > 0 ) {
		for ( const file of oversized ) {
			process.stderr.write(
				`${file.relativePath}: ${file.lines} lines (${file.lines - MAX_SOURCE_LINES} over limit)\n`
			);
		}
		process.stderr.write(
			`\n${oversized.length} maintained source file(s) exceed ${MAX_SOURCE_LINES} physical lines ` +
				"and are not in size-baseline.txt. Split them on a real responsibility boundary.\n"
		);
		process.exit( 1 );
	}

	process.stdout.write(
		`source-size: ${files.length} maintained files, none over ${MAX_SOURCE_LINES} lines outside ` +
			`the ledger (${ledger.size} entries); ` +
			`largest is ${largest.relativePath} (${largest.lines})\n`
	);
}

main();
