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

Worker replies transfer decoded resources or acknowledge released capacity.
================
*/
export type AssetWorkerMessage =
	| { kind: "progress"; progress: AssetProgress; }
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
	// The entry bundle the live page names, or null when it names none.
	| { kind: "release"; id: number; entry: string | null; }
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
	};
/*
================
AssetOwner

The runtime asset owner grants bounded handles with explicit cancellation and disposal.
================
*/
export interface AssetOwner {
	progress(): AssetProgress | null;
	health(): { phase: "running"; } | { phase: "failed"; error: string; } | { phase: "disposed"; };
	available(): number;
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
	cancel( id: number ): void;
	// Starts the worker's background install once; uses no request slot.
	install( listUrl: string ): void;
	dispose(): void;
}
