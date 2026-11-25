/*
===========================================================================

party-summon.ts - the party summon window and invitation box geometry

The v1.696 party summon window (interface 214) is an authored CIF
window in the later client; this client holds no authored layout for
it, so the window is drawn programmatically in the message-box frame
family, sized from its roster. The native row shape is the window's:
one row per member, the name, and the eligibility line (Possible /
Impossible, native bytes 2 / 3). The header lines are the native ones:
Number of Players Allowed: 1-%d, Current summoned number: %d, Time
Limit for Entry, Entry Remaining.

The member's invitation box is the shared CIFMessageBox proposal
geometry (partyProposalLayout) with the summon question
UIIT_STT_PARTY_SUMMON_CONFIRM_BOX naming the leader.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import { partyProposalLayout } from "./party-proposal";

// Window geometry (port-drawn; the authored native layout is unread).
const WINDOW_WIDTH = 320;
const WINDOW_PAD = 12;
const TITLE_HEIGHT = 20;
const LINE_HEIGHT = 14;
const ROW_HEIGHT = 16;
const BUTTON_HEIGHT = 24;

// The native status-cell bytes (social-roster.ts summon rows).
export const PARTY_SUMMON_POSSIBLE = 2;
export const PARTY_SUMMON_IMPOSSIBLE = 3;

/*
================
partySummonQuestion

The box's one question line and its leader-name argument.
================
*/
export function partySummonQuestion( name: string ): { key: string; arg: string; } {
	return { key: "UIIT_STT_PARTY_SUMMON_CONFIRM_BOX", arg: name };
}

/*
================
partySummonBoxLayout

The shared proposal geometry with the question line only, as the
instance invitation box lays itself out.
================
*/
export function partySummonBoxLayout(
	width: number,
	height: number,
	position: readonly [number, number] | null
): {
	frame: UiRect;
	drag: UiRect;
	title: UiRect;
	background: UiRect;
	question: UiRect;
	accept: UiRect;
	refuse: UiRect;
} {
	const box = partyProposalLayout( width, height, undefined, position );
	return {
		frame: box.frame,
		drag: box.drag,
		title: box.title,
		background: box.background,
		question: box.question,
		accept: box.accept,
		refuse: box.refuse
	};
}

/*
================
partySummonWindowLayout

The window's rects for a roster of rowCount members: the frame, the
title drag band, the four header lines, the two column captions, one
row per member (name left, eligibility right), and the Summon and
Close buttons along the bottom.
================
*/
export function partySummonWindowLayout(
	width: number,
	height: number,
	rowCount: number,
	position: readonly [number, number] | null
): {
	frame: UiRect;
	drag: UiRect;
	title: UiRect;
	headers: readonly UiRect[];
	columns: readonly [UiRect, UiRect];
	rows: readonly { name: UiRect; state: UiRect; }[];
	summon: UiRect;
	close: UiRect;
} {
	const frameHeight = TITLE_HEIGHT + 4 * LINE_HEIGHT + LINE_HEIGHT + rowCount * ROW_HEIGHT + BUTTON_HEIGHT +
			3 * WINDOW_PAD,
		x = position ? position[0] : Math.round( (width - WINDOW_WIDTH) / 2 ),
		y = position ? position[1] : Math.round( (height - frameHeight) / 2 ),
		frame: UiRect = [ x, y, WINDOW_WIDTH, frameHeight ],
		at = ( dx: number, dy: number, w: number, h: number ): UiRect => [ x + dx, y + dy, w, h ],
		inner = WINDOW_WIDTH - 2 * WINDOW_PAD,
		rows: { name: UiRect; state: UiRect; }[] = [];
	let cursor = TITLE_HEIGHT + WINDOW_PAD;
	const headers: UiRect[] = [];
	for ( let i = 0; i < 4; i++ ) {
		headers.push( at( WINDOW_PAD, cursor, inner, LINE_HEIGHT ) );
		cursor += LINE_HEIGHT;
	}
	const columns: readonly [UiRect, UiRect] = [
		at( WINDOW_PAD, cursor, inner - 100, LINE_HEIGHT ),
		at( WINDOW_PAD + inner - 100, cursor, 100, LINE_HEIGHT )
	];
	cursor += LINE_HEIGHT;
	for ( let i = 0; i < rowCount; i++ ) {
		rows.push( {
			name: at( WINDOW_PAD, cursor, inner - 100, ROW_HEIGHT ),
			state: at( WINDOW_PAD + inner - 100, cursor, 100, ROW_HEIGHT )
		} );
		cursor += ROW_HEIGHT;
	}
	cursor += WINDOW_PAD;
	return {
		frame,
		drag: at( 0, 0, WINDOW_WIDTH, TITLE_HEIGHT ),
		title: at( 0, 2, WINDOW_WIDTH, 16 ),
		headers,
		columns,
		rows,
		summon: at( WINDOW_PAD, cursor, 96, BUTTON_HEIGHT ),
		close: at( WINDOW_WIDTH - WINDOW_PAD - 76, cursor, 76, BUTTON_HEIGHT )
	};
}

/*
================
createPartySummonPrompt

The dragged position of the open invitation box. An invitation from
another leader (or the next one after it closes) opens centred again;
0 means none is open.
================
*/
export function createPartySummonPrompt() {
	let leader = 0;
	let position: readonly [number, number] | null = null;
	return {
		/*
================
sync
================
		*/
		sync( gid: number ) {
			if ( gid !== leader ) {
				leader = gid;
				position = null;
			}
		},
		/*
================
position
================
		*/
		position(): readonly [number, number] | null {
			return position;
		},
		/*
================
place
================
		*/
		place( frame: readonly [number, number] ) {
			position = frame;
		}
	};
}
