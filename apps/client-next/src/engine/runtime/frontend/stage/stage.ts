/*
===========================================================================

stage.ts - frontend scene admission and experimental terrain replacement

A stage installed to settle (the title: no actors, full terrain detail)
asks the renderer to drop its CPU geometry once it is ready, and requests
its bundle again if a device loss then drops the scene (renderer
world/settle.ts).

A transient load failure (a stalled download, the asset worker's stall
watchdog) is retried on the shared bounded recovery clock, as the world
and navigation streams are (asset-recovery.ts). Before #2862 one such
failure on the title backdrop failed the frontend for good.

===========================================================================
*/
import { decodeNativeTexture } from "@/engine/foundation/assets/native-texture";
import type { WorldScene } from "@/engine/contracts/scene";
import type { AssetOwner, AssetResult } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { StageManifest } from "@/engine/contracts/frontend";
import { createStagePreload } from "./preload";
import { createAssetRecovery } from "@/engine/foundation/assets/asset-recovery";
/*
================
createFrontendStage
================
*/
export function createFrontendStage( assets: AssetOwner, renderer: Renderer, base: string ) {
	const preload = createStagePreload( assets, base );
	let manifest: StageManifest | null = null,
		error: string | null = null,
		// The thrown value behind error, for the incident report's
		// stack (#2559); error keeps its text for the loading screen.
		errorCause: unknown = null,
		worldRequested = false,
		admitted = false,
		disposed = false,
		sceneId: string | null = null;
	const jobs = new Map<number, { kind: "manifest" | "world" | "texture"; path: string; }>();
	// A transient failure waits here for its retry; no new request leaves
	// until it is due (#2862).
	const recovery = createAssetRecovery();
	let retrying = false;
	/*
	================
	request
	================
	*/
	function request( path: string, kind: "manifest" | "world" | "texture" ) {
		if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) ) {
			throw new Error( "Invalid frontend resource path" );
		}
		const id = assets.request(
			new URL( path, base ).href,
			kind === "world" ? 128 << 20 : 16 << 20,
			kind === "world" ?
				"frontend-world" :
				kind === "texture" ?
				(path.endsWith( ".texture" ) ? undefined : path.endsWith( ".dds" ) ? "dds" : "png") :
				undefined
		);
		jobs.set( id, { kind, path } );
	}
	let wanted: string | null = null, terrainDetail: WorldScene["terrainDetail"] = "distance";
	// Whether the installed stage settles once ready, and whether it asked.
	let settles = false, settleAsked = false;
	/*
	================
	admit
	================
	*/
	function admit( result: Extract<AssetResult, { kind: "world"; }> ) {
		let transferred = 0;
		try {
			renderer.adoptWorld( result.world, terrainDetail );
			sceneId = result.world.sceneId;
			for ( const row of result.images ?? [] ) {
				transferred++;
				renderer.setWorldTexture( row.path, row.image );
			}
			worldRequested = true;
			admitted = true;
		} catch ( error ) {
			for ( const row of (result.images ?? []).slice( transferred ) ) row.image.close();
			throw error;
		}
	}
	/*
	================
	ready

	The scene is drawn whole: admitted, current, every group and texture in.
	================
	*/
	function ready() {
		const stats = renderer.worldStats();
		return admitted && !error && stats.sceneId === sceneId && !stats.pendingGroups && !stats.pendingTextures;
	}
	/*
	================
	clear
	================
	*/
	function clear() {
		for ( const id of jobs.keys() ) assets.cancel( id );
		jobs.clear();
		renderer.cancelWorldUpdate();
		manifest = null;
		worldRequested = false;
		admitted = false;
		settleAsked = false;
		error = null;
		errorCause = null;
		recovery.reset();
		retrying = false;
	}
	/*
	================
	failed

	A failed load: a transient one waits for the recovery clock and is
	requested again (the world job by clearing worldRequested; the manifest
	and textures are re-requested by step's own conditions). An exhausted
	sequence, or a permanent failure, fails the stage as before.
	================
	*/
	function failed(
		job: { kind: "manifest" | "world" | "texture"; },
		result: Extract<AssetResult, { kind: "error"; }>,
		nowMs: number
	) {
		const failure = new Error( result.error );
		if ( !result.transient ) throw failure;
		recovery.failed( Object.assign( failure, { transient: true } ), nowMs );
		if ( !recovery.reconnecting() ) throw failure;
		if ( job.kind === "world" ) worldRequested = false;
		retrying = true;
	}
	return {
		/*
		================
		preload
		================
		*/
		preload( path: string ) {
			preload.step( path );
		},
		/*
		================
		install

		after "settle" lets the ready scene give up its CPU geometry: only for
		a stage without actors (their shadows read the terrain) at full detail.
		================
		*/
		install(
			path: string,
			detail: NonNullable<WorldScene["terrainDetail"]> = "distance",
			after: "keep" | "settle" = "keep"
		) {
			clear();
			wanted = path;
			terrainDetail = detail;
			settles = after === "settle";
			const prepared = preload.take( path );
			if ( prepared ) {
				if ( prepared.kind === "manifest" ) jobs.set( prepared.job, { kind: "manifest", path } );
				else {
					manifest = prepared.manifest;
					if ( prepared.kind === "ready" ) admit( prepared.world );
					else {
						worldRequested = true;
						jobs.set( prepared.job, { kind: "world", path: manifest.regionBundlePublicPath } );
					}
				}
			}
		},
		/*
		================
		step

		nowMs drives the transient-retry clock; the frame passes none and
		reads the page clock.
		================
		*/
		step( nowMs = performance.now() ) {
			if ( disposed || !wanted || error ) return;
			try {
				for ( const [id, job] of jobs ) {
					const result = assets.take( id );
					if ( !result ) continue;
					jobs.delete( id );
					if ( result.kind === "error" ) {
						failed( job, result, nowMs );
						continue;
					}
					// A load that completes ends the failure sequence.
					if ( !retrying ) recovery.reset();
					if ( job.kind === "manifest" && result.kind === "bytes" ) {
						manifest = JSON.parse( new TextDecoder().decode( result.buffer ) ) as StageManifest;
					} else if ( job.kind === "world" && result.kind === "world" ) admit( result );
					else if ( job.kind === "texture" && result.kind === "bytes" && job.path.endsWith( ".texture" ) ) {
						renderer.setWorldTexture( job.path, decodeNativeTexture( new Uint8Array( result.buffer ) ) );
					} else if ( job.kind === "texture" && result.kind === "image" ) {
						renderer.setWorldTexture( job.path, result.image );
					} else throw new Error( "Unexpected frontend resource completion" );
				}
				// A settled scene lost with the device is loaded again (settle.ts);
				// only a settled scene can be lost.
				if ( settleAsked && sceneId !== null && renderer.worldStats().lostSceneId === sceneId ) {
					admitted = false;
					worldRequested = false;
					settleAsked = false;
					sceneId = null;
				}
				if ( retrying && recovery.due( nowMs ) ) retrying = false;
				if ( retrying ) return;
				if ( !manifest && !jobs.size && assets.available() > 0 ) request( wanted, "manifest" );
				if ( manifest && !worldRequested && assets.available() > 0 ) {
					request( manifest.regionBundlePublicPath + new URL( wanted, base ).hash, "world" );
					worldRequested = true;
				}
				if ( settles && !settleAsked && sceneId !== null && ready() ) {
					settleAsked = renderer.settleWorld( sceneId );
				}
				if ( admitted ) {
					for ( const path of renderer.neededWorldTextures() ) {
						if ( assets.available() === 0 || jobs.size >= 3 ) break;
						if ( ![ ...jobs.values() ].some( j => j.path === path ) ) request( path, "texture" );
					}
				}
			} catch ( e ) {
				error = String( e );
				errorCause = e;
				for ( const id of jobs.keys() ) assets.cancel( id );
				jobs.clear();
				renderer.cancelWorldUpdate();
			}
		},
		/*
		================
		status
		================
		*/
		status(): string | undefined {
			// Nothing is wanted after clear (world entry): no scene is loading,
			// so report no status rather than a manifest that never comes (#527).
			if ( !wanted ) return undefined;
			if ( error ) return error;
			if ( retrying ) return "Connection interrupted - retrying";
			if ( !manifest ) return "Loading scene description";
			if ( !admitted ) return "Loading scene models";
			// Request concurrency is not remaining work: texture dependencies are
			// discovered during scene preparation and queued behind asset backpressure.
			const textures = [ ...jobs.values() ].some( job => job.kind === "texture" ) ||
				renderer.neededWorldTextures().length > 0;
			return textures ?
				`Preparing textures - ${renderer.neededWorldTextures().length} remaining` :
				"Preparing scene graphics";
		},
		/*
		================
		progress
		================
		*/
		progress() {
			const stats = renderer.worldStats();
			return (Number( !!manifest ) + Number( admitted ) +
				(admitted ?
					stats.residentGroups /
					Math.max( 1, stats.residentGroups + stats.pendingGroups + stats.pendingTextures ) :
					0)) / 3;
		},
		ready,
		manifest: () => manifest,
		error: () => error,
		cause: () => errorCause,
		/*
		================
		clear
		================
		*/
		clear() {
			clear();
			preload.clear();
			wanted = null;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			disposed = true;
			clear();
			preload.dispose();
			wanted = null;
		}
	};
}
