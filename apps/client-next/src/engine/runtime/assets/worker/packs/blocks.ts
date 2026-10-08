import type { PackDescriptor, PackEntry, PackDownload } from "./internal/pack-contract";
// Transport pages have no asset names or scene lists. The admitted manifest
// supplies the identity and bounds; consumers still verify each member digest.
// Owner: one pack reader. Inputs: origin, pack digest, byte offset and length.
// Invalidation: LRU pressure, failed download, disposal. Rebuilt on next demand.
// Only demanded, verified members enter persistent storage in the pack reader.
export function createPackBlocks( download: PackDownload, signal: AbortSignal ) {
	const blockBytes = 1 << 20, budget = 64 << 20, capacity = 4;
	const cache = new Map<string, Uint8Array<ArrayBuffer>>(),
		pending = new Map<string, Promise<Uint8Array<ArrayBuffer>>>();
	const plans = new Map<string, Map<string, { start: number; end: number; }>>();
	let resident = 0, disposed = false, rangeBytes = 0, demandedBytes = 0;
	const demanded = new Set<string>();
	function alive() {
		if ( disposed || signal.aborted ) throw Error( "Pack block owner disposed" );
	}
	function plan( pack: PackDescriptor ) {
		let table = plans.get( pack.sha256 );
		if ( table ) return table;
		table = new Map();
		let group: { start: number; end: number; } | undefined;
		for ( const entry of [ ...pack.entries ].sort( ( a, b ) => a.offset - b.offset ) ) {
			// Keep member boundaries: a large model is one exact read, while hundreds
			// of adjacent small textures become a handful of bounded reads.
			if ( !group || entry.offset + entry.span - group.start > blockBytes ) {
				group = { start: entry.offset, end: entry.offset + entry.span };
			} else group.end = entry.offset + entry.span;
			table.set( entry.path, group );
		}
		plans.set( pack.sha256, table );
		return table;
	}
	async function block( base: string, pack: PackDescriptor, start: number, end: number ) {
		alive();
		const key = base + "/" + pack.sha256 + "/" + start + "/" + end;
		function hit() {
			const bytes = cache.get( key );
			if ( bytes ) {
				cache.delete( key );
				cache.set( key, bytes );
			}
			return bytes;
		}
		const cached = hit();
		if ( cached ) return cached;
		if ( pending.has( key ) ) return pending.get( key )!;
		while ( pending.size >= capacity ) {
			await Promise.race( [ ...pending.values() ].map( work => work.catch( () => undefined ) ) );
			alive();
			const cached = hit();
			if ( cached ) return cached;
			if ( pending.has( key ) ) return pending.get( key )!;
		}
		const operation = (async () => {
			const bytes = await download( base + pack.path, end - start + 1, signal, {
				start,
				end,
				total: pack.bytes
			} );
			alive();
			if ( bytes.length !== end - start + 1 ) throw Error( "Truncated pack block" );
			rangeBytes += bytes.length;
			while ( resident + bytes.length > budget && cache.size ) {
				const oldest = cache.keys().next().value!;
				resident -= cache.get( oldest )!.length;
				cache.delete( oldest );
			}
			cache.set( key, bytes );
			resident += bytes.length;
			return bytes;
		})();
		pending.set( key, operation );
		try {
			return await operation;
		} finally {
			pending.delete( key );
		}
	}
	return {
		stats: () => ({ rangeBytes, demandedBytes }),
		async read( base: string, pack: PackDescriptor, dataStart: number, entry: PackEntry ) {
			alive();
			const group = plan( pack ).get( entry.path );
			if ( !group || dataStart < 12 || dataStart + group.end > pack.bytes ) {
				throw Error( "Invalid pack block range" );
			}
			if ( entry.length === 0 ) return new Uint8Array( 0 );
			const bytes = await block( base, pack, dataStart + group.start, dataStart + group.end - 1 );
			const key = pack.sha256 + "/" + entry.path;
			if ( !demanded.has( key ) ) {
				demanded.add( key );
				demandedBytes += entry.span;
			}
			// The stored bytes; the pack reader decodes a compressed member.
			return bytes.slice( entry.offset - group.start, entry.offset - group.start + entry.span );
		},
		dispose() {
			disposed = true;
			cache.clear();
			plans.clear();
			demanded.clear();
			resident = 0;
		}
	};
}
