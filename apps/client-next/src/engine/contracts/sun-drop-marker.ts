/*
===========================================================================

sun-drop-marker.ts - one marked Seal of Sun drop's ground ring

Port-only, not native (#1640): the runtime publishes the admitted drops
(simulation-stamped groundItem.sunMarker) and the world renderer draws
a soft gold ring under each. Mirrors footprint.ts's pose shape.

===========================================================================
*/
/** A ground ring under one of the player's own degree-9+ Seal of Sun drops. */
export interface SunDropMarker {
	readonly gid: number;
	readonly pose: { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; };
}
