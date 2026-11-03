/*
===========================================================================

packs-recovery.test.mjs - the pack store's transient-failure class

#2337: verification failures on unverified delivered bytes - a pack or
member whose SHA-256 misses, a garbled ranged header - are transport
damage the bounded recovery retries, never corrupt content refused
for good. Verified-delivery behaviour stays in packs.test.mjs; both
suites share tests/helpers/packs-fixture.mjs.

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { fixture, sha } from "../helpers/packs-fixture.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createPacks } = await load( "src/engine/runtime/assets/worker/packs/packs.ts" );
const { transientAssetFailure } = await load( "src/engine/foundation/assets/asset-recovery.ts" );

test("pack bytes that fail SHA-256 are a transient failure, not corrupt content", async () => {
	// A body cut and re-framed by a middlebox ends cleanly with the wrong
	// bytes: the digest is how the read finds out. The persistent path
	// treats that same fact as wrong bytes and refetches (packs.ts), so
	// the network path marks it for the bounded recovery too (#2337).
	const f = fixture(), corrupt = f.bytes.slice();
	corrupt[corrupt.length - 1] ^= 1;
	const packs = createPacks( async url =>
		url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : corrupt
	);
	await assert.rejects(
		packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ),
		error => {
			assert.ok( error instanceof Error );
			assert.match( error.message, /Pack SHA-256 mismatch/ );
			assert.equal( transientAssetFailure( error ), true );
			return true;
		}
	);
	packs.dispose();
});

test("a garbled partial pack header is a transient failure, not corrupt content", async () => {
	// The prefix read passes, then the header read itself arrives garbled:
	// both are unverified ranged bytes, so the header parse refuses the
	// body, not the content - the same class as the prefix identity
	// check before it (#2337).
	const f = fixture(), padded = new Uint8Array( 5 << 20 );
	padded.set( f.bytes );
	f.pack.bytes = padded.length;
	f.pack.sha256 = sha( padded );
	f.manifest.groups[0].packs[0].bytes = padded.length;
	f.manifest.groups[0].packs[0].sha256 = f.pack.sha256;
	const packs = createPacks( async ( url, limit, signal, range ) => {
		if ( url.endsWith( "manifest.json" ) ) return new TextEncoder().encode( JSON.stringify( f.manifest ) );
		const bytes = padded.slice( range.start, range.end + 1 );
		// Only the header download (the prefix read ends at 11) is garbled.
		if ( range.start === 0 && range.end > 11 ) bytes[0] = 0;
		return bytes;
	} );
	await assert.rejects(
		packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ),
		error => {
			assert.ok( error instanceof Error );
			assert.match( error.message, /Invalid pack identity/ );
			assert.equal( transientAssetFailure( error ), true );
			return true;
		}
	);
	packs.dispose();
});

test("member bytes that fail the manifest SHA-256 are a transient failure", async () => {
	// Header and manifest agree on the member's digest; only the delivered
	// bytes are wrong (one flipped in transit), and the pack digest is
	// recomputed so the container itself verifies. What remains is the
	// cut-or-substituted body the digest exists to catch (#2337).
	const f = fixture(), corrupt = f.bytes.slice();
	const headerLength = new DataView( corrupt.buffer ).getUint32( 8, true );
	corrupt[12 + headerLength] ^= 1;
	f.manifest.groups[0].packs[0].sha256 = sha( corrupt );
	const packs = createPacks( async url =>
		url.endsWith( "manifest.json" ) ? new TextEncoder().encode( JSON.stringify( f.manifest ) ) : corrupt
	);
	await assert.rejects(
		packs.read( new URL( "http://localhost/assets/a.bin" ), 100, new AbortController().signal ),
		error => {
			assert.ok( error instanceof Error );
			assert.match( error.message, /Asset SHA-256 mismatch/ );
			assert.equal( transientAssetFailure( error ), true );
			return true;
		}
	);
	packs.dispose();
});
