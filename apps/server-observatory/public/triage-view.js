/*
===========================================================================

triage-view.js - what the Bug reports page shows, as plain functions

No DOM here, so the tests run on node. The page (triage.js) renders what
these return.

===========================================================================
*/

const PRIORITY_ORDER = [ "Critical", "High", "Medium", "Low", "" ];
const STATE_ORDER = [ "pending", "rejected", "auto", "approved" ];
export const FILTERS = [
	{ key: "pending", label: "flagged for you" },
	{ key: "auto", label: "posting on their own" },
	{ key: "approved", label: "approved" },
	{ key: "rejected", label: "rejected, to redraft" },
	{ key: "all", label: "all drafts" }
];

/*
================
bucket

Which filter a thread falls in: an unflagged undecided reply is the
coordinator's call and posts on its own (owner 11-03); only flagged ones
wait for a decision here.
================
*/
export function bucket( t ) {
	return t.decision.state === "pending" && !t.ask ? "auto" : t.decision.state;
}

/*
================
counts
================
*/
export function counts( threads ) {
	const out = { pending: 0, auto: 0, approved: 0, rejected: 0, all: threads.length };
	for ( const t of threads ) out[bucket( t )] += 1;
	return out;
}

/*
================
visible

The drafts one filter, search text and priority leave, most urgent first:
undecided before rejected before approved, then by priority.
================
*/
export function visible( threads, filter, search = "", priority = "" ) {
	const needle = search.trim().toLowerCase();
	return threads
		.filter( t => filter === "all" || bucket( t ) === filter )
		.filter( t => !priority || t.priority === priority )
		.filter( t =>
			!needle || t.title.toLowerCase().includes( needle ) || t.thread.includes( needle ) ||
			t.issues.some( n => ("#" + n).includes( needle ) )
		)
		.sort( ( a, b ) =>
			STATE_ORDER.indexOf( bucket( a ) ) - STATE_ORDER.indexOf( bucket( b ) ) ||
			PRIORITY_ORDER.indexOf( a.priority ) - PRIORITY_ORDER.indexOf( b.priority ) ||
			a.thread.localeCompare( b.thread )
		);
}

/*
================
priorityTone
================
*/
export function priorityTone( priority ) {
	return priority === "Critical" || priority === "High" ? "bad" : priority === "Medium" ? "warn" : "idle";
}

/*
================
actionText

What posting the reply does to the thread: live means a live release
already carries the fix, fixed that the release in flight will.
================
*/
const ACTION_TEXT = Object.freeze( {
	live: "fixed and live: reply and close",
	fixed: "fixed by the release in flight",
	close: "reply and close",
	reply: "reply only"
} );

export function actionText( action ) {
	return ACTION_TEXT[action] ?? ACTION_TEXT.reply;
}

/*
================
postText

What approving does with this thread, or what it already did.
================
*/
export function postText( t ) {
	if ( t.posted ) return { text: t.posted.closed ? "posted, tagged Closed, archived" : "reply posted", tone: "ok" };
	if ( !t.reply ) return { text: "no public reply written yet: cannot post", tone: "bad" };
	if ( t.decision.state === "pending" && !t.ask ) {
		return { text: "not flagged: posts on its own within a minute", tone: "idle" };
	}
	if ( t.decision.state === "rejected" ) return { text: "rejected: waits for a rewrite", tone: "bad" };
	if ( t.decision.state === "approved" ) {
		return { text: "approved: the watcher posts it within a minute", tone: "warn" };
	}
	return {
		text: "Flagged: " + t.ask + ". " + (t.closes ?
			"Approve posts this reply, tags the thread Closed and archives it" :
			"Approve posts this reply"),
		tone: "warn"
	};
}

/*
================
decisionText
================
*/
export function decisionText( d ) {
	if ( d.state === "pending" ) return { text: "to decide", tone: "warn" };
	const who = d.by ? ` by ${d.by}` : "";
	return d.state === "approved" ?
		{ text: "approved" + who, tone: "ok" } :
		{ text: "rejected" + who, tone: "bad" };
}

/*
================
readFilter / writeFilter

The filter lives in the URL so a reload or a shared link keeps it.
================
*/
export function readFilter( search ) {
	const key = new URLSearchParams( search ).get( "show" );
	return FILTERS.some( f => f.key === key ) ? key : "pending";
}

export function writeFilter( search, key ) {
	const params = new URLSearchParams( search );
	if ( key === "pending" ) params.delete( "show" );
	else params.set( "show", key );
	const text = params.toString();
	return text ? "?" + text : "";
}
