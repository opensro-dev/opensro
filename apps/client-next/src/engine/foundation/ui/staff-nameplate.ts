/*
===========================================================================

staff-nameplate.ts - the animated staff nameplate (port-only)

Port-only, not native (owner, 2026-11-28: "a crazy visual effect for GM
nameplates"; 2026-11-28 later: keep the stars, no colour change or
bouncing letters). A staff member's name board gets two layers the
original never draws; the name itself keeps its native look:

	staff-aura   a soft, steady gold halo behind the board that breathes
	staff-spark  small coloured sparks rising off the board and fading

This file only decides who wears it and where the layers stand; the UI
device animates them in its shader from its own clock (device/ui.ts), so
the retained interface never rebuilds per frame for them. On by default
with an Experimental off-switch (staffNameplateFx); off draws the native
board.

Who wears it: a player on the staff roster (staff-roster.ts) whose
nameplate switch is on. The staff member turns it off and on for every
player with the GM command /NAMEPLATE OFF and ON (the server's 0x3FD3);
a name that merely starts with "[GM]" is not staff the server confirms,
so it never shows. A board alias (a job suit's name, #3214) hides who
wears it, so it never shows either.

===========================================================================
*/

import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { staffEntry, type StaffRosterEntry } from "@/engine/foundation/gameplay/staff-roster";

// How far the halo reaches past the board, in UI units.
const AURA_PAD_X = 14;
const AURA_PAD_Y = 8;
// The halo's colour: a warm gold at half strength, so the board stays
// readable over it.
const AURA_COLOR = [ 1, 0.85, 0.45, 0.55 ] as const;
// Sparks across the board: how many, their size and their colour.
const SPARK_COUNT = 9;
const SPARK_SIZE = 3;
const SPARK_COLOR = [ 1, 1, 1, 1 ] as const;
const FULL_UV: UiRect = [ 0, 0, 1, 1 ];

/*
================
isStaffNameplate

Whether a character's board wears the staff effect: a player (or the local
player) under its own name, on the roster with its nameplate switch on.
================
*/
export function isStaffNameplate(
	entity: { readonly kind: string; readonly name?: string; readonly boardName?: string; },
	roster: readonly StaffRosterEntry[] | undefined
): boolean {
	if ( entity.kind !== "player" && entity.kind !== "local-player" ) return false;
	if ( entity.boardName !== undefined || !entity.name ) return false;
	const entry = staffEntry( roster, entity.name );
	return entry !== undefined && !entry.nameplateOff;
}

/*
================
staffNameplateAura

The halo behind a board whose rect is board ([left, top, width, height]).
Drawn before the board's own dark backing, so the backing keeps the text
legible on top of it.
================
*/
export function staffNameplateAura( board: UiRect, clip: UiRect, anchor: number ): UiQuad {
	return {
		characterAnchor: anchor,
		fx: "staff-aura",
		rect: [ board[0] - AURA_PAD_X, board[1] - AURA_PAD_Y, board[2] + 2 * AURA_PAD_X, board[3] + 2 * AURA_PAD_Y ],
		clip,
		uv: FULL_UV,
		texture: "",
		color: AURA_COLOR
	};
}

/*
================
staffNameplateSparks

The sparks over a board, spread evenly along it. Each starts at the
board's middle; the shader lifts and fades it on its own phase (taken
from its position), so no two rise together.
================
*/
export function staffNameplateSparks( board: UiRect, clip: UiRect, anchor: number ): UiQuad[] {
	const sparks: UiQuad[] = [];
	const step = board[2] / SPARK_COUNT, middle = board[1] + board[3] / 2 - SPARK_SIZE / 2;
	for ( let i = 0; i < SPARK_COUNT; i++ ) {
		sparks.push( {
			characterAnchor: anchor,
			fx: "staff-spark",
			rect: [ board[0] + (i + 0.5) * step - SPARK_SIZE / 2, middle, SPARK_SIZE, SPARK_SIZE ],
			clip,
			uv: FULL_UV,
			texture: "",
			color: SPARK_COLOR
		} );
	}
	return sparks;
}
