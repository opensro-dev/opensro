/*
===========================================================================

assets.ts - the asset worker contract

The messages between the main-thread asset owner and the asset worker,
the decoded result kinds, and the AssetOwner interface every runtime owner
loads through.

===========================================================================
*/
import type { WorldTexture } from "@/engine/contracts/texture";

import type { ModelDocument } from "./model";
/*
================
AssetStoreOperation

The store's operations a failure is counted against (packs/persistent.ts).
================
*/
export type AssetStoreOperation = "open" | "has" | "read" | "write" | "remove";

/*
================
AssetStoreFailureReason

Why a store operation failed, as an enumerated word: bug reports carry no
free-form error text (bug-report/archive.ts), so the browser's message is
classified, never copied.

  timeout     an operation outlived its deadline
  suspended   refused while an overdue operation had not settled
  queue-full  refused at the waiter bound
  quota       the origin refused the bytes (QuotaExceededError)
  too-large   a cache too large to list ("Operation too large", #4370)
  unavailable the browser refused Cache Storage itself
  corrupt     a stored entry did not match its length
  other       anything else
================
*/
export type AssetStoreFailureReason =
	| "timeout"
	| "suspended"
	| "queue-full"
	| "quota"
	| "too-large"
	| "unavailable"
	| "corrupt"
	| "other";

/*
================
AssetStoreFailure

The latest failed store operation: what it was, why, and when on the
worker's clock (performance.now() milliseconds).
================
*/
export interface AssetStoreFailure {
	readonly operation: AssetStoreOperation;
	readonly reason: AssetStoreFailureReason;
	readonly atMs: number;
}

/*
================
AssetStoreStats

The persistent asset store's counters since the asset worker started
(packs/persistent.ts, packs/blocks.ts). They decide whether a revisited
area downloads again, so bug reports carry them. Diagnostics only.
================
*/
export interface AssetStoreStats {
	// Reads served from the store, and lookups that found no entry.
	readonly hits: number;
	readonly misses: number;
	readonly writes: number;
	// Verified bytes not stored: backlog full, store suspended, over budget.
	readonly skipped: number;
	// Failed store operations, the sum of failures. A caller's own
	// cancellation is not a failure and is not counted.
	readonly errors: number;
	// Failed operations by kind, and the latest one (#4432): the sum alone
	// cannot say whether a climb is quota, a stalled backend or a cache too
	// large to list.
	readonly failures: Readonly<Record<AssetStoreOperation, number>>;
	readonly lastFailure: AssetStoreFailure | null;
	readonly evictions: number;
	// The origin's quota refused a write with nothing left to evict.
	readonly writesStopped: boolean;
	// Shards dropped because they could not be listed (persistent.ts rebuild).
	readonly resets: number;
	readonly budget: number;
	// Bytes ranged pack reads fetched, and the members' share of them.
	readonly rangeBytes: number;
	readonly demandedBytes: number;
}
/*
================
AssetProgress

Observable installation progress; it never owns a foreground request slot.
================
*/
export interface AssetProgress {
	// Stream bytes include HTTP-cache reads and decompression. They indicate
	// activity, not network usage; bytesReceived retains wire accounting.
	readonly bytesRead?: number;
	readonly bytesReceived: number;
	readonly bytesPerSecond: number;
	readonly filesReady: number;
	readonly filesActive: number;
	readonly cacheHits: number;
	readonly currentFile: string;
	readonly store: AssetStoreStats;
}
/*
================
AssetRequestTiming

One finished HTTP attempt of the shared download pipe, as the worker
measured it: how long the headers took (firstByteMs, -1 when no response
arrived) and how long the whole attempt took, with the bytes it kept and
how it ended. Reports carry the recent attempts (#4580) so a slow link
(a long, steady duration) reads apart from a stalled request (a first
byte that never came, or an outcome of "stall"). Paths only: the asset
owner strips credentials and queries exactly as for job paths.
================
*/
export interface AssetRequestTiming {
	readonly path: string;
	// Milliseconds from the attempt's start to its headers; -1 if none came.
	readonly firstByteMs: number;
	// Milliseconds from the attempt's start to its settlement.
	readonly durationMs: number;
	// Bytes this attempt kept (a resumed read counts only its own share).
	readonly bytes: number;
	readonly outcome: "ok" | "stall" | "http" | "error";
	// The HTTP status when the outcome is "http".
	readonly status?: number;
}

/*
================
AssetWorkerMessage

Worker replies transfer decoded resources or acknowledge a cancellation:
"released" answers a cancel on receipt - nothing is delivered for the id
again - while the worker's execution slot frees separately, at settlement.
================
*/
export type AssetWorkerMessage =
	| { kind: "progress"; progress: AssetProgress; }
	// Per-job liveness for the main-thread watchdog: cumulative stream bytes
	// the shared pipe has moved while a load is active, the mark that its
	// downloads have drained and decoding is what remains, and the flip back
	// when a decode-phase read starts downloading again. None carries a
	// slot or a result; the watchdog in assets.ts owns their meaning.
	| { kind: "job-progress"; id: number; bytes: number; }
	// Finished download attempts since the last report, flushed on the
	// progress beat (#4580). Diagnostics only: the asset owner keeps the
	// recent records for the bug report's assets.json; nothing acts on them.
	| { kind: "request-timings"; records: readonly AssetRequestTiming[]; }
	| { kind: "job-decoding"; id: number; }
	// The decoder's own work notes: cumulative work units (placements and
	// terrain blocks processed) reported from inside the synchronous
	// decode. A city decode is minutes of pure CPU that moves no bytes, so
	// these notes are its only liveness; the watchdog slides the decode
	// window on their increase - never the fetching window, never the
	// absolute ceiling.
	| { kind: "job-decoding-progress"; id: number; units: number; }
	| { kind: "job-fetching"; id: number; }
	// Port-only (#2800): the published build carries the HD set's index
	// (the main pack index says so), learned once that index is read.
	| { kind: "hd-availability"; available: boolean; }
	// A pack this page's manifest names answered 404: the release moved
	// under the page (#3104). The release watch checks at once.
	| { kind: "release-stale"; }
	| Exclude<AssetResult, { kind: "world"; }>
	| {
		kind: "world";
		id: number;
		prepared: import("./world-admission").PreparedWorldScene;
		images?: { path: string; image: ImageBitmap; }[];
	}
	| { kind: "released"; id: number; };
/*
================
AssetLane

"interface": the UI's image demand, which always has a slot of its own
(foundation/assets/asset-slots.ts). A load without a lane is a stream load.
================
*/
export type AssetLane = "interface";

/*
================
AssetRequest

Every load carries its byte limit and optional decoder identity.
================
*/
export type AssetRequest = {
	kind: "load";
	id: number;
	url: string;
	limit: number;
	// The interface lane's own slot (foundation/assets/asset-slots.ts); absent
	// for every stream load.
	lane?: AssetLane;
	// Also return the decoded texture's alpha as a picking mask (world DDS).
	pickAlpha?: boolean;
	decode?:
		| "navigation"
		| "frontend-world"
		| "dds"
		| "crest"
		| "png"
		| "glb"
		| "world"
		| "character"
		| "effects"
		| "effect"
		// The live page: its entry bundle, for release-skew detection.
		| "release";
} | {
	kind: "cancel";
	id: number;
} | {
	// Background install of the build's ordered list (worker/install.ts).
	kind: "install";
	url: string;
} | {
	// Port-only: the re-encoded texture set this device is served, or null
	// for the originals (foundation/assets/texture-variants.ts). Sent once.
	kind: "texture-variant";
	variant: import("@/engine/foundation/assets/texture-variants").TextureVariant | null;
} | {
	// Port-only: the HD set this device is served (#2061), or null while
	// the Experimental row is off (foundation/assets/texture-variants.ts).
	// Unlike the variant it may change while the worker runs: switching
	// the row on starts the set's download, off stops it.
	kind: "hd-textures";
	format: import("@/engine/foundation/assets/texture-variants").HdTextureVariant | null;
} | {
	// The desktop launcher's local copy of the packs (pack-mirror.ts), or
	// null in a browser. Sent once, before any load.
	kind: "pack-mirror";
	origin: string | null;
};
/*
================
AssetResult

The consumer owns delivered resources, including bitmap closure or native mip retention.
================
*/
export type AssetResult =
	| { kind: "navigation"; id: number; product: import("./navigation").NavigationProduct; }
	| { kind: "effects"; id: number; catalog: import("./effects").EffectCatalog; }
	| { kind: "character"; id: number; model: import("./character").CharacterModel; images: WorldTexture[]; }
	| {
		kind: "world";
		id: number;
		soundTerrain?: readonly import("@/engine/foundation/audio/terrain-sounds").SoundTerrain[];
		images?: { path: string; image: ImageBitmap; }[];
		world: import("./world-admission").WorldSceneLease;
	}
	| { kind: "model"; id: number; model: ModelDocument; }
	| {
		kind: "image";
		id: number;
		image: ImageBitmap;
		alpha?: import("@/engine/foundation/rendering/picking").PickAlpha;
	}
	// The entry bundle the live page names, or null when it names none;
	// packsChanged marks a live packs manifest unlike this page's (#3104);
	// packsCheckFailed marks a manifest compare that could not run, which
	// the asset owner reports as a degradation instead of reading the
	// silence as "unchanged".
	| { kind: "release"; id: number; entry: string | null; packsChanged?: boolean; packsCheckFailed?: boolean; }
	| {
		kind: "bytes";
		id: number;
		buffer: ArrayBuffer;
	}
	| {
		kind: "error";
		id: number;
		error: string;
		// The published manifest lacks the path (packs AssetAbsentError): no
		// retry will find it in this release.
		absent?: true;
		// Exhausted bounded transport retries; validation failures never set this.
		transient?: true;
		// The session's manifest lists the file but the server answered 404:
		// the release it describes has been replaced (release-watch.ts).
		stale?: true;
	};
/*
================
AssetJobSnapshot

Copied, bounded request state for diagnosing stalled or uncollected handles.
================
*/
export interface AssetJobSnapshot {
	readonly phase: "running" | "failed" | "disposed";
	readonly error: string | null;
	readonly available: number;
	readonly jobs: readonly {
		readonly id: number;
		readonly path: string;
		readonly decode: string;
		readonly ageMs: number;
		readonly state: "loading" | "completed" | "cancelling";
		readonly result: AssetResult["kind"] | null;
	}[];
	// The store's counters from the worker's latest progress, null before one.
	readonly store: AssetStoreStats | null;
	// Recent finished download attempts, oldest first (#4580).
	readonly requests: readonly AssetRequestTiming[];
}
/*
================
AssetOwner
================
*/
export interface AssetOwner {
	/** Copied diagnostics only; never consumes a result or advances a deadline. */
	snapshot?(): AssetJobSnapshot;
	progress(): AssetProgress | null;
	health(): { phase: "running"; } | { phase: "failed"; error: string; } | { phase: "disposed"; };
	// Free slots for a load of this lane; no lane asks for a stream load.
	available( lane?: AssetLane ): number;
	// Whether a listed file came back 404 (an error's stale flag) since the
	// last call; each call consumes it.
	takeReleaseStale?(): boolean;
	// Non-fatal faults since the last call (a cancellation the worker never
	// acknowledged); each call consumes them. The runtime reports each as a
	// degraded incident.
	takeDegradations?(): readonly import("./network").Degradation[];
	request(
		url: string,
		limit?: number,
		decode?:
			| "navigation"
			| "frontend-world"
			| "dds"
			| "crest"
			| "png"
			| "glb"
			| "world"
			| "character"
			| "effects"
			| "effect"
			| "release",
		options?: { readonly pickAlpha?: boolean; readonly lane?: AssetLane; }
	): number;
	take( id: number ): AssetResult | null;
	// Port-only: the device's texture features, given once when known
	// (foundation/assets/texture-variants.ts).
	textureFeatures( features: readonly string[] ): void;
	// Port-only: the Experimental "HD textures" row's live state (#2061).
	hdTextures( enabled: boolean ): void;
	// Port-only (#2800): whether the published build carries the HD set,
	// once the worker has read the main pack index; false until then, so
	// the row stays hidden and a saved "on" does nothing without a set.
	hdAvailable?(): boolean;
	cancel( id: number ): void;
	// Starts the worker's background install once; uses no request slot.
	install( listUrl: string ): void;
	dispose(): void;
}
