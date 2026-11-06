/*
===========================================================================

check_old_tab.mjs - a pull request that changes what a tab decodes says why

The release train's old-tab audit refuses to compose a release whose commits
change a client decoder, a server encoder line, or server state a browser
receives, unless each squash commit carries an "Old-tab: safe - <reason>"
trailer. That judgement used to happen at release time, long after the
change was reviewed. This gate moves it to the pull request (opensro-ops
#116, piece H): the same reading of the diff, vendored from the audit's
old_tab_files in the ops repo's scripts/release.py, and the same two ways to
answer it - declare it safe with a reason, or bump the protocol, which sends
old tabs a 426 and asks nothing. The audit still lists every commit that
touches the server; it now leaves a client-only decoder commit to this gate.

The gate reads the diff and the pull request's body, so it runs from its own
workflow, not from `pnpm check source`. The body is data: it is only parsed.

===========================================================================
*/

import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", ".." );

// Server lines that write bytes a browser decodes: encoder calls and frame
// literals, matched as calls rather than by path (a record encoder names no
// wire package). Added and removed lines alike: a removed write drops a
// field an old strict decoder still reads.
const SERVER_WIRE_LINE =
	/NewWriter\(|\.(?:U8|U16|U32|U64|I8|I16|I32|I64|F32|F64|Bool|Bytes|Str\w*)\(|\bEncode\w*\(|\bOpcode:|wire\.Frame\{|\bPayload:/;

// Server trees whose state reaches a browser. A value change there can alter
// an existing record without touching an encoder line, so no pattern can
// clear them: every change is declared.
const SERVER_VISIBLE_ROOTS = [
	"apps/server/internal/game/",
	"apps/server/internal/transport/",
	"apps/server/internal/agent/",
	// The game-data bundle encoder: its bytes reach the browser with the
	// client's data.
	"apps/server/internal/gamedata/"
];

// Client trees whose changes alter what the live bundle would decode, from
// the simulation worker's own imports.
const CLIENT_DECODER_ROOTS = [
	// The worker: every packet handler and the frame codec.
	"apps/client-next/src/engine/runtime/simulation/worker/",
	// Every foundation module it imports decodes or validates what it is
	// handed, the announced protocol among them.
	"apps/client-next/src/engine/foundation/",
	// The shapes those decoders produce.
	"apps/client-next/src/engine/contracts/"
];

// The audit's own trailer: the hyphen is literal, so an em dash in the body
// does not become a declaration by accident.
const OLD_TAB_SAFE = /^Old-tab: safe - \S/m;
const OLD_TAB_BUMP = /^Old-tab: bump\b/m;

/*
================
oldTabFiles

What one unified-0 patch changes that an open tab could notice, as
{ wire, visible }: wire are server files with an added or removed encoder
line (a deleted file included) and client decoder files, the changes to
read first; visible are the other server files whose state a browser
receives. Tests are neither. The reading is the release audit's, line for
line; if the two ever disagree, the audit is the one to fix this from.
================
*/
export function oldTabFiles( patch ) {
	const wire = [], visible = [];
	let file = null, removed = null;
	for ( const line of patch.split( "\n" ) ) {
		if ( line.startsWith( "--- " ) ) {
			// A deleted file's path is only on its --- line (+++ /dev/null).
			removed = line.startsWith( "--- a/" ) ? line.slice( 6 ) : null;
			continue;
		}
		if ( line.startsWith( "+++ " ) ) {
			file = line.startsWith( "+++ b/" ) ? line.slice( 6 ) : removed;
			continue;
		}
		if ( file === null || wire.includes( file ) || !(line.startsWith( "+" ) || line.startsWith( "-" )) ) continue;
		if ( file.startsWith( "apps/server/" ) && file.endsWith( ".go" ) && !file.endsWith( "_test.go" ) ) {
			const code = line.slice( 1 ).trim();
			// A comment or blank line changes nothing a browser receives.
			if ( !code || code.startsWith( "//" ) || code.startsWith( "/*" ) || code.startsWith( "*" ) ) continue;
			if ( SERVER_WIRE_LINE.test( code ) ) {
				wire.push( file );
				const at = visible.indexOf( file );
				if ( at >= 0 ) visible.splice( at, 1 );
			} else if ( SERVER_VISIBLE_ROOTS.some( root => file.startsWith( root ) ) && !visible.includes( file ) ) {
				visible.push( file );
			}
		} else if (
			CLIENT_DECODER_ROOTS.some( root => file.startsWith( root ) ) && file.endsWith( ".ts" ) &&
			!file.includes( ".test." )
		) {
			wire.push( file );
		}
	}
	return { wire, visible };
}

/*
================
evaluateOldTab

Plain data in, plain verdict out: the patch text and the pull request's
body, so a test needs no git and no pull request.
================
*/
export function evaluateOldTab( { patch, body } ) {
	const { wire, visible } = oldTabFiles( patch );
	const listed = wire.length + visible.length;
	const ok = listed === 0 || OLD_TAB_SAFE.test( body ) || OLD_TAB_BUMP.test( body );
	return { wire, visible, ok };
}

/*
================
main
================
*/
function main() {
	const base = process.env.PR_BASE_SHA, body = process.env.PR_BODY ?? "";
	if ( !base ) throw new Error( "PR_BASE_SHA is required" );
	const diff = execFileSync(
		"git",
		[ "diff", "--unified=0", "--no-ext-diff", `${base}...HEAD`, "--", "apps/server", "apps/client-next/src" ],
		{
			cwd: rebuildRoot,
			encoding: "utf8",
			maxBuffer: 1 << 30
		}
	);
	const verdict = evaluateOldTab( { patch: diff, body } );
	if ( verdict.ok ) {
		const listed = verdict.wire.length + verdict.visible.length;
		console.log(
			`old tab: ${listed} listed files, ${listed === 0 ? "no declaration needed" : "declaration found"}`
		);
		return;
	}
	console.error( "This pull request changes what an open tab could notice, but its body declares neither:" );
	for ( const file of verdict.wire ) console.error( `  wire ${file}` );
	for ( const file of verdict.visible ) console.error( `  ${file}` );
	console.error( 'Add "Old-tab: safe - <why the live bundle still decodes it>" to the pull request body,' );
	console.error( 'or "Old-tab: bump" if the change moves the release protocol instead.' );
	process.exitCode = 1;
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) main();
