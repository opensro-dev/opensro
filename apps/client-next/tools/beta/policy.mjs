/*
===========================================================================

policy.mjs - public release classification and artifact verification.

Only reviewed asset groups enter a release. File and route checks keep source,
private diagnostics, and unverified payloads outside the public web root.

===========================================================================
*/
import { createHash } from "node:crypto";
import { lstat, readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { gunzipSync } from "node:zlib";
import {
	ASSET_PACK_MAGIC,
	ASSET_PACK_VERSION,
	decodeStoredMember,
	parsePackHeader,
	storedLength,
	storedMemberBytes,
	validStoredForm
} from "../../../../scripts/build/shared/packFormat.mjs";
/*
================
sha
================
*/
export const sha = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );
/*
================
releaseIdentity
================
*/
export const releaseIdentity = m => sha( JSON.stringify( { source: m.sourceHash, files: m.files, routes: m.routes } ) );
/*
================
safeName
================
*/
export function safeName( name ) {
	if (
		typeof name !== "string" || !name || name.startsWith( "/" ) || name.includes( "\\" ) || name.includes( ":" ) ||
		/[\x00-\x1f%?#]/.test( name ) || name.split( "/" ).some( p => !p || p === "." || p === ".." )
	) throw Error( "Unsafe release path: " + name );
	return name;
}
// This is a security classification of groups, never an asset list. New groups
// must explicitly choose an audience; adding a file to an existing group is automatic.
// Dedicated equipment, Hwan and COS groups contain runtime models
// selected by the published roster and NPC manifest (assetPackOwnership.mjs).
const audiences = {
	"developer-labs": "private",
	"game-audio": "public",
	"game-data": "public",
	"game-images": "public",
	"game-models": "public",
	"mission-minimap": "public",
	"mission-npc-vat": "public",
	"native-ui": "public",
	"outdoor-world": "public",
	"title-crowd-vat": "public",
	"equipment-models": "public",
	"hwan-models": "public",
	"mission-cos-models": "public"
};
/*
================
isPrivateAsset

This obsolete implementation report is development evidence, with no runtime
consumer. Older publications placed it in game-data alongside real UI assets.
================
*/
export function isPrivateAsset( name ) {
	return name === "/assets/cif/cif-implementation-status.json" ||
		name === "/assets/cif/cif-implementation-status.json.gz";
}
/*
================
publicIndex
================
*/
export function publicIndex( index ) {
	if (
		index?.format !== "sro-asset-pack-index" || index.version !== ASSET_PACK_VERSION ||
		!Array.isArray( index.groups ) ||
		!Array.isArray( index.assets )
	) throw Error( "Invalid publication authority" );
	const classified = new Set();
	for ( const g of index.groups ) {
		if ( !Object.hasOwn( audiences, g.name ) || classified.has( g.name ) ) {
			throw Error( "Unclassified or duplicate asset group: " + g.name );
		}
		classified.add( g.name );
	}
	for ( const a of index.assets ) {
		if ( !classified.has( a.group ) ) throw Error( "Asset references missing group: " + a.path );
	}
	const groups = index.groups.filter( g => audiences[g.name] === "public" );
	const names = new Set( groups.map( g => g.name ) );
	return { ...index, groups, assets: index.assets.filter( a => names.has( a.group ) && !isPrivateAsset( a.path ) ) };
}
/*
================
inspect
================
*/
export function inspect( name, bytes, { application = false } = {} ) {
	safeName( name );
	// Any SROPACK magic is a pack: an unknown version must fail here, never
	// fall through to the plain-file rules and skip its members' inspection.
	if ( bytes.subarray( 0, 7 ).toString() === ASSET_PACK_MAGIC.slice( 0, 7 ) ) {
		let parsed;
		try {
			parsed = parsePackHeader( bytes, name );
		} catch {
			throw Error( "Invalid embedded pack" );
		}
		const { header, dataStart: start } = parsed;
		let end = 0;
		const names = new Set();
		for ( const e of header.files ) {
			const n = safeName( e.path.slice( 1 ) );
			if (
				names.has( n ) || !Number.isSafeInteger( e.offset ) || !Number.isSafeInteger( e.length ) ||
				e.offset < end || e.length < 0 || !validStoredForm( e ) ||
				start + e.offset + storedLength( e ) > bytes.length
			) throw Error( "Invalid embedded pack member" );
			names.add( n );
			end = e.offset + storedLength( e );
			let b;
			try {
				b = decodeStoredMember( storedMemberBytes( bytes, start, e, name ), e );
			} catch {
				throw Error( "Embedded pack hash mismatch" );
			}
			inspect( n, b );
		}
		return;
	}
	if (
		/(?:^|\/)(?:\.env(?:\.|$)|\.git|private|src|node_modules|tests|tools|docs|reconstruct)(?:\/|$)|\.(?:map|ts|tsx|bndb|pdb|pem|key)$|(?:release-sources|release-profile|execution-map)\./i
			.test( name )
	) throw Error( "Forbidden release artifact: " + name );
	if ( /\.(?:exe|dll|zip|tar|7z|log)$/.test( name ) || !application && /\.(?:js|mjs)$/.test( name ) ) {
		throw Error( "Unexpected executable/archive/log: " + name );
	}
	if ( name.endsWith( ".gz" ) ) {
		const decoded = gunzipSync( bytes, { maxOutputLength: 128 << 20 } );
		// Transport objects have digest filenames. Preserve inspection of JSON even
		// after its authored extension has been replaced by the content-addressed name.
		return inspect(
			name.slice( 0, -3 ) + (/^\s*[\[{]/.test( decoded.subarray( 0, 64 ).toString() ) ? ".json" : ""),
			decoded,
			{ application }
		);
	}
	if ( !/\.(?:js|mjs|css|html|json|txt|wgsl)$/.test( name ) ) return;
	const text = bytes.toString( "utf8" );
	const forbidden = [
		/sourceMappingURL\s*=/,
		/\bsourcesContent"?\s*:/,
		/\/@(?:vite|fs|id)\//,
		/\/__client-next-dev-/,
		/\/(?:src\/bootstrap|src\/engine)\b/,
		/["'`](?:[A-Za-z]:[\\/]|file:\/\/\/)/,
		/-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/,
		/\bAKIA[0-9A-Z]{16}\b/,
		/__(?:playableRuntime|worldProbe\w*|hitRenderer|guideProbe|characterResourceProbe)/
	];
	for ( const rule of forbidden ) {
		if ( rule.test( text ) ) throw Error( "Source/development/secret exposure in " + name + " (" + rule + ")" );
	}
	if ( application && /\.(?:js|mjs)$/.test( name ) && /\bdebugger\s*;/.test( text ) ) {
		throw Error( "Debugger instruction in " + name );
	}
}
/*
================
files
================
*/
export async function files( root, prefix = "" ) {
	const result = [];
	for ( const entry of await readdir( path.join( root, prefix ), { withFileTypes: true } ) ) {
		const name = prefix ? prefix + "/" + entry.name : entry.name;
		safeName( name );
		const s = await lstat( path.join( root, name ) );
		if ( s.isSymbolicLink() || !s.isDirectory() && !s.isFile() ) {
			throw Error( "Non-regular release entry: " + name );
		}
		if ( s.isDirectory() ) result.push( ...await files( root, name ) );
		else result.push( name );
	}
	return result.sort();
}
/*
================
verifyDirectory
================
*/
export async function verifyDirectory( root ) {
	const manifest = JSON.parse( await readFile( path.join( root, "release.json" ), "utf8" ) );
	if (
		manifest.format !== "sro-beta-release-v1" || !Array.isArray( manifest.files ) ||
		!Array.isArray( manifest.routes ) || !/^[a-f0-9]{64}$/.test( manifest.sourceHash ) ||
		manifest.releaseId !== releaseIdentity( manifest )
	) throw Error( "Invalid release manifest identity" );
	const listed = new Set( [ "release.json" ] );
	for ( const e of manifest.files ) {
		safeName( e.path );
		if (
			![ "application", "data" ].includes( e.kind ) || listed.has( e.path ) ||
			!Number.isSafeInteger( e.length ) || e.length < 0 || !/^[a-f0-9]{64}$/.test( e.sha256 )
		) throw Error( "Invalid manifest entry" );
		listed.add( e.path );
	}
	const actual = await files( root );
	if ( actual.length !== listed.size || actual.some( n => !listed.has( n ) ) ) {
		throw Error( "Unmanifested or missing release file" );
	}
	for ( const e of manifest.files ) {
		const b = await readFile( path.join( root, e.path ) );
		if ( b.length !== e.length || sha( b ) !== e.sha256 ) throw Error( "Release hash mismatch: " + e.path );
		inspect( e.path, b, { application: e.kind === "application" } );
	}
	const routes = new Set();
	const entries = new Map( manifest.files.map( e => [ e.path, e ] ) );
	for ( const r of manifest.routes ) {
		safeName( r.url.slice( 1 ) );
		const e = entries.get( r.file );
		if (
			!r.url.startsWith( "/" ) || routes.has( r.url ) || !e || !Number.isSafeInteger( r.offset ) ||
			!Number.isSafeInteger( r.length ) || r.offset < 0 || r.length < 0 || r.offset + r.length > e.length ||
			typeof r.mime !== "string" || !/^[-\w.+]+\/[-\w.+]+$/.test( r.mime ) ||
			r.encoding !== undefined && r.encoding !== "gzip"
		) throw Error( "Invalid release route" );
		if ( r.gzip ) {
			const g = entries.get( r.gzip.file );
			if (
				!g || r.encoding || r.offset !== 0 || r.length !== e.length || r.gzip.offset !== 0 ||
				r.gzip.length !== g.length
			) throw Error( "Invalid compressed route" );
			const raw = gunzipSync( await readFile( path.join( root, g.path ) ), { maxOutputLength: 128 << 20 } );
			if ( raw.length !== e.length || sha( raw ) !== e.sha256 ) {
				throw Error( "Compressed route differs from identity" );
			}
		}
		routes.add( r.url );
	}
	if ( !routes.has( "/index.html" ) || !routes.has( "/assets/packs/manifest.json" ) ) {
		throw Error( "Incomplete release roots" );
	}
	return manifest;
}
