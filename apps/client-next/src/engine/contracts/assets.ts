/*
===========================================================================

assets.ts - the asset worker contract

The messages between the main-thread asset owner and the asset worker,
the decoded result kinds, and the AssetOwner interface every runtime owner
loads through.

===========================================================================
*/

import type { ModelDocument } from "./model";
/*
================
AssetProgress
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
================
*/
export type AssetRequest = {
	kind: "load";
	id: number;
	url: string;
	limit: number;
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
================
*/
export type AssetResult =
	| { kind: "navigation"; id: number; product: import("./navigation").NavigationProduct; }
	| { kind: "effects"; id: number; catalog: import("./effects").EffectCatalog; }
	| { kind: "character"; id: number; model: import("./character").CharacterModel; images: ImageBitmap[]; }
	| {
		kind: "world";
		id: number;
		soundTerrain?: readonly import("@/engine/foundation/audio/terrain-sounds").SoundTerrain[];
		images?: { path: string; image: ImageBitmap; }[];
		world: import("./world-admission").WorldSceneLease;
	}
	| { kind: "model"; id: number; model: ModelDocument; }
	| { kind: "image"; id: number; image: ImageBitmap; }
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
	};
/*
================
AssetOwner
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
			| "release"
	): number;
	take( id: number ): AssetResult | null;
	cancel( id: number ): void;
	// Starts the worker's background install once; uses no request slot.
	install( listUrl: string ): void;
	dispose(): void;
}
