/*
===========================================================================

stage.ts - frontend scene admission and experimental terrain replacement

===========================================================================
*/
import { decodeNativeTexture } from "@/engine/foundation/assets/native-texture";
import type { WorldScene } from "@/engine/contracts/scene";
import type { AssetOwner, AssetResult } from "@/engine/contracts/assets";
import type { Renderer } from "@/engine/contracts/runtime";
import type { StageManifest } from "@/engine/contracts/frontend";
import { createStagePreload } from "./preload";
/*
================
createFrontendStage
================
*/
export function createFrontendStage( assets: AssetOwner, renderer: Renderer, base: string ) {
	const preload = createStagePreload( assets, base );
	let terrainNormals = false;
	let manifest: StageManifest | null = null,
		error: string | null = null,
		worldRequested = false,
		admitted = false,
		disposed = false,
		sceneId: string | null = null;
	const jobs = new Map<number, { kind: "manifest" | "world" | "texture"; path: string; }>();
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
				undefined,
			kind === "world" && terrainNormals ? { terrainNormals: true } : undefined
		);
		jobs.set( id, { kind, path } );
	}
	let wanted: string | null = null, terrainDetail: WorldScene["terrainDetail"] = "distance";
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
		error = null;
	}
	return {
		/*
		================
		setTerrainNormals
		================
		*/
		setTerrainNormals( value: boolean ) {
			if ( disposed || terrainNormals === value ) return;
			terrainNormals = value;
			preload.setTerrainNormals( value );
			clear();
		},
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
		================
		*/
		install( path: string, detail: NonNullable<WorldScene["terrainDetail"]> = "distance" ) {
			clear();
			wanted = path;
			terrainDetail = detail;
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
		================
		*/
		step() {
			if ( disposed || !wanted || error ) return;
			try {
				for ( const [id, job] of jobs ) {
					const result = assets.take( id );
					if ( !result ) continue;
					jobs.delete( id );
					if ( result.kind === "error" ) throw new Error( result.error );
					if ( job.kind === "manifest" && result.kind === "bytes" ) {
						manifest = JSON.parse( new TextDecoder().decode( result.buffer ) ) as StageManifest;
					} else if ( job.kind === "world" && result.kind === "world" ) admit( result );
					else if ( job.kind === "texture" && result.kind === "bytes" && job.path.endsWith( ".texture" ) ) {
						renderer.setWorldTexture( job.path, decodeNativeTexture( new Uint8Array( result.buffer ) ) );
					} else if ( job.kind === "texture" && result.kind === "image" ) {
						renderer.setWorldTexture( job.path, result.image );
					} else throw new Error( "Unexpected frontend resource completion" );
				}
				if ( !manifest && !jobs.size && assets.available() > 0 ) request( wanted, "manifest" );
				if ( manifest && !worldRequested && assets.available() > 0 ) {
					request( manifest.regionBundlePublicPath + new URL( wanted, base ).hash, "world" );
					worldRequested = true;
				}
				if ( admitted ) {
					for ( const path of renderer.neededWorldTextures() ) {
						if ( assets.available() === 0 || jobs.size >= 3 ) break;
						if ( ![ ...jobs.values() ].some( j => j.path === path ) ) request( path, "texture" );
					}
				}
			} catch ( e ) {
				error = String( e );
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
		status() {
			if ( error ) return error;
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
		/*
		================
		ready
		================
		*/
		ready() {
			const stats = renderer.worldStats();
			return admitted && !error && stats.sceneId === sceneId && !stats.pendingGroups && !stats.pendingTextures;
		},
		manifest: () => manifest,
		error: () => error,
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
