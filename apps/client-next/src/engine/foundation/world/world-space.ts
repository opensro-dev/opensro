/*
===========================================================================

world-space.ts - outdoor sector grid, dungeon plane and floor geometry

Owns the client's outdoor/dungeon region coordinate math, mirroring the Go
server's worldgeom package (internal/game/world/geometry.go) name for name:

- Outdoor regions (bit 15 clear) pack (sectorX, sectorY) in the low and high
  bytes of regionId, with local X and Z in [0, 1920) world units.
- Dungeon regions (bit 15 set, 0x8000) carry local coordinates directly in
  one dungeon coordinate frame without 1920-unit sector offsets
  (SWorld_CalculateRegionOffsetVector, 0x00888140; Pos_GetRelativePlanar,
  0x00430AD0).
- Adjacency (Pos_AreSamePlaneAndAdjacentSectors, 0x00430CE0) requires both
  positions to share a plane: equal regionId in a dungeon, or a 3x3 sector
  neighbourhood outdoors.
- Dungeon floor visibility (CObj_IsVisibleInSector, 0x00863330) admits all
  outdoor pairs and requires matching regionId and resolved DOF floor index
  inside a dungeon.

===========================================================================
*/

import { hypot2 } from "@/engine/foundation/math/hypot";
import { minimapPoseKey } from "@/engine/foundation/gameplay/minimap-floor-key";

export const REGION_SIZE = 1920;
export const DUNGEON_REGION_BIT = 0x8000;

/*
================
RegionXZ

One XZ point together with the region frame that gives its local coordinates
meaning.
================
*/
export interface RegionXZ {
	readonly regionId: number;
	readonly x: number;
	readonly z: number;
}

/*
================
DungeonFloorPoint
================
*/
export interface DungeonFloorPoint {
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
}

/*
================
DungeonFloorContext
================
*/
export interface DungeonFloorContext {
	readonly navigationFloor?: number;
	readonly minimapFloors?: Readonly<Record<string, number | undefined>>;
}

/*
================
sectorX

Returns the low-byte outdoor sector coordinate.
================
*/
export function sectorX( regionId: number ): number {
	return regionId & 0xff;
}

/*
================
sectorY

Returns the high-byte outdoor sector coordinate with the dungeon bit masked
out.
================
*/
export function sectorY( regionId: number ): number {
	return (regionId >>> 8) & 0x7f;
}

/*
================
isDungeonRegion

Reports whether regionId has the native dungeon bit (0x8000) set.
================
*/
export function isDungeonRegion( regionId: number ): boolean {
	return (regionId & DUNGEON_REGION_BIT) !== 0;
}

/*
================
samePlane

Reports whether both regions belong to the same outdoor or dungeon coordinate
plane. Sector arithmetic across that boundary is meaningless.
================
*/
export function samePlane( a: number, b: number ): boolean {
	return isDungeonRegion( a ) === isDungeonRegion( b );
}

/*
================
samePlaneAdjacent

Mirrors SR_GameServer Pos_AreSamePlaneAndAdjacentSectors (0x00430CE0) and
worldgeom.SamePlaneAdjacent: two dungeon positions compare only within one
dungeon region; outdoors both sector axes must be within one sector.
================
*/
export function samePlaneAdjacent( a: number, b: number ): boolean {
	if ( !samePlane( a, b ) ) {
		return false;
	}
	if ( isDungeonRegion( a ) ) {
		return a === b;
	}
	const dx = sectorX( a ) - sectorX( b );
	const dz = sectorY( a ) - sectorY( b );
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1;
}

/*
================
worldDelta

Returns the planar displacement [dx, dz] from -> to in world units
(SWorld_CalculateRegionOffsetVector, 0x00888140): local delta when either
endpoint is a dungeon region, or sector-offset delta outdoors.

Note: native client 0x00888140 tests both regionIds for bit 0x8000 and returns
a zero offset vector whenever either endpoint is a dungeon region, whereas Go's
worldgeom.Delta only takes the local-delta branch when both endpoints share the
same dungeon regionId. They agree within one dungeon region and on all outdoor
pairs, and differ on cross-region dungeon or dungeon/outdoor pairs (where
samePlaneAdjacent is false).
================
*/
export function worldDelta( from: RegionXZ, to: RegionXZ ): [number, number] {
	if ( isDungeonRegion( from.regionId ) || isDungeonRegion( to.regionId ) ) {
		return [ to.x - from.x, to.z - from.z ];
	}
	return [
		(sectorX( to.regionId ) - sectorX( from.regionId )) * REGION_SIZE + to.x - from.x,
		(sectorY( to.regionId ) - sectorY( from.regionId )) * REGION_SIZE + to.z - from.z
	];
}

/*
================
planarDistanceSquared

Returns the squared Euclidean distance in the XZ plane across region
boundaries. Selection and range checks rank squared distances without a
square root.
================
*/
export function planarDistanceSquared( from: RegionXZ, to: RegionXZ ): number {
	const [dx, dz] = worldDelta( from, to );
	return dx * dx + dz * dz;
}

/*
================
planarDistance

Returns the Euclidean distance in the XZ plane across region boundaries.
================
*/
export function planarDistance( from: RegionXZ, to: RegionXZ ): number {
	const [dx, dz] = worldDelta( from, to );
	return hypot2( dx, dz );
}

/*
================
sameDungeonFloor

CObj_IsVisibleInSector (0x00863330): outdoors every pair is admitted; in a
dungeon, target must share local's regionId and resolved DOF floor index.
================
*/
export function sameDungeonFloor(
	local: { readonly regionId: number; },
	target: DungeonFloorPoint,
	game?: DungeonFloorContext | null
): boolean {
	if ( !isDungeonRegion( local.regionId ) && !isDungeonRegion( target.regionId ) ) {
		return true;
	}
	if ( local.regionId !== target.regionId || game?.navigationFloor === undefined ) {
		return false;
	}
	return game.minimapFloors?.[minimapPoseKey( target )] === game.navigationFloor;
}
