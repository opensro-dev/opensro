/*
===========================================================================

entry-visibility.ts - how long the page was hidden during a world entry

A client that stops answering the scene-loading pings after receiving
the whole bootstrap is either a backgrounded tab (its worker throttled)
or a stalled device; the entry-rejection incident (#2558) says which
only if something counted the hidden time. The frontend owns the
entry window and samples document.hidden on its step clock - steps
stop while the page is hidden, so a span closes at the first visible
step, within a frame of the truth. Pure accounting: the caller passes
every timestamp and the hidden flag, so tests drive it directly.

===========================================================================
*/

export interface EntryVisibility {
	// Hidden milliseconds from closed spans in this window.
	hiddenMs: number;
	// When the current hidden span began, or -1 while the page is visible.
	hiddenSince: number;
	// An entry window is open.
	tracking: boolean;
}

/*
================
createEntryVisibility
================
*/
export function createEntryVisibility(): EntryVisibility {
	return { hiddenMs: 0, hiddenSince: -1, tracking: false };
}

/*
================
entryVisibilityBegin

Opens a window for one entry attempt: totals reset, and an entry that
starts hidden counts from its first moment.
================
*/
export function entryVisibilityBegin( visibility: EntryVisibility, now: number, hidden: boolean ): void {
	visibility.hiddenMs = 0;
	visibility.hiddenSince = hidden ? now : -1;
	visibility.tracking = true;
}

/*
================
entryVisibilitySetHidden

The page hid or showed at now. Repeated samples of the same state are
free; a flip while no window is open belongs to no entry.
================
*/
export function entryVisibilitySetHidden( visibility: EntryVisibility, hidden: boolean, now: number ): void {
	if ( !visibility.tracking ) return;
	if ( hidden && visibility.hiddenSince < 0 ) visibility.hiddenSince = now;
	if ( !hidden && visibility.hiddenSince >= 0 ) {
		visibility.hiddenMs += Math.max( 0, now - visibility.hiddenSince );
		visibility.hiddenSince = -1;
	}
}

/*
================
entryVisibilityTotal

Hidden milliseconds so far, an ongoing hidden span included.
================
*/
export function entryVisibilityTotal( visibility: EntryVisibility, now: number ): number {
	return visibility.hiddenMs + (visibility.hiddenSince >= 0 ? Math.max( 0, now - visibility.hiddenSince ) : 0);
}

/*
================
entryVisibilityEnd

Closes the window and returns its total; the tracker forgets the
window, so the next attempt starts from zero.
================
*/
export function entryVisibilityEnd( visibility: EntryVisibility, now: number ): number {
	const total = entryVisibilityTotal( visibility, now );
	visibility.hiddenMs = 0;
	visibility.hiddenSince = -1;
	visibility.tracking = false;
	return total;
}
