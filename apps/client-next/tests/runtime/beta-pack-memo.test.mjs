/*
===========================================================================

beta-pack-memo.test.mjs - a remembered pack projection is reused only for the same inputs

The beta build skips projecting and inspecting a pack when the memo holds
it (tools/beta/pack-memo.mjs). A hit must need the same code, the same
pack and the same member rows, and a damaged entry must fall back to a
fresh pass, never ship its bytes.

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { openPackMemo } from "../../tools/beta/pack-memo.mjs";

const sha = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );

/*
================
fixture

A pack row and its bytes, as build.mjs holds them after the drift check.
================
*/
function fixture( text = "SROPACK pack bytes" ) {
	const bytes = Buffer.from( text );
	const pack = { path: "/assets/packs/a.sropack", sha256: sha( bytes ), bytes: bytes.length };
	const members = [ { path: "/assets/a.bin", packPath: pack.path, offset: 0, length: 3, sha256: "x" } ];
	return { bytes, pack, members };
}

/*
================
withRoot
================
*/
async function withRoot( run ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-pack-memo-" ) );
	try {
		await run( root );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
}

test("an unchanged projection is remembered as the pack itself", () =>
	withRoot( async root => {
		const memo = await openPackMemo( { root, codeKey: "code-1" } );
		const { bytes, pack, members } = fixture();
		assert.equal( await memo.lookup( pack, members, bytes ), null, "a cold memo misses" );
		await memo.record( pack, members, bytes, bytes );
		assert.equal( await memo.lookup( pack, members, bytes ), bytes );
		const stored = (await readdir( root, { recursive: true } )).filter( name => name.endsWith( ".bin" ) );
		assert.deepEqual( stored, [], "an identity projection stores no second copy" );
	} ));

test("a changed projection is served from its stored payload", () =>
	withRoot( async root => {
		const memo = await openPackMemo( { root, codeKey: "code-1" } );
		const { bytes, pack, members } = fixture();
		const projected = Buffer.from( "SROPACK projected" );
		await memo.record( pack, members, bytes, projected );
		const hit = await memo.lookup( pack, members, bytes );
		assert.ok( hit && hit.equals( projected ) );
	} ));

test("other code, another pack or other member rows miss", () =>
	withRoot( async root => {
		const { bytes, pack, members } = fixture();
		await (await openPackMemo( { root, codeKey: "code-1" } )).record( pack, members, bytes, bytes );
		const memo = await openPackMemo( { root, codeKey: "code-2" } );
		assert.equal( await memo.lookup( pack, members, bytes ), null, "the projection code changed" );
		const same = await openPackMemo( { root, codeKey: "code-1" } );
		const other = fixture( "SROPACK other bytes" );
		assert.equal( await same.lookup( other.pack, other.members, other.bytes ), null, "another pack" );
		const moved = [ { ...members[0], offset: 1 } ];
		assert.equal( await same.lookup( pack, moved, bytes ), null, "the member rows changed" );
	} ));

test("a damaged stored payload is a miss, never shipped", () =>
	withRoot( async root => {
		const memo = await openPackMemo( { root, codeKey: "code-1" } );
		const { bytes, pack, members } = fixture();
		await memo.record( pack, members, bytes, Buffer.from( "SROPACK projected" ) );
		const [stored] = (await readdir( root, { recursive: true } )).filter( name => name.endsWith( ".bin" ) );
		await writeFile( path.join( root, stored ), "SROPACK tampered!" );
		assert.equal( await memo.lookup( pack, members, bytes ), null );
	} ));
