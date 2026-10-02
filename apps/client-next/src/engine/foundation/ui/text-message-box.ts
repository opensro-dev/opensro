/*
===========================================================================

text-message-box.ts - geometry of a plain text message box with Yes and No

The CIFMessageBox frame (525D60) sized around lines of body text, with the
centred Yes/No pair the drop warning and the resurrection question use.
This module only places rects; the caller owns text and input.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import { messageBox } from "./message-box";

// 6888C0 seeds 360x151; 52BCF0 grows it by the body plus (60,122).
const MIN_WIDTH = 360;
const MIN_HEIGHT = 151;
const BODY_MARGIN_X = 60;
const BODY_MARGIN_Y = 122;
// 52E720 places the body at (30,65); 52A0F0 spaces its lines 23 apart.
const BODY_X = 30;
const BODY_Y = 65;
const LINE_HEIGHT = 23;
// The centred Yes/No pair: 76 wide, 37 above the bottom.
const BUTTON_WIDTH = 76;
const BUTTON_HEIGHT = 24;
const BUTTON_BOTTOM = 37;
const YES_OFFSET = -81;
const NO_OFFSET = 5;

/*
================
textMessageBoxLayout

lineWidths are the measured widths of the body lines, in order.
================
*/
export function textMessageBoxLayout(
	width: number,
	height: number,
	lineWidths: readonly number[],
	position: readonly [number, number] | null = null
) {
	const w = Math.max( MIN_WIDTH, ...lineWidths.map( n => Math.ceil( n ) + BODY_MARGIN_X ) ),
		h = Math.max( MIN_HEIGHT, lineWidths.length * LINE_HEIGHT + BODY_MARGIN_Y ),
		box = messageBox( width, height, w, h, position ),
		[x, y] = box.frame;
	const lines = lineWidths.map( ( _, i ): UiRect => [
		x + BODY_X,
		y + BODY_Y + i * LINE_HEIGHT,
		w - BODY_MARGIN_X,
		LINE_HEIGHT
	] );
	const accept: UiRect = [ x + (w >> 1) + YES_OFFSET, y + h - BUTTON_BOTTOM, BUTTON_WIDTH, BUTTON_HEIGHT ],
		refuse: UiRect = [ x + (w >> 1) + NO_OFFSET, y + h - BUTTON_BOTTOM, BUTTON_WIDTH, BUTTON_HEIGHT ];
	return { ...box, lines, accept, refuse };
}
