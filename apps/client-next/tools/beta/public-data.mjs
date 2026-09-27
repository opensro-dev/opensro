/*
===========================================================================

public-data.mjs - public asset projection

Removes known development reports and build-only metadata while preserving
runtime payloads and pack identity.

===========================================================================
*/
import { gunzipSync, gzipSync } from "node:zlib";
import { sha, isPrivateAsset } from "./policy.mjs";
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
================
*/
export function projectPack( pack, bytes, members, overrides ) {
	const start = 12 + bytes.readUInt32LE( 8 ), header = JSON.parse( bytes.subarray( 12, start ) );
	if ( header.files.filter( e => !isPrivateAsset( e.path ) ).length !== members.length ) {
		throw Error( "Pack membership drift" );
	}
	const byName = new Map( members.map( a => [ a.path, a ] ) ), chunks = [];
	let offset = 0, originalOffset = 0, changed = false;
	const entries = header.files.flatMap( e => {
		const a = byName.get( e.path ), original = bytes.subarray( start + e.offset, start + e.offset + e.length );
		if ( e.offset !== originalOffset ) throw Error( "Unsupported noncontiguous pack: " + e.path );
		originalOffset += e.length;
		if ( !a && isPrivateAsset( e.path ) ) {
			if ( sha( original ) !== e.sha256 ) throw Error( "Pack index drift " + e.path );
			changed = true;
			return [];
		}
		if ( !a || e.offset !== a.offset || e.length !== a.length || sha( original ) !== a.sha256 ) {
			throw Error( "Pack index drift " + e.path );
		}
		const next = projectMember( e.path, original ), digest = sha( next );
		changed ||= digest !== a.sha256;
		if ( digest !== a.sha256 && a.transport ) {
			const zipped = gzipSync( next, { level: 9 } ), id = sha( zipped );
			a.transport = {
				path: "/assets/packs/transport/" + id + ".gz",
				sha256: id,
				length: zipped.length,
				encoding: "gzip"
			};
			overrides.set( a.transport.path, zipped );
		}
		a.offset = offset;
		a.length = next.length;
		a.sha256 = digest;
		chunks.push( next );
		const entry = { ...e, offset, length: next.length, sha256: digest };
		offset += next.length;
		return [ entry ];
	} );
	if ( !changed ) return bytes;
	const table = Buffer.from( JSON.stringify( { ...header, files: entries } ) ), prefix = Buffer.alloc( 12 );
	prefix.write( "SROPACK1" );
	prefix.writeUInt32LE( table.length, 8 );
	const result = Buffer.concat( [ prefix, table, ...chunks ] ), digest = sha( result ), old = pack.path;
	pack.path = old.replace( /-[a-f0-9]{12}\.bin$/, "-" + digest.slice( 0, 12 ) + ".bin" );
	if ( pack.path === old ) throw Error( "Unknown pack naming contract" );
	pack.sha256 = digest;
	pack.bytes = result.length;
	pack.assetCount = entries.length;
	for ( const key of [ "zstdPath", "zstdBytes", "zstdLevel", "zstdWindowLog" ] ) delete pack[key];
	for ( const a of members ) a.packPath = pack.path;
	return result;
}
