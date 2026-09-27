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
import { pngBytes, DECODED_IMAGE_BYTES } from "@/engine/foundation/assets/image-budget";
import { createWorldDecoder } from "./world/world";
import { createModelDecoder } from "./model/model";
import { createPacks } from "./packs/packs";
import { createBackgroundInstaller } from "./install";
import { pageEntryBundle } from "@/engine/foundation/assets/page-entry";
import { readBytes } from "@/engine/foundation/assets/read-bytes";
import type { AssetRequest, AssetWorkerMessage } from "@/engine/contracts/assets";
/*
================
createLoader
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
	let received = 0, lastReceived = 0, lastProgress = performance.now(), speed = 0;
	function progress( force = false ) {
		const now = performance.now(), elapsed = now - lastProgress;
		if ( !force && elapsed < 150 ) return;
		if ( elapsed >= 150 ) {
			const sample = (received - lastReceived) * 1000 / elapsed;
			speed = sample;
			lastReceived = received;
			lastProgress = now;
		}
		if ( !disposed ) {
			send( {
				kind: "progress",
				progress: {
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
	function activity( path: string, event: "start" | "ready" | "end" ) {
		if ( event === "start" ) files.set( path, (files.get( path ) ?? 0) + 1 );
		else if ( event === "ready" ) readyFiles.add( path );
		else {
			const count = (files.get( path ) ?? 1) - 1;
			if ( count ) files.set( path, count );
			else files.delete( path );
		}
		progress( files.size === 0 );
	}
	async function download(
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
			throw Object.assign( new Error( `Asset HTTP ${response.status}` ), { status: response.status } );
		}
		if (
			range &&
			(response.status !== 206 ||
				response.headers.get( "content-range" ) !== `bytes ${range.start}-${range.end}/${range.total}`)
		) {
			await response.body.cancel();
			throw Error( "Invalid asset range response" );
		}
		const bytes = await readBytes(
			response.body,
			limit,
			range ?
				size => {
					received += size;
					progress();
				} :
				undefined
		);
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
	function foregroundIdle(): Promise<void> {
		if ( active.size === 0 || disposed ) return Promise.resolve();
		return new Promise( ( resolve ) => idleWaiters.push( resolve ) );
	}
	function wakeIdleWaiters() {
		const waiters = idleWaiters;
		idleWaiters = [];
		for ( const resolve of waiters ) resolve();
	}
	const installer = createBackgroundInstaller( packs, foregroundIdle );
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
					async function readWorldResource( path: string ) {
						if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
							throw new Error( "Invalid world resource path" );
						}
						const resource = await packs.read( new URL( path, url.origin ), 128 << 20, controller.signal );
						total += resource.byteLength;
						if ( total > (128 << 20) ) throw new Error( "World resource transaction exceeds budget" );
						return resource;
					}
					const resolved = await worlds.resolve( bytes, readWorldResource );
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
					let scene = worlds.decode( resolved, request.decode === "frontend-world" );
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
					let model: import("@/engine/contracts/character").CharacterModel;
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
					const images: ImageBitmap[] = [];
					try {
						const decoded = model.images.reduce( ( sum, image ) => sum + pngBytes( image.bytes ), 0 );
						if ( decoded > DECODED_IMAGE_BYTES ) {
							throw new Error( "Character images exceed decoded image budget" );
						}
						for ( const image of model.images ) {
							const bitmap = await createImageBitmap(
								new Blob( [ image.bytes.buffer as ArrayBuffer ], { type: image.mime } ),
								{ premultiplyAlpha: "none", colorSpaceConversion: "none" }
							);
							images.push( bitmap );
							if ( disposed || controller.signal.aborted ) {
								for ( const bitmap of images ) bitmap.close();
								return;
							}
						}
						const transfers = new Set<Transferable>( images );
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
						for ( const image of model.images ) transfers.add( image.bytes.buffer as ArrayBuffer );
						pending.delete( request.id );
						send( { kind: "character", id: request.id, model, images }, [ ...transfers ] );
					} catch ( error ) {
						for ( const image of images ) image.close();
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
						image = await createImageBitmap(
							new ImageData( decoded.pixels, decoded.width, decoded.height ),
							{ premultiplyAlpha: "none", colorSpaceConversion: "none" }
						);
					if ( disposed || controller.signal.aborted ) {
						image.close();
						return;
					}
					pending.delete( request.id );
					send( { kind: "image", id: request.id, image }, [ image ] );
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
					pending.delete( request.id );
					send( { kind: "image", id: request.id, image }, [ image ] );
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
				send( { kind: "error", id: request.id, error: String( error ) }, [] );
			}
		} finally {
			active.delete( controller );
			if ( active.size === 0 ) wakeIdleWaiters();
			if ( !disposed && controller.signal.aborted ) send( { kind: "released", id: request.id }, [] );
			if ( pending.get( request.id ) === controller ) pending.delete( request.id );
		}
	}
	return {
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
		dispose() {
			if ( disposed ) {
				return;
			}
			disposed = true;
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
