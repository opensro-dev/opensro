/*
===========================================================================

mouse-modes.ts - which mouse button orbits the camera and which uses the
mouse quickslot, per input option

The input option's mouse mode is CGInterface +0x4F1 (OptionManager getter
67B000, default 0 in CIFOption_Game_OnDefault 5CD900). CGInterface_OnWorldMessage
(67CCA0) reads it:

	mode 0: the wheel button (WM_MBUTTONDOWN) uses quickslot 0, the right
	        button orbits the camera while held
	mode 1: the right button (WM_RBUTTONDOWN) uses quickslot 0, the wheel
	        button orbits the camera while held

The options window lists the modes in that order (5CC9D4 passes
UIIT_STT_USE_WHEEL_TO_USE_SKILL as item 0, UIIT_STT_USE_WHEEL_TO_CHANGE_SIGHT
as item 1). Quickslot 0 is the underbar's "M" slot (UIIT_STT_MOUSE_RIGHT_BUTTON).

===========================================================================
*/

// MouseEvent.button values.
const BUTTON_WHEEL = 1, BUTTON_RIGHT = 2;
// PointerEvent.buttons bits.
const BUTTONS_RIGHT = 2, BUTTONS_WHEEL = 4;

/*
================
cameraDragButtons

The PointerEvent.buttons bit that orbits the camera while held.
================
*/
export function cameraDragButtons( mode: 0 | 1 ): number {
	return mode === 0 ? BUTTONS_RIGHT : BUTTONS_WHEEL;
}

/*
================
shortcutButton

The MouseEvent.button that uses quickslot 0.
================
*/
export function shortcutButton( mode: 0 | 1 ): number {
	return mode === 0 ? BUTTON_WHEEL : BUTTON_RIGHT;
}

/*
================
mouseModeLabel

The options window's text for a mode.
================
*/
export function mouseModeLabel( mode: 0 | 1 ): string {
	return mode === 0 ? "UIIT_STT_USE_WHEEL_TO_USE_SKILL" : "UIIT_STT_USE_WHEEL_TO_CHANGE_SIGHT";
}
