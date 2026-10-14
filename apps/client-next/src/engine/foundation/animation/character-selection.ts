/*
===========================================================================

character-selection.ts - which characters take the frame's actor room

Retains the admission order across frames: the local player, its mount,
then the nearest entities with a stable identity tie-break. Animation and
presentation still run every frame. With the Experimental shared actor
room the effect actors of the previous frame rank in the same nearest-first
order (effect-room.ts sharedCharacterRoom), so a far character gives way
to a near effect.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { Pose } from "@/engine/contracts/gameplay";
import { sharedCharacterRoom } from "./effect-room";

/*
================
createCharacterSelection

effects, when given, returns the previous frame's effect actors' squared
distances to the player, ascending, or undefined: they share the limit.
================
*/
export function createCharacterSelection( limit: number, effects?: () => readonly number[] | undefined ) {
	type Row = { entity: EntityState; gid: number; priority: number; distance: number; };
	const input: Row[] = [], ranked: Row[] = [], selected: EntityState[] = [];
	return {
		/*
		================
		select
		================
		*/
		select(
			entities: readonly EntityState[],
			anchor: Pose | undefined,
			localGid: number | undefined,
			mountedOn: number | undefined
		): readonly EntityState[] {
			let changed = input.length !== entities.length;
			for ( let i = 0; i < entities.length; i++ ) {
				const entity = entities[i]!, priority = entity.gid === localGid ? 0 : entity.gid === mountedOn ? 1 : 2;
				const x = anchor ? entity.x + ((entity.regionId & 255) - (anchor.regionId & 255)) * 1920 - anchor.x : 0;
				const z = anchor ? entity.z + ((entity.regionId >>> 8) - (anchor.regionId >>> 8)) * 1920 - anchor.z : 0;
				const distance = x * x + z * z, row = input[i];
				if ( !row ) {
					input.push( { entity, gid: entity.gid, priority, distance } );
					changed = true;
				} else {
					changed = changed || row.gid !== entity.gid || row.priority !== priority ||
						row.distance !== distance;
					row.entity = entity;
					row.gid = entity.gid;
					row.priority = priority;
					row.distance = distance;
				}
			}
			input.length = entities.length;
			if ( changed ) {
				ranked.length = input.length;
				for ( let i = 0; i < input.length; i++ ) ranked[i] = input[i]!;
				ranked.sort( ( a, b ) =>
					a.priority - b.priority || a.distance - b.distance || a.entity.gid - b.entity.gid
				);
			}
			const shared = effects?.();
			selected.length = shared?.length ?
				sharedCharacterRoom( ranked, shared, limit ) :
				Math.min( limit, ranked.length );
			for ( let i = 0; i < selected.length; i++ ) selected[i] = ranked[i]!.entity;
			return selected;
		},
		reset() {
			input.length = 0;
			ranked.length = 0;
			selected.length = 0;
		}
	};
}
