/*
===========================================================================

bug-report.ts - the in-game bug reporter's contract with the game UI

The game UI opens the reporter (the /bug chat command) and edits one
preference in its Option window: whether the last-minute replay records.
The reporter owns that preference and its pending draft; the UI only
forwards the window's open, toggle, Default and OK/Apply moments.

===========================================================================
*/

/*
================
BugReportField

One line of client context attached to a report (name, value).
================
*/
export interface BugReportField {
	readonly name: string;
	readonly value: string;
}

/*
================
BugReportControl
================
*/
export interface BugReportControl {
	/** The server has bug reports enabled. */
	reportsEnabled(): boolean;
	/** Opens the report window with `text` prefilled; false when unavailable. */
	open( text: string ): boolean;
	/** The Option window's pending replay checkbox. */
	replayDraft(): boolean;
	toggleReplayDraft(): void;
	/** The Option window opened: the draft restarts from the saved value. */
	resetReplayDraft(): void;
	/** The Option window's Default: the draft becomes the server default. */
	defaultReplayDraft(): void;
	/** OK or Apply: the draft is saved and recording follows it. */
	applyReplayDraft(): void;
	/**
	 * The chat as the UI presents it, every frame. A whisper naming a report
	 * saved on this device (BR-…) offers the player its full .zip.
	 */
	chat( lines: readonly import("./gameplay").ChatLine[] ): void;
}
