/*
===========================================================================

beta-public-pack.test.mjs - public pack projection preserves runtime bytes.

Development reports can coexist with runtime assets in older publications.
Removing a report must repair offsets, counts, and content identities.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { publicIndex, inspect, sha } from "../../tools/beta/policy.mjs";
import { projectPack } from "../../tools/beta/public-data.mjs";
import { gunzipSync, gzipSync } from "node:zlib";

test("public release drops obsolete reports and retains runtime model groups", () => {
	const groups = [ "equipment-models", "hwan-models", "mission-cos-models", "developer-labs" ];
	const index = publicIndex( {
		format: "sro-asset-pack-index",
		version: 2,
		groups: groups.map( name => ({ name }) ),
		assets: groups.map( group => ({ group, path: "/assets/model.glb" }) )
	} );
	assert.deepEqual( index.groups.map( group => group.name ), groups.slice( 0, -1 ) );
	assert.equal( index.assets.length, groups.length - 1 );
});

/*
================
packOf

An SROPACK2 pack of members; a member given stored bytes is gzip-stored.
================
*/
function packOf( members ) {
	let offset = 0;
	const entries = members.map( ( { path, bytes, stored } ) => {
		const entry = {
			path,
			offset,
			length: bytes.length,
			sha256: sha( bytes ),
			mime: "application/octet-stream",
			...(stored ? { stored: { length: stored.length, encoding: "gzip" } } : {})
		};
		offset += (stored ?? bytes).length;
		return entry;
	} );
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 2, files: entries } ) );
	const prefix = Buffer.alloc( 12 );
	prefix.write( "SROPACK2" );
	prefix.writeUInt32LE( header.length, 8 );
	const original = Buffer.concat( [ prefix, header, ...members.map( m => m.stored ?? m.bytes ) ] );
	const digest = sha( original );
	const pack = {
		path: `/assets/packs/game-data-001-${digest.slice( 0, 12 )}.bin`,
		bytes: original.length,
		sha256: digest,
		assetCount: entries.length
	};
	const index = publicIndex( {
		format: "sro-asset-pack-index",
		version: 2,
		groups: [ { name: "game-data", packs: [ pack ] } ],
		assets: entries.map( entry => ({ ...entry, group: "game-data", packPath: pack.path }) )
	} );
	return { original, digest, pack, index };
}

test("removing a development report republishes a valid pack with unchanged runtime data", () => {
	const privateBytes = Buffer.from( '{"source":"/src/engine/private-report.ts"}' );
	const publicBytes = Buffer.from( "unchanged runtime texture ".repeat( 64 ) ),
		publicStored = gzipSync( publicBytes );
	const { original, digest, pack, index } = packOf( [
		{ path: "/assets/cif/cif-implementation-status.json", bytes: privateBytes },
		{ path: "/assets/texture.bin", bytes: publicBytes, stored: publicStored }
	] );
	const projected = projectPack( pack, original, index.assets );
	inspect( pack.path.slice( 1 ), projected );
	const payloadStart = 12 + projected.readUInt32LE( 8 );
	// The untouched member keeps its stored bytes; nothing is re-encoded.
	assert.deepEqual( projected.subarray( payloadStart ), publicStored );
	assert.equal( pack.assetCount, 1 );
	assert.equal( pack.sha256, sha( projected ) );
	assert.notEqual( pack.sha256, digest );
	assert.equal( index.assets[0].offset, 0 );
	assert.equal( index.assets[0].packPath, pack.path );
	assert.equal( index.assets[0].sha256, sha( publicBytes ) );
	assert.deepEqual( index.assets[0].stored, { length: publicStored.length, encoding: "gzip" } );
});

test("a projected member is stored again by the builder's rule and still decodes", () => {
	const value = {
		format: "sro-world-object-resource-index",
		version: 1,
		objects: Array.from( { length: 64 }, ( _, i ) => ({ id: i, mesh: "/assets/world/mesh-" + i + ".glb" }) ),
		reconstructionSources: [ "D:/private/notes" ],
		missing: [ "x" ],
		missingCount: 1
	};
	const bytes = Buffer.from( JSON.stringify( value ) );
	const { original, pack, index } = packOf( [
		{ path: "/assets/world/object-resources.json", bytes, stored: gzipSync( bytes ) }
	] );
	const projected = projectPack( pack, original, index.assets );
	inspect( pack.path.slice( 1 ), projected );
	const row = index.assets[0], start = 12 + projected.readUInt32LE( 8 );
	assert.equal( row.stored?.encoding, "gzip" );
	const decoded = JSON.parse(
		gunzipSync( projected.subarray( start + row.offset, start + row.offset + row.stored.length ) ).toString()
	);
	assert.equal( decoded.reconstructionSources, undefined );
	assert.equal( decoded.missing, undefined );
	assert.equal( decoded.objects.length, 64 );
	assert.equal( row.sha256, sha( JSON.stringify( decoded ) ) );
});

test("policy refuses a pack of an unknown version instead of skipping its members", () => {
	const { original, pack } = packOf( [ { path: "/assets/texture.bin", bytes: Buffer.from( "runtime" ) } ] );
	const future = Buffer.from( original );
	future.write( "SROPACK3" );
	assert.throws( () => inspect( pack.path.slice( 1 ), future ), /Invalid embedded pack/ );
	inspect( pack.path.slice( 1 ), original );
});
