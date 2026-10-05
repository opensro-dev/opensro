/*
===========================================================================

party-overlay.ts - roster-owned party overlay layout, distance shades and portrait identities

The quick-party board (CIFQuickPartyWnd) lists every member but oneself.
Each slot's portrait is shaded by how far the member is from the local
player (5BD0A0), so a member out of reach reads as faded.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState, Pose } from "@/engine/contracts/gameplay";

// 5BD0A0 compares the squared distance against these (0xBDE03C..0xBDE050).
const PARTY_SHADE_SQUARED_1 = 360000; // 600 units
const PARTY_SHADE_SQUARED_2 = 490000; // 700
const PARTY_SHADE_SQUARED_3 = 640000; // 800
const PARTY_SHADE_SQUARED_4 = 810000; // 900
const PARTY_SHADE_SQUARED_5 = 1000000; // 1000
// The two main mastery icons sit left of the buff row (GDR_QPS_PARTY_BUFF is
// at x 32, pitch 15), on the same 12px cells. A port addition, sent by the
// server only while SRO_PARTY_MASTERIES is on.
const PARTY_MASTERY_X = 2;
const PARTY_MASTERY_PITCH = 15;
const PARTY_MASTERY_Y = 39;
const PARTY_MASTERY_CELL = 12;
const PARTY_REGION_SIZE = 1920;
const PARTY_DUNGEON_REGION = 0x8000;
// Only the position matters to the distance shade; entities carry no heading.
type PartyShadePosition = Pick<Pose, "regionId" | "x" | "y" | "z">;
// 5BCBB0: exclude self by name, preserve roster order, wrap before placing
// each row. The 230px bottom reserve is independent of the buff preference.
// Each slot's CIFBuffViewer (5BAD30 / 5BA840) is owned by the UI runtime,
// which keeps its one-second diff state across frames.
/*
================
partyMembers
================
*/
export function partyMembers( game: GameplayState ) {
	return (game.social?.members ?? []).filter( m => m.name !== game.social?.localName );
}
/*
================
partyOverlay
================
*/
export function partyOverlay(
	game: GameplayState,
	entities: readonly EntityState[],
	height: number,
	x: number,
	y: number,
	buffs: boolean
) {
	let column = 0, rowY = 0;
	return partyMembers( game ).map( member => {
		if ( height - y - rowY - 73 < 230 ) {
			column += 133;
			rowY = 0;
		}
		const entity = entities.find( e => e.kind === "player" && e.name === member.name ),
			position = [ x + column + 13, y + rowY ] as const;
		rowY += buffs ? 73 : 56;
		// An untrained slot is 0 and stays empty; a roster without the pair
		// (switch off) yields nothing.
		const masteries = [ member.primaryMastery, member.secondaryMastery ].flatMap( ( id, slot ) => {
			if ( !id ) return [];
			const rect = [
				position[0] + PARTY_MASTERY_X + PARTY_MASTERY_PITCH * slot,
				position[1] + PARTY_MASTERY_Y,
				PARTY_MASTERY_CELL,
				PARTY_MASTERY_CELL
			] as const;
			return [ { id, rect } ];
		} );
		return {
			member,
			masteries,
			entity,
			position,
			hp: Math.min( 10, member.status & 15 ) / 10,
			mp: Math.min( 10, member.status >>> 4 ) / 10,
			leader: member.id === game.social?.leader
		};
	} );
}

/*
================
partyPortraitGid

Portrait identities live above the unsigned wire GID space. They identify
roster slots, never targetable world objects or negative effect actors.
================
*/
export function partyPortraitGid( memberId: number ): number {
	const PARTY_PORTRAIT_ID_BASE = 0x100000000;
	return PARTY_PORTRAIT_ID_BASE + memberId;
}

/*
================
partyRosterPose

The pose 5BD0A0 measures for a member out of view: the roster record's
region and planar position (+0x5A, +0x5C, +0x64). The record's height is
never copied, so y stays 0 (the zeroed local at 5BD17E).
================
*/
export function partyRosterPose( member: { readonly region: number; readonly x: number; readonly z: number; } ): Pose {
	return { regionId: member.region, x: member.x, y: 0, z: member.z, angle: 0 };
}

/*
================
partyDistanceShade

CIFQuickPartyWnd_UpdateDistanceOverlays (5BD0A0): the squared 3D distance
from the local player to a member (Navigation_CalculateDisplacementAndDistanceSq
879250, float stores between steps) picks the slot's face shade, 0 for none
up to 5 at 1000 units or more. Each threshold is inclusive.

SWorld_CalculateRegionOffsetVector (888140) adds the outdoor sector offset
and returns zero when either region is indoor (0x8000), so inside a dungeon
the plain local coordinates are compared.
================
*/
export function partyDistanceShade( local: PartyShadePosition, member: PartyShadePosition ): number {
	const f = Math.fround;
	let dx = f( member.x - local.x ), dz = f( member.z - local.z );
	const dy = f( member.y - local.y );
	if ( !((local.regionId | member.regionId) & PARTY_DUNGEON_REGION) ) {
		dx = f( dx + ((member.regionId & 255) - (local.regionId & 255)) * PARTY_REGION_SIZE );
		dz = f( dz + ((member.regionId >>> 8) - (local.regionId >>> 8)) * PARTY_REGION_SIZE );
	}
	const squared = f( dx * dx + dy * dy + dz * dz );
	if ( squared >= PARTY_SHADE_SQUARED_5 ) return 5;
	if ( squared >= PARTY_SHADE_SQUARED_4 ) return 4;
	if ( squared >= PARTY_SHADE_SQUARED_3 ) return 3;
	if ( squared >= PARTY_SHADE_SQUARED_2 ) return 2;
	if ( squared >= PARTY_SHADE_SQUARED_1 ) return 1;
	return 0;
}

/*
================
partyShadeImage

CIFQuickPartySlot_SetDistanceOverlay (5BA400): shades 1..5 set
qpt_face_faraway_60..100 on the slot's GDR_QPS_STATUS (id 5); 0 clears it.
================
*/
export function partyShadeImage( shade: number ): string | null {
	const PARTY_SHADE_BASE_PERCENT = 50, PARTY_SHADE_STEP_PERCENT = 10;
	if ( shade <= 0 ) return null;
	return "interface/quickparty/qpt_face_faraway_" + (PARTY_SHADE_BASE_PERCENT + shade * PARTY_SHADE_STEP_PERCENT) +
		".png";
}
