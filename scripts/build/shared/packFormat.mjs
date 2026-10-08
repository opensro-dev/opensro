/*
===========================================================================

packFormat.mjs - the SROPACK2 asset pack layout every reader shares

A pack is the 8-byte magic, a u32 little-endian header length, the JSON
header {format: "sro-asset-pack", version: 2, files}, then the members'
stored bytes. A member row carries its decoded identity (length, sha256)
and, when it travels compressed, stored: {length, encoding}; offsets are
over the stored bytes. The pack index (manifest.json) repeats each row.

Every script reader (builders, checks, tools, tests) reads members through
this module, so the layout has one owner on the Node side; the browser's
reader is apps/client-next/.../packs/index/index.ts.

===========================================================================
*/

import { createHash } from "node:crypto";
import zlib from "node:zlib";

export const ASSET_PACK_MAGIC = "SROPACK2";
export const ASSET_PACK_VERSION = 2;
export const ASSET_PACK_HEADER_FORMAT = "sro-asset-pack";
export const ASSET_PACK_PREFIX_BYTES = 12;
// Members are stored gzip-compressed: every browser decodes gzip natively
// (DecompressionStream), so the client needs no decoder of its own.
export const MEMBER_ENCODING_GZIP = "gzip";

/*
================
storedLength

The bytes a member occupies in its pack.
================
*/
export function storedLength( row ) {
	return row.stored ? row.stored.length : row.length;
}

/*
================
validStoredForm

True when a row's stored form is absent or a known encoding that saves bytes.
================
*/
export function validStoredForm( row ) {
	if ( row.stored === undefined ) return true;
	const stored = row.stored;
	return Boolean( stored ) && typeof stored === "object" && stored.encoding === MEMBER_ENCODING_GZIP &&
		Number.isInteger( stored.length ) && stored.length > 0 && stored.length < row.length &&
		Object.keys( stored ).length === 2;
}

/*
================
sameStoredForm
================
*/
export function sameStoredForm( left, right ) {
	return (left.stored?.encoding ?? null) === (right.stored?.encoding ?? null) &&
		storedLength( left ) === storedLength( right );
}

/*
================
readPackPrefix

Validates the magic and returns the data start (12 + header length).
================
*/
export function readPackPrefix( bytes, label ) {
	if ( bytes.length < ASSET_PACK_PREFIX_BYTES || bytes.subarray( 0, 8 ).toString( "ascii" ) !== ASSET_PACK_MAGIC ) {
		throw new Error( `Invalid asset-pack header: ${label}.` );
	}
	return ASSET_PACK_PREFIX_BYTES + bytes.readUInt32LE( 8 );
}

/*
================
parsePackHeader

The header object of a whole pack, with its data start. Throws on any other
format or version.
================
*/
export function parsePackHeader( bytes, label ) {
	const dataStart = readPackPrefix( bytes, label );
	if ( dataStart > bytes.length ) throw new Error( `Invalid asset-pack table length: ${label}.` );
	const header = JSON.parse( bytes.subarray( ASSET_PACK_PREFIX_BYTES, dataStart ).toString( "utf8" ) );
	if (
		header?.format !== ASSET_PACK_HEADER_FORMAT || header.version !== ASSET_PACK_VERSION ||
		!Array.isArray( header.files )
	) {
		throw new Error( `Invalid asset-pack table: ${label}.` );
	}
	return { header, dataStart };
}

/*
================
storedMemberBytes

The stored bytes of one member inside a whole pack.
================
*/
export function storedMemberBytes( bytes, dataStart, row, label ) {
	const start = dataStart + row.offset, end = start + storedLength( row );
	if ( row.offset < 0 || end > bytes.length ) throw new Error( `Packed asset ${row.path} exceeds ${label}.` );
	return bytes.subarray( start, end );
}

/*
================
decodeStoredMember

The decoded bytes of one stored member, checked against its row's length
and SHA-256.
================
*/
export function decodeStoredMember( stored, row ) {
	if ( !validStoredForm( row ) ) throw new Error( `Unsupported pack member stored form: ${row.path}` );
	const bytes = row.stored ?
		zlib.gunzipSync( stored, { maxOutputLength: Math.max( 1, row.length ) } ) :
		stored;
	if (
		bytes.length !== row.length ||
		createHash( "sha256" ).update( bytes ).digest( "hex" ) !== row.sha256.toLowerCase()
	) {
		throw new Error( `Pack member integrity mismatch: ${row.path}` );
	}
	return bytes;
}
