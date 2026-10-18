/*
===========================================================================

assets.ts - the main-thread owner of the asset worker

Every asset request crosses to the asset worker through this owner, which
holds at most four request slots shared by all runtime owners, enforces
per-request deadlines and byte budgets, and fails every handle when the
worker dies. install() starts the worker's background install without
using a slot.

A cancelled request keeps its slot until the worker acknowledges it, and the
release can lag when the worker is inside a native decode that AbortSignal
cannot interrupt (loader.ts createImageBitmap / DecompressionStream). A slow
release is abandoned, not fatal: its slot stays reserved and the fault is
reported as a degraded incident. Only a worker holding every slot in
unacknowledged cancellations is treated as dead.

===========================================================================
*/

import { assetRequestBudget } from "@/engine/foundation/assets/asset-budget";
import { createWorldLease } from "./world-lease";
import type { AssetOwner, AssetRequest, AssetResult, AssetWorkerMessage } from "@/engine/contracts/assets";
import type { Degradation } from "@/engine/contracts/network";
import { storedExperimentalOptions } from "@/engine/foundation/ui/experimental-options";
import { chooseTextureVariant } from "@/engine/foundation/assets/texture-variants";
const REQUEST_CAPACITY = 4;
// A load's watchdog is a stall window, not a total budget: the worker notes
// the bytes it reads per job (job-progress), and a job that keeps moving
// bytes may run past any fixed total on a slow link (a trickling route to
// the host measured 40 KB/s in production, where the old flat 120 s cap
// executed healthy loads). Silence is what dies: this long with no byte
// progress while downloading, the shorter decode window once the worker
// marks the primary bytes in (job-decoding; what remains is CPU-bound, and
// a slow phone's decode is measured in seconds), and an absolute ceiling
// that even an endless trickle cannot cross.
const REQUEST_STALL_MS = 120000;
const REQUEST_DECODE_STALL_MS = 60000;
const REQUEST_ABSOLUTE_MS = 600000;
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
		// Cumulative worker bytes noted for this job, and whether the worker
		// marked its primary bytes in (the decode stall window then applies).
		bytes: number;
		decoding: boolean;
		cancelled?: boolean;
		// The watchdog expired it: its error result is deliverable, and the
		// worker's late result or release is expected, not a protocol fault.
		// The slot stays the job's until the worker acknowledges (settled),
		// exactly like a cancellation: the worker still counts it against
		// its own capacity, so freeing it early would over-admit.
		expired?: boolean;
		// The worker's execution is finished (late result or release seen):
		// only the consumer's take (or dispose) still collects the job.
		settled?: boolean;
		// The consumer already collected the result through take().
		delivered?: boolean;
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

	Enforce the stall windows of active loads and of cancellation
	acknowledgements. A load dies only of silence (no worker bytes inside its
	window, the shorter one once decoding) or of the absolute ceiling; byte
	progress slides the window (noteJobProgress), so a slow link is slow,
	never fatal. Expiry is per job: the job takes the same transient error
	the old whole-runtime failure carried, the worker is asked to cancel it,
	and every other job continues. The expired job keeps its slot until the
	worker acknowledges (its late result or release), like a cancellation:
	the worker still counts the job against its own capacity.
	An overdue acknowledgement abandons its job: the slot stays reserved (the
	worker may still be running it) and a degraded fault is recorded. Only when
	every slot is abandoned has the worker stopped answering.
	A stalled network read never reaches it: the loader abandons a download
	after its no-progress window and retries (about 91 s for headers that
	arrive late and a body that then stalls), then answers with a transient
	error. What remains for this watchdog is a worker that stopped answering,
	and a decode that hung without moving another byte.
	================
	*/
	function checkDeadline() {
		if ( disposed || failure ) return;
		const now = performance.now();
		for ( const [id, job] of jobs ) {
			if ( job.abandoned || job.settled ) continue;
			const path = new URL( job.url ).pathname;
			if ( job.cancelled || job.expired ) {
				if ( now < job.deadline ) continue;
				job.abandoned = true;
				degradations.push( {
					code: CANCEL_UNACKNOWLEDGED,
					message: `Asset worker did not acknowledge cancelling ${path} within ${CANCEL_ACK_MS / 1000} s`
				} );
				continue;
			}
			if ( job.result ) continue;
			if ( now < job.deadline && now - job.startedAt < REQUEST_ABSOLUTE_MS ) continue;
			job.result = {
				kind: "error",
				id,
				error: "Asset worker timed out while loading " + path,
				transient: true
			};
			job.expired = true;
			job.deadline = now + CANCEL_ACK_MS;
			send( { kind: "cancel", id } );
		}
		const abandoned = [ ...jobs.values() ].filter( job => job.abandoned );
		if ( abandoned.length >= REQUEST_CAPACITY ) {
			fail(
				"Asset worker stopped acknowledging cancellations: " +
					abandoned.map( job => new URL( job.url ).pathname ).join( ", " )
			);
		}
	}

	/*
	================
	noteJobProgress

	The worker read more of a job's stream: slide its stall window. The
	window that slides is the phase the job is in; the absolute ceiling in
	checkDeadline never slides. Notes for finished, cancelled or unknown
	jobs are inert (a load's last bytes can race its own completion).
	================
	*/
	function noteJobProgress( id: number, bytes: number ) {
		const job = jobs.get( id );
		if ( !job || job.result || job.cancelled || bytes <= job.bytes ) return;
		job.bytes = bytes;
		job.deadline = performance.now() + (job.decoding ? REQUEST_DECODE_STALL_MS : REQUEST_STALL_MS);
	}

	/*
	================
	noteJobDecoding

	The worker's downloads have drained under a job whose primary bytes are
	in; what remains is decode. The job's window becomes the decode window
	from this mark, because CPU work notes no bytes: silence after it is a
	hung decode, not a slow link.
	================
	*/
	function noteJobDecoding( id: number ) {
		const job = jobs.get( id );
		if ( !job || job.result || job.cancelled || job.decoding ) return;
		job.decoding = true;
		job.deadline = performance.now() + REQUEST_DECODE_STALL_MS;
	}

	/*
	================
	noteJobFetching

	A decode-phase read started downloading again (the worker flips the job
	back): the download stall window applies once more. The next drain
	re-marks the job decoding.
	================
	*/
	function noteJobFetching( id: number ) {
		const job = jobs.get( id );
		if ( !job || job.result || job.cancelled || !job.decoding ) return;
		job.decoding = false;
		job.deadline = performance.now() + REQUEST_STALL_MS;
	}
	worker.onmessage = ( event: MessageEvent<AssetWorkerMessage> ) => {
		if ( event.data.kind === "progress" ) {
			if ( !disposed && !failure ) progress = event.data.progress;
			return;
		}
		if ( event.data.kind === "job-progress" ) {
			if ( !disposed && !failure ) noteJobProgress( event.data.id, event.data.bytes );
			return;
		}
		if ( event.data.kind === "job-decoding" ) {
			if ( !disposed && !failure ) noteJobDecoding( event.data.id );
			return;
		}
		if ( event.data.kind === "job-fetching" ) {
			if ( !disposed && !failure ) noteJobFetching( event.data.id );
			return;
		}
		if ( event.data.kind === "released" ) {
			const job = jobs.get( event.data.id );
			if ( job?.cancelled ) jobs.delete( event.data.id );
			// An expired job's release is its cancellation acknowledgement:
			// the slot frees only if the consumer already collected the
			// error, else the job settles and the error stays deliverable.
			else if ( job?.expired ) {
				if ( job.delivered ) jobs.delete( event.data.id );
				else job.settled = true;
			} else if ( job && !disposed && !failure ) fail( "Unexpected asset capacity release" );
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
		// An expired job's late completion changes nothing it delivers: the
		// execution is settled (its release follows), and the transient
		// error stays deliverable until the consumer collects it.
		if ( job?.expired ) {
			close( result );
			if ( job.delivered ) jobs.delete( result.id );
			else job.settled = true;
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
			jobs.set( id, {
				startedAt,
				deadline: startedAt + REQUEST_STALL_MS,
				url,
				limit,
				decode,
				result: null,
				bytes: 0,
				decoding: false
			} );

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
			if ( !job?.result || job.cancelled || job.delivered ) {
				return null;
			}
			job.delivered = true;
			// An expired job the worker has not acknowledged keeps its slot
			// (the worker still counts it against its own capacity);
			// deleting here would over-admit a retry the worker must refuse.
			if ( !job.expired || job.settled ) jobs.delete( id );
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
			if ( job.expired && !job.settled ) {
				// Expiry already asked the worker to cancel; the consumer
				// dropping the handle makes it an ordinary cancellation
				// awaiting the same acknowledgement, slot still reserved.
				job.cancelled = true;
				job.deadline = performance.now() + CANCEL_ACK_MS;
				send( { kind: "cancel", id } );
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
