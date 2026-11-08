/*
===========================================================================

packs.ts - verified reads from the published asset packs

Every asset the client loads is read here, in the asset worker: resolved
through the pack index, fetched whole or by range, decoded when its pack
stores it gzip-compressed (SROPACK2, native DecompressionStream), and
checked against the manifest's length and SHA-256 before anyone sees it.
Verified bytes are kept in the persistent store so a later session reads
them locally.

install() is the background half: it makes a file local without delivering
it, silently, so the installer never shows on a loading screen.

===========================================================================
*/

import { createPersistentAssets } from "./persistent";
import { createPackIndex, PACK_MAGIC } from "./index/index";
import type { PackDescriptor, PackEntry, PackDownload } from "./internal/pack-contract";
import { assetFailure } from "@/engine/foundation/assets/asset-recovery";
import { gunzipBytes } from "@/engine/foundation/assets/read-bytes";
import { createPackBlocks } from "./blocks";
import {
	HD_TEXTURE_VARIANT_ROOT,
	hdTextureVariantPath,
	textureVariantPath,
	type HdTextureVariant,
	type TextureVariant
} from "@/engine/foundation/assets/texture-variants";
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

/*
================
untilCancelled

A shared wait (the manifest, a whole small pack, a large pack's header or
range block, the device's texture variant) seen from one request: it
rejects as soon as the request is cancelled, while the shared work goes on
for its other callers. A cancelled request's execution slot frees only
when the request settles (loader.ts), so a request parked on a slow
shared download held its slot - and the demand queued behind it - for the
whole download: "did not acknowledge cancelling /assets/effects/programs.json" (#2871).
================
*/
function untilCancelled<T>( work: Promise<T>, signal: AbortSignal ): Promise<T> {
	if ( signal.aborted ) return Promise.reject( new Error( "Asset request cancelled" ) );
	return new Promise<T>( ( resolve, reject ) => {
		const cancel = () => reject( new Error( "Asset request cancelled" ) );
		signal.addEventListener( "abort", cancel, { once: true } );
		work.then(
			value => {
				signal.removeEventListener( "abort", cancel );
				resolve( value );
			},
			error => {
				signal.removeEventListener( "abort", cancel );
				reject( error );
			}
		);
	} );
}

// Packs up to this size are fetched and persisted whole; larger ones by member.
const SMALL_PACK_BYTES = 4 << 20;
// Whole small packs kept in memory after their read: eight of them. A
// small pack is persisted whole when it is fetched (pack below), so one
// pushed out is reread from the persistent store, not downloaded again.
const RESIDENT_PACK_BYTES = 32 << 20;

/*
================
createPacks

The worker's pack reader: resolves a published path through the pack index
and serves verified bytes from the persistent store, a resident pack, a
pack member or a loose file.
================
*/
export function createPacks(
	download: PackDownload,
	activity: ( path: string, event: "start" | "ready" | "end" ) => void = () => {},
	hdAvailability: ( available: boolean ) => void = () => {}
) {
	const persistent = createPersistentAssets(), parser = createPackIndex(), lifetime = new AbortController();
	const blocks = createPackBlocks( download, lifetime.signal );
	type Index = ReturnType<typeof parser.manifest>;
	type Loaded = { bytes: Uint8Array<ArrayBuffer>; start: number; };
	let origin: string | null = null, index: Promise<Index> | null = null, disposed = false, residentBytes = 0;
	const unpacked = new Set<string>();
	const loading = new Map<string, Promise<Loaded>>(), cache = new Map<string, Loaded>();
	let storedBytes = 0, decodedBytes = 0, hashCalls = 0, hashBytes = 0;
	// The texture variant this device is served, decided once (textureVariant).
	let variant: Promise<TextureVariant | null> = Promise.resolve( null );
	// The HD set (#2061), decided live (hdTextures): null is the native
	// default, under which nothing below is ever fetched. hdIndex is the
	// set's own pack index, learned only once a format is chosen; the
	// install controller owns the download that follows the choice.
	type Served = { registry: Index; entry: PackEntry; };
	let hdDecision: HdTextureVariant | null = null,
		hdIndex: Promise<Index | null> | null = null,
		hdInstall: AbortController | null = null,
		hdInstallKicked = false;
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
				// The prefix is unverified network bytes: a garbled identity
				// or length is a cut or substituted body, not content (#2337).
				if ( prefix.length !== 12 || new TextDecoder().decode( prefix.subarray( 0, 8 ) ) !== PACK_MAGIC ) {
					throw assetFailure( "Invalid pack identity", true );
				}
				const length = new DataView( prefix.buffer, prefix.byteOffset, 12 ).getUint32( 8, true );
				if ( length > (16 << 20) || length + 12 > descriptor.bytes ) {
					throw assetFailure( "Invalid pack header length", true );
				}
				const bytes = await download( base + descriptor.path, length + 12, lifetime.signal, {
					start: 0,
					end: length + 11,
					total: descriptor.bytes
				} );
				try {
					return parser.header( bytes, descriptor, true );
				} catch ( error ) {
					// The header bytes are the same unverified ranged read
					// as the prefix above: a garbled identity, truncation
					// or unparsable table is a cut or substituted body,
					// not content (#2337). The whole-pack header parse in
					// pack() runs on SHA-verified bytes and stays permanent.
					throw assetFailure( error instanceof Error ? error.message : String( error ), true );
				}
			})();
			headers.set( descriptor.path, header );
			void header.catch( () => {
				headers.delete( descriptor.path );
			} );
		}
		const start = await untilCancelled( header, signal );
		if ( disposed || signal.aborted ) throw Error( "Asset request cancelled" );
		const stored = await untilCancelled( blocks.read( base, descriptor, start, entry ), signal );
		if ( disposed || signal.aborted ) throw Error( "Asset request cancelled" );
		return decoded( entry, stored );
	}
	/*
	================
	decoded

	A member's bytes as the manifest names them: the stored bytes of a raw
	member, or the gzip decoding of a compressed one. The caller still checks
	length and SHA-256.
	================
	*/
	async function decoded( entry: PackEntry, stored: Uint8Array<ArrayBuffer> ): Promise<Uint8Array<ArrayBuffer>> {
		if ( !entry.stored ) return stored;
		storedBytes += stored.length;
		decodedBytes += entry.length;
		let bytes: Uint8Array<ArrayBuffer>;
		try {
			bytes = await gunzipBytes( stored, entry.length );
		} catch ( error ) {
			// The stored bytes are unverified until the caller's digest
			// check, which this refusal pre-empts: a gzip that will not
			// inflate is a cut or substituted body, not content (#2337).
			throw assetFailure( String( error ), true );
		}
		if ( bytes.length !== entry.length ) throw assetFailure( "Stored member decodes to the wrong length", true );
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
		// A decision made before the origin was known starts its
		// download here, the first moment it can.
		kickHdInstall();
		if ( !index ) {
			const operation = download( base + "/assets/packs/manifest.json", 16 << 20, lifetime.signal ).then(
				bytes => {
					const admitted = parser.manifest(
						JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) )
					);
					// The published build's HD certification (#2800), told
					// once with the index that carries it.
					hdAvailability( admitted.hd );
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
	hdRegistry

	The HD set's own index, fetched from /hd only while a format is
	chosen (hdTextures). Any failure - a build without the set, a cut
	connection - admits an empty set: the originals serve, and the
	answer is kept for the rest of the decision so a missing set costs
	one fetch, not one per texture. A new decision learns again.
	================
	*/
	function hdRegistry( base: string ): Promise<Index | null> {
		if ( !hdIndex ) {
			hdIndex = download( base + "/hd/packs/manifest.json", 16 << 20, lifetime.signal )
				.then( bytes =>
					parser.manifest(
						JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) ),
						"/hd"
					)
				)
				.catch( () => null );
		}
		return hdIndex;
	}
	/*
	================
	kickHdInstall

	Starts the chosen set's download once its origin is known: the
	decision may arrive before the first manifest read names it, and
	manifest() calls here when it does. No decision, no fetch - with
	the row off this owner never names an /hd path.
	================
	*/
	function kickHdInstall() {
		if ( disposed || !hdDecision || !origin || hdInstallKicked ) return;
		hdInstallKicked = true;
		const controller = new AbortController();
		hdInstall = controller;
		void installHdSet( origin, controller.signal ).catch( () => {
			// The set stays demand-served; a foreground read of any of
			// its textures still upgrades on its own.
		} );
	}
	/*
	================
	installHdSet

	Makes the whole chosen set local, with activity reported so the
	download shows: each member is installed under its original path,
	whose read resolves to the HD entry (servedEntry), so the bytes
	land in the persistent store under the HD digest. Members are
	manual load, never startup-pinned, so the store's ordinary
	eviction owns them once the row is off again.
	================
	*/
	async function installHdSet( base: string, signal: AbortSignal ) {
		const registry = await hdRegistry( base );
		if ( !registry ) return;
		for ( const entry of registry.assets.values() ) {
			if ( disposed || signal.aborted ) return;
			// The original a member upgrades: the HD path minus its root
			// and format, under /assets again.
			const prefix = HD_TEXTURE_VARIANT_ROOT + hdDecision + "/";
			if ( !entry.path.startsWith( prefix ) ) continue;
			try {
				await install( new URL( base + "/assets/" + entry.path.slice( prefix.length ) ), signal, true );
			} catch {
				// One absent or failed member stops nothing; its first
				// read falls back to the original, as any miss does.
			}
		}
	}
	/*
	================
	hdSet

	The live HD decision: a format starts the set (index first, then
	the download), null stops the download and forgets the set, so the
	originals serve and a later switch-on learns afresh.
	================
	*/
	function hdSet( format: HdTextureVariant | null ) {
		if ( disposed || format === hdDecision ) return;
		hdInstall?.abort();
		hdInstall = null;
		hdInstallKicked = false;
		hdDecision = format;
		hdIndex = null;
		kickHdInstall();
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
			let bytes = await persistent.read( base, descriptor.sha256, descriptor.bytes, lifetime.signal );
			if ( bytes && await sha( bytes ) !== descriptor.sha256 ) {
				void persistent.remove( base, descriptor.sha256 );
				bytes = null;
			}
			const missing = !bytes;
			bytes ??= await download( base + descriptor.path, descriptor.bytes, lifetime.signal );
			// Wrong bytes, not wrong content: the persistent path above
			// evicts and refetches this same fact, so a network body cut or
			// re-framed in transit is transient for the bounded recovery
			// (#2337). A corrupt origin still ends visible when the
			// recovery budget runs out.
			if ( missing && await sha( bytes ) !== descriptor.sha256 ) {
				throw assetFailure( "Pack SHA-256 mismatch", true );
			}
			const loaded = { bytes, start: parser.header( bytes, descriptor ) };
			if ( disposed ) throw new Error( "Pack owner disposed" );
			if ( missing ) persistent.enqueue( base, descriptor.sha256, bytes );
			while ( residentBytes + bytes.length > RESIDENT_PACK_BYTES && cache.size ) {
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

	One verified asset read: persistent store, resident pack, pack
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
			const registry = await untilCancelled( manifest( url.origin ), signal );
			// Manifest paths are filenames; URL pathname preserves percent escapes.
			const pathname = decodeURIComponent( url.pathname ).toLowerCase();
			const served = await untilCancelled( servedEntry( url.origin, registry, pathname ), signal );
			let entry = served?.entry, gzip = false;
			// The registry that owns the entry: the HD set's own for an
			// HD member, whose pack the default index does not hold.
			const owner = served?.registry ?? registry;
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
				await persistent.read( url.origin, entry.sha256, entry.length, signal );
			if ( saved && await sha( saved ) !== entry.sha256 ) {
				void persistent.remove( url.origin, entry.sha256 );
				saved = null;
			}
			if ( saved ) bytes = saved;
			else if ( cache.has( entry.packPath ) ) {
				const loaded = await untilCancelled(
					pack( url.origin, owner.packs.get( entry.packPath )! ),
					signal
				);
				bytes = await decoded(
					entry,
					loaded.bytes.slice( loaded.start + entry.offset, loaded.start + entry.offset + entry.span )
				);
			} else if ( unpacked.has( entry.packPath ) ) bytes = await loose( url.origin, entry, signal );
			else {
				try {
					const descriptor = owner.packs.get( entry.packPath )!;
					if ( descriptor.bytes > SMALL_PACK_BYTES ) {
						bytes = await member( url.origin, descriptor, entry, signal );
					} else {
						const loaded = await untilCancelled( pack( url.origin, descriptor ), signal );
						bytes = await decoded(
							entry,
							loaded.bytes.slice(
								loaded.start + entry.offset,
								loaded.start + entry.offset + entry.span
							)
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
			// A short or substituted body again: wrong bytes, marked
			// transient like the pack digest above (#2337).
			if ( bytes.length !== entry.length ) throw assetFailure( "Asset length disagrees with manifest", true );
			if ( !saved && await sha( bytes ) !== entry.sha256 ) throw assetFailure( "Asset SHA-256 mismatch", true );
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
	its own verified bytes, or, for a small pack read whole, the pack's. null
	when the store cannot answer.
	================
	*/
	async function installed(
		origin: string,
		registry: Awaited<Index>,
		entry: PackEntry,
		signal: AbortSignal
	): Promise<boolean | null> {
		const own = await persistent.has( origin, entry.sha256, signal );
		if ( own !== false ) return own;
		const descriptor = registry.packs.get( entry.packPath );
		if ( !descriptor || descriptor.bytes > SMALL_PACK_BYTES ) return false;
		return persistent.has( origin, descriptor.sha256, signal );
	}

	/*
	================
	servedEntry

	The manifest entry a path is served from, and the registry that owns
	it: with the HD row on, an HD member of the set's own index, ahead of
	a native texture's published variant when this device is served one
	and the build made it (texture-variants.ts), else the path's own
	entry. The retail texture is the fallback at every step: a path the
	HD set does not hold serves its original, and no read waits for the
	HD decision - it only upgrades what the decision already names.
	================
	*/
	async function servedEntry( base: string, registry: Index, pathname: string ): Promise<Served | undefined> {
		if ( hdDecision && pathname.endsWith( ".texture" ) ) {
			const hdPath = hdTextureVariantPath( pathname, hdDecision );
			if ( hdPath ) {
				const hdReg = await hdRegistry( base );
				const hdEntry = hdReg?.assets.get( hdPath );
				if ( hdReg && hdEntry ) return { registry: hdReg, entry: hdEntry };
			}
		}
		const served = pathname.endsWith( ".texture" ) ? await variant : null;
		const variantPath = served ? textureVariantPath( pathname, served ) : null;
		const entry = (variantPath && registry.assets.get( variantPath )) || registry.assets.get( pathname );
		return entry ? { registry, entry } : undefined;
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
	async function install( url: URL, signal: AbortSignal, report = false ): Promise<boolean> {
		if ( disposed || signal.aborted ) return false;
		const registry = await untilCancelled( manifest( url.origin ), signal );
		const served = await untilCancelled(
			servedEntry( url.origin, registry, decodeURIComponent( url.pathname ).toLowerCase() ),
			signal
		);
		// An unanswerable store (suspended, saturated) skips the file: a fetch
		// could not be stored, and its first play fetches it on demand anyway.
		if ( !served || await installed( url.origin, served.registry, served.entry, signal ) !== false ) return false;
		await readVerified( url, served.entry.length, signal, report );
		await persistent.flush();
		return true;
	}

	return {
		/*
		================
		textureVariant

		Sets the variant this device is served. Reads of native textures wait
		for it, so none resolves before the decision.
		================
		*/
		textureVariant( decision: Promise<TextureVariant | null> ) {
			variant = decision;
		},
		/*
		================
		hdTextures

		The HD row's live decision (#2061): the format this device is
		served, or null for the native textures. May change while the
		worker runs; nothing is fetched while it stays null.
		================
		*/
		hdTextures( format: HdTextureVariant | null ) {
			hdSet( format );
		},
		stats: () => ({
			...persistent.stats(),
			...blocks.stats(),
			storedBytes,
			decodedBytes,
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
			hdInstall?.abort();
			hdInstall = null;
			hdDecision = null;
			hdIndex = null;
			lifetime.abort();
			blocks.dispose();
			index = null;
			cache.clear();
			headers.clear();
			loading.clear();
			unpacked.clear();
			residentBytes = 0;
		}
	};
}
