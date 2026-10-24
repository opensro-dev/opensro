/*
===========================================================================

flagwar-timer-plate.ts - the Capture the Flag mission timer's digits

The native plate is CIFTimerWnd (ginterface GDR_TIMER_WND, id 410, at
(624, 7) in the 1600x1200 authored space), a CGInterface child created
from the ginterface layout. Its loader (CIFTimerWnd_LoadTimerLayout
0x5181B0) binds the iftimerwnd statics by id: 1/2 the minute digits,
3 the colon, 4/5 the second digits, and hides the countdown statics
(11/12/13 - a second mode of the same window the flag war never arms).
The flag war handler (0x4B2530) arms the control through
CIFEventTimer_SetDurationAndDelay (0x517F30) and CIFEventTimer_Start
(0x518660); CIFEventTimer_UpdateDigitTextures (0x5182C0) then paints
the remaining time as MM:SS, one "interface\timer\timer_<d>.ddj"
texture per digit, and under a minute left paints all five statics
red and sounds the countdown cue. This module owns the reading: the
digits, their textures and the urgent tint. The runtime draws them
with the authored layout rects; the worker owns the clock.

===========================================================================
*/

// Below this remainder the native painter turns the digits red
// (0x5182C0 compares the remainder against 0xEA60 milliseconds).
export const FLAG_WAR_TIMER_URGENT_MS = 0xea60;

// The native urgent tint: the painter writes 0xFFFF0000 into each
// static's color field (+0x120).
export const flagWarTimerUrgentTint = [ 1, 0, 0, 1 ] as const;

/*
================
FlagWarTimerReading

The four plate digits and the urgent flag for one remainder.
================
*/
export interface FlagWarTimerReading {
	readonly minTens: number;
	readonly minOnes: number;
	readonly secTens: number;
	readonly secOnes: number;
	readonly urgent: boolean;
}

/*
================
flagWarTimerReading

The painter's arithmetic: the remainder in whole seconds, minutes
and seconds out of that, one digit per static. A remainder of zero
restores the normal color (the painter's expiry branch), so urgent
needs time actually left; minutes clamp at 99 because the plate has
two minute statics (the native match duration is 20 minutes).
================
*/
export function flagWarTimerReading( remainingMs: number ): FlagWarTimerReading {
	const remaining = Math.max( 0, Math.floor( remainingMs ) ),
		totalSeconds = Math.floor( remaining / 1000 ),
		minutes = Math.min( 99, Math.floor( totalSeconds / 60 ) ),
		seconds = totalSeconds % 60;
	return {
		minTens: Math.floor( minutes / 10 ),
		minOnes: minutes % 10,
		secTens: Math.floor( seconds / 10 ),
		secOnes: seconds % 10,
		urgent: remaining > 0 && remaining < FLAG_WAR_TIMER_URGENT_MS
	};
}

/*
================
flagWarTimerDigitTexture

One digit's texture. The painter snprintf's the DDJ path
"interface/timer/timer_%d.ddj" per digit; the published form is the
extracted PNG.
================
*/
export function flagWarTimerDigitTexture( digit: number ): string {
	return "/assets/images/Media_extracted/interface/timer/timer_" + digit + ".png";
}
