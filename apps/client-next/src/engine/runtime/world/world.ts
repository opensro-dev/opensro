/*
===========================================================================

world.ts - region admission and the follow camera presentation

Owns the world asset transaction: which region bundle is loading, the
camera it draws from, and the scripted cameras. The drawn zoom distance
glides to the native wheel distance (camera-zoom-ease.ts).

===========================================================================
*/

import { createZoomEase } from "@/engine/foundation/rendering/camera-zoom-ease";
import { sampleSoundTerrain, type SoundTerrain } from "@/engine/foundation/audio/terrain-sounds";
import { decodeNativeTexture } from "@/engine/foundation/assets/native-texture";
import type { AssetOwner, AssetResult } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { Pose } from "@/engine/contracts/gameplay";
import type { CameraInput } from "@/engine/contracts/input";
import { createCameraScripts } from "./camera/camera";
import { createTerrainParts } from "./terrain-parts";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { CameraScript } from "@/engine/contracts/camera-script";
import { assetFailure, createAssetRecovery } from "@/engine/foundation/assets/asset-recovery";

type Transaction =
	| { phase: "idle"; }
	| { phase: "catalog" | "loading" | "ready"; region: number; }
	| { phase: "failed"; region: number; error: string; };
// An outdoor scene: its centre and the regions whose terrain it composes.
type Outdoor = { readonly region: number; readonly regions: readonly number[]; };
type Job = { kind: "catalog" | "world" | "texture"; path: string; outdoor?: Outdoor; };
type WorldResult = Extract<AssetResult, { kind: "world"; }>;

/*
================
regionKey
================
*/
function regionKey( region: number ): string {
	return `0x${region.toString( 16 ).padStart( 4, "0" )}`;
}

/*
================
createWorldStream
================
*/
export function createWorldStream(
	assets: AssetOwner,
	renderer: Renderer,
	origin: string,
	random: PresentationRandom
) {
	const scripts = createCameraScripts( random );
	const recovery = createAssetRecovery();
	let catalog: Record<string, { area?: string; source?: string; bundlePublicPath: string; }[]> | null = null;
	/*
	================
	missionRegion
	================
	*/
	function missionRegion( region: number ) {
		const rows = catalog?.[regionKey( region )];
		// Title/create routes overlap mission sectors. Catalog order is not a
		// scene-purpose contract; never admit a frontend route into gameplay.
		return rows?.find( row => row.source === "mission-outdoor-global" ) ??
			rows?.find( row => row.area === "outdoor" ) ?? rows?.find( row => !row.area && !row.source );
	}
	let dungeonBlock: number | undefined;
	let displayedRegion: number | null = null;
	let soundTerrain: readonly SoundTerrain[] = [];
	let transaction: Transaction = { phase: "idle" }, disposed = false;
	// One predicted neighbouring scene, derived from movement near a sector edge.
	// It never mutates the renderer until that region becomes authoritative.
	let future:
		| {
			region: number;
			path: string;
			job: number;
			anchor: number;
			outdoor: Outdoor;
			result?: WorldResult;
		}
		| null = null;
	// Region terrain composed into outdoor scenes (terrain-parts.ts). The
	// worker resolves a neighbour only when it has an outdoor bundle.
	const terrain = createTerrainParts(
		assets,
		origin,
		region => missionRegion( region )?.bundlePublicPath,
		region => !!catalog?.[regionKey( region )]?.some( row => row.area === "outdoor" )
	);
	// The outdoor scene loading now, and its objects once decoded: it is
	// admitted when every region of its terrain is resident.
	let outdoor: Outdoor | null = null, waiting: WorldResult | null = null;
	let previous: Pose | null = null, failedFuture: number | null = null;
	/*
	================
	clearFuture
	================
	*/
	function clearFuture() {
		if ( !future ) return;
		if ( future.result ) { for ( const row of future.result.images ?? [] ) row.image.close(); }
		else assets.cancel( future.job );
		future = null;
	}
	const jobs = new Map<number, Job>();
	/*
	================
	cancelTransaction
	================
	*/
	function cancelTransaction() {
		for ( const id of jobs.keys() ) assets.cancel( id );
		jobs.clear();
		if ( waiting ) { for ( const row of waiting.images ?? [] ) row.image.close(); }
		waiting = null;
		outdoor = null;
		renderer.cancelWorldUpdate();
	}
	/*
	================
	admit
	================
	*/
	function admit( result: WorldResult, scene?: Outdoor ) {
		let transferred = 0;
		try {
			renderer.adoptWorld( result.world, undefined, scene ? terrain.parts( scene.regions ) : undefined );
			if ( scene ) terrain.keepAround( scene.region );
			soundTerrain = result.soundTerrain ?? [];
			for ( const row of result.images ?? [] ) {
				transferred++;
				renderer.setWorldTexture( row.path, row.image );
			}
		} catch ( error ) {
			for ( const row of (result.images ?? []).slice( transferred ) ) row.image.close();
			throw error;
		}
	}
	/*
	================
	load
	================
	*/
	function load( path: string, kind: Job["kind"], scene?: Outdoor ) {
		if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
			throw new Error( "World asset path is not published" );
		}
		const id = assets.request(
			new URL( path, origin ).href,
			kind === "world" ? 128 << 20 : 16 << 20,
			kind === "world" ?
				"world" :
				kind === "texture" ?
				(path.endsWith( ".texture" ) ? undefined : path.toLowerCase().endsWith( ".dds" ) ? "dds" : "png") :
				undefined,
			// The worker returns a DDS texture's picking mask with it (pick-alpha.ts).
			kind === "texture" ? { pickAlpha: true } : undefined
		);
		jobs.set( id, { kind, path, ...(scene ? { outdoor: scene } : {}) } );
	}
	/*
	================
	objectsPath

	An outdoor scene's objects in the anchor's coordinates; its terrain
	arrives as region parts.
	================
	*/
	function objectsPath( bundle: string, anchor: number ): string {
		return `${bundle}#anchor=${anchor.toString( 16 ).padStart( 4, "0" )}&part=objects`;
	}
	/*
	================
	startOutdoor
	================
	*/
	function startOutdoor( region: number, bundle: string ) {
		const anchor = terrain.begin( region );
		outdoor = { region, regions: terrain.neighbourhood( region ) };
		load( objectsPath( bundle, anchor ), "world", outdoor );
		terrain.request( outdoor.regions );
	}
	const zoomEase = createZoomEase();
	/*
	================
	updateCamera

	Camera presentation is independent of asset transaction success.
	================
	*/
	function updateCamera(
		pose: Pose,
		camera: CameraInput | undefined,
		target: import("@/engine/contracts/scene").FollowCameraTarget | undefined,
		offset: readonly [number, number]
	) {
		const yaw = camera?.yaw ?? 0, pitch = camera?.pitch ?? Math.PI / 18;
		const distance = camera?.distance ?? 80;
		renderer.setWorldCamera( {
			dungeonBlock,
			originRegion: pose.regionId,
			follow: { yaw, pitch, distance, height: target?.height, mounted: target?.mounted, offset },
			eye: [
				pose.x + Math.sin( yaw ) * Math.cos( pitch ) * distance,
				pose.y + 20 + Math.sin( pitch ) * distance,
				pose.z + Math.cos( yaw ) * Math.cos( pitch ) * distance
			],
			target: [ pose.x, pose.y, pose.z ],
			fov: Math.PI / 3,
			near: 1,
			far: 3500
		} );
	}
	/*
	================
	advance
	================
	*/
	function advance( pose: Pose | null, nowMs: number ) {
		if ( pose && transaction.phase === "failed" && transaction.region !== pose.regionId ) {
			recovery.reset();
			transaction = { phase: "idle" };
		}
		if ( pose && transaction.phase === "failed" && recovery.due( nowMs ) ) {
			transaction = { phase: "idle" };
		}
		// Teleports and rapid crossings must not wait for obsolete transactions.
		if ( pose && transaction.phase === "loading" && transaction.region !== pose.regionId ) {
			cancelTransaction();
			recovery.reset();
			transaction = { phase: "idle" };
		}
		if ( future && !future.result ) {
			const result = assets.take( future.job );
			if ( result ) {
				if ( result.kind === "world" ) future.result = result;
				else {
					if ( result.kind === "image" ) result.image.close();
					failedFuture = future.region;
					future = null;
				}
			}
		}

		for ( const [id, job] of jobs ) {
			const result = assets.take( id );
			if ( !result ) continue;
			jobs.delete( id );
			if ( result.kind === "error" ) throw assetFailure( `${job.path}: ${result.error}`, result.transient );
			if ( job.kind === "catalog" && result.kind === "bytes" ) {
				const value = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) );
				if (
					!value?.regionsById || typeof value.regionsById !== "object" || Array.isArray( value.regionsById )
				) {
					throw new Error( "Invalid region catalog" );
				}
				catalog = value.regionsById;
				transaction = { phase: "idle" };
			} else if ( job.kind === "world" && result.kind === "world" ) {
				if ( job.outdoor ) waiting = result;
				else admit( result );
			} else if ( job.kind === "texture" && result.kind === "bytes" && job.path.endsWith( ".texture" ) ) {
				renderer.setWorldTexture( job.path, decodeNativeTexture( new Uint8Array( result.buffer ) ) );
			} else if ( job.kind === "texture" && result.kind === "image" ) {
				renderer.setWorldTexture( job.path, result.image, result.alpha );
			} else {
				if ( result.kind === "image" ) result.image.close();
				throw new Error( "World asset result type mismatch" );
			}
		}
		terrain.poll();
		if ( outdoor && transaction.phase === "loading" ) {
			terrain.request( outdoor.regions );
			if ( waiting && terrain.ready( outdoor.regions ) ) {
				const result = waiting;
				waiting = null;
				admit( result, outdoor );
			}
		}
		if ( future && !failedFuture && future.anchor === terrain.anchorFor( future.region ) ) {
			terrain.request( future.outdoor.regions, 1 );
		}
		if ( transaction.phase === "loading" || transaction.phase === "ready" ) {
			const neededTextures = renderer.neededWorldTextures();
			for ( const path of neededTextures ) {
				if ( jobs.size >= 3 || assets.available() === 0 ) break;
				if ( ![ ...jobs.values() ].some( job => job.path === path ) ) load( path, "texture" );
			}
			const stats = renderer.worldStats();
			if (
				!jobs.size && !waiting && !neededTextures.length && stats.pendingTextures === 0 &&
				stats.pendingGroups === 0
			) {
				displayedRegion = transaction.region;
				transaction = { phase: "ready", region: transaction.region };
			}
		}
		if ( !pose ) {
			clearFuture();
			previous = null;
			failedFuture = null;
			return;
		}
		const prior = previous;
		previous = { ...pose };
		if (
			transaction.phase === "ready" && transaction.region === pose.regionId && catalog &&
			!(pose.regionId & 0x8000)
		) {
			const dx = prior?.regionId === pose.regionId ? pose.x - prior.x : 0,
				dz = prior?.regionId === pose.regionId ? pose.z - prior.z : 0;
			let sx = 0, sz = 0;
			if ( pose.x > 1440 && dx > 0 ) sx = 1;
			else if ( pose.x < 480 && dx < 0 ) sx = -1;
			if ( pose.z > 1440 && dz > 0 ) sz = 1;
			else if ( pose.z < 480 && dz < 0 ) sz = -1;
			const x = (pose.regionId & 255) + sx, z = (pose.regionId >>> 8) + sz, candidate = (z << 8) | x;
			const entry = (sx || sz) && x >= 0 && x < 256 && z >= 0 && z < 128 ? missionRegion( candidate ) : null;
			if ( entry && future?.region !== candidate && failedFuture !== candidate ) {
				failedFuture = null;
				clearFuture();
				if ( assets.available() > 1 ) {
					const path = entry.bundlePublicPath;
					if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
						throw Error( "Invalid future region path" );
					}
					const anchor = terrain.anchorFor( candidate ), objects = objectsPath( path, anchor );
					future = {
						region: candidate,
						path: objects,
						anchor,
						outdoor: { region: candidate, regions: terrain.neighbourhood( candidate ) },
						job: assets.request( new URL( objects, origin ).href, 128 << 20, "world" )
					};
				}
			} else if ( !entry && (dx !== 0 || dz !== 0) ) {
				clearFuture();
				failedFuture = null;
			}
		}

		if (
			transaction.phase !== "idle" && !(transaction.phase === "ready" && transaction.region !== pose.regionId)
		) return;
		// A failed neighbor transaction leaves the displayed scene intact.
		// Returning to that region does not require a duplicate scene admission.
		if ( displayedRegion === pose.regionId ) {
			// Weather or selection textures can be demanded after scene admission.
			// Reusing the scene must not renew a failed texture's recovery budget.
			const stats = renderer.worldStats();
			transaction = {
				phase: stats.pendingTextures || stats.pendingGroups || renderer.neededWorldTextures().length ?
					"loading" :
					"ready",
				region: pose.regionId
			};
			return;
		}
		if ( future && future.region === pose.regionId && future.anchor === terrain.anchorFor( pose.regionId ) ) {
			const prepared = future;
			future = null;
			transaction = { phase: "loading", region: pose.regionId };
			terrain.begin( pose.regionId );
			outdoor = prepared.outdoor;
			if ( prepared.result ) waiting = prepared.result;
			else jobs.set( prepared.job, { kind: "world", path: prepared.path, outdoor } );
			terrain.request( outdoor.regions );
			return;
		}
		if ( future && future.region !== pose.regionId ) clearFuture();
		if ( assets.available() === 0 ) return;
		if ( pose.regionId & 0x8000 ) {
			transaction = { phase: "loading", region: pose.regionId };
			// A dungeon has its own coordinates; outdoor terrain is not kept meanwhile.
			terrain.clear();
			load( `/assets/world/dungeon/regions/0x${pose.regionId.toString( 16 ).padStart( 4, "0" )}.json`, "world" );
		} else if ( !catalog ) {
			transaction = { phase: "catalog", region: pose.regionId };
			load( "/assets/world/world-region-catalog.json", "catalog" );
		} else {
			transaction = { phase: "loading", region: pose.regionId };
			const entry = missionRegion( pose.regionId );
			if ( !entry ) throw new Error( `No published region ${pose.regionId}` );
			startOutdoor( pose.regionId, entry.bundlePublicPath );
		}
	}
	return {
		/*
		================
		pumpCameraScripts

		A0F567 pumps state timers before the process update (+2C), hence
		before action callbacks can consume projectile/particle random draws.
		================
		*/
		pumpCameraScripts( nowMs: number ) {
			if ( !disposed ) scripts.step( nowMs, [] );
		},
		step(
			pose: Pose | null,
			camera?: CameraInput,
			target?: import("@/engine/contracts/scene").FollowCameraTarget | null,
			block?: number,
			nowMs = 0,
			events: readonly CameraScript[] = []
		) {
			dungeonBlock = block;
			if ( disposed ) return;
			const offset = scripts.step( nowMs, events );
			// The drawn distance glides to the native wheel distance (camera-zoom-ease.ts).
			const drawn = camera ? { ...camera, distance: zoomEase.step( camera.distance, nowMs ) } : camera;
			if ( pose && target !== null ) updateCamera( target?.pose ?? pose, drawn, target, offset );
			try {
				advance( pose, nowMs );
				if ( transaction.phase === "ready" ) recovery.reset();
			} catch ( error ) {
				const region = transaction.phase === "idle" ? pose?.regionId ?? 0 : transaction.region;
				cancelTransaction();
				clearFuture();
				terrain.cancelPending();
				transaction = { phase: "failed", region, error: String( error ) };
				recovery.failed( error, nowMs );
			}
		},
		soundSurface: ( pose: Pose ) => sampleSoundTerrain( soundTerrain, pose ),
		ready: () => transaction.phase === "ready" && previous?.regionId === transaction.region,
		/*
		================
		progress
		================
		*/
		progress() {
			const stats = renderer.worldStats();
			const admitted = transaction.phase === "loading" || transaction.phase === "ready";
			const pendingWorld = !!waiting || [ ...jobs.values() ].some( job => job.kind === "world" );
			return !admitted ?
				0 :
				pendingWorld ?
				0.25 :
				0.5 +
				0.5 * stats.residentGroups /
					Math.max( 1, stats.residentGroups + stats.pendingGroups + stats.pendingTextures );
		},
		/*
		================
		loadingRegion
		================
		*/
		loadingRegion() {
			if (
				!previous || displayedRegion === null || displayedRegion === previous.regionId ||
				transaction.phase === "ready" || transaction.phase === "failed"
			) return undefined;
			const region = previous.regionId;
			const adjacent = !(region & 0x8000) && !(displayedRegion & 0x8000) &&
				Math.abs( (region & 255) - (displayedRegion & 255) ) <= 1 &&
				Math.abs( (region >>> 8) - (displayedRegion >>> 8) ) <= 1;
			return adjacent ? undefined : region;
		},
		error: () => transaction.phase === "failed" ? transaction.error : null,
		/*
		================
		reconnecting
		================
		*/
		reconnecting: () => transaction.phase === "failed" && recovery.reconnecting(),
		/*
		================
		retryTransient
		================
		*/
		retryTransient() {
			if ( !disposed && transaction.phase === "failed" && recovery.transient() ) {
				recovery.reset();
				transaction = { phase: "idle" };
			}
		},
		/*
		================
		retry
		================
		*/
		retry() {
			if ( !disposed && transaction.phase === "failed" ) {
				recovery.reset();
				transaction = { phase: "idle" };
			}
		},
		/*
		================
		reset
		================
		*/
		reset() {
			if ( disposed ) return;
			scripts.reset();
			recovery.reset();
			zoomEase.reset();
			cancelTransaction();
			clearFuture();
			terrain.clear();
			previous = null;
			failedFuture = null;
			soundTerrain = [];
			displayedRegion = null;
			transaction = { phase: "idle" };
			renderer.setWorld( null );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			scripts.dispose();
			recovery.reset();
			cancelTransaction();
			clearFuture();
			terrain.clear();
			previous = null;
			failedFuture = null;
			soundTerrain = [];
			displayedRegion = null;
			soundTerrain = [];
			catalog = null;
		}
	};
}
