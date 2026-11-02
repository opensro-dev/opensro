/*
===========================================================================

navigation.ts - navigation asset demand and admission

Owns requests, cancellation and transient transport recovery. Admission
failures never become network retries.

Collision authority belongs to simulation. The stream also keeps the main
thread's own clipper (GH #1439): every product it hands the simulation
worker is installed here first, under the same request id, so the view
holds exactly what the worker's movement owner holds once that worker
reports the id admitted. A consumer trusts the view only while the
worker's published navigation request id equals view().requestId; a
worker-side clear or failure then retires it without a guess here.

===========================================================================
*/
import type { AssetOwner } from "@/engine/contracts/assets";
import type { Pose, GameplayCommand } from "@/engine/contracts/gameplay";
import { assetFailure, assetFailurePath, createAssetRecovery } from "@/engine/foundation/assets/asset-recovery";
import { featuresAt } from "@/engine/foundation/world/plane";
import { createNavigation, type NavigationView } from "@/engine/foundation/navigation/clipper";

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
	let disposed = false, requestId = 0, deadline = 0, progressBytes = -1;
	const recovery = createAssetRecovery();
	// The asset the open request is loading, and the latest failure's
	// facts for the incident its terminal state files (#2337).
	// terminalFailure hands each terminal episode out exactly once.
	let loadPath: string | undefined;
	let lastFailure: { error: string; path?: string; } | undefined;
	let reportedFailure: { error: string; path?: string; } | undefined;
	// The main thread's clipper and the request id of the product it holds
	// (0: none). The same product goes to the worker under that id.
	const clipper = createNavigation();
	let viewRequestId = 0;
	/*
	================
	installView

	Installs the product the worker is about to receive. The clipper's state
	is a function of its installs, so a product the worker admits gives the
	same view here. install() is atomic (only its memo caches reset before a
	throw), and the worker's movement owner keeps its previous product and
	request id when it refuses one (movement.ts navigation); a refused
	product leaves this view on the previous one the same way.
	================
	*/
	function installView( id: number, regionId: number, product: unknown ) {
		try {
			clipper.install( regionId, product );
			viewRequestId = id;
		} catch {
			// The worker refuses the same bytes and reports the failure.
		}
	}
	/*
	================
	loadProgressBytes

	Stream bytes delivered so far (the loading detail's activity counter).
	-1 while the owner has published none.
	================
	*/
	function loadProgressBytes(): number {
		const progress = assets.progress?.();
		return progress ? (progress.bytesRead ?? progress.bytesReceived) : -1;
	}
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
				// The worker's admission answer is not an asset failure:
				// it files no asset incident (#2337).
				lastFailure = undefined;
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
						// Byte progress slides the load deadline (GH #1244): a
						// slow link that keeps delivering is still loading,
						// not stalled. Only a full window without a single
						// new byte may expire - and that answer is a transport
						// symptom (the loader's own no-progress retries settle
						// earlier), so it retries.
						const bytes = loadProgressBytes();
						if ( bytes > progressBytes ) {
							progressBytes = bytes;
							deadline = now + LOAD_TIMEOUT_MS;
						}
						if ( now >= deadline ) {
							throw assetFailure( "Navigation loading timed out for region " + state.region, true );
						}
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
						installView( ++requestId, result.product.regionId, result.product );
						emit( {
							kind: "navigation",
							requestId,
							regionId: result.product.regionId,
							bundle: result.product
						} );
						deadline = now + ADMISSION_TIMEOUT_MS;
						state = { phase: "admitting", region: pose.regionId };
					}
				}
				if ( state.phase !== "idle" || !assets.available() ) return;
				const dungeon = !featuresAt( pose.regionId ).regionCatalog, isCatalog = !dungeon && !catalog;
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
				loadPath = path;
				progressBytes = loadProgressBytes();
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
				// The admitting deadline is the worker's silence, not an
				// asset failure: it files no asset incident (#2337).
				if ( state.phase !== "admitting" ) {
					lastFailure = { error: String( error ), path: assetFailurePath( error ) ?? loadPath };
				}
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
		terminalFailure

		The failure that ended this stream's retries, once per episode:
		a permanent failure at once, a transient one only when its
		automatic retries are used up. The runtime files it as a client
		incident (#2337); a later failure is a new episode and reports
		again under its own facts.
		================
		*/
		terminalFailure() {
			if ( state.phase !== "failed" || recovery.reconnecting() ) return undefined;
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
		view

		The main thread's clipper and the request id of the product it holds,
		or null before any product was installed. Use it only while the
		worker's published navigationRequestId equals requestId.
		================
		*/
		view(): { readonly requestId: number; readonly navigation: NavigationView; } | null {
			return viewRequestId ? { requestId: viewRequestId, navigation: clipper } : null;
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
			clipper.clear();
			viewRequestId = 0;
			disposed = true;
		}
	};
}
