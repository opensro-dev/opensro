/*
===========================================================================

check_asset_delivery.mjs - the published asset packs deliver what they claim

Validates the pack index and delivery plan, the packed font atlases and the
runtime effect catalogs, then proves every compressed transport payload is a
lossless encoding of its member: the gzip file hashes to its name, inflates
to the member's length and hash, and actually saves space.

Transport payloads are content-addressed (named by their sha256) and never
change in place, so a payload verified once is recorded in
.state/n/asset-delivery-verified.json under its name, size and mtime. Later
runs verify only new or changed files, in parallel. SRO_CHECK_FORCE=1, a
fresh clone and CI verify every payload.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { validateAssetDelivery } from "../build/assetDelivery.mjs";
import { validatePackedFontAtlases } from "../build/assetPackPublication.mjs";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { gunzip } from "node:zlib";
import { promisify } from "node:util";
import { readPublishedAssetBytesSync } from "../lib/publishedAsset.mjs";

const root = path.resolve( import.meta.dirname, "../.." ),
	publicRoot = CLIENT_PUBLIC_ROOT,
	recordPath = path.join( root, ".state", "n", "asset-delivery-verified.json" ),
	decode = promisify( gunzip );
const RECORD_FORMAT = 1;
const VERIFY_CONCURRENCY = 8;
const RUNTIME_EFFECT_CATALOGS = [
	"/assets/skill/effectRecords.json",
	"/assets/skill/namedEffectRecords.json",
	"/assets/effects/programs.json"
];

/*
================
hash
================
*/
function hash( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
readRecord

Payloads verified by an earlier run, by content name. Unreadable or foreign
records verify everything again.
================
*/
async function readRecord() {
	if ( process.env.SRO_CHECK_FORCE === "1" ) return new Map();
	try {
		const value = JSON.parse( await readFile( recordPath, "utf8" ) );
		if ( value.format !== RECORD_FORMAT || typeof value.payloads !== "object" ) return new Map();
		return new Map( Object.entries( value.payloads ) );
	} catch {
		return new Map();
	}
}

/*
================
verifyPayload

The decoded identity of one transport payload: its compressed bytes hash to
the content name, and inflate within the 64 MiB member limit.
================
*/
async function verifyPayload( transport, assetPath ) {
	const encoded = await readFile( path.join( publicRoot, transport.path ) );
	if ( encoded.length !== transport.length || hash( encoded ) !== transport.sha256 ) {
		throw Error( "Compressed integrity: " + assetPath );
	}
	const raw = await decode( encoded, { maxOutputLength: 64 << 20 } );
	return { length: raw.length, sha256: hash( raw ) };
}

/*
================
verifyTransports

Every unique transport payload, recorded ones by file identity and the rest
in parallel batches. Returns the decoded identity of each payload.
================
*/
async function verifyTransports( index, record ) {
	const payloads = new Map();
	for ( const e of index.assets ) {
		if ( !e.transport ) continue;
		const t = e.transport;
		if ( t.path !== `/assets/packs/transport/${t.sha256}.gz` || t.encoding !== "gzip" ) {
			throw Error( "Invalid transport descriptor: " + e.path );
		}
		if ( !payloads.has( t.sha256 ) ) payloads.set( t.sha256, { transport: t, asset: e.path } );
	}
	const verified = new Map(), next = new Map();
	let fresh = 0;
	const pending = [ ...payloads.values() ];
	async function worker() {
		for ( let item = pending.pop(); item; item = pending.pop() ) {
			const t = item.transport, info = await stat( path.join( publicRoot, t.path ) );
			const known = record.get( t.sha256 );
			let row;
			if ( known && known.size === info.size && known.mtimeMs === info.mtimeMs ) {
				row = { length: known.length, sha256: known.decoded };
			} else {
				row = await verifyPayload( t, item.asset );
				fresh++;
			}
			verified.set( t.sha256, row );
			next.set( t.sha256, { size: info.size, mtimeMs: info.mtimeMs, length: row.length, decoded: row.sha256 } );
		}
	}
	await Promise.all( Array.from( { length: VERIFY_CONCURRENCY }, worker ) );
	return { verified, next, fresh };
}

const index = JSON.parse( await readFile( path.join( publicRoot, "assets/packs/manifest.json" ), "utf8" ) );
validateAssetDelivery(
	index,
	JSON.parse( await readFile( path.join( publicRoot, "assets/packs/delivery.json" ), "utf8" ) )
);
await validatePackedFontAtlases( index, publicRoot );
// Effect records are runtime inputs, including the named table loaded on the
// first item/cure event. Loose files are not evidence of worker delivery.
for ( const catalog of RUNTIME_EFFECT_CATALOGS ) {
	if ( !index.assets.some( e => e.path === catalog || e.path === catalog + ".gz" ) ) {
		throw Error( "Missing runtime effect catalog in asset publication: " + catalog );
	}
}

const { verified, next, fresh } = await verifyTransports( index, await readRecord() );
let members = 0, identityBytes = 0, compressedBytes = 0, animationManifests = 0;
for ( const e of index.assets ) {
	if ( e.transport ) {
		const t = e.transport, row = verified.get( t.sha256 );
		if ( row.length !== e.length || row.sha256 !== e.sha256 || t.length > e.length * 0.9 ) {
			throw Error( "Lossless compression contract: " + e.path );
		}
		members++;
		identityBytes += e.length;
		compressedBytes += t.length;
	}
	if ( /^\/assets\/world\/[^/]+\/animated-objects\.json(?:\.gz)?$/.test( e.path ) ) {
		let bytes = readPublishedAssetBytesSync( e.path, publicRoot );
		if ( e.path.endsWith( ".gz" ) ) bytes = await decode( bytes );
		const actual = Object.keys( JSON.parse( bytes.toString( "utf8" ) ).objects ).sort();
		if ( e.animationDigest !== e.sha256 || JSON.stringify( actual ) !== JSON.stringify( e.animationSources ) ) {
			throw Error( "Animation index drift: " + e.path );
		}
		animationManifests++;
	}
}
// Recorded only after every check above passed.
await mkdir( path.dirname( recordPath ), { recursive: true } );
await writeFile( recordPath, JSON.stringify( { format: RECORD_FORMAT, payloads: Object.fromEntries( next ) } ) );

const report = {
	assets: index.assets.length,
	members,
	uniqueCompressedPayloads: verified.size,
	freshlyVerified: fresh,
	identityBytes,
	compressedBytes,
	animationManifests,
	lossless: true
};
const output = path.join( root, "apps/client-next/temp/artifacts/asset-delivery" );
await mkdir( output, { recursive: true } );
await writeFile( path.join( output, "publication.json" ), JSON.stringify( report, null, 2 ) );
console.log( JSON.stringify( report ) );
