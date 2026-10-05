/*
===========================================================================

chat-time.ts - the local clock time shown when the mouse rests on a chat line

The wire carries no send time, so a line is stamped when this client receives
it (ChatLine.sentAt). Showing that stamp in the viewer's own time zone is the
closest honest answer to "when was this sent".

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
chatLineTime

Clock time down to the second. A line without a stamp has no time to show.
================
*/
export function chatLineTime( sentAt: number | undefined, format: ChatTimeFormat = {} ): string {
	if ( sentAt === undefined || !Number.isFinite( sentAt ) ) return "";
	return new Intl.DateTimeFormat( format.locale, {
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
		timeZone: format.timeZone
	} ).format( sentAt );
}
