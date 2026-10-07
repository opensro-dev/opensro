/*
===========================================================================

bug-report.ts - the in-game bug reporter's contract with the game UI

The game UI opens the reporter (the /bug chat command) and hands it the
chat. Recording a clip is the reporter's own Record/Stop control beside
its launcher; the game's Option window has no reporter setting.

===========================================================================
*/

/*
================
ReplayState

What the report window says when no recording is attached: the player
did not record, or this browser cannot.
================
*/
export type ReplayState = "idle" | "unsupported";

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
	/**
	 * Opens the report window with `text` prefilled. "off": the server has
	 * reporting switched off. "unavailable": its settings could not be read
	 * yet (the Agent was restarting); the reporter is asking again.
	 */
	open( text: string ): "opened" | "off" | "unavailable";
	/**
	 * Every UI frame: a running recording's timer and its cap. chat() runs
	 * only when the HUD is assembled, which is not every frame.
	 */
	recordingFrame(): void;
	/**
	 * The chat as the UI presents it, every frame. A whisper naming a report
	 * saved on this device (BR-…) offers the player its full .zip.
	 */
	chat( lines: readonly import("./gameplay").ChatLine[] ): void;
}
