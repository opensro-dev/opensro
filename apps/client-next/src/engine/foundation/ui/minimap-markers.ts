/*
===========================================================================

minimap-markers.ts - minimap actor, roster, quest, and hunting-point markers

Projects nearby entities, party/academy roster entries, and hunting points
onto the minimap radar and filters dungeon markers to the local player's
DOF floor.

===========================================================================
*/
import { minimapSameFloor } from "./minimap-floor";
import { REGION_SIZE, worldDelta } from "@/engine/foundation/world/world-space";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState, Pose } from "@/engine/contracts/gameplay";

/*
================
MinimapMarker
================
*/
export interface MinimapMarker {
	readonly x: number;
	readonly y: number;
	readonly size: number;
	readonly rotation: number;
	readonly path: string;
}

const root = "/assets/images/Media_extracted/interface/minimap/mm_sign_";

/*
================
minimapEntityIcon

54CF1D: equality with rarity 3, not the target-window rarity family.
================
*/
export function minimapEntityIcon( entity: EntityState ): string | null {
	if ( entity.appearanceState?.[2] === 4 ) return null;
	const kind = entity.kind === "monster" ?
		(entity.rarity === 3 ? "unique" : "monster") :
		entity.kind === "cos" ?
		"animal" :
		entity.kind === "npc" ?
		"npc" :
		entity.kind === "player" ?
		"otherplayer" :
		null;
	return kind ? root + kind + ".png" : null;
}

/*
================
minimapOffset

888140 returns a zero sector offset if either region is a dungeon.
================
*/
export function minimapOffset(
	local: Pick<Pose, "regionId" | "x" | "z">,
	target: Pick<Pose, "regionId" | "x" | "z">,
	zoom: number
): readonly [number, number] | null {
	const [dx, dz] = worldDelta( local, target );
	return [ dx * zoom / REGION_SIZE, -dz * zoom / REGION_SIZE ];
}

/*
================
minimapEdge
================
*/
export function minimapEdge(
	offset: readonly [number, number],
	kind: "party" | "apprenticeship" | "quest"
): MinimapMarker {
	const [x, y] = offset, distance = Math.hypot( x, y ), outside = distance >= 47;
	let angle = Math.acos( Math.max( -1, Math.min( 1, x / distance ) ) );
	if ( y <= 0 ) angle = -angle;
	return {
		x: outside ? x * 47 / distance : x,
		y: outside ? y * 47 / distance : y,
		size: outside ? (kind === "party" ? 16 : 32) : kind === "quest" ? 16 : 8,
		rotation: outside ? -(angle + Math.PI) : 0,
		path: root + kind + (outside ? "arrow" : kind === "quest" ? "npc" : "") + ".png"
	};
}

/*
================
RosterPosition
================
*/
export interface RosterPosition {
	readonly name: string;
	readonly kind: "apprenticeship" | "party";
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
}

/*
================
rosterPositions

The roster admission both map surfaces share: 54D24A/54DB1E on the minimap
and 57CE80 on the world map walk the same two rosters under the same guards
(self, offline, fortress-war world id, dungeon floor) and both prefer a
visible entity's live pose over the last roster broadcast. Only the
projection and the sprites differ, so the selection lives here once and the
callers own their own geometry.
================
*/
export function rosterPositions(
	local: Pose,
	game: GameplayState | null,
	entities: readonly EntityState[]
): readonly RosterPosition[] {
	const result: RosterPosition[] = [], war = game?.fortress?.worldId ?? 0x10001;
	/*
	================
	live
	================
	*/
	const live = ( name: string, fallback: RosterPosition ): RosterPosition => {
		const row = entities.find( e => e.name === name && (e.kind === "player" || e.kind === "local-player") );
		return row ? { ...fallback, regionId: row.regionId, x: row.x, y: row.y, z: row.z } : fallback;
	};
	if ( game?.academy?.member ) {
		for ( const row of [ ...(game.academy.members ?? []) ].sort( ( a, b ) => a.id - b.id ) ) {
			if (
				row.name === game.social?.localName || row.offline || row.war !== war || row.regionId === undefined ||
				row.x === undefined || row.z === undefined
			) continue;
			const roster = { regionId: row.regionId, x: row.x, y: row.y ?? 0, z: row.z, angle: 0 };
			if ( !minimapSameFloor( local, roster, game ) ) continue;
			result.push(
				live( row.name, {
					kind: "apprenticeship",
					name: row.name,
					regionId: roster.regionId,
					x: roster.x,
					y: roster.y,
					z: roster.z
				} )
			);
		}
	}
	for ( const row of game?.social?.members ?? [] ) {
		if ( row.id === game?.social?.self || row.war !== war ) continue;
		const roster = { regionId: row.region, x: row.x, y: row.y, z: row.z, angle: 0 };
		if ( !minimapSameFloor( local, roster, game ) ) continue;
		result.push(
			live( row.name, {
				kind: "party",
				name: row.name,
				regionId: roster.regionId,
				x: roster.x,
				y: roster.y,
				z: roster.z
			} )
		);
	}
	return result;
}

/*
================
minimapMarkers

The native registry walk draws every streamed entity; 0x863330 gates only
CIFMinimap_vf30's name-resolved pins, never the dot loop. Because the server
streams a dungeon region as a single interest block (apps/server/internal/game/world/interest.go),
multi-floor dungeons sharing one regionId (such as Donwhang Stone Cave 0x8001
F1..F4) keep all floors' entities in the client registry; gating dungeon
entities on minimapSameFloor (0x863330's rule) restricts dots to the local
player's DOF floor.
================
*/
export function minimapMarkers(
	local: Pose,
	game: GameplayState | null,
	entities: readonly EntityState[],
	zoom: number,
	radius: number
): readonly MinimapMarker[] {
	const result: MinimapMarker[] = [];
	// Requested visibility adjustment: render the 12px unique artwork at native asset resolution; other actor dots remain 8px.
	for ( const entity of entities ) {
		if ( entity.gid === game?.localGid ) continue;
		if ( !minimapSameFloor( local, entity, game ) ) continue;
		const path = minimapEntityIcon( entity ), offset = path && minimapOffset( local, entity, zoom );
		if ( !path || !offset || Math.hypot( ...offset ) >= radius - 8 ) continue;
		result.push( {
			x: offset[0],
			y: offset[1],
			size: entity.kind === "monster" && entity.rarity === 3 ? 12 : 8,
			rotation: 0,
			path
		} );
	}
	for ( const row of rosterPositions( local, game, entities ) ) {
		const offset = minimapOffset( local, row, zoom );
		if ( !offset ) continue;
		result.push( minimapEdge( offset, row.kind ) );
	}
	return result;
}

/*
================
minimapHunting
================
*/
export function minimapHunting(
	local: Pose,
	game: GameplayState | null,
	entities: readonly EntityState[],
	zoom: number
): readonly MinimapMarker[] {
	const result: MinimapMarker[] = [];
	for ( const point of game?.huntingPoints ?? [] ) {
		const live = entities.find( e => e.gid === point.gid ), offset = minimapOffset( local, live ?? point, zoom );
		if ( !offset ) continue;
		const distance = Math.hypot( ...offset ), scale = distance >= 47 ? 47 / distance : 1;
		result.push( {
			x: offset[0] * scale,
			y: offset[1] * scale,
			size: 16,
			rotation: -point.angle * Math.PI * 2 / 65536,
			path: "/assets/images/Media_extracted/interface/worldmap/wmap_sign_huntingpoint.png"
		} );
	}
	return result;
}
