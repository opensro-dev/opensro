/*
===========================================================================

pack-memo.mjs - the beta build's projected packs, remembered by content

The beta build projects every published pack (public-data.mjs projectPack:
private members out, members rewritten for the public audience) and
inspects the result (policy.mjs inspect) before it copies the payload into
the package. Both passes are pure: the same pack bytes, the same member
rows and the same code always give the same payload and the same verdict.
On r204m's re-freeze 458 of 459 packs came out byte-identical, yet the two
passes cost about 107 s of the package step (profile 2026-11-25 over
3.9 GB: projection 59 s, inspection 48 s; build-waste #28, #4386).

An entry is named by a digest of the code behind both passes (codeStamp
codeHash of public-data.mjs, whose closure includes policy.mjs), the
pack's path, sha256 and length, and its member rows. It records the
payload's sha256 and length, and stores the payload itself only when the
projection changed the bytes. Only a payload that passed inspection is
recorded, so a hit vouches for both passes. A hit whose stored payload no
longer matches its record is a miss, never an error: the pass reruns.

===========================================================================
*/
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { BUILD_STATE_ROOT, sharedCachePath } from "../../../../scripts/lib/generatedRoot.mjs";
import { codeHash } from "../../../../scripts/build/shared/codeStamp.mjs";
import { writeStateFile, writeStateJson } from "../../../../scripts/build/shared/stateFile.mjs";

export const PACK_MEMO_FORMAT = "sro-beta-pack-memo-v1";
const PACK_MEMO_NAME = "beta-pack-memo";
// The projection's entry module; its code closure includes the policy.
const PROJECTION_ENTRY = fileURLToPath( new URL( "./public-data.mjs", import.meta.url ) );

/*
================
digest
================
*/
function digest( bytes ) {
	return createHash( "sha256" ).update( bytes ).digest( "hex" );
}

/*
================
packMemoKey

The entry name: everything the two passes read.
================
*/
export function packMemoKey( codeKey, pack, members ) {
	return digest( JSON.stringify( [ PACK_MEMO_FORMAT, codeKey, pack.path, pack.sha256, pack.bytes, members ] ) );
}

/*
================
openPackMemo

root: the memo's folder, under the shared cache when the release sets one
(SRO_SHARED_CACHE_ROOT), else beside this tree's build state. codeKey:
the projection code's hash; a test passes its own.
================
*/
/** @param {{ root?: string, codeKey?: string }} [options] */
export async function openPackMemo(
	{ root = sharedCachePath( PACK_MEMO_NAME, path.join( BUILD_STATE_ROOT, PACK_MEMO_NAME ) ), codeKey } = {}
) {
	const code = codeKey ?? await codeHash( PROJECTION_ENTRY );
	const entryPath = key => path.join( root, key.slice( 0, 2 ), key + ".json" );
	const payloadPath = key => path.join( root, key.slice( 0, 2 ), key + ".bin" );
	return {
		/*
		================
		lookup

		The remembered payload for this pack, or null. packBytes are the
		pack as read, already checked against pack.sha256 by the caller.
		================
		*/
		async lookup( pack, members, packBytes ) {
			const key = packMemoKey( code, pack, members );
			let entry;
			try {
				entry = JSON.parse( await readFile( entryPath( key ), "utf8" ) );
			} catch {
				return null;
			}
			if ( entry?.format !== PACK_MEMO_FORMAT ) return null;
			if ( entry.same ) {
				return entry.sha256 === pack.sha256 && entry.length === packBytes.length ? packBytes : null;
			}
			let payload;
			try {
				payload = await readFile( payloadPath( key ) );
			} catch {
				return null;
			}
			return payload.length === entry.length && digest( payload ) === entry.sha256 ? payload : null;
		},
		/*
		================
		record

		Remember a payload that passed inspection. The payload is written
		before its entry, so a reader never finds an entry without it.
		================
		*/
		async record( pack, members, packBytes, payload ) {
			const key = packMemoKey( code, pack, members );
			const same = payload === packBytes || payload.equals( packBytes );
			if ( !same ) await writeStateFile( payloadPath( key ), payload );
			await writeStateJson( entryPath( key ), {
				format: PACK_MEMO_FORMAT,
				same,
				length: payload.length,
				sha256: same ? pack.sha256 : digest( payload )
			} );
		}
	};
}
