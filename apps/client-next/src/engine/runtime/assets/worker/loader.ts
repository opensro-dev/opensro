/*
===========================================================================

loader.ts - the asset worker's request dispatcher

Receives load, cancel and install requests from the main thread. Each load
reads verified bytes through the pack reader, decodes them for the
requested kind (image, model, character, world, effects, navigation) and
transfers the result back. Foreground loads hold one of four capacity
slots; the background installer runs only while none is held.

===========================================================================
*/

import { decodeGuildCrest } from "@/engine/foundation/ui/guild-crest";
import { assetRequestBudget } from "@/engine/foundation/assets/asset-budget";
import { prepareWorldScene, worldSceneTransfers } from "@/engine/foundation/rendering/world-scene";
import { createNavigationResources } from "./navigation/navigation";
import { decodeDxt1 } from "@/engine/foundation/assets/dds";
import { createEffectDecoder } from "./effects/effects";
import {
	decodeNativeTexture,
	NATIVE_TEXTURE_MIME,
	validateNativeTexture
} from "@/engine/foundation/assets/native-texture";
import type { WorldTexture } from "@/engine/contracts/texture";
import { pngBytes, DECODED_IMAGE_BYTES } from "@/engine/foundation/assets/image-budget";
import { createWorldDecoder } from "./world/world";
import { createModelDecoder } from "./model/model";
import { AssetAbsentError, createPacks } from "./packs/packs";
import { createBackgroundInstaller } from "./install";
import { pageEntryBundle } from "@/engine/foundation/assets/page-entry";
import { readBytes } from "@/engine/foundation/assets/read-bytes";
import { assetFailure, transientAssetFailure } from "@/engine/foundation/assets/asset-recovery";
import type { AssetRequest, AssetWorkerMessage } from "@/engine/contracts/assets";
import { rgbaPickAlpha, bitmapPickAlpha } from "@/engine/foundation/rendering/pick-alpha";
// Loading-screen progress publications are coalesced to this interval.
const PROGRESS_INTERVAL_MS = 150;
const DOWNLOAD_RETRY_DELAYS_MS = [ 250, 1000 ] as const;
const TRANSIENT_HTTP_STATUS = new Set( [ 408, 429, 500, 502, 503, 504 ] );
/*
================
createLoader

Own decoders, foreground requests and the background installation scheduler.
================
*/
export function createLoader( send: ( result: AssetWorkerMessage, transfer: Transferable[] ) => void ) {
	const navigation = createNavigationResources();
	const effects = createEffectDecoder();
	const worlds = createWorldDecoder();
	const models = createModelDecoder();
	// Parsed immutable metadata belongs to this worker/manifest lifetime. Model
	// buffers remain owned by individual decode transactions and are transferred.
	const animationCatalogs = new Map<
		string,
		{ objects: Record<string, NonNullable<Awaited<ReturnType<typeof worlds.resolve>>["animated"]>[number]>; }
	>();
	// Demand can be cancelled immediately; execution holds capacity until its
	// promise settles, including native decodes that AbortSignal cannot stop.
	const pending = new Map<number, AbortController>();
	const active = new Set<AbortController>();
	let disposed = false, effectBytes: Uint8Array<ArrayBuffer> | null = null;
	const files = new Map<string, number>(), readyFiles = new Set<string>();
	let bytesRead = 0;
	let received = 0, lastReceived = 0, lastProgress = performance.now(), speed = 0;
	let progressTimer: ReturnType<typeof setTimeout> | null = null;
	/*
	================
	progress

	Publish installation progress, at most once per PROGRESS_INTERVAL_MS.
	A change inside the interval arms one trailing publication that carries
	the latest state, so a settled queue still reaches the loading screen.
	Publishing every settle instead posted a message per file read (0.8 s of
	worker time in a 17 s streaming trace, plus its main-thread receipt).
	================
	*/
	function progress() {
		const elapsed = performance.now() - lastProgress;
		if ( elapsed >= PROGRESS_INTERVAL_MS ) {
			publishProgress();
			return;
		}
		progressTimer ??= setTimeout( publishProgress, PROGRESS_INTERVAL_MS - elapsed );
	}
	/*
	================
	publishProgress
	================
	*/
	function publishProgress() {
		if ( progressTimer !== null ) clearTimeout( progressTimer );
		progressTimer = null;
		const now = performance.now(), elapsed = now - lastProgress;
		if ( elapsed > 0 ) speed = (received - lastReceived) * 1000 / elapsed;
		lastReceived = received;
		lastProgress = now;
		if ( !disposed ) {
			send( {
				kind: "progress",
				progress: {
					bytesRead,
					bytesReceived: received,
					bytesPerSecond: speed,
					filesReady: readyFiles.size,
					filesActive: files.size,
					cacheHits: packs.stats().hits,
					currentFile: [ ...files.keys() ].at( -1 ) ?? ""
				}
			}, [] );
		}
	}
	/*
	================
	activity

	Track foreground activity so installation cannot compete with an active load.
	================
	*/
	function activity( path: string, event: "start" | "ready" | "end" ) {
		if ( event === "start" ) files.set( path, (files.get( path ) ?? 0) + 1 );
		else if ( event === "ready" ) readyFiles.add( path );
		else {
			const count = (files.get( path ) ?? 1) - 1;
			if ( count ) files.set( path, count );
			else files.delete( path );
		}
		progress();
	}
	/*
	================
	download

	Read a bounded asset through the shared pack and network owner.
	================
	*/
	async function download(
		url: string,
		limit: number,
		signal: AbortSignal,
		range?: import("./packs/packs").PackRange
	) {
		for ( let attempt = 0;; attempt++ ) {
			signal.throwIfAborted();
			try {
				return await downloadAttempt( url, limit, signal, range );
			} catch ( error ) {
				const transient = error instanceof TypeError ||
					(error instanceof Error && "status" in error &&
						TRANSIENT_HTTP_STATUS.has( Number( error.status ) ));
				const delay = DOWNLOAD_RETRY_DELAYS_MS[attempt];
				if ( signal.aborted || !transient ) throw error;
				if ( delay === undefined ) throw assetFailure( String( error ), true );
				await retryDownloadAfter( delay, signal );
			}
		}
	}
	/*
	================
	retryDownloadAfter

	Keep the existing request slot during backoff. Cancellation releases the
	timer immediately; a disposed worker must never start another download.
	================
	*/
	function retryDownloadAfter( delay: number, signal: AbortSignal ): Promise<void> {
		return new Promise( ( resolve, reject ) => {
			/*
			================
			cancel
			================
			*/
			function cancel() {
				clearTimeout( timer );
				signal.removeEventListener( "abort", cancel );
				reject( signal.reason );
			}
			const timer = setTimeout( () => {
				signal.removeEventListener( "abort", cancel );
				resolve();
			}, delay );
			signal.addEventListener( "abort", cancel, { once: true } );
			if ( signal.aborted ) cancel();
		} );
	}
	/*
	================
	downloadAttempt

	Only transport failures are retried. Range validation and byte limits fail
	immediately; manifest, hash and decoder validation stay with their owners.
	================
	*/
	async function downloadAttempt(
		url: string,
		limit: number,
		signal: AbortSignal,
		range?: import("./packs/packs").PackRange
	) {
		const started = performance.now();
		// Cache Storage owns verified members. Keep partial HTTP responses out
		// of the browser cache: different ranges share the container URL.
		const response = await fetch( url, {
			signal,
			credentials: "omit",
			redirect: "error",
			cache: range ?
				"no-store" :
				/(?:\/marks\/[GA][0-9_]+\.crb|\/assets\/packs\/transport\/[a-f0-9]{64}\.gz)$/.test(
						new URL( url ).pathname
					) ?
				"default" :
				"no-cache",
			headers: range ? { Range: `bytes=${range.start}-${range.end}` } : undefined
		} );
		if ( !response.ok || !response.body ) {
			await response.body?.cancel().catch( () => {} );
			throw Object.assign( new Error( `Asset HTTP ${response.status}` ), { status: response.status } );
		}
		if (
			range &&
			(response.status !== 206 ||
				response.headers.get( "content-range" ) !== `bytes ${range.start}-${range.end}/${range.total}`)
		) {
			await response.body.cancel().catch( () => {} );
			throw Error( "Invalid asset range response" );
		}
		const bytes = await readBytes( response.body, limit, size => {
			bytesRead += size;
			if ( range ) received += size;
			progress();
		} );
		// Ranges are identity bytes with no HTTP cache. For other responses,
		// Resource Timing distinguishes compressed transfers from HTTP cache
		// reads; counting decoded stream chunks would invent warm downloads.
		if ( !range ) {
			const timing = performance.getEntriesByName( url, "resource" ).filter( row => row.startTime >= started ).at(
				-1
			) as PerformanceResourceTiming | undefined;
			if ( timing ) received += timing.transferSize;
			progress();
		}
		return bytes;
	}
	const packs = createPacks( download, activity );
	// Background work runs only while no foreground load holds capacity. The
	// last foreground load to settle wakes it: an event, not a poll.
	let idleWaiters: (() => void)[] = [];
	/*
	================
	foregroundIdle

	Wait until no foreground owner holds a request slot.
	================
	*/
	function foregroundIdle(): Promise<void> {
		if ( active.size === 0 || disposed ) return Promise.resolve();
		return new Promise( ( resolve ) => idleWaiters.push( resolve ) );
	}
	/*
	================
	wakeIdleWaiters

	Release installation waiters only after foreground work has settled.
	================
	*/
	function wakeIdleWaiters() {
		const waiters = idleWaiters;
		idleWaiters = [];
		for ( const resolve of waiters ) resolve();
	}
	// The install list describes this release, like the pack index itself.
	// It is a release route rather than a member of the packs it enumerates;
	// each listed asset still passes through packs.install and SHA verification.
	const installer = createBackgroundInstaller( {
		read: ( url, limit, signal ) => download( url.href, limit, signal ),
		install: packs.install
	}, foregroundIdle );
	/*
	================
	load

	Decode a bounded request and transfer resource ownership exactly once.
	================
	*/
	async function load(
		request: Extract<AssetRequest, {
			kind: "load";
		}>,
		controller: AbortController
	) {
		try {
			const url = new URL( request.url );
			if ( ![ "http:", "https:" ].includes( url.protocol ) || url.username || url.password ) {
				throw new Error( "Invalid asset URL" );
			}
			const bytes = request.decode === "effect" && effectBytes ?
				effectBytes :
				url.pathname.startsWith( "/assets/" ) ?
				await packs.read( url, request.limit, controller.signal ) :
				await download( url.href, request.limit, controller.signal );
			if ( !disposed && pending.get( request.id ) === controller ) {
				if ( request.decode === "navigation" ) {
					let size = bytes.byteLength;
					const product = await navigation.resolve( bytes, Number( url.hash.slice( 1 ) ), async path => {
						if (
							typeof path !== "string" || !path.startsWith( "/assets/" ) || path.includes( ".." ) ||
							path.includes( "\\" )
						) throw new Error( "Invalid navigation asset path" );
						const data = await packs.read( new URL( path, url.origin ), 64 << 20, controller.signal );
						size += data.byteLength;
						if ( size > 128 << 20 ) throw new Error( "Navigation transaction budget" );
						return data;
					} );
					if ( disposed || controller.signal.aborted || pending.get( request.id ) !== controller ) return;
					pending.delete( request.id );
					send( { kind: "navigation", id: request.id, product }, [] );
				} else if ( request.decode === "effects" ) {
					const catalog = effects.decode( bytes );
					pending.delete( request.id );
					send( { kind: "effects", id: request.id, catalog }, [] );
				} else if ( (request.decode === "world" || request.decode === "frontend-world") ) {
					let total = bytes.byteLength;
					/*
					================
					readWorldResource

					Resolve authored world dependencies through the same bounded asset reader.
					================
					*/
					async function readWorldResource( path: string ) {
						if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
							throw new Error( "Invalid world resource path" );
						}
						const resource = await packs.read( new URL( path, url.origin ), 128 << 20, controller.signal );
						total += resource.byteLength;
						if ( total > (128 << 20) ) throw new Error( "World resource transaction exceeds budget" );
						return resource;
					}
					// An outdoor scene names its coordinate anchor and which part it
					// wants (world.ts WorldDecodeOptions); a terrain part is one region.
					const hash = new URLSearchParams( url.hash.slice( 1 ) ),
						anchor = hash.get( "anchor" ),
						part = (hash.get( "part" ) ?? "all") as import("./world/world").WorldDecodePart,
						origin = anchor === null ? undefined : Number.parseInt( anchor, 16 );
					if ( anchor !== null && !/^[0-9a-f]{1,4}$/.test( anchor ) ) {
						throw new Error( "Invalid world anchor" );
					}
					if ( part === "terrain" ) {
						const terrain = await worlds.resolveTerrain( bytes, readWorldResource, url.pathname );
						if ( disposed || controller.signal.aborted || pending.get( request.id ) !== controller ) return;
						const prepared = prepareWorldScene( worlds.decode( terrain, false, { origin, part } ) );
						send( { kind: "world", id: request.id, prepared }, worldSceneTransfers( prepared.scene ) );
						pending.delete( request.id );
						return;
					}
					const resolved = await worlds.resolve( bytes, readWorldResource, url.pathname );
					if ( disposed || controller.signal.aborted || pending.get( request.id ) !== controller ) return;
					const placedIds = new Set( resolved.objects.placements.map( row => row.objectId ) );
					const refs = new Set(
						resolved.objects.resources.bsr.filter( row => placedIds.has( row.objectId ) ).flatMap( row =>
							(row.branches ?? [ row ]).map( branch => branch.sourcePath?.toLowerCase() )
						)
					);
					const animated = new Map<string, NonNullable<typeof resolved.animated>[number]>();
					for ( const path of await packs.worldAnimationManifests( url.origin, refs ) ) {
						let manifest = animationCatalogs.get( path );
						if ( !manifest ) {
							const document = JSON.parse(
								new TextDecoder().decode( await readWorldResource( path ) )
							) as {
								objects: Record<
									string,
									Omit<NonNullable<typeof resolved.animated>[number], "sourcePath">
								>;
							};
							manifest = {
								objects: Object.fromEntries(
									Object.entries( document.objects ).map( (
										[sourcePath, entry]
									) => [ sourcePath.toLowerCase(), { ...entry, sourcePath } ] )
								)
							};
							animationCatalogs.set( path, manifest );
						}
						for ( const sourcePath of refs ) {
							if ( sourcePath && manifest.objects[sourcePath] ) {
								animated.set( sourcePath, manifest.objects[sourcePath] );
							}
						}
					}
					// GLBs provide geometry and animation only. Native BMTs own
					// world texture identity and draw state, for both backends.
					for ( const entry of animated.values() ) {
						entry.model = {
							...models.character( models.decode( await readWorldResource( entry.glbPublicPath ) ) ),
							images: []
						};
						if ( disposed || controller.signal.aborted || pending.get( request.id ) !== controller ) return;
					}
					resolved.animated = [ ...animated.values() ];
					let scene = worlds.decode( resolved, request.decode === "frontend-world", { origin, part } );
					const propsPath = new URLSearchParams( url.hash.slice( 1 ) ).get( "props" );
					if ( propsPath && request.decode === "frontend-world" ) {
						const manifest = JSON.parse(
							new TextDecoder( "utf-8", { fatal: true } ).decode( await readWorldResource( propsPath ) )
						) as {
							props: {
								objectId: number;
								name: string;
								position: { x: number; y: number; z: number; };
								yaw: number;
								scale: number;
							}[];
							resources: typeof resolved.objects.resources;
						};
						const added = [], addedScenery: NonNullable<typeof scene.scenery>[number][] = [];
						for ( const prop of manifest.props ) {
							if ( prop.name === "idol_lizard" ) continue;
							if ( prop.scale !== 1 ) throw Error( "Unsupported interface model scale" );
							const region = resolved.source.sectorX | (resolved.source.sectorY << 8);
							const product = worlds.decode( {
								source: resolved.source,
								terrain: { blocks: [] },
								terrainTextures: { tileCatalog: { referencedTiles: [] } },
								objects: {
									resources: manifest.resources,
									placements: [ { ...prop, uid: prop.objectId, regionId: String( region ) } ]
								}
							}, true );
							added.push(
								...product.groups.map( group => ({
									...group,
									id: "interface:" + prop.objectId + ":" + group.id,
									visibility: undefined,
									material: { ...group.material, objectFade: false }
								}) )
							);
							addedScenery.push(
								...(product.scenery ?? []).map( emitter => ({
									...emitter,
									id: "interface:" + prop.objectId + ":" + emitter.id,
									placement: "interface:" + prop.objectId + ":" + emitter.placement
								}) )
							);
						}
						scene = {
							...scene,
							groups: [ ...scene.groups, ...added ],
							scenery: [ ...(scene.scenery ?? []), ...addedScenery ]
						};
					}
					if ( disposed || controller.signal.aborted || pending.get( request.id ) !== controller ) return;
					const prepared = prepareWorldScene( scene ), transfer = worldSceneTransfers( prepared.scene );
					send( { kind: "world", id: request.id, prepared }, transfer );
					pending.delete( request.id );
				} else if ( request.decode === "character" || request.decode === "effect" ) {
					let model: import("@/engine/contracts/character").CharacterSource;
					if ( request.decode === "effect" ) {
						effectBytes = bytes;
						const decoded = effects.model( bytes, decodeURIComponent( url.hash.slice( 1 ) ) ), raw = [];
						let size = 0;
						for ( const path of decoded.imagePaths ) {
							if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) ) {
								throw new Error( "Invalid effect image path" );
							}
							const pixels = await packs.read( new URL( path, url.origin ), 16 << 20, controller.signal );
							size += pixels.byteLength;
							if ( size > (32 << 20) ) throw new Error( "Effect image transaction exceeds budget" );
							raw.push( { bytes: pixels, mime: "image/png" } );
						}
						model = { ...decoded.model, images: raw };
					} else model = models.character( models.decode( bytes ) );
					const images: WorldTexture[] = [];
					try {
						const nativeImages = model.images.map( image =>
							image.mime === NATIVE_TEXTURE_MIME ? decodeNativeTexture( image.bytes ) : null
						);
						const decoded = model.images.reduce(
							( sum, image, index ) =>
								sum + (nativeImages[index] ?
									validateNativeTexture( nativeImages[index]! ) :
									pngBytes( image.bytes )),
							0
						);
						if ( decoded > DECODED_IMAGE_BYTES ) {
							throw new Error( "Character images exceed decoded image budget" );
						}
						for ( const [index, image] of model.images.entries() ) {
							const bitmap = nativeImages[index] ?? await createImageBitmap(
								new Blob( [ image.bytes as Uint8Array<ArrayBuffer> ], { type: image.mime } ),
								{ premultiplyAlpha: "none", colorSpaceConversion: "none" }
							);
							images.push( bitmap );
							if ( disposed || controller.signal.aborted ) {
								for ( const bitmap of images ) if ( !("kind" in bitmap) ) bitmap.close();
								return;
							}
						}
						const transfers = new Set<Transferable>();
						for ( const image of images ) {
							if ( "kind" in image ) {
								for ( const level of image.levels ) transfers.add( level.buffer as ArrayBuffer );
							} else transfers.add( image );
						}
						for ( const p of model.primitives ) {
							for (
								const a of [
									p.inverseBind,
									p.geometry.positions,
									p.geometry.normals,
									p.geometry.uvs,
									p.geometry.indices,
									p.geometry.joints,
									p.geometry.weights,
									p.geometry.transform
								]
							) if ( a ) transfers.add( a.buffer as ArrayBuffer );
						}
						for ( const clip of model.clips ) {
							for ( const channel of clip.channels ) {
								transfers.add( channel.times.buffer as ArrayBuffer );
								transfers.add( channel.values.buffer as ArrayBuffer );
							}
						}
						// The page retains either native mip blocks or decoded bitmaps.
						// Encoded PNGs stay in the worker and become collectible.
						const delivered: import("@/engine/contracts/character").CharacterModel = {
							...model,
							images: images.map( bitmap => ({ width: bitmap.width, height: bitmap.height }) )
						};
						pending.delete( request.id );
						send( { kind: "character", id: request.id, model: delivered, images }, [ ...transfers ] );
					} catch ( error ) {
						for ( const image of images ) if ( !("kind" in image) ) image.close();
						throw error;
					}
				} else if ( request.decode === "glb" ) {
					const model = models.decode( bytes );
					pending.delete( request.id );
					send( { kind: "model", id: request.id, model }, [ model.binary ] );
				} else if ( request.decode === "crest" ) {
					const image = await createImageBitmap( new ImageData( decodeGuildCrest( bytes ), 16, 16 ), {
						premultiplyAlpha: "none",
						colorSpaceConversion: "none"
					} );
					if ( disposed || controller.signal.aborted ) {
						image.close();
						return;
					}
					pending.delete( request.id );
					send( { kind: "image", id: request.id, image }, [ image ] );
				} else if ( request.decode === "dds" ) {
					const decoded = decodeDxt1( bytes, DECODED_IMAGE_BYTES ),
						alpha = request.pickAlpha ?
							rgbaPickAlpha( decoded.width, decoded.height, decoded.pixels ) :
							undefined,
						image = await createImageBitmap(
							new ImageData( decoded.pixels, decoded.width, decoded.height ),
							{ premultiplyAlpha: "none", colorSpaceConversion: "none" }
						);
					if ( disposed || controller.signal.aborted ) {
						image.close();
						return;
					}
					pending.delete( request.id );
					send(
						{ kind: "image", id: request.id, image, ...(alpha ? { alpha } : {}) },
						alpha ? [ image, alpha.pixels.buffer ] : [ image ]
					);
				} else if ( request.decode === "png" ) {
					pngBytes( bytes );
					const image = await createImageBitmap( new Blob( [ bytes ], { type: "image/png" } ), {
						premultiplyAlpha: "none",
						colorSpaceConversion: "none"
					} );
					if ( disposed || controller.signal.aborted ) {
						image.close();
						return;
					}
					// World textures read their picking mask here, off the main thread.
					const alpha = request.pickAlpha ? bitmapPickAlpha( image ) : undefined;
					pending.delete( request.id );
					send(
						{ kind: "image", id: request.id, image, ...(alpha ? { alpha } : {}) },
						alpha ? [ image, alpha.pixels.buffer ] : [ image ]
					);
				} else if ( request.decode === "release" ) {
					// The live page (fetched no-cache): report the entry bundle it names.
					const html = new TextDecoder( "utf-8" ).decode( bytes );
					pending.delete( request.id );
					send( { kind: "release", id: request.id, entry: pageEntryBundle( html, url.origin ) }, [] );
				} else {
					pending.delete( request.id );
					send( { kind: "bytes", id: request.id, buffer: bytes.buffer }, [ bytes.buffer ] );
				}
			}
		} catch ( error ) {
			if ( !disposed && pending.get( request.id ) === controller ) {
				pending.delete( request.id );
				send( {
					kind: "error",
					id: request.id,
					error: String( error ),
					...(transientAssetFailure( error ) ? { transient: true as const } : {}),
					...(error instanceof AssetAbsentError ? { absent: true as const } : {})
				}, [] );
			}
		} finally {
			active.delete( controller );
			if ( active.size === 0 ) wakeIdleWaiters();
			if ( !disposed && controller.signal.aborted ) send( { kind: "released", id: request.id }, [] );
			if ( pending.get( request.id ) === controller ) pending.delete( request.id );
		}
	}
	return {
		/*
		================
		receive

		Dispatch load, cancellation and installation messages to their owning state.
		================
		*/
		receive( request: AssetRequest ) {
			if ( disposed ) {
				return;
			}
			if ( request.kind === "cancel" ) {
				pending.get( request.id )?.abort();
				pending.delete( request.id );
				return;
			}
			if ( request.kind === "install" ) {
				installer.start( request.url );
				return;
			}
			if (
				pending.has( request.id ) || active.size >= 4 || !Number.isSafeInteger( request.limit ) ||
				request.limit < 1 || request.limit > assetRequestBudget( request.decode )
			) {
				send( { kind: "error", id: request.id, error: "Asset request exceeds budget" }, [] );
				return;
			}
			const controller = new AbortController();
			pending.set( request.id, controller );
			active.add( controller );
			void load( request, controller );
		},
		/*
		================
		dispose

		Abort requests and release decoder state before terminating the worker.
		================
		*/
		dispose() {
			if ( disposed ) {
				return;
			}
			disposed = true;
			if ( progressTimer !== null ) clearTimeout( progressTimer );
			progressTimer = null;
			animationCatalogs.clear();
			effectBytes = null;
			effects.dispose();
			installer.dispose();
			wakeIdleWaiters();
			packs.dispose();
			for ( const controller of pending.values() ) {
				controller.abort();
			}
			pending.clear();
		}
	};
}
