/*
===========================================================================

maintenance-notice.ts - the title screens' maintenance line (port-only)

Port-only, not native (#3925): the login and server-list screens show the
running maintenance the public schedule feed (GET /public/v1/schedule,
#3520) already carries, as one line: when it started, when it is
expected to end, and the release note. The pure model under it; the
fetch state lives in runtime/ui/hud/maintenance-notice-hud.ts, the line
draws in runtime/ui/title/title.ts. Off (the Experimental row), the
title never reads the feed and shows the native screens exactly.

Every time is server time, read in UTC like the calendar's.

===========================================================================
*/

import { clockText, dateText, type ScheduleFeed } from "./event-calendar";

// The schedule's maintenance kind (publicstats.ScheduleKindMaintenance).
const MAINTENANCE_KIND = "maintenance";

/*
================
maintenanceNoticeText

The notice line for the feed's running maintenance entry, or null when
no maintenance runs: "since <start>" while its end is open (the release
tool publishes no end), "<start> to <end>" when one is announced.
================
*/
export function maintenanceNoticeText( feed: ScheduleFeed ): string | null {
	const event = feed.events.find( row => row.kind === MAINTENANCE_KIND && row.running );
	if ( !event ) return null;
	const start = dateText( event.startsAtMs ) + " " + clockText( event.startsAtMs );
	if ( event.endsAtMs === null ) {
		return "Maintenance since " + start + " - a release is in progress, login opens when it ends.";
	}
	const end = dateText( event.endsAtMs ) + " " + clockText( event.endsAtMs );
	return "Maintenance " + start + " to " + end + " - login opens when it ends.";
}
