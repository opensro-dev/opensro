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
import { regionOffsetXZ } from "@/engine/foundation/world/world-space";

// 5BD0A0 compares the squared distance against these (0xBDE03C..0xBDE050).
const PARTY_SHADE_SQUARED_1 = 360000; // 600 units
const PARTY_SHADE_SQUARED_2 = 490000; // 700
const PARTY_SHADE_SQUARED_3 = 640000; // 800
const PARTY_SHADE_SQUARED_4 = 810000; // 900
const PARTY_SHADE_SQUARED_5 = 1000000; // 1000
// The two main mastery icons sit left of the buff row (GDR_QPS_PARTY_BUFF is
// at x 32, pitch 15), on the same 12px cells. The pair itself is native wire
// data (member-row mask bit 0x08, +0x50/+0x54); drawing it here is the
// browser board's own presentation of that data.
const PARTY_MASTERY_X = 2;
const PARTY_MASTERY_PITCH = 15;
const PARTY_MASTERY_Y = 39;
const PARTY_MASTERY_CELL = 12;
// Only the position matters to the distance shade; entities carry no heading.
type PartyShadePosition = Pick<Pose, "regionId" | "x" | "y" | "z">;
// 5BCBB0: exclude self by name, preserve roster order, wrap before placing
// each row. In job mode 8675F0 (8679C4..867A03) overwrites g_pLocalPlayer+0x108
// with the job alias (+0x1898), so a suited player's roster entry carries
// their alias and must be excluded too. The 230px bottom reserve is
// independent of the buff preference. Each slot's CIFBuffViewer (5BAD30 /
// 5BA840) is owned by the UI runtime, which keeps its one-second diff state
// across frames.
// The wrap's bottom reserve (73px row + 230px chat zone) never shrinks with
// the viewport, so below ~440 UI pixels the native test wraps after every
// row and the board marches one member per column across the screen. Small
// viewports instead place the board in a bounded left band (partyColumns)
// and let the UI runtime scale the whole group into it - port-only.
const PARTY_ROW_PITCH = 56;
const PARTY_COLUMN_PITCH = 133;
const PARTY_ROW_PITCH_WITH_BUFFS = 73;
/*
================
partyMembers
================
*/
export function partyMembers( game: GameplayState ) {
	const social = game.social, alias = game.job?.alias;
	return (social?.members ?? []).filter(
		m => m.name !== social?.localName && (!alias || m.name !== alias) && (!social?.self || m.id !== social.self)
	);
}
/*
================
partyColumns

The column count whose board best fits a band `bandWidth` x `bandHeight`
without the buff rows: the uniform scale the UI runtime would apply,
min(width fit, height fit), is maximized. Keeping the whole board inside
the band is what stops the roster from walking toward the screen center.
================
*/
export function partyColumns( count: number, bandWidth: number, bandHeight: number ): number {
	if ( count <= 1 ) return 1;
	const widest = Math.max( 1, Math.floor( bandWidth / PARTY_COLUMN_PITCH ) );
	const fit = ( columns: number ) =>
		Math.min(
			bandWidth / (columns * PARTY_COLUMN_PITCH),
			bandHeight / (Math.ceil( count / columns ) * PARTY_ROW_PITCH)
		);
	let best = 1;
	for ( let columns = 2; columns <= Math.min( count, widest ); columns++ ) {
		if ( fit( columns ) > fit( best ) ) best = columns;
	}
	return best;
}
/*
================
partyOverlay

`columns` fixes the grid (port-only, small viewports); without it the native
wrap runs: one column while the next row's bottom stays 230px above the
viewport floor, then the next 133px column, as 5BCBB0 places the board.
================
*/
export function partyOverlay(
	game: GameplayState,
	entities: readonly EntityState[],
	height: number,
	x: number,
	y: number,
	buffs: boolean,
	columns?: number
) {
	const members = partyMembers( game ), pitch = buffs ? PARTY_ROW_PITCH_WITH_BUFFS : PARTY_ROW_PITCH;
	const rowsPerColumn = columns ?
		Math.max( 1, Math.ceil( members.length / columns ) ) :
		Math.max( 1, Math.floor( (height - y - PARTY_ROW_PITCH_WITH_BUFFS - 230) / pitch ) + 1 );
	return members.map( ( member, index ) => {
		const column = Math.floor( index / rowsPerColumn ) * PARTY_COLUMN_PITCH,
			rowY = index % rowsPerColumn * pitch,
			entity = entities.find( e => e.kind === "player" && e.name === member.name ),
			position = [ x + column + 13, y + rowY ] as const;
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
partyLocalPose

Where the distance shade measures from. The local player's live pose is the
one the map follows; the local entity record lags it while the player walks,
so measuring from the entity left every member looking near to a player who
had walked away. The entity stands in until the first pose arrives.
================
*/
export function partyLocalPose( game: GameplayState, entities: readonly EntityState[] ): PartyShadePosition | null {
	return game.pose ?? entities.find( e => e.gid === game.localGid ) ?? null;
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
	const f = Math.fround,
		[ox, oz] = regionOffsetXZ( local.regionId, member.regionId ),
		dx = f( f( member.x - local.x ) + ox ),
		dz = f( f( member.z - local.z ) + oz ),
		dy = f( member.y - local.y );
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

/*
================
partyShadeImages

Every shade image, so the board can request them all while a party exists.
A shade image is drawn only once it has loaded, and asking for one the
first time its distance band is reached left that frame without the tint:
the face flashed back for an instant at each band's first crossing.
================
*/
export function partyShadeImages(): string[] {
	const PARTY_SHADE_MAX = 5;
	const images: string[] = [];
	for ( let shade = 1; shade <= PARTY_SHADE_MAX; shade++ ) images.push( partyShadeImage( shade )! );
	return images;
}
