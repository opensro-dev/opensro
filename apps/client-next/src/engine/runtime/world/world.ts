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
import { assetFailure, assetFailurePath, createAssetRecovery } from "@/engine/foundation/assets/asset-recovery";
import {
	cameraFieldOfViewRadians,
	initialCameraFieldOfView,
	initialCameraPitch,
	initialCameraYaw
} from "@/engine/foundation/rendering/camera-options";
import { featuresAt } from "@/engine/foundation/world/plane";
import { sectorX, sectorY } from "@/engine/foundation/world/world-space";
import { sampleCameraWaypoints } from "@/engine/foundation/ui/camera-waypoints";
import {
	createFreeCameraFades,
	FREE_CAMERA_DEFAULT_SPEED,
	freeCameraEye,
	seedFreeCamera,
	stepFreeCamera,
	type FreeCameraKeys,
	type FreeCameraRig
} from "@/engine/foundation/rendering/free-camera";

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
	// The latest failure's facts for the incident its terminal state
	// files (#2337): the error text and the asset it was loading.
	// terminalFailure hands each terminal episode out exactly once.
	let lastFailure: { error: string; path?: string; } | undefined;
	let reportedFailure: { error: string; path?: string; } | undefined;
	// Native UI construction resets App+0x38C (818790 via vtable+0x24).
	// Asset resets reuse this runtime's UI and keep its committed projection.
	let fieldOfView = initialCameraFieldOfView();
	// The interface's free-camera byte (CGInterface +0x3CE): while it
	// stands, ApplyCameraOrbit (68FAD0) and UpdateCameraOrbitTarget
	// (68F830) return early and the free-fly arm (690146) flies the
	// camera from the keyboard (free-camera.ts, #1787). The rig is
	// seeded from the last applied camera on the first free step; the
	// fades live on the interface natively and persist across toggles.
	// keys is null while a text field owns the keyboard: the arm then
	// returns before it reads a key or steps a fade.
	let freeCamera = false;
	let freeRig: FreeCameraRig | null = null;
	const freeFades = createFreeCameraFades();
	let freeSpeed = FREE_CAMERA_DEFAULT_SPEED;
	let freeKeys: FreeCameraKeys | null = null;
	let freeLastStepMs: number | null = null;
	// The interface track's port copy (CGInterface +0x3FC) and its play
	// byte (+0x3CD): the console owns the store and forwards both; the
	// stepper below owns the track clock (+0xE8) it advances while the
	// play byte stands, and the last step's timestamp it diffs against.
	let waypointKeyframes: readonly import("@/engine/foundation/ui/camera-waypoints").CameraWaypoint[] = [];
	let waypointPlaying = false;
	let waypointClockSeconds = 0;
	let waypointLastStepMs: number | null = null;
	let lastCamera: import("@/engine/contracts/scene").WorldCamera | null = null;
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
	rotateWaypointVector

	A sampled orientation applied to a basis vector (v + 2 * q x (q x v +
	w * v) for the quaternion's vector part q): the aim the keyframe's
	quaternion describes, in the engine's basis where identity looks
	along -Z (the follow camera at yaw 0 sits at +Z behind its target).
	================
	*/
	function rotateWaypointVector(
		orientation: { readonly x: number; readonly y: number; readonly z: number; readonly w: number; },
		vector: readonly [number, number, number]
	): [number, number, number] {
		const qx = orientation.x, qy = orientation.y, qz = orientation.z, qw = orientation.w;
		const vx = vector[0], vy = vector[1], vz = vector[2];
		// Inner cross q x v, then the full turn v + 2 * q x (inner + w * v).
		const ix = qy * vz - qz * vy, iy = qz * vx - qx * vz, iz = qx * vy - qy * vx;
		const tx = ix + qw * vx, ty = iy + qw * vy, tz = iz + qw * vz;
		return [
			vx + 2 * (qy * tz - qz * ty),
			vy + 2 * (qz * tx - qx * tz),
			vz + 2 * (qx * ty - qy * tx)
		];
	}
	/*
	================
	stepWaypointCamera

	While the play byte (+0x3CD) stands, CGInterface_UpdatePointerTracking
	(68FE50) advances the track clock by the frame delta and applies the
	sampled pose, and CGInterface_UpdateCameraOrbitTarget (68F830)
	returns at once, so the follow stays frozen: this step does the same
	instead of updateCamera and reports that it drove the camera.
	TimedValue_Advance clamps the clock to the last record's time and
	the 6900F8 tail clears the play byte once the clock reaches it, the
	final pose staying applied (lastCamera holds it for the free-camera
	hold, as the tail copies the camera back to +0x42C). The native
	delta is a smoothed frame delta times a rate the trimmed binary
	does not resolve to a constant; the port steps by the step's own
	timestamp delta, never backwards. An empty track cannot be playing
	through the console gates; if it happens the byte just clears.
	================
	*/
	function stepWaypointCamera( pose: Pose, nowMs: number ): boolean {
		if ( !waypointPlaying ) return false;
		if ( !waypointKeyframes.length ) {
			waypointPlaying = false;
			waypointLastStepMs = null;
			return false;
		}
		const endSeconds = waypointKeyframes[waypointKeyframes.length - 1]!.timeSeconds;
		if ( waypointLastStepMs !== null ) {
			const deltaSeconds = Math.max( 0, (nowMs - waypointLastStepMs) / 1000 );
			waypointClockSeconds = Math.min( endSeconds, waypointClockSeconds + deltaSeconds );
		}
		waypointLastStepMs = nowMs;
		const sample = sampleCameraWaypoints( waypointKeyframes, waypointClockSeconds );
		if ( !sample ) return false;
		const forward = rotateWaypointVector( sample.orientation, [ 0, 0, -1 ] );
		const spec: import("@/engine/contracts/scene").WorldCamera = {
			dungeonBlock,
			originRegion: pose.regionId,
			eye: [ sample.position.x, sample.position.y, sample.position.z ],
			target: [
				sample.position.x + forward[0],
				sample.position.y + forward[1],
				sample.position.z + forward[2]
			],
			up: rotateWaypointVector( sample.orientation, [ 0, 1, 0 ] ),
			fov: fieldOfView,
			near: 1,
			far: 3500
		};
		lastCamera = spec;
		renderer.setWorldCamera( spec );
		if ( waypointClockSeconds >= endSeconds ) {
			waypointPlaying = false;
			waypointLastStepMs = null;
		}
		return true;
	}
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
		offset: readonly [number, number],
		nowMs: number
	) {
		const yaw = camera?.yaw ?? initialCameraYaw(), pitch = camera?.pitch ?? initialCameraPitch();
		const distance = camera?.distance ?? 80;
		const spec: import("@/engine/contracts/scene").WorldCamera = {
			dungeonBlock,
			originRegion: pose.regionId,
			follow: { yaw, pitch, distance, height: target?.height, mounted: target?.mounted, offset },
			eye: [
				pose.x + Math.sin( yaw ) * Math.cos( pitch ) * distance,
				pose.y + 20 + Math.sin( pitch ) * distance,
				pose.z + Math.cos( yaw ) * Math.cos( pitch ) * distance
			],
			target: [ pose.x, pose.y, pose.z ],
			fov: fieldOfView,
			near: 1,
			far: 3500
		};
		if ( freeCamera ) {
			stepFreeFly( pose, spec, nowMs );
			return;
		}
		lastCamera = spec;
		renderer.setWorldCamera( spec );
	}
	/*
	================
	stepFreeFly

	68FAD0 and 68F830 apply nothing while +0x3CE stands, so the player can
	walk away from the camera; the free-fly arm (690146) moves it instead.
	Until a key first moves the rig the last applied camera is re-applied
	as it was, follow and all, so the renderer keeps resolving it the
	way it drew it. Once the rig moves the camera is the rig's own,
	without the follow: native skips the clamp while the byte stands.
	A camera the clamp had pulled in therefore springs to the eye the
	follow requested on that first move, since the port's clamp lives in
	the renderer; native baked the clamped distance into +0x37C before
	the byte stood.
	================
	*/
	function stepFreeFly(
		pose: Pose,
		live: import("@/engine/contracts/scene").WorldCamera,
		nowMs: number
	) {
		// The byte can stand before any camera was applied (the first
		// frame): the live follow becomes the held camera then.
		if ( !lastCamera ) lastCamera = live;
		const seed = lastCamera;
		if ( !freeRig ) freeRig = seedFreeCamera( seed );
		const dtSeconds = freeLastStepMs === null ? 0 : Math.max( 0, (nowMs - freeLastStepMs) / 1000 );
		freeLastStepMs = nowMs;
		if ( freeKeys ) stepFreeCamera( freeRig, freeFades, freeKeys, freeSpeed, dtSeconds );
		if ( !freeRig.moved ) {
			renderer.setWorldCamera( seed );
			return;
		}
		const { eye, up } = freeCameraEye( freeRig );
		const spec: import("@/engine/contracts/scene").WorldCamera = {
			dungeonBlock,
			originRegion: seed.originRegion ?? pose.regionId,
			eye,
			target: freeRig.target,
			up,
			fov: fieldOfView,
			near: 1,
			far: 3500
		};
		lastCamera = spec;
		renderer.setWorldCamera( spec );
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
			if ( result.kind === "error" ) {
				throw assetFailure( `${job.path}: ${result.error}`, result.transient, job.path );
			}
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
			// No pose means the player has left this scene (the dock, the
			// title): the retained scene takes what already landed but
			// issues no new work. The incoming scene's stage may only ask
			// for an asset slot at its own step, ahead of this one in the
			// frame, and a stream that refills behind it every frame
			// leaves it none until the whole queue drains (#1975). The
			// stream resumes with the next world pose.
			if ( pose ) terrain.request( outdoor.regions );
			if ( waiting && terrain.ready( outdoor.regions ) ) {
				const result = waiting;
				waiting = null;
				admit( result, outdoor );
			}
		}
		if ( future && !failedFuture && future.anchor === terrain.anchorFor( future.region ) ) {
			if ( pose ) terrain.request( future.outdoor.regions, 1 );
		}
		if ( transaction.phase === "loading" || transaction.phase === "ready" ) {
			const neededTextures = renderer.neededWorldTextures();
			if ( pose ) {
				for ( const path of neededTextures ) {
					if ( jobs.size >= 3 || assets.available() === 0 ) break;
					if ( ![ ...jobs.values() ].some( job => job.path === path ) ) load( path, "texture" );
				}
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
			featuresAt( pose.regionId ).sectorFrames
		) {
			const dx = prior?.regionId === pose.regionId ? pose.x - prior.x : 0,
				dz = prior?.regionId === pose.regionId ? pose.z - prior.z : 0;
			let sx = 0, sz = 0;
			if ( pose.x > 1440 && dx > 0 ) sx = 1;
			else if ( pose.x < 480 && dx < 0 ) sx = -1;
			if ( pose.z > 1440 && dz > 0 ) sz = 1;
			else if ( pose.z < 480 && dz < 0 ) sz = -1;
			const x = sectorX( pose.regionId ) + sx, z = sectorY( pose.regionId ) + sz, candidate = (z << 8) | x;
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
		if ( !featuresAt( pose.regionId ).regionCatalog ) {
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
		setFieldOfView

		The GM console supplies the native command's bounded degree value.
		The world camera owns its projection; no setting is sent to the server.
		================
		*/
		setFieldOfView( degrees: number ) {
			if ( !disposed ) fieldOfView = cameraFieldOfViewRadians( degrees );
		},
		/*
		================
		setFreeCamera

		690C40 case 0x14 (/camera): while the byte stands the camera stays
		where it was; clearing it re-aims (UpdateCameraOrbitTarget), which
		the next step's live follow spec does on its own. The native off
		path also zeroes App+0x378; the port recomputes its eye and target
		every step, so no such residue exists to clear.
		================
		*/
		setFreeCamera( free: boolean ) {
			if ( disposed ) return;
			freeCamera = free;
			// Each entry starts from the follow's latest camera (68F830
			// rewrites +0x42C every frame the byte is clear).
			freeRig = null;
			freeLastStepMs = null;
		},
		/*
		================
		setFreeCameraSpeed

		690C40 case 136 (/cs) writes +0x7B0, clamped 1..500 by the console:
		the free-fly arm's translation speed (6902AD, 69040B).
		================
		*/
		setFreeCameraSpeed( speed: number ) {
			if ( !disposed ) freeSpeed = speed;
		},
		/*
		================
		setFreeCameraKeys

		The keys the free-fly arm reads this frame, or null while a text
		field owns the keyboard (690146 returns before reading them).
		================
		*/
		setFreeCameraKeys( keys: FreeCameraKeys | null ) {
			if ( !disposed ) freeKeys = keys;
		},
		/*
		================
		setCameraWaypoints

		The console's waypoint store forwarded whole (load and /cwclear):
		the port copy of the track the playback stepper samples (#1716).
		================
		*/
		setCameraWaypoints(
			keyframes: readonly import("@/engine/foundation/ui/camera-waypoints").CameraWaypoint[]
		) {
			if ( disposed ) return;
			waypointKeyframes = keyframes;
			// /cwclear empties the track whose reset also wipes the clock.
			if ( !keyframes.length ) {
				waypointPlaying = false;
				waypointClockSeconds = 0;
				waypointLastStepMs = null;
			}
		},
		/*
		================
		setCameraPlaying

		690C40 ids 131-133 write the play byte (+0x3CD): while it stands
		the stepper drives the camera from the track (#1716 PR2); the
		follow stays frozen either way while the free-camera byte stands.
		================
		*/
		setCameraPlaying( playing: boolean ) {
			if ( disposed ) return;
			waypointPlaying = playing;
			// /cwgo zeroes the track clock (+0xE8) as it writes the byte;
			// either write drops the last step's timestamp, so the next
			// playing step starts from a zero delta again.
			waypointClockSeconds = 0;
			waypointLastStepMs = null;
		},
		/*
		================
		setCameraCollision

		690C40 ids 118/119 (/colon, /coloff) write the interface byte
		+0x3CC. Only the clamp consults it (68FAD0): set, the
		eye-to-target segment is cast and a hit pulls the camera in;
		clear, the clamp takes its no-hit arm. The follow (68F830)
		never reads the byte, so this only forwards to the renderer,
		which owns the clamp.
		================
		*/
		setCameraCollision( collision: boolean ) {
			if ( !disposed ) renderer.setWorldCameraCollision( collision );
		},
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
			// While the play byte stands the stepper drives the camera and
			// the follow stays frozen (68F830 returns at once); the end
			// clear inside it hands the next step back to the follow.
			const played = pose ? stepWaypointCamera( pose, nowMs ) : false;
			if ( pose && target !== null && !played ) {
				updateCamera( target?.pose ?? pose, drawn, target, offset, nowMs );
			}
			// Playback writes the app camera itself (6900F8 copies it back
			// to +0x42C), so the free arm resumes from the played pose.
			if ( played ) {
				freeRig = null;
				freeLastStepMs = null;
			}
			try {
				advance( pose, nowMs );
				if ( transaction.phase === "ready" ) recovery.reset();
			} catch ( error ) {
				const region = transaction.phase === "idle" ? pose?.regionId ?? 0 : transaction.region;
				cancelTransaction();
				clearFuture();
				terrain.cancelPending();
				transaction = { phase: "failed", region, error: String( error ) };
				lastFailure = { error: String( error ), path: assetFailurePath( error ) };
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
			const adjacent = featuresAt( region ).sectorFrames && featuresAt( displayedRegion ).sectorFrames &&
				Math.abs( sectorX( region ) - sectorX( displayedRegion ) ) <= 1 &&
				Math.abs( sectorY( region ) - sectorY( displayedRegion ) ) <= 1;
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
		terminalFailure

		The failure that ended this stream's retries, once per episode:
		a permanent failure at once, a transient one only when its
		automatic retries are used up. The runtime files it as a client
		incident (#2337); a later failure is a new episode and reports
		again under its own facts.
		================
		*/
		terminalFailure() {
			if ( transaction.phase !== "failed" || recovery.reconnecting() ) return undefined;
			const failure = lastFailure;
			if ( !failure || failure === reportedFailure ) return undefined;
			reportedFailure = failure;
			return {
				error: failure.error,
				path: failure.path,
				transient: recovery.transient(),
				attempts: recovery.attempts()
			};
		},
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
