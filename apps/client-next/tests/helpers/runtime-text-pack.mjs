/*
===========================================================================

runtime-text-pack.mjs - a complete raw-table pack for release fixtures

The fixture carries real pack headers and hashes so omission tests can
remove a member while retaining a structurally valid publication.

===========================================================================
*/
import { createHash } from "node:crypto";
import { REQUIRED_RUNTIME_TEXT_ASSETS } from "../../../../scripts/build/assetPackOwnership.mjs";
import {
	ASSET_PACK_MAGIC,
	ASSET_PACK_VERSION,
	ASSET_PACK_PREFIX_BYTES,
	ASSET_PACK_HEADER_FORMAT
} from "../../../../scripts/build/shared/packFormat.mjs";

/*
================
runtimeTextPack
================
*/
export function runtimeTextPack( paths = REQUIRED_RUNTIME_TEXT_ASSETS ) {
	const payload = Buffer.from( [ 0xb0, 0xa1, 0x0d, 0x0a ] );
	const hash = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );
	const packPath = "/assets/packs/runtime-text.bin";
	const assets = paths.map( ( path, index ) => ({
		path,
		group: "game-data",
		packPath,
		offset: index * payload.length,
		length: payload.length,
		sha256: hash( payload ),
		mime: "text/plain"
	}) );
	const header = Buffer.from( JSON.stringify( {
		format: ASSET_PACK_HEADER_FORMAT,
		version: ASSET_PACK_VERSION,
		files: assets
	} ) );
	const prefix = Buffer.alloc( ASSET_PACK_PREFIX_BYTES );
	prefix.write( ASSET_PACK_MAGIC );
	prefix.writeUInt32LE( header.length, ASSET_PACK_PREFIX_BYTES - 4 );
	const bytes = Buffer.concat( [ prefix, header, ...paths.map( () => payload ) ] );
	const index = {
		format: "sro-asset-pack-index",
		version: ASSET_PACK_VERSION,
		groups: [ {
			name: "game-data",
			load: "startup",
			assetCount: assets.length,
			totalBytes: assets.length * payload.length,
			packs: [ { path: packPath, bytes: bytes.length, sha256: hash( bytes ), assetCount: assets.length } ]
		} ],
		assets
	};
	return { index, bytes, packPath, payload };
}
