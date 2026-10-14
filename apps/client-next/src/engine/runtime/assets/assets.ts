/*
===========================================================================

assets.ts - the main-thread owner of the asset worker

Every asset request crosses to the asset worker through this owner, which
holds at most four request slots shared by all runtime owners, enforces
per-request deadlines and byte budgets, and fails every handle when the
worker dies. install() starts the worker's background install without
using a slot.

A cancelled request keeps its slot until the worker releases it, and the
release can lag: a member read waits for its whole shared pack fetch
(packs.ts pack()). A slow release is abandoned, not fatal: its slot stays
reserved and the fault is reported as a degraded incident. Only a worker
holding every slot in unacknowledged cancellations is treated as dead.

===========================================================================
*/

import { assetRequestBudget } from "@/engine/foundation/assets/asset-budget";
import { createWorldLease } from "./world-lease";
import type { AssetOwner, AssetRequest, AssetResult, AssetWorkerMessage } from "@/engine/contracts/assets";
import type { Degradation } from "@/engine/contracts/network";
import { storedExperimentalOptions } from "@/engine/foundation/ui/experimental-options";
import { chooseTextureVariant } from "@/engine/foundation/assets/texture-variants";
const REQUEST_CAPACITY = 4;
const REQUEST_DEADLINE_MS = 120000;
// A cancellation's own acknowledgement window, from the cancel: a request
// cancelled late in its deadline still gets this long to be released.
const CANCEL_ACK_MS = 30000;
// The degraded code an unacknowledged cancellation reports (client AGENTS.md).
const CANCEL_UNACKNOWLEDGED = "asset-cancel-unacknowledged";
/*
================
createAssets

Own bounded request slots and fail outstanding handles if the worker becomes unusable.
================
*/
export function createAssets(): AssetOwner {
	const worker = new Worker( new URL( "./worker/entry.ts", import.meta.url ), {
		type: "module",
		name: "sro-assets"
	} );
	const jobs = new Map<number, {
		startedAt: number;
		deadline: number;
		url: string;
		limit: number;
		decode?: Extract<AssetRequest, { kind: "load"; }>["decode"];
		result: AssetResult | null;
		cancelled?: boolean;
		// Its release is overdue: the slot stays reserved, nothing waits on it.
		abandoned?: boolean;
	}>();
	const degradations: Degradation[] = [];
	let progress: import("@/engine/contracts/assets").AssetProgress | null = null;
	let releaseStale = false;
	let nextId = 0, disposed = false, failure: string | null = null;

	/*
	================
	close

	Release bitmap results that never transfer to a consumer; native arrays are collectible.
	================
	*/
	const close = ( result: AssetResult | AssetWorkerMessage | null ) => {
		if ( result?.kind === "world" ) { for ( const row of result.images ?? [] ) row.image.close(); }
		if ( result?.kind === "image" ) result.image.close();
		if ( result?.kind === "character" ) {
			for ( const image of result.images ) if ( !("kind" in image) ) image.close();
		}
	};

	/*
	================
	send

	A failed worker submission terminates the channel and all outstanding handles.
	================
	*/
	function send( message: AssetRequest ) {
		try {
			worker.postMessage( message );
		} catch ( error ) {
			// An unusable worker channel is terminal, not ordinary backpressure.
			// Fail every owned handle so callers never wait on work not submitted.

			/*
			================
			fail

			Retire the worker and convert every pending handle into an observable error.
			================
			*/
			fail( "Asset worker submission failed: " + String( error ) );
			throw error;
		}
	}
	const fail = ( message: string ) => {
		if ( disposed || failure ) return;
		failure = message;
		worker.terminate();
		for ( const [id, job] of jobs ) {
			close( job.result );
			job.result = { kind: "error", id, error: message };
		}
	};

	/*
	================
	checkDeadline

	Enforce the deadline of active loads and of cancellation acknowledgements.
	An overdue acknowledgement abandons its job: the slot stays reserved (the
	worker may still be running it) and a degraded fault is recorded. Only when
	every slot is abandoned has the worker stopped answering.
	A stalled network read never reaches it: the loader abandons a download
	after its no-progress window and retries (about 91 s for headers that
	arrive late and a body that then stalls), then answers with a transient
	error. What remains for this watchdog is a worker that
	stopped answering, and a request whose bytes keep arriving for more than
	the whole deadline, which the worker cannot tell apart from a hung decode.
	================
	*/
	function checkDeadline() {
		if ( disposed || failure ) return;
		const now = performance.now();
		for ( const job of jobs.values() ) {
			if ( job.result || job.abandoned || now < job.deadline ) continue;
			const path = new URL( job.url ).pathname;
			if ( !job.cancelled ) {
				fail( "Asset worker timed out while loading " + path );
				return;
			}
			job.abandoned = true;
			degradations.push( {
				code: CANCEL_UNACKNOWLEDGED,
				message: `Asset worker did not acknowledge cancelling ${path} within ${CANCEL_ACK_MS / 1000} s`
			} );
		}
		const abandoned = [ ...jobs.values() ].filter( job => job.abandoned );
		if ( abandoned.length >= REQUEST_CAPACITY ) {
			fail(
				"Asset worker stopped acknowledging cancellations: " +
					abandoned.map( job => new URL( job.url ).pathname ).join( ", " )
			);
		}
	}
	worker.onmessage = ( event: MessageEvent<AssetWorkerMessage> ) => {
		if ( event.data.kind === "progress" ) {
			if ( !disposed && !failure ) progress = event.data.progress;
			return;
		}
		if ( event.data.kind === "released" ) {
			const job = jobs.get( event.data.id );
			if ( job?.cancelled ) jobs.delete( event.data.id );
			else if ( !disposed && !failure ) fail( "Unexpected asset capacity release" );
			return;
		}
		if ( disposed || failure ) {
			close( event.data );
			return;
		}
		const result = event.data, job = jobs.get( result.id );
		if ( result.kind === "error" && result.stale ) releaseStale = true;
		if ( job?.cancelled ) {
			close( result );
			jobs.delete( result.id );
			return;
		}
		if ( !job ) {
			close( result );
			return;
		}
		if ( job.result ) {
			if ( result !== job.result ) close( result );
			fail( "Duplicate asset completion" );
			return;
		}
		if (
			result.kind === "bytes" && (!(result.buffer instanceof ArrayBuffer) || result.buffer.byteLength > job.limit)
		) {
			fail( "Invalid asset worker result" );
			return;
		}
		job.result = result.kind === "world" ?
			{
				kind: "world",
				id: result.id,
				soundTerrain: result.prepared.scene.soundTerrain,
				world: createWorldLease( {
					...result.prepared,
					scene: { ...result.prepared.scene, soundTerrain: undefined }
				} ),
				images: result.images
			} :
			result;
	};
	worker.onerror = () => fail( "Asset worker failed" );
	worker.onmessageerror = () => fail( "Asset worker message could not be decoded" );
	// Port-only: whether a device without BC is served re-encoded textures
	// (texture-variants.ts). The switch is read here because the worker
	// starts before the platform; native texture reads wait for the answer,
	// so with the switch off it is given at once, else when the device knows
	// its features (textureFeatures).
	let compressedTextures = false, variantSent = false;
	try {
		compressedTextures = storedExperimentalOptions( globalThis.localStorage ).options.compressedTextures;
	} catch {}
	if ( !compressedTextures ) {
		send( { kind: "texture-variant", variant: null } );
		variantSent = true;
	}
	return {
		/*
		================
		textureFeatures

		The device's texture features, once: the variant it is served.
		================
		*/
		textureFeatures( features: readonly string[] ) {
			if ( disposed || variantSent ) return;
			variantSent = true;
			send( { kind: "texture-variant", variant: chooseTextureVariant( new Set( features ), true ) } );
		},
		/*
		================
		snapshot

		Distinguish worker waits from completed handles a consumer has not collected.
		Exclude URL credentials and query parameters from exported diagnostics.
		================
		*/
		snapshot() {
			const now = performance.now();
			return {
				phase: disposed ? "disposed" : failure ? "failed" : "running",
				error: failure,
				available: disposed || failure ? 0 : REQUEST_CAPACITY - jobs.size,
				jobs: [ ...jobs ].map( ( [id, job] ) => ({
					id,
					path: new URL( job.url ).pathname,
					decode: job.decode ?? "bytes",
					ageMs: Math.max( 0, now - job.startedAt ),
					state: job.cancelled ?
						"cancelling" as const :
						job.result ?
						"completed" as const :
						"loading" as const,
					result: job.result?.kind ?? null
				}) )
			};
		},
		progress: () => progress,
		health: () => {
			checkDeadline();
			return disposed ?
				{ phase: "disposed" } :
				failure ?
				{ phase: "failed", error: failure } :
				{ phase: "running" };
		},
		available: () => disposed || failure ? 0 : REQUEST_CAPACITY - jobs.size,
		takeReleaseStale: () => {
			const stale = releaseStale;
			releaseStale = false;
			return stale;
		},
		takeDegradations: () => degradations.splice( 0 ),

		/*
		================
		request

		Reserve a bounded handle before submitting work to the asset worker.
		================
		*/
		request( value, limit = 16 << 20, decode, options ) {
			if ( disposed || failure ) {
				throw new Error( failure ?? "Assets disposed" );
			}
			const url = new URL( value ).href;
			if (
				jobs.size >= REQUEST_CAPACITY || !Number.isSafeInteger( limit ) || limit < 1 ||
				limit > assetRequestBudget( decode )
			) {
				throw new Error( "Asset request exceeds budget" );
			}
			const id = ++nextId;
			const startedAt = performance.now();
			jobs.set( id, { startedAt, deadline: startedAt + REQUEST_DEADLINE_MS, url, limit, decode, result: null } );

			try {
				send( {
					kind: "load",
					id,
					url,
					limit,
					decode,
					...(options?.pickAlpha ? { pickAlpha: true } : {})
				} );
			} catch ( error ) {
				jobs.delete( id );
				throw error;
			}
			return id;
		},

		/*
		================
		take

		Transfer ownership of a completed result and release its request slot.
		================
		*/
		take( id ) {
			const job = jobs.get( id );
			if ( job?.cancelled || !job?.result ) {
				return null;
			}
			jobs.delete( id );
			return job.result;
		},

		/*
		================
		cancel

		Retain an in-flight slot until the worker acknowledges cancellation.
		================
		*/
		cancel( id ) {
			const job = jobs.get( id );
			if ( !job || job.cancelled ) {
				return;
			}
			close( job.result );
			if ( job.result || failure ) {
				jobs.delete( id );
				return;
			}
			job.cancelled = true;
			// The acknowledgement gets its own window, whatever the request had left.
			job.deadline = performance.now() + CANCEL_ACK_MS;
			send( { kind: "cancel", id } );
		},

		/*
		================
		install

		Start background installation without consuming a foreground request slot.
		================
		*/
		install( listUrl ) {
			if ( disposed || failure ) return;
			send( { kind: "install", url: new URL( listUrl ).href } );
		},

		/*
		================
		dispose

		Detach worker callbacks and release all unclaimed results.
		================
		*/
		dispose() {
			if ( disposed ) {
				return;
			}
			disposed = true;
			worker.onmessage = null;
			worker.onerror = null;
			worker.onmessageerror = null;
			worker.terminate();
			for ( const job of jobs.values() ) close( job.result );
			jobs.clear();
		}
	};
}
