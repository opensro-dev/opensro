/*
===========================================================================

application-release.test.mjs - coordinated application builds over retained data

Exercise the real build and manifest identity. A protocol change must keep
the verified data inventory. A changed asset schema or missing browser-loaded
native route requires new data before compilation creates any output.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { buildApplicationRelease } from "../../tools/beta/application-release.mjs";
import { freezeSource } from "../../tools/beta/build.mjs";
import { releaseIdentity, sha } from "../../tools/beta/policy.mjs";
import { ASSET_SCHEMA, RELEASE_PROTOCOL } from "../../src/engine/foundation/release/protocol.ts";
import { nativeAssetUrls, cursorAssetUrl } from "../../src/engine/foundation/assets/native-assets.ts";

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
		routes: [
			{
				url: "/assets/retained.json",
				file: "payload/retained",
				offset: 0,
				length: 2,
				mime: "application/json"
			},
			...nativeAssetUrls().map( url => ({
				url,
				file: "payload/retained",
				offset: 0,
				length: 2,
				mime: url.endsWith( ".cur" ) ? "image/x-icon" : "image/png"
			}) )
		]
	};
	manifest.routes.sort( ( first, second ) => first.url.localeCompare( second.url ) );
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

test("application release requires every native route even when the asset schema matches", async context => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-native-route-admission-" ) );
	context.after( () => rm( root, { recursive: true, force: true } ) );
	for ( const url of nativeAssetUrls() ) {
		await context.test( url, async () => {
			const manifest = baseManifest();
			manifest.routes = manifest.routes.filter( row => row.url !== url );
			manifest.releaseId = releaseIdentity( manifest );
			await assert.rejects(
				buildApplicationRelease( {
					manifest,
					source: {},
					destination: path.join( root, "build" )
				} ),
				error => {
					assert.ok( error instanceof Error );
					assert.match( error.message, /native route; build a data release/ );
					assert.ok( error.message.includes( url ) );
					return true;
				}
			);
			assert.deepEqual( await readdir( root ), [], "admission must precede compilation and output creation" );
		} );
	}
});

test("a clock cursor route owned by the replaced application cannot satisfy admission", async context => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-replaced-native-route-" ) );
	context.after( () => rm( root, { recursive: true, force: true } ) );
	const manifest = baseManifest();
	const url = cursorAssetUrl( 0xa6 );
	const cursor = manifest.routes.find( row => row.url === url );
	assert.ok( cursor );
	cursor.file = "application/old-clock.png";
	manifest.files.push( { path: cursor.file, length: 2, sha256: sha( "{}" ), kind: "application" } );
	manifest.releaseId = releaseIdentity( manifest );
	await assert.rejects(
		buildApplicationRelease( {
			manifest,
			source: {},
			destination: path.join( root, "build" )
		} ),
		error => {
			assert.ok( error instanceof Error );
			assert.match( error.message, /native route; build a data release/ );
			assert.ok( error.message.includes( url ) );
			return true;
		}
	);
	assert.deepEqual( await readdir( root ), [], "a replaced route must fail before output creation" );
});
