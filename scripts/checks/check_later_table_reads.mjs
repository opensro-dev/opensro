/*
===========================================================================

check_later_table_reads.mjs - one reader for a later client's textdata

The later clients split their big textdata tables into shards behind an
index of the old name (v1.696 23 of them, VSRO textdataname, textquest
and skilldataenc). An index read as a table is one-cell rows of file
names, and the importers' width filters drop them without a word: the
shape of #4730, where Karkadann lost its presentation and the fortresses
their VSRO names. scripts/later_tables.py owns the read: Archive follows
an index in rows, rows_of and text, and its one-file read refuses an
index.

So this gate refuses, in every tracked Python file under scripts/ but the
owner, each shape of a textdata read that goes around it (#4761):

	a payload or read_vsro_file read naming TEXTDATA
	an archive-index lookup or membership test on TEXTDATA + name
	the retired owner form table_text(media, index, name)
	a walk of the archive's textdata names by prefix (startswith(TEXTDATA)
	or startswith("textdata_"): an index, not a prefix, names the files
	a textdata path spelled as a literal ("server_dep/silkroad/textdata/...")
	Archive's private members (._read_file, ._has_file)

Comment lines and docstrings are prose and do not count. Writing an
archive (a test fixture's {TEXTDATA + name: bytes}) is not a read and
does not match.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );

// The owner, and the stage-1 module that defines TEXTDATA (its one line
// spells the path the constant names).
const OWNER = "scripts/later_tables.py";
const TEXTDATA_DEFINITION = /^TEXTDATA\s*=\s*["']server_dep\/silkroad\/textdata\/["']\s*$/;

const TEXTDATA = "\\b(?:\\w+\\.)?TEXTDATA\\b";
const RULES = [
	{ why: "a payload read naming TEXTDATA", pattern: new RegExp( "\\bpayload\\s*\\(.*" + TEXTDATA ) },
	{ why: "a read_vsro_file read naming TEXTDATA", pattern: new RegExp( "\\bread_vsro_file\\s*\\(.*" + TEXTDATA ) },
	{
		why: "an index lookup on TEXTDATA + name",
		pattern: new RegExp( "\\w*index\\w*\\s*(?:\\.get\\s*\\(|\\[)\\s*\\(?\\s*" + TEXTDATA )
	},
	{
		why: "an index membership test on TEXTDATA + name",
		pattern: new RegExp( TEXTDATA + "\\s*\\+[^#]*\\bin\\s+\\w*index\\w*\\b" )
	},
	{
		why: "the retired owner form table_text(media, index, name)",
		pattern: /\btable_text\s*\(\s*[\w.]+\s*,\s*[\w.]+\s*,/
	},
	{ why: "a walk of textdata names by prefix", pattern: new RegExp( "\\.startswith\\s*\\(\\s*\\(?\\s*" + TEXTDATA ) },
	{ why: "a walk of a table's files by name prefix", pattern: /\.startswith\s*\(\s*["']textdata_/i },
	{ why: "a textdata path literal", pattern: /["']server_dep\/silkroad\/textdata\//i },
	{ why: "Archive's private members", pattern: /\._(?:read_file|has_file)\b/ }
];

/*
================
codeLines

The lines of one Python file that are code: comment lines and the body
of every triple-quoted string (the file banner, function docstrings)
are prose.
================
*/
function codeLines( text ) {
	const lines = [];
	let quote = null;
	text.split( "\n" ).forEach( ( line, index ) => {
		const trimmed = line.trim();
		if ( quote ) {
			if ( trimmed.includes( quote ) ) quote = null;
			return;
		}
		if ( trimmed.startsWith( "#" ) ) return;
		const opening = trimmed.match( /^(?:[rbuf]{0,2})("""|''')/i );
		if ( opening ) {
			const rest = trimmed.slice( opening[0].length );
			if ( !rest.includes( opening[1] ) ) quote = opening[1];
			return;
		}
		lines.push( { line: index + 1, text: line } );
	} );
	return lines;
}

/*
================
findLaterTableReads

The lines of one Python file that read later textdata around its owner,
with the rule each breaks.
================
*/
export function findLaterTableReads( text ) {
	const hits = [];
	for ( const { line, text: source } of codeLines( text ) ) {
		if ( TEXTDATA_DEFINITION.test( source.trim() ) ) continue;
		const code = source.replace( /\s+#.*$/, "" );
		const rule = RULES.find( candidate => candidate.pattern.test( code ) );
		if ( rule ) hits.push( { line, text: source.trim(), why: rule.why } );
	}
	return hits;
}

/*
================
trackedPython
================
*/
function trackedPython() {
	return execFileSync( "git", [ "ls-files", "-z", "--", "scripts" ], { cwd: rebuildRoot, encoding: "utf8" } )
		.split( "\0" )
		.filter( file => file.endsWith( ".py" ) && file !== OWNER );
}

/*
================
main
================
*/
function main() {
	const violations = [];
	for ( const file of trackedPython() ) {
		let text;
		try {
			text = readFileSync( path.join( rebuildRoot, file ), "utf8" );
		} catch {
			continue; // listed but deleted in the working tree
		}
		for ( const hit of findLaterTableReads( text ) ) {
			violations.push( `${file}:${hit.line}: ${hit.why}: ${hit.text}` );
		}
	}
	if ( violations.length ) {
		console.error( "later-table-reads: these read later textdata around scripts/later_tables.py." );
		console.error( "Read through later_tables.Archive (rows, rows_of, text, files, file):" );
		for ( const violation of violations ) console.error( "  " + violation );
		process.exit( 1 );
	}
	console.log( "later-table-reads: every later textdata read goes through later_tables.Archive" );
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) main();
