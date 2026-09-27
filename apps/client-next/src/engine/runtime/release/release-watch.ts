/*
===========================================================================

release-watch.ts - notice when a newer client release is live

A running tab keeps its release until reloaded. After a publish it would
keep talking to an updated server with outdated code, so this owner asks
the asset worker for the entry bundle the live page names (fetched
no-cache) and compares it with the one this page loaded. A different entry
means a newer release is live; the platform then offers a refresh. Nothing
reloads on its own: that would drop a player mid-session.

It checks when a trigger arrives (the title screen opens, the connection
drops, the tab becomes visible again), no more than once per
RELEASE_TRIGGER_GAP_MS, and otherwise every RELEASE_CHECK_INTERVAL_MS. The
frame clock drives it; it owns no timer. A page without a hashed entry (a
development server) never checks.

===========================================================================
*/

import type { AssetOwner } from "@/engine/contracts/assets";

export const RELEASE_CHECK_INTERVAL_MS = 10 * 60 * 1000;
export const RELEASE_TRIGGER_GAP_MS = 30 * 1000;
const RELEASE_PAGE_BYTES = 256 * 1024;
// Leave foreground loads a slot; the check is never urgent.
const RELEASE_MIN_FREE_SLOTS = 2;

/*
================
createReleaseWatch

`runningEntry` is the entry bundle this page loaded (null disables the
watch); `pageUrl` is the live page to compare with.
================
*/
export function createReleaseWatch( assets: AssetOwner, pageUrl: string, runningEntry: string | null ) {
	let job: number | null = null, lastCheckMs = -Infinity, newer = false;

	return {
		/*
		================
		step

		Called once per frame with the frame time and whether a trigger fired.
		================
		*/
		step( nowMs: number, triggered: boolean ) {
			if ( runningEntry === null || newer ) return;
			if ( job !== null ) {
				const result = assets.take( job );
				if ( !result ) return;
				job = null;
				// An error (offline, a 5xx) is simply retried at the next check.
				if ( result.kind === "release" && result.entry !== null && result.entry !== runningEntry ) newer = true;
				return;
			}
			const due = nowMs - lastCheckMs >= RELEASE_CHECK_INTERVAL_MS ||
				triggered && nowMs - lastCheckMs >= RELEASE_TRIGGER_GAP_MS;
			if ( !due || assets.available() < RELEASE_MIN_FREE_SLOTS ) return;
			lastCheckMs = nowMs;
			job = assets.request( pageUrl, RELEASE_PAGE_BYTES, "release" );
		},
		/*
		================
		newerAvailable
		================
		*/
		newerAvailable: () => newer,
		/*
		================
		dispose
		================
		*/
		dispose() {
			if ( job !== null ) assets.cancel( job );
			job = null;
		}
	};
}
