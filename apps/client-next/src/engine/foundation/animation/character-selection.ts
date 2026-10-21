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
import type { GameplayState, Pose } from "@/engine/contracts/gameplay";
import { minimapPoseKey } from "@/engine/foundation/gameplay/minimap-floor-key";
import { isDungeonRegion, planarDistanceSquared, sameDungeonFloor } from "@/engine/foundation/world/world-space";
import { sharedCharacterRoom } from "./effect-room";

/*
================
DungeonAdmissionInput
================
*/
export interface DungeonAdmissionInput {
	readonly entities: readonly EntityState[];
	readonly anchor?: Pick<Pose, "regionId" | "x" | "y" | "z">;
	readonly gameplay: GameplayState | null;
	readonly localGid?: number;
	readonly mountedOn?: number;
	readonly dungeonCharacterPvs?: boolean;
	readonly dungeonBlockHides?: ( block: number, activeBlock?: number ) => boolean;
}

/*
================
dungeonCharacterAdmission

Port-only, not native (Experimental "Dungeon character PVS", default ON;
when off, returns undefined so selection behaves identically to native):
filters out dungeon entities before top-K selection when:
1. Room PVS: the entity's mover has a dungeonBlock that the active dungeon
   block does not draw (dungeonBlockHides(block, activeBlock)).
2. Dungeon floor: gameplay.navigationFloor is known and sameDungeonFloor
   reports the entity's mover is on another floor.
Never rejects:
- the local player, their mount, or their active COS/pet;
- the currently selected target (gameplay.target / selectionDecal);
- an entity currently in an active cast/attack with the local player or pet;
- a rider whose mount passes, or a mount whose rider passes.
================
*/
export function dungeonCharacterAdmission(
	input: DungeonAdmissionInput
): (( entity: EntityState ) => boolean) | undefined {
	const { entities, gameplay, dungeonCharacterPvs = true, dungeonBlockHides } = input;
	const anchor = input.anchor ?? gameplay?.pose ?? undefined;
	if ( !dungeonCharacterPvs || !anchor || !isDungeonRegion( anchor.regionId ) ) {
		return undefined;
	}
	const localAnchor: Pick<Pose, "regionId" | "x" | "y" | "z"> = anchor;
	const checkBlock = dungeonBlockHides !== undefined;
	const checkFloor = gameplay?.navigationFloor !== undefined;
	if ( !checkBlock && !checkFloor ) {
		return undefined;
	}
	const entitiesByGid = new Map<number, EntityState>();
	for ( const entity of entities ) {
		entitiesByGid.set( entity.gid, entity );
	}
	const localParty = new Set<number>();
	const localGid = input.localGid ?? (gameplay?.localGid || undefined);
	if ( localGid !== undefined ) {
		localParty.add( localGid );
		const local = entitiesByGid.get( localGid );
		if ( local?.mountedOn !== undefined ) {
			localParty.add( local.mountedOn );
		}
	}
	if ( input.mountedOn !== undefined ) {
		localParty.add( input.mountedOn );
	}
	if ( gameplay?.activeCos ) {
		localParty.add( gameplay.activeCos.gid );
	}
	if ( gameplay?.selectedCosGid !== undefined ) {
		localParty.add( gameplay.selectedCosGid );
	}
	if ( gameplay?.petTraining ) {
		localParty.add( gameplay.petTraining.gid );
	}
	for ( const cos of gameplay?.cosRecords ?? [] ) {
		localParty.add( cos.gid );
	}
	for ( const cos of gameplay?.cosStatusRecords ?? [] ) {
		localParty.add( cos.gid );
	}
	for ( const entity of entities ) {
		if (
			entity.kind === "local-player" ||
			(localGid !== undefined && (entity.ownerGid === localGid || entity.mountedOn === localGid))
		) {
			localParty.add( entity.gid );
			if ( entity.mountedOn !== undefined ) {
				localParty.add( entity.mountedOn );
			}
		}
	}
	const exempt = new Set<number>( localParty );
	if ( (gameplay?.target ?? 0) > 0 ) {
		exempt.add( gameplay!.target );
	}
	if ( gameplay?.selectionDecal?.kind === "target" ) {
		exempt.add( gameplay.selectionDecal.gid );
	}
	const casts = gameplay?.castPrediction ?
		[ ...(gameplay.casts ?? []), gameplay.castPrediction ] :
		gameplay?.casts ?? [];
	for ( const cast of casts ) {
		if ( cast.cancelledAtMs !== undefined ) {
			continue;
		}
		if ( localParty.has( cast.caster ) ) {
			if ( cast.target > 0 ) {
				exempt.add( cast.target );
			}
			for ( const result of cast.results ?? [] ) {
				if ( result.target > 0 ) {
					exempt.add( result.target );
				}
			}
		}
		const targetsLocal = localParty.has( cast.target ) ||
			(cast.results ?? []).some( result => localParty.has( result.target ) );
		if ( targetsLocal && cast.caster > 0 ) {
			exempt.add( cast.caster );
		}
	}
	for ( const entity of entities ) {
		if ( entity.mountedOn === undefined ) {
			continue;
		}
		if ( exempt.has( entity.gid ) ) {
			exempt.add( entity.mountedOn );
		} else if ( exempt.has( entity.mountedOn ) ) {
			exempt.add( entity.gid );
		}
	}
	const activeBlock = gameplay?.navigationBlock;
	/*
	================
	admitCharacter
	================
	*/
	function admitCharacter( entity: EntityState ): boolean {
		if ( exempt.has( entity.gid ) ) {
			return true;
		}
		const mover = entity.mountedOn !== undefined ?
			entitiesByGid.get( entity.mountedOn ) ?? entity :
			entity;
		if ( exempt.has( mover.gid ) ) {
			return true;
		}
		if (
			mover.dungeonBlock !== undefined &&
			dungeonBlockHides?.( mover.dungeonBlock, activeBlock ) === true
		) {
			return false;
		}
		const hasFloor = mover.regionId !== localAnchor.regionId ||
			gameplay?.minimapFloors?.[minimapPoseKey( mover )] !== undefined;
		if ( checkFloor && hasFloor && !sameDungeonFloor( localAnchor, mover, gameplay ) ) {
			return false;
		}
		return true;
	}
	return admitCharacter;
}

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
			mountedOn: number | undefined,
			admit?: ( entity: EntityState ) => boolean
		): readonly EntityState[] {
			let count = 0, changed = false;
			for ( let i = 0; i < entities.length; i++ ) {
				const entity = entities[i]!;
				if ( admit && !admit( entity ) ) continue;
				const priority = entity.gid === localGid ? 0 : entity.gid === mountedOn ? 1 : 2;
				const distance = anchor ? planarDistanceSquared( anchor, entity ) : 0;
				const row = input[count];
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
				count++;
			}
			if ( input.length !== count ) {
				input.length = count;
				changed = true;
			}
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
		/*
		================
		reset
		================
		*/
		reset() {
			input.length = 0;
			ranked.length = 0;
			selected.length = 0;
		}
	};
}
