/*
===========================================================================

resourceFingerprintServer.test.mjs - the server's exports key the resource fingerprint

Split from resourceBuildGraph.test.mjs so its three full fingerprints run
beside that file's two instead of after them (#4337).

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { computeResourceBuildFingerprint, movedRoots } from "../../build/shared/resourceBuildFingerprint.mjs";
import { STUB_FINGERPRINT_KEYS } from "../helpers/fingerprintKeys.mjs";

test("the server's exports, not its Go sources, key the resource fingerprint", async () => {
	// A server change that leaves every export's bytes alone keeps the
	// fingerprint; one that moves an export misses it (#4337).
	const same = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	const again = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	const moved = await computeResourceBuildFingerprint( { serverExports: () => "a moved roster" } );
	assert.equal( again.hash, same.hash );
	assert.notEqual( moved.hash, same.hash );
	assert.deepEqual(
		movedRoots( same, moved ).filter( ( label ) => !label.startsWith( "code:" ) ),
		[ "server-exports" ]
	);
});
