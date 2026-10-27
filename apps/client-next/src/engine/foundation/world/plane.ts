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
	// terrainGrid: the outdoor terrain's 20-unit cell grid and the terrain
	// owner path (navigation clipper). A cave has no terrain grid: its
	// floors are the dungeon's own navmesh objects.
	readonly terrainGrid: boolean;
	// residentOnly: the clipper holds one admitted dungeon at a time, so a
	// cave pose is covered only by the resident one, whole (complete).
	readonly residentOnly: boolean;
	// rooms: the dungeon's block (room) and floor queries.
	readonly rooms: boolean;
	// sectorFrames: poses rebase across 1920-unit sector frames. A cave's
	// locals are one frame for the whole region.
	readonly sectorFrames: boolean;
	// sky: the sky dome, its lens flares and star field. A cave has a
	// ceiling, not a sky.
	readonly sky: boolean;
	// animatedWater: the outdoor water's animated normal frames over a
	// block's water. Cave water is the dungeon's own surfaces (dungeonWater).
	readonly animatedWater: boolean;
	// dungeonWater: the dungeon's authored water surfaces; an outdoor bundle
	// carrying them is malformed.
	readonly dungeonWater: boolean;
	// signedLocalCoordinates: a wire pose's local coordinates are signed.
	// A cave pose is a signed 16-bit plane; a field pose is an unsigned
	// 1920-unit cell (movement-wire's admitPose).
	readonly signedLocalCoordinates: boolean;
	// worldMap: the world map (M) projects the position onto its overview
	// and town pages, and the hunting guide's anchors live there. The map
	// draws sector space only; a cave has no place on it.
	readonly worldMap: boolean;
	// floorMinimap: the minimap draws the floor's own art (minimap_d,
	// chosen by the floor query) instead of the sector tiles.
	readonly floorMinimap: boolean;
	// regionCatalog: a region is found through the world region catalog
	// and loads its own outdoor bundle; a cave loads its dungeon bundle
	// (world) and the dungeon resources (navigation) by region id instead.
	readonly regionCatalog: boolean;
}

const OUTDOOR_FEATURES = {
	minimapCoordinates: true,
	reverseReturnPoints: true,
	characterPvs: false,
	terrainGrid: true,
	residentOnly: false,
	rooms: false,
	sectorFrames: true,
	signedLocalCoordinates: false,
	worldMap: true,
	floorMinimap: false,
	regionCatalog: true,
	sky: true,
	animatedWater: true,
	dungeonWater: false
} as const;

const CAVE_FEATURES = {
	minimapCoordinates: false,
	reverseReturnPoints: false,
	characterPvs: true,
	terrainGrid: false,
	residentOnly: true,
	rooms: true,
	sectorFrames: false,
	signedLocalCoordinates: true,
	worldMap: false,
	floorMinimap: true,
	regionCatalog: false,
	sky: false,
	animatedWater: false,
	dungeonWater: true
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
