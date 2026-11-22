/*
===========================================================================

filing.ts - the bug report's session filing lane

A composed report handed to the session owner, which posts it with the
token its game session was admitted with (#4109): the login cookie is
only the browser's copy of that session - set once, never renewed, and
erasable by another tab - so a player still in the world was told to
log in. This module owns the lane's shapes and the wait for the
owner's answer in the published session state; the reporter's direct
cookie fetch stays its own lane for the title screens.

===========================================================================
*/

// How long a filing waits on the session owner's answer: past the
// owner's own send cap, so a settled answer always lands first.
const BUG_REPORT_WAIT_MS = 330000;

/*
================
ReportParts

One composed report, ready to send: the fields the direct fetch folds
into its form, handed to the session owner's filing instead.
================
*/
export interface ReportParts {
	readonly description: string;
	readonly meta: string;
	readonly diagnostics: Blob | null;
	readonly media: { readonly field: "clip" | "screenshot"; readonly blob: Blob; } | null;
}

/*
================
ReportAnswer

The Agent's word on a filed report, as the refusal texts read it.
================
*/
export interface ReportAnswer {
	readonly ok: boolean;
	readonly code?: string;
	readonly retryAfter?: number;
}

/*
================
BugReportSessionLane

The session owner's side of a filing, as the runtime wires it: file
sends the parts as one numbered operation, wake resolves on the next
worker message, failed says the host is gone for good.
================
*/
export interface BugReportSessionLane {
	file( parts: ReportParts ): number;
	wake(): Promise<void>;
	failed(): boolean;
}

/*
================
fileReport

Files through the session owner and waits for its answer in the
published session state. NO_SESSION resolves null - the owner holds
no session, so the caller's cookie fetch is the lane - while a wait
that ends answerless fails the send: it never retries down the cookie
lane behind a filing that may already have posted.
================
*/
export async function fileReport(
	lane: BugReportSessionLane,
	parts: ReportParts,
	state: () => { readonly session: import("@/engine/contracts/session").SessionState | null; }
): Promise<ReportAnswer | null> {
	const operationId = lane.file( parts );
	const deadline = performance.now() + BUG_REPORT_WAIT_MS;
	for ( ;; ) {
		const result = state().session?.bugReportResult;
		if ( result && result.operationId === operationId ) {
			return result.code === "NO_SESSION" ? null : result;
		}
		if ( lane.failed() || performance.now() >= deadline ) return { ok: false };
		await lane.wake();
	}
}
