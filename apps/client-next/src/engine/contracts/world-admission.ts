/*
===========================================================================

world-admission.ts - how a decoded world scene reaches the renderer

Types only. The asset worker publishes a prepared scene; the host wraps it
in a one-use lease. An outdoor scene also names the terrain parts it
shares with the scenes around it: one region's terrain, decoded once in
the scene's anchor coordinates and kept resident while any scene uses it.

===========================================================================
*/
import type { WorldGroup, WorldScene } from "./scene";

/** Worker-only publication, after full render admission. Structured transfer
 * isolates its arrays/metadata before a host lease can be constructed. */
export interface PreparedWorldScene {
	readonly scene: WorldScene;
	readonly bytes: number;
	readonly starBytes: number;
}

/** One-use move capability. No scene/array reference is exposed before takeWorld().
 * Consumption transfers ownership even when the receiving renderer rejects it. */
export interface WorldSceneLease {
	readonly sceneId: string;
	takeWorld(): PreparedWorldScene;
}

/** One outdoor region's terrain, lightmap and water groups in the
 * coordinates of origin (the scene anchor). The same part object is
 * composed into every scene that covers the region, so its groups keep
 * their GPU resources across region crossings. bytes is its admitted size. */
export interface WorldTerrainPart {
	readonly region: number;
	readonly origin: number;
	readonly groups: readonly WorldGroup[];
	readonly bytes: number;
}
