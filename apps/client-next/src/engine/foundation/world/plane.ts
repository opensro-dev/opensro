/*
===========================================================================

plane.ts - the navigation plane a position lives on, as one client owner

The open world and the caves (the dungeon region bit) run the same jobs
with different rules. Callers ask this owner instead of testing the region
bit themselves, so the outdoor and cave versions of a job live side by
side and cannot drift (#1646 step 4, #1793). It mirrors the server's
simulation.Plane and PlaneFeatures: planeKindOf names the plane, and
featuresAt answers, by name, the features only one plane has. world-space.ts
keeps the region arithmetic; this file keeps the behaviour.

The cave column of the features table is today's behaviour. While every
field differs between the planes, a door that read the wrong field would
fail the table test, which is the pin; a door's own cave-and-outdoor test
comes with the change that first makes a field agree on both planes.

===========================================================================
*/

import { isDungeonRegion } from "@/engine/foundation/world/world-space";

export type PlaneKind = "outdoor" | "cave";

/*
================
planeKindOf

The plane regionId belongs to: the one place outside world-space.ts that
reads the dungeon bit for behaviour.
================
*/
export function planeKindOf( regionId: number ): PlaneKind {
	return isDungeonRegion( regionId ) ? "cave" : "outdoor";
}

/*
================
PlaneFeatures
================
*/
export interface PlaneFeatures {
	// minimapCoordinates: the minimap's X/Y readout. CIFMinimap converts
	// outdoor sector coordinates only; a cave shows none.
	readonly minimapCoordinates: boolean;
	// reverseReturnPoints: a reverse return scroll destination. The table's
	// points are field positions the scroll lands on in the open world; a
	// cave region is refused when the table is decoded.
	readonly reverseReturnPoints: boolean;
	// characterPvs: the port-only dungeon character admission (Experimental
	// "Dungeon character PVS"), which culls actors in rooms or floors the
	// local player cannot see. Caves only: the open world has no rooms.
	readonly characterPvs: boolean;
}

const OUTDOOR_FEATURES = {
	minimapCoordinates: true,
	reverseReturnPoints: true,
	characterPvs: false
} as const;

const CAVE_FEATURES = {
	minimapCoordinates: false,
	reverseReturnPoints: false,
	characterPvs: true
} as const;

/*
================
featuresAt

The features of the plane regionId belongs to.
================
*/
export function featuresAt( regionId: number ): PlaneFeatures {
	return planeKindOf( regionId ) === "cave" ? CAVE_FEATURES : OUTDOOR_FEATURES;
}
