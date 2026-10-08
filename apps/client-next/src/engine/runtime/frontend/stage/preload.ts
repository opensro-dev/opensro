/*
===========================================================================

preload.ts - one cancellable frontend scene lease in the current terrain mode

===========================================================================
*/
import type { AssetOwner, AssetResult } from "@/engine/contracts/assets";
import type { StageManifest } from "@/engine/contracts/frontend";
type World = Extract<AssetResult, { kind: "world"; }>;
type State =
	| { kind: "idle"; }
	| { kind: "manifest"; path: string; job: number; }
	| { kind: "world"; path: string; manifest: StageManifest; job: number; }
	| { kind: "ready"; path: string; manifest: StageManifest; world: World; }
	| { kind: "failed"; path: string; };
// A single future scene lease. No renderer mutation until the active stage
// takes it; cancellation closes every transferred image exactly once.
/*
================
createStagePreload
================
*/
export function createStagePreload( assets: AssetOwner, base: string ) {
	let state: State = { kind: "idle" }, disposed = false, terrainNormals = false;
	/*
	================
	clear
	================
	*/
	function clear() {
		if ( state.kind === "manifest" || state.kind === "world" ) assets.cancel( state.job );
		if ( state.kind === "ready" ) { for ( const row of state.world.images ?? [] ) row.image.close(); }
		state = { kind: "idle" };
	}
	return {
		/*
		================
		setTerrainNormals
		================
		*/
		setTerrainNormals( value: boolean ) {
			if ( disposed || terrainNormals === value ) return;
			clear();
			terrainNormals = value;
		},
		/*
		================
		step
		================
		*/
		step( path: string ) {
			if ( disposed ) return;
			if ( state.kind !== "idle" && state.path !== path ) clear();
			if ( state.kind === "idle" && assets.available() > 0 ) {
				try {
					state = { kind: "manifest", path, job: assets.request( new URL( path, base ).href, 16 << 20 ) };
				} catch {
					state = { kind: "failed", path };
				}
			}
			if ( state.kind !== "manifest" && state.kind !== "world" ) return;
			const result = assets.take( state.job );
			if ( !result ) return;
			if ( state.kind === "manifest" && result.kind === "bytes" ) {
				try {
					const manifest = JSON.parse( new TextDecoder().decode( result.buffer ) ) as StageManifest;
					if (
						!manifest.regionBundlePublicPath.startsWith( "/assets/" ) ||
						manifest.regionBundlePublicPath.includes( ".." )
					) throw Error( "Invalid stage path" );
					state = {
						kind: "world",
						path,
						manifest,
						job: assets.request(
							new URL( manifest.regionBundlePublicPath + new URL( path, base ).hash, base ).href,
							128 << 20,
							"frontend-world",
							terrainNormals ? { terrainNormals: true } : undefined
						)
					};
				} catch {
					state = { kind: "failed", path };
				}
			} else if ( state.kind === "world" && result.kind === "world" ) {
				state = { kind: "ready", path, manifest: state.manifest, world: result };
			} else {
				if ( result.kind === "world" ) { for ( const row of result.images ?? [] ) row.image.close(); }
				state = { kind: "failed", path };
			}
		},
		/*
		================
		take
		================
		*/
		take( path: string ) {
			if ( state.kind === "idle" || state.kind === "failed" || state.path !== path ) {
				clear();
				return null;
			}
			const result = state;
			state = { kind: "idle" };
			return result;
		},
		clear,
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			clear();
		}
	};
}
