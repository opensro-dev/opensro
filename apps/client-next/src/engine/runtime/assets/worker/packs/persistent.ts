/*
===========================================================================

persistent.ts - the durable store of verified asset bytes

One owner for durable verified bytes, bounded publication and LRU order,
kept in Cache Storage keyed by content digest. Cache failure is an
optional-storage failure, never an asset admission failure.

===========================================================================
*/

import { readBytes } from "@/engine/foundation/assets/read-bytes";

/*
================
createPersistentAssets
================
*/
export function createPersistentAssets() {
	let opened: Promise<Cache | null> | null = null, tail: Promise<void> = Promise.resolve();
	let inventory: Map<string, number> | null = null, total = 0, budget = 512 << 20, estimatedAt = -Infinity;
	let hits = 0, misses = 0, writes = 0, errors = 0, evictions = 0, queuedBytes = 0, skipped = 0;
	const pending = new Set<string>(), touched = new Set<string>();
	function open() {
		return opened ??= Promise.resolve().then( () =>
			typeof caches === "undefined" ? null : caches.open( "sro-next-verified-v1" )
		).catch( () => {
			errors++;
			return null;
		} );
	}
	function key( origin: string, digest: string ) {
		return origin + "/assets/.verified/" + digest;
	}
	function touch( url: string ) {
		if ( inventory?.has( url ) ) {
			const size = inventory.get( url )!;
			inventory.delete( url );
			inventory.set( url, size );
		} else {
			touched.delete( url );
			touched.add( url );
			if ( touched.size > 65536 ) touched.delete( touched.values().next().value! );
		}
	}
	async function remove( origin: string, digest: string ) {
		try {
			const url = key( origin, digest );
			await (await open())?.delete( url );
			if ( inventory?.has( url ) ) {
				total -= inventory.get( url )!;
				inventory.delete( url );
			}
			touched.delete( url );
		} catch {
			errors++;
		}
	}
	async function publish( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
		const cache = await open();
		if ( !cache ) return;
		try {
			if ( performance.now() - estimatedAt >= 60000 ) {
				estimatedAt = performance.now();
				try {
					const estimate = await navigator.storage.estimate();
					if ( estimate.quota ) budget = Math.min( 512 << 20, Math.floor( estimate.quota / 4 ) );
				} catch {}
			}
			if ( bytes.length > budget ) return;
			if ( !inventory ) {
				inventory = new Map();
				total = 0;
				for ( const request of await cache.keys() ) {
					const response = await cache.match( request ),
						size = Number( response?.headers.get( "content-length" ) ) || 0;
					total += size;
					inventory.set( request.url, size );
				}
				const recent = [ ...touched ];
				touched.clear();
				for ( const url of recent ) if ( inventory.has( url ) ) touch( url );
			}
			const target = key( origin, digest );
			if ( inventory.has( target ) ) {
				touch( target );
				return;
			}
			async function evict() {
				const first = inventory!.entries().next().value;
				if ( !first ) return false;
				const [url, size] = first;
				await cache!.delete( url );
				total -= size;
				inventory!.delete( url );
				evictions++;
				return true;
			}
			while ( total + bytes.length > budget && await evict() ) {}
			const put = () =>
				cache.put(
					target,
					new Response( bytes, {
						headers: {
							"content-length": String( bytes.length ),
							"content-type": "application/octet-stream"
						}
					} )
				);
			try {
				await put();
			} catch ( error ) {
				if ( !(error instanceof DOMException) || error.name !== "QuotaExceededError" ) throw error;
				const count = Math.max( 1, Math.ceil( inventory.size / 4 ) );
				for ( let i = 0; i < count; i++ ) if ( !await evict() ) break;
				await put();
			}
			inventory.set( target, bytes.length );
			total += bytes.length;
			writes++;
		} catch {
			errors++;
			inventory = null;
		}
	}
	function write( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
		const operation = tail.then( () => publish( origin, digest, bytes ) );
		tail = operation.catch( () => {
			errors++;
		} );
		return tail;
	}
	return {
		// Whether verified bytes are stored, without reading them. A storage
		// failure reads as absent: the caller simply fetches again.
		async has( origin: string, digest: string ) {
			try {
				const response = await (await open())?.match( key( origin, digest ) );
				if ( !response ) return false;
				await response.body?.cancel();
				touch( key( origin, digest ) );
				return true;
			} catch {
				errors++;
				return false;
			}
		},
		async read( origin: string, digest: string, length: number ) {
			try {
				const url = key( origin, digest ), response = await (await open())?.match( url );
				if ( !response ) {
					if ( inventory?.has( url ) ) {
						total -= inventory.get( url )!;
						inventory.delete( url );
					}
					touched.delete( url );
					misses++;
					return null;
				}
				if ( Number( response.headers.get( "content-length" ) ) !== length || !response.body ) {
					await remove( origin, digest );
					misses++;
					return null;
				}
				const bytes = await readBytes( response.body, length );
				if ( bytes.length !== length ) throw Error( "Incomplete persistent asset" );
				hits++;
				touch( url );
				return bytes;
			} catch {
				errors++;
				await remove( origin, digest );
				return null;
			}
		},
		remove,
		write,
		// Copy before the caller transfers/detaches its buffer. Never wait for disk on
		// the admission path; excess demand skips optional persistence, not rendering.
		enqueue( origin: string, digest: string, bytes: Uint8Array<ArrayBuffer> ) {
			const id = key( origin, digest );
			if ( pending.has( id ) ) return;
			if ( pending.size >= 64 || queuedBytes + bytes.length > (32 << 20) ) {
				skipped++;
				return;
			}
			const owned = bytes.slice();
			queuedBytes += owned.length;
			pending.add( id );
			void write( origin, digest, owned ).finally( () => {
				queuedBytes -= owned.length;
				pending.delete( id );
			} );
		},
		flush: () => tail,
		stats: () => ({ hits, misses, writes, errors, evictions, queuedBytes, skipped })
	};
}
