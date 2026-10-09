/*
===========================================================================

public-data.mjs - public asset projection

Removes known development reports and build-only metadata while preserving
runtime payloads and pack identity.

===========================================================================
*/
import { gunzipSync, gzipSync } from "node:zlib";
import { sha, isPrivateAsset } from "./policy.mjs";
import {
	ASSET_PACK_MAGIC,
	decodeStoredMember,
	parsePackHeader,
	storedLength,
	storedMemberBytes
} from "../../../../scripts/build/shared/packFormat.mjs";
import { encodeStoredMemberSync } from "../../../../scripts/build/shared/memberCompression.mjs";
// Version-qualified metadata projection. Runtime consumers read bsr/materialSets/
// textures/meshFiles; missing reports and reconstruction citations are build-only.
// Never delete arbitrary keys recursively: asset paths and protocol names matter.
/*
================
projectMember
================
*/
export function projectMember( name, bytes ) {
	if ( !/\.json(?:\.gz)?$/.test( name ) ) return bytes;
	const compressed = name.endsWith( ".gz" ),
		raw = compressed ? gunzipSync( bytes, { maxOutputLength: 128 << 20 } ) : bytes;
	const value = JSON.parse( raw.toString( "utf8" ) );
	if ( value?.format !== "sro-world-object-resource-index" || value.version !== 1 ) return bytes;
	const { reconstructionSources, missing, missingCount, ...publicValue } = value;
	if ( reconstructionSources === undefined && missing === undefined && missingCount === undefined ) return bytes;
	const projected = Buffer.from( JSON.stringify( publicValue ) );
	return compressed ? gzipSync( projected, { level: 9 } ) : projected;
}
/*
================
projectPack

The pack without private members and with build-only metadata projected
out of the members that carry it. A rewritten member is stored again by the
builder's rule (memberCompression.mjs); untouched members keep their stored
bytes. Returns the original bytes when nothing changed.
================
*/
export function projectPack( pack, bytes, members, overrides ) {
	const { header, dataStart } = parsePackHeader( bytes, pack.path );
	if ( header.files.filter( e => !isPrivateAsset( e.path ) ).length !== members.length ) {
		throw Error( "Pack membership drift" );
	}
	const byName = new Map( members.map( a => [ a.path, a ] ) ), chunks = [];
	let offset = 0, originalOffset = 0, changed = false;
	const entries = header.files.flatMap( e => {
		const a = byName.get( e.path ), stored = storedMemberBytes( bytes, dataStart, e, pack.path );
		if ( e.offset !== originalOffset ) throw Error( "Unsupported noncontiguous pack: " + e.path );
		originalOffset += storedLength( e );
		const original = decodeStoredMember( stored, e );
		if ( !a && isPrivateAsset( e.path ) ) {
			changed = true;
			return [];
		}
		if ( !a || e.offset !== a.offset || e.length !== a.length || e.sha256 !== a.sha256 ) {
			throw Error( "Pack index drift " + e.path );
		}
		const next = projectMember( e.path, original ), digest = sha( next );
		let entry = { ...e, offset }, payload = stored;
		if ( digest !== a.sha256 ) {
			changed = true;
			const form = encodeStoredMemberSync( next );
			payload = form.stored;
			const { stored: _retired, ...identity } = e;
			entry = {
				...identity,
				offset,
				length: next.length,
				sha256: digest,
				...(form.encoding ? { stored: { length: form.stored.length, encoding: form.encoding } } : {})
			};
		}
		a.offset = offset;
		a.length = entry.length;
		a.sha256 = entry.sha256;
		if ( entry.stored ) a.stored = entry.stored;
		else delete a.stored;
		chunks.push( payload );
		offset += payload.length;
		return [ entry ];
	} );
	if ( !changed ) return bytes;
	const table = Buffer.from( JSON.stringify( { ...header, files: entries } ) ), prefix = Buffer.alloc( 12 );
	prefix.write( ASSET_PACK_MAGIC );
	prefix.writeUInt32LE( table.length, 8 );
	const result = Buffer.concat( [ prefix, table, ...chunks ] ), digest = sha( result ), old = pack.path;
	pack.path = old.replace( /-[a-f0-9]{12}\.bin$/, "-" + digest.slice( 0, 12 ) + ".bin" );
	if ( pack.path === old ) throw Error( "Unknown pack naming contract" );
	pack.sha256 = digest;
	pack.bytes = result.length;
	pack.assetCount = entries.length;
	for ( const a of members ) a.packPath = pack.path;
	return result;
}
