/*
===========================================================================

beta-packager-ledger.test.mjs - the beta packager refuses a thinned pack index

buildBeta verifies the published pack index against the publication
ledger before compiling anything: an index that lost an asset a build
owner claims (2026-11-10, three Korean-named textures) must stop the
package with the lost paths named, not ship a thinner world.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

/*
================
buildBeta

Refusal happens before the application build, so the fixture needs no
compiled client: a temp generated root (ledger + public tree), a pack
index that drops one claimed model, and a frozen source snapshot.
================
*/
test("the beta packager refuses an index that lost a claimed asset", async () => {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-beta-ledger-" ) );
	process.env.SRO_GENERATED_ROOT = root;
	const ledger = await import( "../../../../scripts/build/shared/publicationLedger.mjs" );
	const { buildBeta } = await import( "../../tools/beta/build.mjs" );
	const { ASSET_SCHEMA } = await import( "../../src/engine/foundation/release/protocol.ts" );
	const { REQUIRED_RUNTIME_TEXT_ASSETS } = await import( "../../../../scripts/build/assetPackOwnership.mjs" );
	const { ASSET_PACK_VERSION } = await import( "../../../../scripts/build/shared/packFormat.mjs" );

	const assetRoot = path.join( root, "client-public" );
	const claimed = [ "/assets/models/a.glb", "/assets/models/b.glb", ...REQUIRED_RUNTIME_TEXT_ASSETS ];
	for ( const owner of ledger.expectedOwners() ) {
		await ledger.beginPublication( owner );
		if ( owner === "resource-build" ) await ledger.claimPublicPaths( claimed );
		await ledger.commitPublication();
	}
	const packs = path.join( assetRoot, "assets", "packs" );
	await mkdir( packs, { recursive: true } );
	const index = {
		format: "sro-asset-pack-index",
		version: ASSET_PACK_VERSION,
		assetSchema: ASSET_SCHEMA,
		groups: [ { name: "game-models" } ],
		assets: claimed
			.filter( publicPath => publicPath !== "/assets/models/b.glb" )
			.map( publicPath => ({ path: publicPath, group: "game-models", length: 1 }) )
	};
	await writeFile( path.join( packs, "manifest.json" ), JSON.stringify( index ) );
	const snapshot = path.join( root, "source.json" );
	await writeFile( snapshot, "{}" );
	const message = await buildBeta( {
		destination: path.join( root, "out" ),
		assetRoot,
		sourceSnapshot: snapshot,
		makeArchive: false
	} ).then( () => "", error => error.message );
	assert.match( message, /claimed by a build owner but missing from the pack index/ );
	assert.ok( message.includes( "/assets/models/b.glb" ), "the refusal names the lost asset" );
});
