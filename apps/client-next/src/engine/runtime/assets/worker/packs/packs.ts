/*
===========================================================================

packs.ts - verified reads from the published asset packs

Every asset the client loads is read here, in the asset worker: resolved
through the pack index, fetched the cheapest way the delivery allows, and
checked against the manifest's length and SHA-256 before anyone sees it.
Verified bytes are kept in the persistent store so a later session reads
them locally.

install() is the background half: it makes a file local without delivering
it, silently, so the installer never shows on a loading screen.

===========================================================================
*/

import { createPersistentAssets } from "./persistent";
import { createPackIndex } from "./index/index";
import type { PackDescriptor, PackEntry, PackDownload } from "./internal/pack-contract";
import { gunzipBytes } from "@/engine/foundation/assets/read-bytes";
import { createPackBlocks } from "./blocks";
export type { PackRange } from "./internal/pack-contract";

/*
================
AssetAbsentError

The published manifest has no entry for the path: no retry can find it in
this release. Consumers whose native counterpart falls back on a failed
load (the slot icons' icon_default) read this apart from transient faults.
================
*/
export class AssetAbsentError extends Error {
	constructor( pathname: string ) {
		super( `Asset absent from published manifest: ${pathname}` );
		this.name = "AssetAbsentError";
	}
}

// Packs up to this size are fetched and persisted whole; larger ones by member.
const SMALL_PACK_BYTES = 4 << 20;

/*
================
createPacks

The worker's pack reader: resolves a published path through the pack index
and serves verified bytes from the persistent store, a resident pack, a
per-asset transport, a pack member or a loose file.
================
*/
export function createPacks(
	download: PackDownload,
	activity: ( path: string, event: "start" | "ready" | "end" ) => void = () => {}
) {
	const persistent = createPersistentAssets(), parser = createPackIndex(), lifetime = new AbortController();
	const blocks = createPackBlocks( download, lifetime.signal );
	type Index = ReturnType<typeof parser.manifest>;
	type Loaded = { bytes: Uint8Array<ArrayBuffer>; start: number; };
	let origin: string | null = null, index: Promise<Index> | null = null, disposed = false, residentBytes = 0;
	const unpacked = new Set<string>();
	const loading = new Map<string, Promise<Loaded>>(), cache = new Map<string, Loaded>();
	// Manifest-owned offsets only; failed reads are evicted, disposal clears all.
	const transports = new Map<string, Promise<Uint8Array<ArrayBuffer>>>();
	let transportBytes = 0, transportDecodedBytes = 0, hashCalls = 0, hashBytes = 0;
	const headers = new Map<string, Promise<number>>();
	/*
	================
	member
	================
	*/
	async function member( base: string, descriptor: PackDescriptor, entry: PackEntry, signal: AbortSignal ) {
		let header = headers.get( descriptor.path );
		if ( !header ) {
			header = (async () => {
				const prefix = await download( base + descriptor.path, 12, lifetime.signal, {
					start: 0,
					end: 11,
					total: descriptor.bytes
				} );
				if ( prefix.length !== 12 || new TextDecoder().decode( prefix.subarray( 0, 8 ) ) !== "SROPACK1" ) {
					throw Error( "Invalid pack identity" );
				}
				const length = new DataView( prefix.buffer, prefix.byteOffset, 12 ).getUint32( 8, true );
				if ( length > (16 << 20) || length + 12 > descriptor.bytes ) {
					throw Error( "Invalid pack header length" );
				}
				const bytes = await download( base + descriptor.path, length + 12, lifetime.signal, {
					start: 0,
					end: length + 11,
					total: descriptor.bytes
				} );
				return parser.header( bytes, descriptor, true );
			})();
			headers.set( descriptor.path, header );
			void header.catch( () => {
				headers.delete( descriptor.path );
			} );
		}
		const start = await header;
		if ( disposed || signal.aborted ) throw Error( "Asset request cancelled" );
		const bytes = await blocks.read( base, descriptor, start, entry );
		return bytes;
	}
	/*
	================
	sha
	================
	*/
	async function sha( bytes: Uint8Array<ArrayBuffer> ) {
		hashCalls++;
		hashBytes += bytes.length;
		return Array.from(
			new Uint8Array( await crypto.subtle.digest( "SHA-256", bytes ) ),
			b => b.toString( 16 ).padStart( 2, "0" )
		).join( "" );
	}
	/*
	================
	manifest
	================
	*/
	function manifest( base: string ): Promise<Index> {
		if ( origin !== null && origin !== base ) throw new Error( "Asset origin changed during session" );
		origin = base;
		if ( !index ) {
			const operation = download( base + "/assets/packs/manifest.json", 16 << 20, lifetime.signal ).then(
				bytes => {
					const admitted = parser.manifest(
						JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) )
					);
					// The startup packs and their members are what eviction keeps.
					persistent.setStartup(
						base,
						[ ...admitted.packs.values() ].filter( pack => pack.load === "startup" ).flatMap( pack => [
							pack.sha256,
							...pack.entries.map( entry => entry.sha256 )
						] )
					);
					return admitted;
				}
			);
			index = operation;
			// Share in-flight work and successful admission, never cache a failure.
			void operation.catch( () => {
				if ( index === operation ) index = null;
			} );
		}
		return index;
	}
	/*
	================
	pack
	================
	*/
	async function pack( base: string, descriptor: PackDescriptor ): Promise<Loaded> {
		const hit = cache.get( descriptor.path );
		if ( hit ) {
			cache.delete( descriptor.path );
			cache.set( descriptor.path, hit );
			return hit;
		}
		const inflight = loading.get( descriptor.path );
		if ( inflight ) return inflight;
		// Cancelling a member does not cancel a shared pack fetch. A replacement
		// request waits for that capacity instead of permanently failing world entry.
		while ( loading.size >= 4 ) {
			await Promise.race( [ ...loading.values() ].map( work => work.catch( () => undefined ) ) );
			if ( disposed ) throw new Error( "Pack owner disposed" );
			const ready = cache.get( descriptor.path );
			if ( ready ) return ready;
			const shared = loading.get( descriptor.path );
			if ( shared ) return shared;
		}
		const operation = (async () => {
			let bytes = await persistent.read( base, descriptor.sha256, descriptor.bytes );
			if ( bytes && await sha( bytes ) !== descriptor.sha256 ) {
				await persistent.remove( base, descriptor.sha256 );
				bytes = null;
			}
			const missing = !bytes;
			bytes ??= await download( base + descriptor.path, descriptor.bytes, lifetime.signal );
			if ( missing && await sha( bytes ) !== descriptor.sha256 ) throw new Error( "Pack SHA-256 mismatch" );
			const loaded = { bytes, start: parser.header( bytes, descriptor ) };
			if ( disposed ) throw new Error( "Pack owner disposed" );
			if ( missing ) persistent.enqueue( base, descriptor.sha256, bytes );
			while ( residentBytes + bytes.length > (128 << 20) && cache.size ) {
				const key = cache.keys().next().value!;
				residentBytes -= cache.get( key )!.bytes.length;
				cache.delete( key );
			}
			cache.set( descriptor.path, loaded );
			residentBytes += bytes.length;
			return loaded;
		})();
		loading.set( descriptor.path, operation );
		try {
			return await operation;
		} finally {
			loading.delete( descriptor.path );
		}
	}
	/*
	================
	compressed
	================
	*/
	async function compressed( base: string, entry: PackEntry ) {
		const t = entry.transport!, key = t.sha256;
		while ( transports.size >= 4 && !transports.has( key ) ) {
			await Promise.race( [ ...transports.values() ].map( work => work.catch( () => {} ) ) );
			if ( disposed ) throw Error( "Pack owner disposed" );
		}
		let work = transports.get( key );
		if ( !work ) {
			work = (async () => {
				const encoded = await download( base + t.path, t.length, lifetime.signal );
				if ( encoded.length !== t.length || await sha( encoded ) !== t.sha256 ) {
					throw Error( "Compressed transport SHA-256 mismatch" );
				}
				const bytes = await gunzipBytes( encoded, entry.length );
				transportBytes += encoded.length;
				transportDecodedBytes += bytes.length;
				return bytes;
			})();
			transports.set( key, work );
			void work.finally( () => {
				transports.delete( key );
			} ).catch( () => {} );
		}
		return (await work).slice();
	}
	/*
	================
	loose
	================
	*/
	async function loose( base: string, entry: PackEntry, signal: AbortSignal ) {
		const bytes = await download( base + entry.publicPath, entry.length, signal );
		return bytes;
	}
	/*
	================
	readVerified

	One verified asset read: persistent store, resident pack, transport, pack
	member or loose file, in that order, checked against the manifest's
	length and SHA-256. `report` publishes loading activity; the background
	installer reads silently so it never shows on a loading screen.
	================
	*/
	async function readVerified(
		url: URL,
		limit: number,
		signal: AbortSignal,
		report: boolean
	): Promise<Uint8Array<ArrayBuffer>> {
		if ( disposed || signal.aborted ) throw new Error( "Asset request cancelled" );
		if ( report ) activity( url.pathname, "start" );
		try {
			const registry = await manifest( url.origin );
			// Manifest paths are filenames; URL pathname preserves percent escapes.
			const pathname = decodeURIComponent( url.pathname ).toLowerCase();
			let entry = registry.assets.get( pathname ), gzip = false;
			if ( !entry && pathname.endsWith( ".json" ) ) {
				entry = registry.assets.get( pathname + ".gz" );
				gzip = Boolean( entry );
			}
			if ( !entry ) throw new AssetAbsentError( url.pathname );
			if ( entry.length > limit ) throw new Error( "Asset exceeds byte budget" );
			if ( signal.aborted ) throw new Error( "Asset request cancelled" );
			let bytes: Uint8Array<ArrayBuffer>;
			// Loose deployments have no containers. A verified member surviving refresh
			// can satisfy demand directly without probing a known-absent pack again.
			let saved = cache.has( entry.packPath ) ?
				null :
				await persistent.read( url.origin, entry.sha256, entry.length );
			if ( saved && await sha( saved ) !== entry.sha256 ) {
				await persistent.remove( url.origin, entry.sha256 );
				saved = null;
			}
			if ( saved ) bytes = saved;
			else if ( cache.has( entry.packPath ) ) {
				const loaded = await pack( url.origin, registry.packs.get( entry.packPath )! );
				bytes = loaded.bytes.slice(
					loaded.start + entry.offset,
					loaded.start + entry.offset + entry.length
				);
			} else if ( entry.transport ) bytes = await compressed( url.origin, entry );
			else if ( unpacked.has( entry.packPath ) ) bytes = await loose( url.origin, entry, signal );
			else {
				try {
					const descriptor = registry.packs.get( entry.packPath )!;
					if ( descriptor.bytes > SMALL_PACK_BYTES ) {
						bytes = await member( url.origin, descriptor, entry, signal );
					} else {
						const loaded = await pack( url.origin, descriptor );
						bytes = loaded.bytes.slice(
							loaded.start + entry.offset,
							loaded.start + entry.offset + entry.length
						);
					}
				} catch ( error ) {
					// The installed authority supports loose and compact delivery. A missing
					// container does not remove the manifest's per-file integrity contract.
					// Never recover from corruption, cancellation or a different HTTP error.
					if ( !(error instanceof Error) || !("status" in error) || error.status !== 404 ) throw error;
					unpacked.add( entry.packPath );
					bytes = await loose( url.origin, entry, signal );
				}
			}
			if ( disposed || signal.aborted ) throw new Error( "Asset request cancelled" );
			if ( bytes.length !== entry.length ) throw new Error( "Asset length disagrees with manifest" );
			if ( !saved && await sha( bytes ) !== entry.sha256 ) throw new Error( "Asset SHA-256 mismatch" );
			if ( disposed || signal.aborted ) throw Error( "Asset request cancelled" );
			if ( !saved && !cache.has( entry.packPath ) ) {
				persistent.enqueue( url.origin, entry.sha256, bytes );
			}
			const result = gzip ?
				await gunzipBytes( bytes, limit ) :
				bytes;
			if ( disposed || signal.aborted ) throw Error( "Asset request cancelled" );
			if ( report ) activity( url.pathname, "ready" );
			return result;
		} finally {
			if ( report ) activity( url.pathname, "end" );
		}
	}

	/*
	================
	installed

	True when a read of this entry would be served from the persistent store:
	its own verified bytes, or, for a small pack read whole, the pack's.
	================
	*/
	async function installed( origin: string, registry: Awaited<Index>, entry: PackEntry ) {
		if ( await persistent.has( origin, entry.sha256 ) ) return true;
		const descriptor = registry.packs.get( entry.packPath );
		return Boolean(
			descriptor && descriptor.bytes <= SMALL_PACK_BYTES && await persistent.has( origin, descriptor.sha256 )
		);
	}

	/*
	================
	install

	Makes one published file local without delivering it: skipped when it is
	already persisted, otherwise read silently and written through before
	returning, so the persistence queue never drops it. Returns whether bytes
	were fetched.
	================
	*/
	async function install( url: URL, signal: AbortSignal ): Promise<boolean> {
		if ( disposed || signal.aborted ) return false;
		const registry = await manifest( url.origin );
		const entry = registry.assets.get( decodeURIComponent( url.pathname ).toLowerCase() );
		if ( !entry || await installed( url.origin, registry, entry ) ) return false;
		await readVerified( url, entry.length, signal, false );
		await persistent.flush();
		return true;
	}

	return {
		stats: () => ({
			...persistent.stats(),
			...blocks.stats(),
			transportBytes,
			transportDecodedBytes,
			hashCalls,
			hashBytes
		}),
		flush: () => persistent.flush(),
		read: ( url: URL, limit: number, signal: AbortSignal, report = true ) =>
			readVerified( url, limit, signal, report ),
		install,
		/*
		================
		worldAnimationManifests
		================
		*/
		async worldAnimationManifests( base: string, sources: ReadonlySet<string | undefined> ) {
			const registry = await manifest( base );
			const paths = registry.animations ?
				[
					...new Set(
						[ ...sources ].flatMap( source =>
							source ? registry.animations!.get( source.toLowerCase() ) ?? [] : []
						)
					)
				] :
				registry.animationManifests;
			paths.sort( ( a, b ) => registry.animationOrder.get( a )! - registry.animationOrder.get( b )! );
			return paths.map( path => path.replace( /\.gz$/, "" ) );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			lifetime.abort();
			blocks.dispose();
			index = null;
			cache.clear();
			headers.clear();
			transports.clear();
			loading.clear();
			unpacked.clear();
			residentBytes = 0;
		}
	};
}
