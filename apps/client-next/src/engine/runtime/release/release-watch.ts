/*
===========================================================================

release-watch.ts - notice when a newer client release is live

A running tab keeps its release until reloaded. After a publish it would
keep talking to an updated server with outdated code, so this owner asks
the asset worker for the entry bundle the live page names (fetched
no-cache) and compares it with the one this page loaded. A different entry
means a newer release is live; the platform then offers a refresh. Nothing
reloads on its own: that would drop a player mid-session. The check also
compares the live packs manifest with this page's: a data publish keeps
the entry bundle, so only the manifest shows it moved (#3104).

It checks when a trigger arrives (the title screen opens, the connection
drops, the tab becomes visible again), no more than once per
RELEASE_TRIGGER_GAP_MS, and otherwise every RELEASE_CHECK_INTERVAL_MS. A
file the page's manifest lists coming back 404 (the asset owner's
takeReleaseStale) is the publish itself showing: that checks at once, with
a single free slot, no more than once per RELEASE_STALE_GAP_MS. The check
consumes it, so a 404 that is not a release (a lost file) costs one check. The
frame clock drives it; it owns no timer. A page without a hashed entry (a
development server) never checks.

A server that refuses this page's protocol (426) while the live page still
names this page's entry is a coordinated publish between its two switches:
the server is new, the page is not yet. A reload then would load the same
outdated page and be refused again, so the watch says "updating" (no
Refresh button) and checks every RELEASE_OUTDATED_GAP_MS. Once the live
page moves, the refused page reloads on its own: its session is already
refused, so the reload drops nothing (releaseNotice).

===========================================================================
*/

import type { AssetOwner } from "@/engine/contracts/assets";
import type { ReleaseNotice } from "@/engine/contracts/runtime";

export const RELEASE_CHECK_INTERVAL_MS = 10 * 60 * 1000;
export const RELEASE_TRIGGER_GAP_MS = 30 * 1000;
export const RELEASE_STALE_GAP_MS = 5 * 1000;
export const RELEASE_OUTDATED_GAP_MS = 3 * 1000;
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
	let job: number | null = null, lastCheckMs = -Infinity, newer = false, stalePending = false;

	return {
		/*
		================
		step

		Called once per frame with the frame time, whether a trigger fired and
		whether the server refuses this page's release (426).
		================
		*/
		step( nowMs: number, triggered: boolean, outdated = false ) {
			if ( runningEntry === null || newer ) return;
			if ( assets.takeReleaseStale?.() ) stalePending = true;
			if ( job !== null ) {
				const result = assets.take( job );
				if ( !result ) return;
				job = null;
				// An error (offline, a 5xx) is simply retried at the next check.
				if (
					result.kind === "release" &&
					(result.entry !== null && result.entry !== runningEntry || result.packsChanged === true)
				) newer = true;
				return;
			}
			// A stale file means the scene in hand cannot finish, and a refused
			// release cannot play at all: check now, with a single free slot.
			const urgent = outdated && nowMs - lastCheckMs >= RELEASE_OUTDATED_GAP_MS ||
				stalePending && nowMs - lastCheckMs >= RELEASE_STALE_GAP_MS;
			const due = nowMs - lastCheckMs >= RELEASE_CHECK_INTERVAL_MS ||
				triggered && nowMs - lastCheckMs >= RELEASE_TRIGGER_GAP_MS;
			if ( urgent ? assets.available() < 1 : !due || assets.available() < RELEASE_MIN_FREE_SLOTS ) return;
			lastCheckMs = nowMs;
			stalePending = false;
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

/*
================
releaseNotice

What the page shows for a release skew. `newer` is the watch's verdict (the
live page names another entry); `outdated` is the server's 426.

- newer, refused: "reload". Nothing is left to keep, so the page reloads.
- newer, still served: "newer". The player is mid-session; Refresh is
  their click.
- refused, no newer page yet: "updating". The publish has switched the
  server and not yet the page; a reload would only be refused again.
================
*/
export function releaseNotice( newer: boolean, outdated: boolean ): ReleaseNotice {
	if ( newer ) return outdated ? "reload" : "newer";
	return outdated ? "updating" : "none";
}
