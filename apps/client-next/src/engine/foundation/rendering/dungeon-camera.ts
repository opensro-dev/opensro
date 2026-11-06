/*
===========================================================================

dungeon-camera.ts - the far plane a scene projects with

The Video background distance is the far plane of the outdoor camera only.
8B17E0 draws a dungeon through the camera at SWorld +0x20B2, a copy (8B951A)
that never receives it, so a long room is never sliced by that option.

===========================================================================
*/

// A1B620: the far plane every camera starts with. Only the outdoor camera at
// SWorld +0x2206 is ever given the Video background distance.
export const DUNGEON_CAMERA_FAR = 500000;

/*
================
sceneCameraFar

undefined keeps the caller's far. Outdoors 8B1050 installs the background
distance; in a dungeon 8B951F hands it to the culling object instead. Inferred:
the source camera of the 8B951A copy keeps A1B620's default, as nothing in
SWorld writes that field on it.
================
*/
export function sceneCameraFar( dungeon: boolean, backgroundDistance: number | undefined ): number | undefined {
	return dungeon ? DUNGEON_CAMERA_FAR : backgroundDistance;
}
