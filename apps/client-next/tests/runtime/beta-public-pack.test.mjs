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

test("public release drops obsolete reports and retains runtime model groups", () => {
	const groups = [ "cosmetic-models", "equipment-models", "hwan-models", "mission-cos-models", "developer-labs" ];
	const index = publicIndex( {
		format: "sro-asset-pack-index",
		version: 1,
		groups: groups.map( name => ({ name }) ),
		assets: groups.map( group => ({ group, path: "/assets/model.glb" }) )
	} );
	assert.deepEqual( index.groups.map( group => group.name ), groups.slice( 0, -1 ) );
	assert.equal( index.assets.length, groups.length - 1 );
});

test("removing a development report republishes a valid pack with unchanged runtime data", () => {
	const privateBytes = Buffer.from( '{"source":"/src/engine/private-report.ts"}' );
	const publicBytes = Buffer.from( "unchanged runtime texture" );
	const names = [ "/assets/cif/cif-implementation-status.json", "/assets/texture.bin" ];
	const entries = [ privateBytes, publicBytes ].map( ( bytes, index ) => ({
		path: names[index],
		offset: index === 0 ? 0 : privateBytes.length,
		length: bytes.length,
		sha256: sha( bytes ),
		mime: "application/octet-stream"
	}) );
	const header = Buffer.from( JSON.stringify( { format: "sro-asset-pack", version: 1, files: entries } ) );
	const prefix = Buffer.alloc( 12 );
	prefix.write( "SROPACK1" );
	prefix.writeUInt32LE( header.length, 8 );
	const original = Buffer.concat( [ prefix, header, privateBytes, publicBytes ] );
	const digest = sha( original );
	const pack = {
		path: `/assets/packs/game-data-001-${digest.slice( 0, 12 )}.bin`,
		bytes: original.length,
		sha256: digest,
		assetCount: entries.length
	};
	const index = publicIndex( {
		format: "sro-asset-pack-index",
		version: 1,
		groups: [ { name: "game-data", packs: [ pack ] } ],
		assets: entries.map( entry => ({ ...entry, group: "game-data", packPath: pack.path }) )
	} );
	const projected = projectPack( pack, original, index.assets, new Map() );
	inspect( pack.path.slice( 1 ), projected );
	const payloadStart = 12 + projected.readUInt32LE( 8 );
	assert.deepEqual( projected.subarray( payloadStart ), publicBytes );
	assert.equal( pack.assetCount, 1 );
	assert.equal( pack.sha256, sha( projected ) );
	assert.notEqual( pack.sha256, digest );
	assert.equal( index.assets[0].offset, 0 );
	assert.equal( index.assets[0].packPath, pack.path );
	assert.equal( index.assets[0].sha256, sha( publicBytes ) );
});
