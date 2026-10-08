/*
===========================================================================

check_generated_root.mjs - one owner for where the built asset tree lives

The repository-root generated tree (client-public, intermediate,
observatory) is resolved by scripts/lib/generatedRoot.mjs, its Python twin
scripts/sro_paths.py and its Go test twin testsupport/licensed. They resolve
the main checkout's tree (or SRO_GENERATED_ROOT), so a worktree reads the
shared build without a junction. A path built anywhere else ("../../.generated/client-public",
path.join( root, ".generated", "client-public" )) would ignore that and
bring the junctions back, so this gate refuses it.

The server's module-local apps/server/.generated and per-package .generated
folders are separate trees and do not match: the patterns require
client-public, intermediate or observatory right after .generated.

The same goes for the root variables themselves (SRO_GENERATED_ROOT,
SRO_SERVER_GAME_DATA_ROOT, SRO_GAME_ROOT): only the owners read them. A tool
that reads one directly resolves its own idea of the tree - a worktree's
empty copy, a stale junction - which is how a build worked on one machine
and failed on the next. Deployment scripts that set a variable for the
process they start are not readers.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );

// The owners themselves, and fixtures that test path handling on literals.
const ALLOWED = new Set( [
	"scripts/lib/generatedRoot.mjs",
	"scripts/sro_paths.py",
	"apps/server/internal/testsupport/licensed/licensed.go",
	"scripts/checks/check_generated_root.mjs",
	"scripts/test/pipeline/generatedRoot.test.mjs",
	// gameRelativePath() turns an absolute path into a game-relative one; its
	// fixture spells a generated path as data, not as a location to read.
	"scripts/test/pipeline/gameRelativePath.test.mjs"
] );

const SOURCE = /\.(?:mjs|cjs|js|ts|py|go|ps1)$/;

// The owners that read the root variables: the JS, Python and Go resolvers.
// Everything else asks them.
const ROOT_VARIABLE_OWNERS = new Set( [
	"scripts/lib/generatedRoot.mjs",
	"scripts/build/world/paths.mjs",
	"scripts/sro_paths.py",
	"apps/server/internal/testsupport/licensed/licensed.go",
	"apps/server/internal/gamedata/resolve.go",
	"scripts/checks/check_generated_root.mjs"
] );
const ROOT_VARIABLE = "SRO_(?:GENERATED_ROOT|SERVER_GAME_DATA_ROOT|GAME_ROOT)";
const ROOT_VARIABLE_READS = [
	// process.env.SRO_GENERATED_ROOT, env[ "SRO_GAME_ROOT" ], process.env?.SRO_...
	new RegExp( "\\benv\\??\\.?\\s*(?:\\.\\s*|\\[\\s*[\"'`])" + ROOT_VARIABLE + "\\b" ),
	// Python: os.environ["SRO_..."], os.environ.get("SRO_..."), os.getenv("SRO_...")
	new RegExp( "\\bos\\.(?:environ(?:\\.get)?|getenv)\\s*[\\(\\[]\\s*[\"']" + ROOT_VARIABLE + "[\"']" ),
	// Go: os.Getenv("SRO_..."), os.LookupEnv("SRO_...")
	new RegExp( '\\bos\\.(?:Getenv|LookupEnv)\\(\\s*"' + ROOT_VARIABLE + '"' )
];

// The tree's own sub-roots: what a hard-coded root path always names next.
const SUBTREE = "(?:client-public|intermediate|observatory)";
const PATTERNS = [
	// "../../.generated/client-public/...", '.generated/intermediate'
	new RegExp( "[\"'`](?:\\.{1,2}/)*\\.generated/" + SUBTREE ),
	// path.join( root, ".generated", "client-public" ), filepath.Join(..., ".generated", "client-public")
	new RegExp( "[\"'`]\\.generated[\"'`]\\s*,\\s*[\"'`]" + SUBTREE + "[\"'`]" ),
	// Python: REPO_ROOT / ".generated" / "client-public"
	new RegExp( "/\\s*[\"']\\.generated[\"']\\s*/\\s*[\"']" + SUBTREE + "[\"']" )
];

/*
================
findGeneratedPaths

The lines of one source file that build a generated-tree path themselves.
Comment lines are prose and do not count.
================
*/
export function findGeneratedPaths( text ) {
	const hits = [];
	const lines = text.split( "\n" );
	for ( let i = 0; i < lines.length; i++ ) {
		const line = lines[i];
		if ( /^\s*(?:\/\/|\/\*|\*|#)/.test( line ) ) continue;
		if ( PATTERNS.some( pattern => pattern.test( line ) ) ) hits.push( { line: i + 1, text: line.trim() } );
	}
	return hits;
}

/*
================
findRootVariableReads

The lines of one source file that read a root variable directly.
================
*/
export function findRootVariableReads( text ) {
	const hits = [];
	const lines = text.split( "\n" );
	for ( let i = 0; i < lines.length; i++ ) {
		const line = lines[i];
		if ( /^\s*(?:\/\/|\/\*|\*|#)/.test( line ) ) continue;
		if ( ROOT_VARIABLE_READS.some( pattern => pattern.test( line ) ) ) {
			hits.push( { line: i + 1, text: line.trim() } );
		}
	}
	return hits;
}

/*
================
trackedSources
================
*/
function trackedSources() {
	return execFileSync( "git", [ "ls-files", "-z", "--", "apps", "scripts" ], { cwd: rebuildRoot, encoding: "utf8" } )
		.split( "\0" )
		.filter( file => file && SOURCE.test( file ) && !ALLOWED.has( file ) );
}

/*
================
main
================
*/
function main() {
	const violations = [];
	for ( const file of trackedSources() ) {
		let text;
		try {
			text = readFileSync( path.join( rebuildRoot, file ), "utf8" );
		} catch {
			continue; // listed but deleted in the working tree
		}
		for ( const hit of findGeneratedPaths( text ) ) violations.push( `${file}:${hit.line}: ${hit.text}` );
		if ( !ROOT_VARIABLE_OWNERS.has( file ) && !file.includes( "/test/" ) && !file.includes( "/tests/" ) ) {
			for ( const hit of findRootVariableReads( text ) ) {
				violations.push( `${file}:${hit.line}: reads a root variable: ${hit.text}` );
			}
		}
	}
	if ( violations.length ) {
		console.error( "generated-root: these build a generated-tree path or read a root variable outside its owner." );
		console.error(
			"Use scripts/lib/generatedRoot.mjs (JS), sro_paths.py (Python) or licensed.ClientPublicRoot (Go):"
		);
		for ( const violation of violations ) console.error( "  " + violation );
		process.exit( 1 );
	}
	console.log( "generated-root: every generated-tree path resolves through its owner" );
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) main();
