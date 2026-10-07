/*
===========================================================================

chat-time.ts - the local clock time shown when the mouse rests on a chat line

A live line carries no send time on the wire, so it is stamped when this
client receives it; a line replayed from the server's public history keeps
the server's send time (ChatLine.sentAt). Showing that stamp in the viewer's
own time zone is the closest honest answer to "when was this sent". A line from an earlier day
than today (in that same zone) also names its date.

===========================================================================
*/

/*
================
ChatTimeFormat

Both fields default to the viewer's own locale and time zone. They exist so a
test can pin the output.
================
*/
export interface ChatTimeFormat {
	readonly locale?: string;
	readonly timeZone?: string;
}

/*
================
calendarDay

The calendar date of `at` in the given time zone, comparable as a string.
================
*/
function calendarDay( at: number, timeZone: string | undefined ) {
	return new Intl.DateTimeFormat( "en-CA", { year: "numeric", month: "2-digit", day: "2-digit", timeZone } ).format(
		at
	);
}

/*
================
chatLineTime

Clock time down to the second, with the date in front when the line is from
an earlier day. A line without a stamp has no time to show.
================
*/
export function chatLineTime( sentAt: number | undefined, format: ChatTimeFormat = {} ): string {
	if ( sentAt === undefined || !Number.isFinite( sentAt ) ) return "";
	const today = calendarDay( sentAt, format.timeZone ) === calendarDay( Date.now(), format.timeZone );
	return new Intl.DateTimeFormat( format.locale, {
		...(today ? {} : { year: "numeric", month: "2-digit", day: "2-digit" }),
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
		timeZone: format.timeZone
	} ).format( sentAt );
}
