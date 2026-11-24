/*
===========================================================================

blocks.ts - ranged reads of a large pack's member blocks

A large pack is read by block: adjacent members grouped into reads of up
to 1 MiB, each fetched once per window while in flight and kept in a small
LRU of resident blocks. The block's name and its other members are given
to the pack reader (packs.ts) so one window fetches a block for every
window of the origin and stores all its members (#4268).

===========================================================================
*/

import type { PackDescriptor, PackEntry, PackDownload } from "./internal/pack-contract";
import { assetFailure } from "@/engine/foundation/assets/asset-recovery";
// Transport pages have no asset names or scene lists. The admitted manifest
// supplies the identity and bounds; consumers still verify each member digest.
// Owner: one pack reader. Inputs: origin, pack digest, byte offset and length.
// Invalidation: LRU pressure, failed download, disposal. Rebuilt on next demand.
// Only demanded, verified members enter persistent storage in the pack reader.

// The ranges kept in memory after their read. A block only serves the
// members beside the one that fetched it (a scene's textures arrive close
// together), and every demanded member is verified into the persistent store, so
// a phone pays for this cache in RAM it does not have: 16 blocks.
const BLOCK_CACHE_BYTES = 16 << 20;

export function createPackBlocks( download: PackDownload, signal: AbortSignal ) {
	const blockBytes = 1 << 20, budget = BLOCK_CACHE_BYTES, capacity = 4;
	const cache = new Map<string, Uint8Array<ArrayBuffer>>(),
		pending = new Map<string, Promise<Uint8Array<ArrayBuffer>>>();
	type Group = { start: number; end: number; members: PackEntry[]; };
	const plans = new Map<string, Map<string, Group>>();
	let resident = 0, disposed = false, rangeBytes = 0, demandedBytes = 0;
	const demanded = new Set<string>();
	function alive() {
		if ( disposed || signal.aborted ) throw Error( "Pack block owner disposed" );
	}
	function plan( pack: PackDescriptor ) {
		let table = plans.get( pack.sha256 );
		if ( table ) return table;
		table = new Map();
		let group: Group | undefined;
		for ( const entry of [ ...pack.entries ].sort( ( a, b ) => a.offset - b.offset ) ) {
			// Keep member boundaries: a large model is one exact read, while hundreds
			// of adjacent small textures become a handful of bounded reads.
			if ( !group || entry.offset + entry.span - group.start > blockBytes ) {
				group = { start: entry.offset, end: entry.offset + entry.span, members: [] };
			} else group.end = entry.offset + entry.span;
			group.members.push( entry );
			table.set( entry.path, group );
		}
		plans.set( pack.sha256, table );
		return table;
	}
	function blockKey( base: string, pack: PackDescriptor, start: number, end: number ) {
		return base + "/" + pack.sha256 + "/" + start + "/" + end;
	}
	/*
	================
	groupOf

	The block an entry is read in, checked against the pack's bounds.
	================
	*/
	function groupOf( pack: PackDescriptor, dataStart: number, entry: PackEntry ) {
		const group = plan( pack ).get( entry.path );
		if ( !group || dataStart < 12 || dataStart + group.end > pack.bytes ) {
			throw Error( "Invalid pack block range" );
		}
		return group;
	}
	async function block( base: string, pack: PackDescriptor, start: number, end: number ) {
		alive();
		const key = blockKey( base, pack, start, end );
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
			// A stream that ends cleanly short of its claimed range was cut
			// or re-framed in transit: transient, not content (#2337).
			if ( bytes.length !== end - start + 1 ) throw assetFailure( "Truncated pack block", true );
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
		/*
		================
		blockName

		The name of the block an entry is read in, the same in every
		window of the origin: the cross-window lock that lets one window
		fetch a block while the others wait for the store (#4268).
		================
		*/
		blockName( base: string, pack: PackDescriptor, dataStart: number, entry: PackEntry ) {
			const group = groupOf( pack, dataStart, entry );
			return "sro-pack-block:" + blockKey( base, pack, dataStart + group.start, dataStart + group.end - 1 );
		},
		/*
		================
		neighbours

		The other members of an entry's block, sliced from this window's
		resident copy: bytes already paid for that the persistent store
		may still lack. Empty once the block has left memory - never a
		fetch.
		================
		*/
		neighbours( base: string, pack: PackDescriptor, dataStart: number, entry: PackEntry ) {
			const group = groupOf( pack, dataStart, entry );
			const bytes = cache.get( blockKey( base, pack, dataStart + group.start, dataStart + group.end - 1 ) );
			if ( !bytes ) return [];
			return group.members.filter( member => member !== entry && member.length > 0 ).map( member => ({
				entry: member,
				stored: bytes.slice( member.offset - group.start, member.offset - group.start + member.span )
			}) );
		},
		async read( base: string, pack: PackDescriptor, dataStart: number, entry: PackEntry ) {
			alive();
			const group = groupOf( pack, dataStart, entry );
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
