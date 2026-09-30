/*
===========================================================================

application-release.test.mjs - coordinated application builds over retained data

Exercise the real build and manifest identity. A protocol change must keep
the verified data inventory, while an asset schema change requires new data.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { buildApplicationRelease } from "../../tools/beta/application-release.mjs";
import { freezeSource } from "../../tools/beta/build.mjs";
import { releaseIdentity, sha } from "../../tools/beta/policy.mjs";
import { ASSET_SCHEMA, RELEASE_PROTOCOL } from "../../src/engine/foundation/release/protocol.ts";

/*
================
baseManifest

Use an immutable data entry and an older application's protocol to model
the production input to a coordinated application-only release.
================
*/
function baseManifest( assetSchema = ASSET_SCHEMA ) {
	const manifest = {
		format: "sro-beta-release-v1",
		protocol: RELEASE_PROTOCOL - 1,
		assetSchema,
		sourceHash: sha( "old-source" ),
		files: [ { path: "payload/retained", length: 2, sha256: sha( "{}" ), kind: "data" } ],
		routes: [ {
			url: "/assets/retained.json",
			file: "payload/retained",
			offset: 0,
			length: 2,
			mime: "application/json"
		} ]
	};
	return { ...manifest, releaseId: releaseIdentity( manifest ) };
}

test("application release declares the compiled protocol and preserves verified data", async context => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-application-release-" ) );
	context.after( () => rm( root, { recursive: true, force: true } ) );
	const manifest = baseManifest();
	const result = await buildApplicationRelease( {
		manifest,
		source: await freezeSource(),
		destination: path.join( root, "build" )
	} );
	assert.equal( result.manifest.protocol, RELEASE_PROTOCOL );
	assert.equal( result.manifest.assetSchema, ASSET_SCHEMA );
	assert.equal( result.manifest.releaseId, releaseIdentity( result.manifest ) );
	assert.notEqual( result.manifest.releaseId, manifest.releaseId );
	assert.deepEqual( result.manifest.files.filter( row => row.kind !== "application" ), manifest.files );
	assert.deepEqual( result.manifest.routes.filter( row => !row.file.startsWith( "application/" ) ), manifest.routes );
});

test("application release rejects a different data schema before creating output", async () => {
	await assert.rejects(
		buildApplicationRelease( {
			manifest: baseManifest( ASSET_SCHEMA + 1 ),
			source: {},
			destination: "must-not-be-created"
		} ),
		/schema.*data release/
	);
});
