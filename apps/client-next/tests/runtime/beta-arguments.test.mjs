/*
===========================================================================

beta-arguments.test.mjs - tools/beta/build.mjs's command line

The release build passes --no-archive: it reads only <destination>/package,
and client-beta.tar (4.18 GB) cost about two minutes of every release
build with no reader (#4337).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";

const { betaArguments } = await import( "../../tools/beta/build.mjs" );

test("the release's --no-archive skips the archive; the default keeps it", () => {
	const destination = path.resolve( "out" );
	assert.deepEqual( betaArguments( [ "out", "--no-archive" ] ), { destination, makeArchive: false } );
	assert.deepEqual( betaArguments( [ "out" ] ), { destination } );
	assert.deepEqual( betaArguments( [] ), {} );
	assert.deepEqual(
		betaArguments( [ "out", "--source", "snap", "--no-archive" ] ),
		{ destination, sourceSnapshot: path.resolve( "snap" ), makeArchive: false }
	);
	assert.throws( () => betaArguments( [ "out", "--archive-please" ] ), /unknown argument --archive-please/ );
});
