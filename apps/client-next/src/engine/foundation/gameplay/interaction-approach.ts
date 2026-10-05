/*
===========================================================================

interaction-approach.ts - walking into reach of an NPC or a gate first

CGInterface_OnWorldClick (698740) selects an NPC or a teleport gate only
when the player is already within its interaction reach. Farther away it
stores the target (+0x62C), an approach distance (+0x630) and a stop
tolerance (+0x634), starts CNavigationDeadreckon_StartApproachTargetEntity,
and CNavigationDeadreckon_OnTick dispatches the pending select (693AD0
type 0) once the walk ends. The server checks the same interaction in
CGObjChar_CheckHitRange (4A8E10: NPC 300, gate 800), so a far click never
opens a dialog it would refuse.

A player keeping a stall (title mode 4) is selected wherever it stands;
698740 then sends the visit (0x761F) inside 100 units, else approaches to
80 and dispatches the visit on arrival (693AD0 type 2).

The simulation foundation cannot import XState: the closed transition below
owns only the approach intent; movement owns its packet and receipt.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";

// 698740: an NPC is selected inside 240 units, else approached to 240.
const NPC_SELECT_REACH = 240;
const NPC_APPROACH_DISTANCE = 240;
// 698740: a gate is selected inside 800 units, else approached to 640.
const GATE_SELECT_REACH = 800;
const GATE_APPROACH_DISTANCE = 640;
// 698740: a stall is visited inside 100 units, else approached to 80.
const STALL_VISIT_REACH = 100;
const STALL_APPROACH_DISTANCE = 80;
// CICharactor_SetStallState: appearance title mode of a player keeping a stall.
export const STALL_TITLE_MODE = 4;
const REGION_SIZE = 1920;
const DUNGEON_REGION = 0x8000;

/*
================
interactionReach

The native select reach and approach distance for an entity kind, or null
for an entity the player selects wherever it stands.
================
*/
export function interactionReach( entity: EntityState ): { select: number; approach: number; } | null {
	if ( entity.kind === "teleport" ) return { select: GATE_SELECT_REACH, approach: GATE_APPROACH_DISTANCE };
	if ( entity.kind === "npc" ) return { select: NPC_SELECT_REACH, approach: NPC_APPROACH_DISTANCE };
	if ( keepsStall( entity ) ) return { select: STALL_VISIT_REACH, approach: STALL_APPROACH_DISTANCE };
	return null;
}

/*
================
keepsStall
================
*/
export function keepsStall( entity: EntityState ): boolean {
	return entity.kind === "player" && entity.appearanceState?.[6] === STALL_TITLE_MODE;
}

/*
================
interactionApproach

The ground point to walk to before selecting the entity, or null when the
player is already within reach. The 3D distance decides reach, as 698740
reads the entity's cached distance; the approach point lies on the ground
line from the entity toward the player.
================
*/
export function interactionApproach( pose: Pose, entity: EntityState ): Pose | null {
	const reach = interactionReach( entity );
	if ( !reach ) return null;
	if ( (pose.regionId | entity.regionId) & DUNGEON_REGION && pose.regionId !== entity.regionId ) {
		throw Error( "Interaction target is in another dungeon" );
	}
	const dx = pose.x - entity.x + ((pose.regionId & 255) - (entity.regionId & 255)) * REGION_SIZE,
		dz = pose.z - entity.z + ((pose.regionId >>> 8) - (entity.regionId >>> 8)) * REGION_SIZE,
		dy = pose.y - entity.y;
	if ( Math.hypot( dx, dy, dz ) <= reach.select ) return null;
	const distance = Math.hypot( dx, dz );
	if ( !distance ) return { ...pose, y: entity.y };
	let x = entity.x + dx / distance * reach.approach,
		z = entity.z + dz / distance * reach.approach,
		regionId = entity.regionId;
	if ( !(regionId & DUNGEON_REGION) ) {
		const rx = Math.floor( x / REGION_SIZE ), rz = Math.floor( z / REGION_SIZE );
		regionId += rx + rz * 256;
		x -= rx * REGION_SIZE;
		z -= rz * REGION_SIZE;
	}
	return { regionId, x, y: pose.y, z, angle: pose.angle };
}

export type InteractionApproachState =
	| { readonly phase: "idle"; }
	| { readonly phase: "moving"; readonly target: EntityState; readonly visit: boolean; };
export type InteractionApproachEvent =
	| { readonly kind: "begin"; readonly target: EntityState; readonly visit?: boolean; }
	| { readonly kind: "cancel"; }
	| { readonly kind: "arrived"; }
	| { readonly kind: "despawn"; readonly gid: number; };

/*
================
interactionApproachTransition

The approach intent: begun by a far click, ended by arrival, any other
command, or the target leaving the world.
================
*/
export function interactionApproachTransition(
	state: InteractionApproachState,
	event: InteractionApproachEvent
): InteractionApproachState {
	switch ( event.kind ) {
		case "begin":
			return { phase: "moving", target: event.target, visit: !!event.visit };
		case "cancel":
		case "arrived":
			return state.phase === "idle" ? state : { phase: "idle" };
		case "despawn":
			return state.phase === "moving" && state.target.gid === event.gid ? { phase: "idle" } : state;
	}
}
