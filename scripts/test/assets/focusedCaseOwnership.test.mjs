/*
===========================================================================

focusedCaseOwnership.test.mjs - focused publication uses client path identity

Exercise the real family and sparse publishers in isolated generated roots.
An alternate spelling must replace its existing member, never append an owner.

===========================================================================
*/
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const run = promisify( execFile );
const ORIGINAL = "/assets/images/Media_extracted/ICON/Example.png";
const UPDATED = "/assets/images/Media_extracted/icon/example.png";
const SIBLING = "/assets/images/Media_extracted/icon/sibling.png";
const TIMEOUT_MS = 60000;

/*
================
fixture

Check actual stored bytes, including a compacted sibling in the rebuilt pack.
Image decoding is outside this publication test.
================
*/
async function fixture( mode ) {
	const { CLIENT_PUBLIC_ROOT: root, GENERATED_ROOT } = await import( "../../lib/generatedRoot.mjs" );
	const { buildAssetPacks } = await import( "../../build/assetPacks.mjs" );
	const { publishLooseFamily } = await import( "../../build/shared/looseFamilyPublication.mjs" );
	const { refreshPackGroups } = await import( "../../build/shared/packGroupRefresh.mjs" );
	const { readPackedAssetBytesSync } = await import( "../../lib/publishedAsset.mjs" );
	const owner = mode === "explicit" ? "native-ui" : "ui-icons";
	const beforeBytes = Buffer.from( "old packed bytes" );
	const afterBytes = mode === "unchanged" ? beforeBytes : Buffer.from( "replacement packed bytes" );
	const siblingBytes = Buffer.from( "compacted sibling" );
	const originalFile = path.join( root, ORIGINAL.slice( 1 ) );
	await mkdir( path.dirname( originalFile ), { recursive: true } );
	await writeFile( originalFile, beforeBytes );
	const siblingFile = path.join( root, SIBLING.slice( 1 ) );
	await mkdir( path.dirname( siblingFile ), { recursive: true } );
	await writeFile( siblingFile, siblingBytes );
	await buildAssetPacks( {
		publicRoot: root,
		outputRoot: path.join( root, "assets/packs" ),
		hashCachePath: path.join( GENERATED_ROOT, "hashes.json" ),
		memberCacheRoot: path.join( GENERATED_ROOT, "members" ),
		groups: [
			{ name: owner, load: "startup", files: [ ORIGINAL, SIBLING ] },
			...(owner === "native-ui" ? [ { name: "ui-icons", load: "startup", files: [] } ] : [])
		]
	} );
	const indexPath = path.join( root, "assets/packs/manifest.json" );
	const before = JSON.parse( await readFile( indexPath, "utf8" ) );
	assert.equal( before.groups.find( group => group.name === owner ).packs.length, 1 );
	await rm( siblingFile );
	if ( mode !== "linux-stale-spelling" ) await rm( originalFile );
	const updatedFile = path.join( root, UPDATED.slice( 1 ) );
	await mkdir( path.dirname( updatedFile ), { recursive: true } );
	await writeFile( updatedFile, afterBytes );
	if ( mode === "linux-stale-spelling" ) {
		assert.deepEqual( await readFile( originalFile ), beforeBytes, "distinct old spelling remains stale" );
	}
	const publish = () =>
		mode === "sparse" ?
			refreshPackGroups( { name: "case", deltas: [ { groupName: owner, files: [ UPDATED ] } ] } ) :
			publishLooseFamily( { name: "case", owner: "slot-effect", files: [ UPDATED ], defaultGroup: "ui-icons" } );
	await publish();
	const after = JSON.parse( await readFile( indexPath, "utf8" ) );
	assert.equal( after.assets.length, 2 );
	const matching = after.assets.filter( row => row.path.toLowerCase() === ORIGINAL.toLowerCase() );
	assert.equal( matching.length, 1 );
	assert.equal( matching[0].group, owner );
	assert.deepEqual( readPackedAssetBytesSync( matching[0].path, root ), afterBytes );
	assert.deepEqual( readPackedAssetBytesSync( SIBLING, root ), siblingBytes );
	await assert.rejects( readFile( siblingFile ), { code: "ENOENT" } );
	if ( mode === "unchanged" ) assert.deepEqual( after, before, "spelling alone must not rebuild packs" );
	else assert.equal( matching[0].path, UPDATED, "changed bytes use the supplied filesystem spelling" );
	assert.equal( after.groups.find( group => group.name === owner ).load, "startup" );
	await publish();
	assert.deepEqual( JSON.parse( await readFile( indexPath, "utf8" ) ), after );
}

if ( process.argv[2] === "--fixture" ) {
	await fixture( process.argv[3] );
} else {
	for ( const mode of [ "explicit", "same-owner", "sparse", "unchanged", "linux-stale-spelling" ] ) {
		test(`case-folded focused publication: ${mode}`, async t => {
			if ( mode === "linux-stale-spelling" && process.platform !== "linux" ) {
				t.skip( "requires distinct Linux filesystem spellings" );
				return;
			}
			const temporary = await mkdtemp( path.join( os.tmpdir(), "sro-owner-case-" ) );
			t.after( async () => {
				assert.equal( path.dirname( temporary ), path.resolve( os.tmpdir() ) );
				await rm( temporary, { recursive: true, force: true } );
			} );
			await run( process.execPath, [ fileURLToPath( import.meta.url ), "--fixture", mode ], {
				env: {
					...process.env,
					SRO_GENERATED_ROOT: temporary,
					SRO_BUILD_HASH_CACHE: "0",
					SRO_ASSET_PACK_BASELINE: "",
					SRO_BUILD_JOBS: "1"
				},
				windowsHide: true,
				timeout: TIMEOUT_MS
			} );
		});
	}
}
