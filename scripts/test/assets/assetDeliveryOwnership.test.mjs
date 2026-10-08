/*
===========================================================================

assetDeliveryOwnership.test.mjs - delivery metadata after the transports

Members travel inside their packs (SROPACK2), so delivery writes no file of
its own: it stamps the index's delivery version and indexes each world
animation catalog's sources, read loose or from a gzip-stored pack member.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import { DELIVERY_VERSION, prepareAssetDelivery, validateAssetDelivery } from "../../build/assetDelivery.mjs";
import { ASSET_PACK_MAGIC, ASSET_PACK_VERSION, MEMBER_ENCODING_GZIP } from "../../build/shared/packFormat.mjs";

const CATALOG = "/assets/world/region-test/animated-objects.json";
const PACK = "/assets/packs/world-001-000000000000.bin";

/*
================
sha
================
*/
function sha( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
packWith

One SROPACK2 pack holding a member gzip-stored, and its index row.
================
*/
function packWith( publicPath, bytes ) {
	const stored = gzipSync( bytes, { level: 9 } );
	const row = {
		path: publicPath,
		offset: 0,
		length: bytes.length,
		mime: "application/json",
		sha256: sha( bytes ),
		stored: { length: stored.length, encoding: MEMBER_ENCODING_GZIP }
	};
	const header = Buffer.from(
		JSON.stringify( { format: "sro-asset-pack", version: ASSET_PACK_VERSION, files: [ row ] } )
	);
	const prefix = Buffer.alloc( 12 );
	prefix.write( ASSET_PACK_MAGIC );
	prefix.writeUInt32LE( header.length, 8 );
	return { pack: Buffer.concat( [ prefix, header, stored ] ), row: { ...row, packPath: PACK, group: "world" } };
}

/*
================
fixture
================
*/
async function fixture( t ) {
	const root = await mkdtemp( path.join( tmpdir(), "sro-delivery-owner-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	// Repetitive enough that gzip saves more than a tenth.
	const objects = Object.fromEntries( [ "native/b.bsr", "native/a.bsr" ].map( n => [ n, { frames: 128 } ] ) );
	const catalog = Buffer.from( JSON.stringify( { objects, padding: "x".repeat( 2048 ) } ) );
	const { pack, row } = packWith( CATALOG, catalog );
	await mkdir( path.join( root, "assets/packs" ), { recursive: true } );
	await writeFile( path.join( root, PACK ), pack );
	const index = {
		groups: [ {
			name: "world",
			packs: [ { path: PACK, bytes: pack.length, sha256: sha( pack ), assetCount: 1 } ]
		} ],
		assets: [ row ]
	};
	return { root, index, catalog };
}

test("delivery indexes a gzip-stored animation catalog and writes no file of its own", async t => {
	const f = await fixture( t );
	assert.ok( f.index.assets[0].stored.length < f.catalog.length, "the fixture member is stored compressed" );
	assert.deepEqual( await prepareAssetDelivery( f.index, f.root ), { animationManifests: 1 } );
	assert.equal( f.index.deliveryVersion, DELIVERY_VERSION );
	assert.deepEqual( f.index.assets[0].animationSources, [ "native/a.bsr", "native/b.bsr" ] );
	assert.equal( f.index.assets[0].animationDigest, f.index.assets[0].sha256 );
	// The retired catalog and transports are never written again.
	assert.deepEqual( await readdir( path.join( f.root, "assets/packs" ) ), [ path.basename( PACK ) ] );
});

test("a loose catalog is preferred and still checked against its row", async t => {
	const f = await fixture( t );
	await mkdir( path.join( f.root, path.dirname( CATALOG ) ), { recursive: true } );
	await writeFile( path.join( f.root, CATALOG ), Buffer.from( '{"objects":{}}' ) );
	await assert.rejects( prepareAssetDelivery( f.index, f.root ), /integrity mismatch/ );
	await writeFile( path.join( f.root, CATALOG ), f.catalog );
	await prepareAssetDelivery( f.index, f.root );
	assert.deepEqual( f.index.assets[0].animationSources, [ "native/a.bsr", "native/b.bsr" ] );
});

test("a corrupted stored member is refused, never indexed", async t => {
	const f = await fixture( t );
	f.index.assets[0].sha256 = "0".repeat( 64 );
	await assert.rejects( prepareAssetDelivery( f.index, f.root ), /integrity mismatch/ );
	assert.equal( f.index.assets[0].animationSources, undefined );
});

test("validation refuses a transport row, a stale index and an unknown delivery version", async t => {
	const f = await fixture( t );
	await prepareAssetDelivery( f.index, f.root );
	validateAssetDelivery( f.index );
	const transport = structuredClone( f.index );
	transport.assets[0].transport = { path: "/assets/packs/transport/x.gz" };
	assert.throws( () => validateAssetDelivery( transport ), /Retired delivery transport/ );
	const stale = structuredClone( f.index );
	stale.assets[0].animationDigest = "0".repeat( 64 );
	assert.throws( () => validateAssetDelivery( stale ), /animation source index/ );
	const old = structuredClone( f.index );
	old.deliveryVersion = 1;
	assert.throws( () => validateAssetDelivery( old ), /unsupported/ );
});

test("a pack path cannot escape the publication root", async t => {
	const f = await fixture( t );
	const escaped = "/../escaped-001-000000000000.bin";
	f.index.groups[0].packs[0].path = escaped;
	f.index.assets[0].packPath = escaped;
	await assert.rejects( prepareAssetDelivery( f.index, f.root ), /escaped publication/ );
});
