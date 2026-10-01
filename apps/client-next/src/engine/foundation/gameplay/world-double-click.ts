/*
===========================================================================

world-double-click.ts - a drifted second click on the same monster still attacks

DELIBERATE DEVIATION FROM NATIVE. DO NOT REVERT WITHOUT THE PROJECT OWNER.

Native attacks only on WM_LBUTTONDBLCLK: CGInterface_OnWorldMessage (67CCA0)
routes it to CGInterface_InteractWithEntity, while a single click
(CGInterface_OnWorldClick 698740 -> CGInterface_SelectTargetEntity 67AA60)
only selects, even an already selected monster. Windows reports a double
click only when the second press lands inside a 4x4 pixel box around the
first, and the browser applies the same rule. A player who double-clicks a
monster right after moving the mouse drifts a few pixels; retail and the
port then silently select instead of attacking. Players reported this as
"auto attack does not resume" (2026-10-02, recorded and confirmed).

The owner chose to rescue that case: a second press on the same monster
within the double-click time promotes to the double-click action. A real
double click is unchanged; the browser's own dblclick that follows a
promoted pair is absorbed so the attack is issued once.

===========================================================================
*/

// Windows' default GetDoubleClickTime; the browser exposes no OS value.
export const WORLD_DOUBLE_CLICK_MS = 500;

/*
================
WorldDoubleClick
================
*/
export interface WorldDoubleClick {
	/** A single press picked `gid`; true when it completes a drifted pair. */
	press( gid: number | null, now: number ): boolean;
	/** The browser's dblclick picked `gid`; false when a promotion already acted. */
	double( gid: number | null, now: number ): boolean;
}

/*
================
createWorldDoubleClick

Owned by the runtime's world click. Only monsters reach it: a double click
does nothing else, so other entities never promote.
================
*/
export function createWorldDoubleClick(): WorldDoubleClick {
	let last: { gid: number; at: number; } | null = null,
		promoted: { gid: number; at: number; } | null = null;
	return {
		press( gid, now ) {
			const pair = gid !== null && last !== null && last.gid === gid && now - last.at <= WORLD_DOUBLE_CLICK_MS;
			// A completed pair starts over, as the OS counts a third press as a
			// new first click.
			last = pair || gid === null ? null : { gid, at: now };
			promoted = pair ? { gid: gid!, at: now } : promoted;
			return pair;
		},
		double( gid, now ) {
			const absorbed = gid !== null && promoted !== null && promoted.gid === gid &&
				now - promoted.at <= WORLD_DOUBLE_CLICK_MS;
			promoted = null;
			last = null;
			return !absorbed;
		}
	};
}
