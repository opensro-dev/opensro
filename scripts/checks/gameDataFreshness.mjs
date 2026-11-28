/*
===========================================================================

gameDataFreshness.mjs - is the server game-data projection older than
the pipeline that produces it

The Go server's game-data tests read the projection built once into the
main checkout (apps/server/.generated/game-data/1.150/server) and shared
by every worktree. Present is not current: a tree built weeks ago lacks
content newer tests expect, and those tests then fail with a content
message ("reward item ... is not in the item data") that reads as a
broken branch (#4759).

The projection's manifest records the commit it was built from
(sourceRevision, buildServerGameDataBundle.mjs). staleReason compares that
commit with the working tree over the paths that produce game data
(PRODUCER_PATHSPECS) and returns why the tree is behind, or "" when it is
current or cannot be judged. check_go_server.mjs passes the reason to the
tests in SRO_GAME_DATA_STALE, where clientdata.RequireGameData skips (or,
under SRO_REQUIRE_GAME_DATA=1, fails) on it.

The comparison is by producer, not by content: a producer change that
leaves the data's bytes as they were still counts, because only a rebuild
can show that it did.

===========================================================================
*/
import { readFileSync } from "node:fs";
import path from "node:path";

// The paths whose changes can change the built game data: the importers
// at the top of scripts/, the asset pipeline, its data tables and their
// shared libraries. Tests, gates, the task runner and local tooling never
// write game data.
export const PRODUCER_PATHSPECS = [
	":(glob)scripts/*.py",
	":(glob)scripts/*.mjs",
	"scripts/build",
	"scripts/data",
	"scripts/lib"
];

// A full commit name, as buildServerGameDataBundle.mjs records it from git.
const COMMIT_NAME = /^[0-9a-f]{40}$/;

// The changed producers a reason names before "and N more".
const NAMED_PRODUCERS = 3;

/*
================
readSourceRevision

The sourceRevision of the projection's manifest, or "" when there is no
manifest (absent data is RequireGameData's own case) or it names none.
================
*/
export function readSourceRevision( projectionRoot ) {
	try {
		const manifest = JSON.parse( readFileSync( path.join( projectionRoot, "manifest.json" ), "utf8" ) );
		return typeof manifest.sourceRevision === "string" ? manifest.sourceRevision : "";
	} catch {
		return "";
	}
}

/*
================
staleReason

Why a projection built from revision is older than the working tree's
producers, or "" when it is not. git(args) runs git in the repository and
returns { status, stdout }. A revision that is not a commit name ("dev",
"workspace") cannot be judged and counts as current; a commit this clone
does not hold cannot be compared, which is itself a reason.
================
*/
export function staleReason( revision, git ) {
	if ( !COMMIT_NAME.test( revision ) ) {
		return "";
	}
	const short = revision.slice( 0, 9 );
	if ( git( [ "cat-file", "-e", `${revision}^{commit}` ] ).status !== 0 ) {
		return `built from ${short}, a commit this clone does not hold`;
	}
	const changed = git( [ "diff", "--name-only", revision, "--", ...PRODUCER_PATHSPECS ] );
	if ( changed.status !== 0 ) {
		return `built from ${short}; git could not compare its producers`;
	}
	const files = changed.stdout.split( /\r?\n/ ).filter( ( line ) => line.length > 0 );
	if ( files.length === 0 ) {
		return "";
	}
	const named = files.slice( 0, NAMED_PRODUCERS ).join( ", " );
	const more = files.length > NAMED_PRODUCERS ? ` and ${files.length - NAMED_PRODUCERS} more` : "";
	return `built from ${short}; ${files.length} producer file(s) changed since (${named}${more})`;
}
