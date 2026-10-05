/*
===========================================================================

entity-lod.ts - the distance-based animation level of detail

The local player's 0xD timer rescans, once a second (or at the next frame
after a character spawns), the camera's distance to every entity (8646A0)
and divides it by 800 into the 0..1 fraction that animation, particle and
equipment-glow detail read. The distance is measured to where the entity
actually is, not to a row that may lag it.

===========================================================================
*/
import type { EntityState, WorldEvent } from "@/engine/contracts/world";
export type LodPoint = Pick<EntityState, "regionId" | "x" | "y" | "z">;
/*
================
nativeEntityDistance

8646A0 stores float deltas, the squared sum, and the sqrt separately.
================
*/
export function nativeEntityDistance( a: LodPoint, b: LodPoint ): number {
	const f = Math.fround, dungeon = !!(a.regionId & 0x8000);
	const x = f( a.x - b.x + (dungeon ? 0 : ((a.regionId & 255) - (b.regionId & 255)) * 1920) );
	const y = f( a.y - b.y ), z = f( a.z - b.z + (dungeon ? 0 : ((a.regionId >>> 8) - (b.regionId >>> 8)) * 1920) );
	return f( Math.sqrt( f( y * y + x * x + z * z ) ) );
}
/*
================
nativeEntityLod
================
*/
export function nativeEntityLod( distance: number, enabled = true ): number {
	return enabled ? Math.fround( Math.min( 1, Math.max( 0, Math.fround( distance / 800 ) ) ) ) : 0;
}
/*
================
createEntityLod

The local player's 0xD timer owns one global scan, not one timer per model.
Invoked by the existing presentation frame; no asynchronous scheduler.
================
*/
export function createEntityLod() {
	let timer: { gid: number; next: number; } | null = null, forced = false, crowded = false;
	const distances = new Map<number, number>();
	return {
		receive( events: readonly WorldEvent[] ) {
			for ( const event of events ) {
				if ( event.kind === "reset" ) {
					timer = null;
					forced = false;
					crowded = false;
					distances.clear();
				} else if ( event.kind === "despawn" ) distances.delete( event.gid );
				else if (
					event.kind === "spawn" && timer &&
					[ "player", "monster", "npc", "cos" ].includes( event.entity.kind ) && !event.entity.spawnAppearance
				) forced = true;
			}
		},
		// localPose: where the local player (and localMount, the vehicle it
		// rides) actually is. 8646A0 measures the live object, and the local
		// row can still hold its spawn point while its movement owner runs.
		step(
			entities: readonly EntityState[],
			localGid: number | undefined,
			camera: LodPoint | null,
			nowMs: number,
			localPose?: LodPoint,
			localMount?: number
		) {
			const local = entities.find( e => e.gid === localGid );
			if ( !local ) {
				timer = null;
				forced = false;
				crowded = false;
				distances.clear();
				return;
			}
			if ( !timer || timer.gid !== local.gid ) {
				timer = { gid: local.gid, next: nowMs + 1000 };
				distances.clear();
			}
			if ( !camera || !forced && nowMs < timer.next ) return;
			if ( nowMs >= timer.next ) timer.next = nowMs + 1000;
			forced = false;
			crowded = false;
			distances.clear();
			crowded = entities.filter( e => [ "player", "monster", "npc", "cos" ].includes( e.kind ) ).length >= 100;
			for ( let i = 0; i < Math.min( 10000, entities.length ); i++ ) {
				const entity = entities[i]!;
				const live = localPose && (entity.gid === localGid || entity.gid === localMount);
				distances.set( entity.gid, nativeEntityDistance( camera, live ? localPose : entity ) );
			}
		},
		crowded() {
			return crowded;
		},
		fraction( gid: number ) {
			return nativeEntityLod( distances.get( gid ) ?? 0 );
		},
		// CIGIDObject +0x244, the same scan's distance; the constructor (8517F0)
		// seeds 1000 until the first scan measures it.
		distance( gid: number ) {
			return distances.get( gid ) ?? 1000;
		},
		reset() {
			timer = null;
			forced = false;
			crowded = false;
			distances.clear();
		}
	};
}
