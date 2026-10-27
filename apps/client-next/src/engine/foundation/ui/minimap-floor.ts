/*
===========================================================================

minimap-floor.ts - dungeon floor admission for minimap and world-map markers

Implements CObj_IsVisibleInSector (0x863330) for dungeon sectors (regionId
with bit 0x8000 set) and collects the poses whose DOF floor indices the
simulation worker resolves into GameplayState.minimapFloors. Native calls
0x863330 only from CIFMinimap_vf30's name-resolved pin paths (quest
targets, party and academy members); its registry dot loop does not. The
port gates entity dots with it too, because its server streams a whole
dungeon region as one interest block, so every floor's entities reach the
client and would otherwise all draw.

===========================================================================
*/
import type { GameplayState, Pose } from "@/engine/contracts/gameplay";
import { minimapPoseKey } from "@/engine/foundation/gameplay/minimap-floor-key";
import { featuresAt } from "@/engine/foundation/world/plane";
import { type DungeonFloorContext, sameDungeonFloor } from "@/engine/foundation/world/world-space";

export { minimapPoseKey };

/*
================
minimapSameFloor

Delegates to sameDungeonFloor (CObj_IsVisibleInSector, 0x00863330) in
foundation/world/world-space.ts.
================
*/
export function minimapSameFloor(
	local: Pick<Pose, "regionId" | "x" | "y" | "z">,
	target: Pick<Pose, "regionId" | "x" | "y" | "z">,
	game?: DungeonFloorContext | null
): boolean {
	return sameDungeonFloor( local, target, game );
}

/*
================
minimapFloorQueries

Collects unique dungeon-sector poses (quest targets, spawned entities, party
members, and academy members) that share the local player's dungeon region so
the worker can resolve their DOF floor indices.
================
*/
export function minimapFloorQueries(
	game: GameplayState | null,
	targets: readonly Pose[],
	entities: readonly Pick<Pose, "regionId" | "x" | "y" | "z">[] = []
): readonly Pose[] {
	if ( !game?.pose || !featuresAt( game.pose.regionId ).rooms ) return [];
	const rows: Pose[] = [ ...targets ];
	for ( const e of entities ) {
		rows.push( { regionId: e.regionId, x: e.x, y: e.y, z: e.z, angle: 0 } );
	}
	for ( const m of game.social?.members ?? [] ) {
		rows.push( { regionId: m.region, x: m.x, y: m.y, z: m.z, angle: 0 } );
	}
	if ( game.academy?.member ) {
		for ( const m of game.academy.members ?? [] ) {
			if ( m.regionId !== undefined && m.x !== undefined && m.y !== undefined && m.z !== undefined ) {
				rows.push( { regionId: m.regionId, x: m.x, y: m.y, z: m.z, angle: 0 } );
			}
		}
	}
	return [
		...new Map( rows.filter( p => p.regionId === game.pose!.regionId ).map( p => [ minimapPoseKey( p ), p ] ) )
			.values()
	].slice(
		0,
		4096
	);
}
