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
AssetRequest

Every load carries its byte limit and optional decoder identity.
================
*/
export type AssetRequest = {
	kind: "load";
	id: number;
	url: string;
	limit: number;
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
	available(): number;
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
		options?: { readonly pickAlpha?: boolean; }
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
