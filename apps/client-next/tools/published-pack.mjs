import { readFile, stat } from "node:fs/promises";
import { createHash } from "node:crypto";
import { zstdDecompress } from "node:zlib";
import { promisify } from "node:util";
import { readPackPrefix } from "../../../scripts/build/shared/packFormat.mjs";
// The installed sidecar is authoritative. Decode once for identity-byte ranges;
// bound residency and concurrency, invalidate on source metadata change/close.
export function createPublishedPacks() {
	const decode = promisify( zstdDecompress ), cache = new Map(), pending = new Map();
	let resident = 0, disposed = false;
	return {
		async read( target ) {
			const match = /-([a-f0-9]{12})\.bin$/.exec( target );
			if ( !match ) return null;
			let source;
			try {
				source = await stat( target + ".zst" );
			} catch ( error ) {
				if ( error.code === "ENOENT" ) return null;
				throw error;
			}
			if ( source.size > (64 << 20) ) throw Error( "Compressed pack exceeds budget" );
			const key = target + ":" + source.size + ":" + source.mtimeMs + ":" + source.ctimeMs;
			const hit = () => {
				const row = cache.get( key );
				if ( row ) {
					cache.delete( key );
					cache.set( key, row );
				}
				return row;
			};
			const ready = hit();
			if ( ready ) return ready;
			if ( pending.has( key ) ) return pending.get( key );
			while ( pending.size >= 2 ) {
				await Promise.race( [ ...pending.values() ].map( work => work.catch( () => {} ) ) );
				if ( disposed ) throw Error( "Pack server closed" );
				const ready = hit();
				if ( ready ) return ready;
				if ( pending.has( key ) ) return pending.get( key );
			}
			const work = (async () => {
				const bytes = await decode( await readFile( target + ".zst" ), { maxOutputLength: 64 << 20 } );
				readPackPrefix( bytes, target );
				if ( !createHash( "sha256" ).update( bytes ).digest( "hex" ).startsWith( match[1] ) ) {
					throw Error( "Compressed pack identity mismatch" );
				}
				const row = {
					bytes,
					stat: { size: bytes.length, mtime: source.mtime, mtimeMs: source.mtimeMs, ctimeMs: source.ctimeMs }
				};
				if ( !disposed ) {
					while ( resident + bytes.length > (128 << 20) && cache.size ) {
						const oldest = cache.keys().next().value;
						resident -= cache.get( oldest ).bytes.length;
						cache.delete( oldest );
					}
					cache.set( key, row );
					resident += bytes.length;
				}
				return row;
			})();
			pending.set( key, work );
			try {
				return await work;
			} finally {
				pending.delete( key );
			}
		},
		dispose() {
			disposed = true;
			cache.clear();
			resident = 0;
		}
	};
}
