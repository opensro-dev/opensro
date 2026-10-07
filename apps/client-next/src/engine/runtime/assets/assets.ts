/*
===========================================================================

assets.ts - the main-thread owner of the asset worker

Every asset request crosses to the asset worker through this owner, which
holds at most four request slots shared by all runtime owners, enforces
per-request deadlines and byte budgets, and fails every handle when the
worker dies. install() starts the worker's background install without
using a slot.

===========================================================================
*/

import { assetRequestBudget } from "@/engine/foundation/assets/asset-budget";
import { createWorldLease } from "./world-lease";
import type { AssetOwner, AssetRequest, AssetResult, AssetWorkerMessage } from "@/engine/contracts/assets";
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
		deadline: number;
		url: string;
		limit: number;
		decode?: Extract<AssetRequest, { kind: "load"; }>["decode"];
		result: AssetResult | null;
		cancelled?: boolean;
	}>();
	let progress: import("@/engine/contracts/assets").AssetProgress | null = null;
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

	Enforce one deadline for both active loads and cancellation acknowledgements.
	A stalled network read never reaches it: the loader abandons a download
	after its no-progress window and retries (46.25 s at most), then answers
	with a transient error. What remains for this watchdog is a worker that
	stopped answering, and a request whose bytes keep arriving for more than
	the whole deadline, which the worker cannot tell apart from a hung decode.
	================
	*/
	function checkDeadline() {
		if ( disposed || failure ) return;
		const now = performance.now();
		for ( const job of jobs.values() ) {
			if ( !job.result && now >= job.deadline ) {
				fail(
					"Asset worker timed out while " + (job.cancelled ? "cancelling " : "loading ") +
						new URL( job.url ).pathname
				);
				return;
			}
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
	return {
		progress: () => progress,
		health: () => {
			checkDeadline();
			return disposed ?
				{ phase: "disposed" } :
				failure ?
				{ phase: "failed", error: failure } :
				{ phase: "running" };
		},
		available: () => disposed || failure ? 0 : 4 - jobs.size,

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
				jobs.size >= 4 || !Number.isSafeInteger( limit ) || limit < 1 || limit > assetRequestBudget( decode )
			) {
				throw new Error( "Asset request exceeds budget" );
			}
			const id = ++nextId;
			jobs.set( id, { deadline: performance.now() + 120000, url, limit, decode, result: null } );

			try {
				send( { kind: "load", id, url, limit, decode, ...(options?.pickAlpha ? { pickAlpha: true } : {}) } );
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
