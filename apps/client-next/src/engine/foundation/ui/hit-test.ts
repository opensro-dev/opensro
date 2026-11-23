/*
===========================================================================

hit-test.ts - which control a point in UI coordinates lands on

One owner for the point-in-control rule the UI's own dispatch uses (a drop
target, a release over the pressed control). The platform places each
control's input element on the same area (controlHitRect), so the browser's
hit test and this one agree.

===========================================================================
*/
import type { UiControl, UiRect } from "@/engine/contracts/ui";

/*
================
containsPoint
================
*/
export function containsPoint( rect: UiRect, x: number, y: number ): boolean {
	return x >= rect[0] && y >= rect[1] && x < rect[0] + rect[2] && y < rect[1] + rect[3];
}

/*
================
controlHitRect

The area that takes the pointer: the control's hitRect when it has one
(larger than what it draws, as a bag cell's share of the gap around it),
else its rect.
================
*/
export function controlHitRect( control: UiControl ): UiRect {
	return control.hitRect ?? control.rect;
}

// How far from a range thumb's centre a press still counts as a press
// on the thumb, in UI units (the caller converts the press from CSS
// pixels, so the grab scales with the UI): the thumb the canvas draws
// is 16 units wide, plus a few units of slack for the finger or cursor.
const RANGE_THUMB_GRAB_UNITS = 14;

/*
================
rangePressOnThumb

Whether a press at offsetX (UI units) in a range width units wide lands on the
thumb itself, given the element's current value between min and max.
A thumbDragOnly range refuses any other press: a browser moves the
value to wherever the groove was pressed, which silently re-picks the
choice the groove belongs to (#4121).
================
*/
export function rangePressOnThumb(
	min: number,
	max: number,
	value: number,
	width: number,
	offsetX: number
): boolean {
	const fraction = max > min ? Math.min( 1, Math.max( 0, (value - min) / (max - min) ) ) : 0;
	return Math.abs( offsetX - fraction * width ) <= RANGE_THUMB_GRAB_UNITS;
}

/*
================
topmostControlAt

The last control whose hit area holds the point. Disabled controls still
occlude controls below them; dispatch owns eligibility.
================
*/
export function topmostControlAt( controls: readonly UiControl[], x: number, y: number ): UiControl | undefined {
	for ( let i = controls.length - 1; i >= 0; i-- ) {
		if ( containsPoint( controlHitRect( controls[i]! ), x, y ) ) return controls[i];
	}
	return undefined;
}
