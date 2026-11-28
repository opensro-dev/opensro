/*
===========================================================================

gameDataFreshness.test.mjs - a stale server projection is named, a
current one is not

staleReason driven by an injected git (scripts/checks/gameDataFreshness.mjs,
#4759): the projection's build commit against the working tree's producers.

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { PRODUCER_PATHSPECS, readSourceRevision, staleReason } from "../../checks/gameDataFreshness.mjs";

const BUILT = "481128124cf985f23e0607c102133d2b3a882bb1";

/*
================
fakeGit

A git that holds the commits in held and reports changed as the producer
diff; calls records every argument list it was given.
================
*/
function fakeGit( { held = [ BUILT ], changed = [], diffStatus = 0 } = {} ) {
	const calls = [];
	const git = ( args ) => {
		calls.push( args );
		if ( args[0] === "cat-file" ) {
			return { status: held.includes( args[2].replace( "^{commit}", "" ) ) ? 0 : 1, stdout: "" };
		}
		return { status: diffStatus, stdout: changed.map( ( file ) => file + "\n" ).join( "" ) };
	};
	return { git, calls };
}

test("a projection built before its producers changed is named with the changes", () => {
	const changed = [
		"scripts/build/server/buildServerGameDataBundle.mjs",
		"scripts/styria_import.py",
		"scripts/data/a.json",
		"scripts/lib/b.mjs"
	];
	const { git, calls } = fakeGit( { changed } );
	const reason = staleReason( BUILT, git );
	assert.equal(
		reason,
		"built from 481128124; 4 producer file(s) changed since (scripts/build/server/buildServerGameDataBundle.mjs, " +
			"scripts/styria_import.py, scripts/data/a.json and 1 more)"
	);
	assert.deepEqual( calls[1], [ "diff", "--name-only", BUILT, "--", ...PRODUCER_PATHSPECS ] );
});

test("a projection whose producers are unchanged is current", () => {
	assert.equal( staleReason( BUILT, fakeGit().git ), "" );
});

test("a build label that is not a commit cannot be judged and counts as current", () => {
	for ( const label of [ "", "dev", "workspace", BUILT.slice( 0, 9 ) ] ) {
		const { git, calls } = fakeGit( { changed: [ "scripts/x.py" ] } );
		assert.equal( staleReason( label, git ), "" );
		assert.equal( calls.length, 0 );
	}
});

test("a commit the clone does not hold, or a failed diff, is a reason", () => {
	assert.equal(
		staleReason( BUILT, fakeGit( { held: [] } ).git ),
		"built from 481128124, a commit this clone does not hold"
	);
	assert.equal(
		staleReason( BUILT, fakeGit( { diffStatus: 128 } ).git ),
		"built from 481128124; git could not compare its producers"
	);
});

test("the build revision is read from the projection's manifest", async () => {
	const root = await mkdtemp( path.join( os.tmpdir(), "freshness-" ) );
	try {
		assert.equal( readSourceRevision( root ), "" );
		await writeFile( path.join( root, "manifest.json" ), JSON.stringify( { sourceRevision: BUILT } ) );
		assert.equal( readSourceRevision( root ), BUILT );
		await writeFile( path.join( root, "manifest.json" ), "{ not json" );
		assert.equal( readSourceRevision( root ), "" );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
});
