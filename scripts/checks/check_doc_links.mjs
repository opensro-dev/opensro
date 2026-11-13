/*
===========================================================================

check_doc_links.mjs - every Markdown link a reader can follow lands

Two rules, both about links a person or an agent clicks:

1. A relative link in a tracked Markdown file names a file or folder that
   exists in the checkout (an anchor after `#` is not checked).
2. The community files GitHub shows on the repository's home page
   (.github/CONTRIBUTING.md, SECURITY.md, SUPPORT.md, CODE_OF_CONDUCT.md)
   use absolute links. The home page's tabs resolve a relative link against
   the repository root without the branch, so `../docs/native/RIZIN.md`
   became /blob/docs/native/RIZIN.md there: a 404 every new contributor
   met first, while the same file opened from the tree worked.

Fenced code blocks are skipped: their text is an example, not a link.

===========================================================================
*/

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../.." );
export const REPO_BLOB = "https://github.com/opensro-dev/opensro/blob/main/";
export const HOME_PAGE_FILES = Object.freeze( [
	".github/CONTRIBUTING.md",
	".github/SECURITY.md",
	".github/SUPPORT.md",
	".github/CODE_OF_CONDUCT.md"
] );
const SCHEME = /^[a-z][a-z0-9+.-]*:/i;

/*
================
markdownLinks

The inline link targets of one Markdown text, outside fenced blocks, with
their 1-based line numbers.
================
*/
export function markdownLinks( text ) {
	const links = [];
	let fenced = false;
	text.split( "\n" ).forEach( ( line, index ) => {
		if ( /^\s*(```|~~~)/.test( line ) ) {
			fenced = !fenced;
			return;
		}
		if ( fenced ) return;
		for ( const match of line.matchAll( /\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)/g ) ) {
			links.push( { target: match[1], line: index + 1 } );
		}
	} );
	return links;
}

/*
================
linkProblems

The problems in one file's links. `exists` answers whether a repo-relative
path exists, so a test can supply its own tree.
================
*/
export function linkProblems( file, text, exists ) {
	const problems = [];
	const homePage = HOME_PAGE_FILES.includes( file );
	for ( const { target, line } of markdownLinks( text ) ) {
		if ( target.startsWith( "#" ) || SCHEME.test( target ) ) continue;
		if ( homePage ) {
			problems.push(
				`${file}:${line}: ${target} is relative; the home page resolves it without the branch (use ${REPO_BLOB}...)`
			);
			continue;
		}
		const pathPart = decodeURIComponent( target.split( "#" )[0] );
		const resolved = pathPart.startsWith( "/" ) ?
			pathPart.slice( 1 ) :
			path.posix.normalize( path.posix.join( path.posix.dirname( file ), pathPart ) );
		if ( resolved.startsWith( "../" ) || !exists( resolved ) ) {
			problems.push( `${file}:${line}: ${target} does not exist (${resolved})` );
		}
	}
	return problems;
}

/*
================
main
================
*/
function main() {
	const files = execFileSync( "git", [ "ls-files", "-z", "--", "*.md" ], { cwd: REPO_ROOT, encoding: "utf8" } )
		.split( "\0" )
		.filter( Boolean );
	const exists = ( relative ) => existsSync( path.join( REPO_ROOT, relative ) );
	const problems = files.flatMap( ( file ) =>
		linkProblems( file, readFileSync( path.join( REPO_ROOT, file ), "utf8" ), exists )
	);
	if ( problems.length > 0 ) {
		console.error( problems.join( "\n" ) );
		console.error( `doc links: ${problems.length} broken link(s) in ${files.length} Markdown file(s)` );
		process.exit( 1 );
	}
	console.log( `doc links: ${files.length} Markdown file(s), every link lands` );
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) main();
