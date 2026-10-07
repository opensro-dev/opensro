/*
===========================================================================

navigation.ts - navigation asset demand and admission

Owns requests, cancellation and transient transport recovery. Collision
state belongs to simulation; admission failures never become network retries.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose, GameplayCommand } from "@/engine/contracts/gameplay";
import { assetFailure, createAssetRecovery } from "@/engine/foundation/assets/asset-recovery";

const LOAD_TIMEOUT_MS = 60000;
const ADMISSION_TIMEOUT_MS = 15000;
const CATALOG_LIMIT_BYTES = 16 << 20;
const NAVIGATION_LIMIT_BYTES = 128 << 20;

/*
================
createNavigationStream
================
*/
export function createNavigationStream(
	assets: AssetOwner,
	emit: ( command: GameplayCommand ) => void,
	origin: string
) {
	let catalog: Record<string, { bundlePublicPath: string; }[]> | null = null;
	let state:
		| { phase: "idle"; }
		| { phase: "loading"; id: number; region: number; catalog: boolean; }
		| { phase: "admitting" | "ready"; region: number; }
		| { phase: "failed"; region: number; error: string; } = { phase: "idle" };
	let disposed = false, requestId = 0, deadline = 0;
	const recovery = createAssetRecovery();
	/*
	================
	cancel
	================
	*/
	function cancel() {
		if ( state.phase === "loading" ) assets.cancel( state.id );
		state = { phase: "idle" };
	}
	return {
		/*
		================
		step
		================
		*/
		step(
			pose: Pose | null,
			admittedRegion?: number,
			failure?: { region: number; requestId?: number; error: string; },
			admittedRequestId?: number,
			now = performance.now()
		) {
			if ( disposed ) return;
			if ( !pose ) {
				cancel();
				recovery.reset();
				return;
			}
			if ( state.phase !== "idle" && state.region !== pose.regionId ) {
				cancel();
				recovery.reset();
			}
			if ( state.phase === "failed" && recovery.due( now ) ) state = { phase: "idle" };
			if ( state.phase === "admitting" && failure?.region === state.region && failure.requestId === requestId ) {
				state = { phase: "failed", region: state.region, error: failure.error };
				recovery.reset();
				return;
			}
			if ( state.phase === "admitting" && admittedRegion === state.region && admittedRequestId === requestId ) {
				state = { phase: "ready", region: state.region };
				recovery.reset();
			}
			try {
				if ( state.phase === "admitting" && now >= deadline ) {
					throw Error( "Navigation admitting timed out for region " + state.region );
				}
				if ( state.phase === "loading" ) {
					const result = assets.take( state.id );
					if ( !result ) {
						if ( now >= deadline ) throw Error( "Navigation loading timed out for region " + state.region );
						return;
					}
					if ( result.kind === "error" ) throw assetFailure( result.error, result.transient === true );
					if ( state.catalog ) {
						if ( result.kind !== "bytes" ) throw new Error( "Navigation catalog response" );
						const value = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) );
						if ( !value.regionsById || typeof value.regionsById !== "object" ) {
							throw new Error( "Navigation catalog" );
						}
						catalog = value.regionsById;
						state = { phase: "idle" };
					} else {
						if ( result.kind !== "navigation" ) throw new Error( "Navigation product response" );
						if ( result.product.regionId !== pose.regionId ) {
							throw Error( "Navigation product region mismatch" );
						}
						emit( {
							kind: "navigation",
							requestId: ++requestId,
							regionId: result.product.regionId,
							bundle: result.product
						} );
						deadline = now + ADMISSION_TIMEOUT_MS;
						state = { phase: "admitting", region: pose.regionId };
					}
				}
				if ( state.phase !== "idle" || !assets.available() ) return;
				const dungeon = !!(pose.regionId & 0x8000), isCatalog = !dungeon && !catalog;
				const path = dungeon ?
					"/assets/world/dungeon/dungeon-resources.json" :
					isCatalog ?
					"/assets/world/world-region-catalog.json" :
					catalog?.[`0x${pose.regionId.toString( 16 ).padStart( 4, "0" )}`]?.[0]?.bundlePublicPath;
				if ( !path || !path.startsWith( "/assets/" ) || path.includes( ".." ) || path.includes( "\\" ) ) {
					throw new Error( "No published navigation for region " + pose.regionId );
				}
				const url = new URL( path, origin );
				if ( !isCatalog ) url.hash = String( pose.regionId );
				deadline = now + LOAD_TIMEOUT_MS;
				state = {
					phase: "loading",
					region: pose.regionId,
					catalog: isCatalog,
					id: assets.request(
						url.href,
						isCatalog ? CATALOG_LIMIT_BYTES : NAVIGATION_LIMIT_BYTES,
						isCatalog ? undefined : "navigation"
					)
				};
			} catch ( error ) {
				cancel();
				state = { phase: "failed", region: pose.regionId, error: String( error ) };
				recovery.failed( error, now );
			}
		},
		/*
		================
		phase
		================
		*/
		phase() {
			return state.phase;
		},
		/*
		================
		error
		================
		*/
		error() {
			return state.phase === "failed" ? state.error : null;
		},
		/*
		================
		reconnecting
		================
		*/
		reconnecting() {
			return state.phase === "failed" && recovery.reconnecting();
		},
		/*
		================
		retryTransient
		================
		*/
		retryTransient() {
			if ( !disposed && state.phase === "failed" && recovery.transient() ) {
				recovery.reset();
				state = { phase: "idle" };
			}
		},
		/*
		================
		retry
		================
		*/
		retry() {
			if ( disposed || state.phase !== "failed" ) return;
			recovery.reset();
			state = { phase: "idle" };
		},
		/*
		================
		reset
		================
		*/
		reset() {
			cancel();
			recovery.reset();
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( disposed ) return;
			cancel();
			recovery.reset();
			catalog = null;
			disposed = true;
		}
	};
}
