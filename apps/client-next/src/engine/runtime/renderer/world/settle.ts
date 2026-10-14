/*
===========================================================================

settle.ts - a frontend scene gives up its CPU geometry once it is drawn

A prepared scene's streams share one packed arena (world-scene.ts
packGroupStreams), so the CPU copy of a scene lives as long as any group
keeps a view of it: at the title about 190 MB, more than the scene's GPU
buffers. A frontend scene at full terrain detail reads almost none of it
after its first frame. Every terrain range is chosen once at LOD 0 and
never re-chosen, its seams are stitched once, nothing picks or rains on
it, and only the race models are picked (pickInterface). So a settled
scene keeps:

- terrain: nothing. The draw has its indices and vertices on the GPU; the
  group keeps empty streams and its device draw drops its mirror.
- every other group: its own small copies (positions, indices, instances,
  transform and the rest), off the arena, so picking the interface models
  and per-frame instance selection still read them.

A settled scene cannot be uploaded again from the CPU; after a device loss
its owner requests it again (world.ts invalidate, stage.ts).

===========================================================================
*/
import type { WorldGroup, WorldScene } from "@/engine/contracts/scene";
import type { PickBounds } from "@/engine/foundation/rendering/picking";
import type { GeometryCommands, GeometryDraw } from "../internal/gpu-contract";

const NO_FLOATS = new Float32Array( 0 );
const NO_INDICES = new Uint32Array( 0 );

/*
================
SettleState

The world renderer's maps a settle rewrites.
================
*/
export interface SettleState {
	readonly geometry: GeometryCommands;
	readonly draws: ReadonlyMap<WorldGroup, GeometryDraw>;
	readonly selections: ReadonlyMap<WorldGroup, { indices: Uint32Array; chosen?: unknown; }>;
	readonly pickBounds: WeakMap<Float32Array, PickBounds>;
}

/*
================
settleReady

True when every terrain group of scene has chosen its ranges: the only
moment its index and position streams are read.
================
*/
export function settleReady( scene: WorldScene, state: SettleState ): boolean {
	return scene.groups.every( group => !group.ranges || !!state.selections.get( group )?.chosen );
}

/*
================
own

A copy of a stream on its own buffer, or the value unchanged.
================
*/
function own<T>( value: T ): T {
	return ArrayBuffer.isView( value ) ? (value as unknown as Float32Array).slice() as unknown as T : value;
}

/*
================
settleScene

Rewrites scene's groups in place (they are the renderer's own copies) so no
group views the packed arena.
================
*/
export function settleScene( scene: WorldScene, state: SettleState ): void {
	for ( const group of scene.groups ) {
		// The groups are the renderer's own copies (structured transfer), so the
		// readonly contract type is bypassed here alone.
		const geometry = group.geometry as unknown as Record<string, unknown>;
		if ( group.ranges ) {
			const draw = state.draws.get( group );
			if ( draw && group.geometry.dynamicVertices ) state.geometry.dropMirror( draw );
			for ( const key of Object.keys( geometry ) ) {
				if ( ArrayBuffer.isView( geometry[key] ) && key !== "transform" && key !== "instances" ) {
					geometry[key] = key === "indices" ? NO_INDICES : NO_FLOATS;
				}
			}
			geometry.transform = own( geometry.transform );
			geometry.instances = own( geometry.instances );
			for ( const range of group.ranges ) (range as { seamVertices?: Uint32Array; }).seamVertices = undefined;
			const cache = state.selections.get( group );
			if ( cache ) cache.indices = NO_INDICES;
			continue;
		}
		const positions = group.geometry.positions, bounds = state.pickBounds.get( positions );
		for ( const key of Object.keys( geometry ) ) geometry[key] = own( geometry[key] );
		if ( bounds ) state.pickBounds.set( group.geometry.positions, bounds );
	}
}
