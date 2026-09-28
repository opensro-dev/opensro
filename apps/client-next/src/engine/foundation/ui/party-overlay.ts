/*
===========================================================================

party-overlay.ts - party-overlay.ts - roster-owned party overlay layout and portrait identities

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
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
		return {
			member,
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
