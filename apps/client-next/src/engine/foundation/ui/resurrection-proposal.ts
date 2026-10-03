/*
===========================================================================

resurrection-proposal.ts - the resurrection question box: lines and position

0x3393 type 4 opens confirm box kind 4 (7644E0 case 3 -> 5C82D0 -> 52F460
case 3); type 8, a revival with an rmut skill, opens the REBIRTH_MUTATION
message box (7644E0 case 7). The question has its own social slot and its
own box, so it also owns its dragged position, separate from the
invitation box beside which it can be open.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import { messageBox } from "./message-box";

/*
================
resurrectionQuestion

The system-text keys of a question's lines, top to bottom. 0x3393 type 4
(7644E0 case 3) opens confirm box kind 4, whose three lines
CIFMessageBox_ConfigureByType (52F460 case 3) fills; type 8 (7644E0 case 7)
is the revival with an rmut skill, a simple message box asking
UIIT_MSG_MSGBOX_ASK_REBIRTH_MUTATION.
================
*/
export function resurrectionQuestion( mutation = false ): readonly string[] {
	return mutation ? [ "UIIT_MSG_MSGBOX_ASK_REBIRTH_MUTATION" ] : [
		"UIIT_MSG_MSGBOX_ASK_SKL_RESURRECTION_0",
		"UIIT_MSG_MSGBOX_ASK_SKL_RESURRECTION_1",
		"UIIT_MSG_MSGBOX_ASK_SKL_RESURRECTION_2"
	];
}

// CIFConfirmBox_ShowByType (5C82D0) centres a 308x148 rect, then 52F460 case 3
// resizes the box to 400x210 from that same origin.
const CONFIRM_CENTRE_WIDTH = 0x134;
const CONFIRM_CENTRE_HEIGHT = 0x94;
const CONFIRM_WIDTH = 0x190;
const CONFIRM_HEIGHT = 0xd2;
// 52F460 case 3: the three lines (306x16) and the Yes/No buttons.
const CONFIRM_LINE_X = 0x2e;
const CONFIRM_LINE_1_Y = 0x3b;
const CONFIRM_LINE_2_Y = 0x4d;
const CONFIRM_LINE_3_Y = 0x71;
const CONFIRM_LINE_WIDTH = 0x132;
const CONFIRM_LINE_HEIGHT = 0x10;
const CONFIRM_ACCEPT_X = 0x7b;
const CONFIRM_REFUSE_X = 0xcb;
const CONFIRM_BUTTON_Y = 0xa8;
const CONFIRM_BUTTON_WIDTH = 76;
const CONFIRM_BUTTON_HEIGHT = 24;
// The third line's font colour, 0xFFFFF1D3 (CGFontTexture_SetFontColor).
const NOTE_COLOR_ARGB = 0xfffff1d3;

/*
================
resurrectionNoteColor

The third line's colour as rgba.
================
*/
export function resurrectionNoteColor(): [number, number, number, number] {
	return [
		(NOTE_COLOR_ARGB >>> 16 & 0xff) / 255,
		(NOTE_COLOR_ARGB >>> 8 & 0xff) / 255,
		(NOTE_COLOR_ARGB & 0xff) / 255,
		(NOTE_COLOR_ARGB >>> 24) / 255
	];
}

/*
================
resurrectionBoxLayout

Confirm box kind 4 at its native geometry. An undragged box keeps the
308x148 centring origin it was created at; a dragged one stays in place.
================
*/
export function resurrectionBoxLayout(
	width: number,
	height: number,
	position: readonly [number, number] | null
): {
	frame: UiRect;
	drag: UiRect;
	title: UiRect;
	background: UiRect;
	lines: readonly UiRect[];
	accept: UiRect;
	refuse: UiRect;
} {
	const origin = position ?? [
		Math.trunc( width / 2 ) - (CONFIRM_CENTRE_WIDTH >> 1),
		Math.trunc( height / 2 ) - (CONFIRM_CENTRE_HEIGHT >> 1)
	] as const;
	const box = messageBox( width, height, CONFIRM_WIDTH, CONFIRM_HEIGHT, origin ), [x, y] = box.frame;
	return {
		...box,
		lines: [ CONFIRM_LINE_1_Y, CONFIRM_LINE_2_Y, CONFIRM_LINE_3_Y ].map( ( dy ): UiRect => [
			x + CONFIRM_LINE_X,
			y + dy,
			CONFIRM_LINE_WIDTH,
			CONFIRM_LINE_HEIGHT
		] ),
		accept: [ x + CONFIRM_ACCEPT_X, y + CONFIRM_BUTTON_Y, CONFIRM_BUTTON_WIDTH, CONFIRM_BUTTON_HEIGHT ],
		refuse: [ x + CONFIRM_REFUSE_X, y + CONFIRM_BUTTON_Y, CONFIRM_BUTTON_WIDTH, CONFIRM_BUTTON_HEIGHT ]
	};
}

/*
================
createResurrectionPrompt

The dragged position of the open question. A question from another caster
(or the next one after it closes) opens centred again; 0 means none is open.
================
*/
export function createResurrectionPrompt() {
	let caster = 0;
	let position: readonly [number, number] | null = null;
	return {
		/*
================
sync
================
		*/
		sync( gid: number ) {
			if ( gid !== caster ) {
				caster = gid;
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
opens

Whether gid's question is new: another caster, or the first since none.
================
		*/
		opens( gid: number ): boolean {
			return gid !== 0 && gid !== caster;
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
