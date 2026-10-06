/*
===========================================================================
history-view.js - the ledger in plain words: labels, durations, outcomes

history.js reads and correlates records; this module only says them the
way an operator asks about a player ("how long did they play, and why did
they drop?"). Every label keeps its raw code beside it in the page, so the
friendly word never hides the evidence.
===========================================================================
*/

// Event kinds, as the journal records them.
const KIND_LABELS = {
	login_succeeded: "Signed in",
	login_refused: "Sign-in refused",
	connected: "Entered the world",
	world_entered: "Entered the world",
	resumed: "Reconnected",
	detached: "Connection lost",
	ended: "Session ended",
	client_incident: "Client reported a problem",
	log: "Server log"
};

// Outcome categories (the journal's classification, never re-derived here).
const CATEGORY_LABELS = {
	expected: "Normal",
	connection: "Connection",
	software: "Error",
	unknown: "Unclear"
};

// Codes worth a sentence; any other code is shown with its underscores as spaces.
const CODE_LABELS = {
	authenticated: "signed in",
	invalid_credentials: "wrong id or password",
	logout: "logged out",
	connection_closed: "the connection closed without a reason",
	socket_closed: "the connection dropped",
	world_entered: "entered the world",
	packet_application_failed: "a server message could not be applied",
	device_lost: "the graphics device was lost",
	server_shutdown: "the server restarted",
	replaced: "signed in somewhere else",
	idle_timeout: "idle for too long"
};

/*
================
kindLabel
================
*/
export function kindLabel( kind ) {
	return KIND_LABELS[kind] ?? String( kind ?? "event" ).replaceAll( "_", " " );
}

/*
================
categoryLabel
================
*/
export function categoryLabel( category ) {
	return CATEGORY_LABELS[category] ?? "Event";
}

/*
================
codeLabel
================
*/
export function codeLabel( code ) {
	if ( !code ) return "";
	return CODE_LABELS[code] ?? String( code ).replaceAll( "_", " " );
}

/*
================
duration

Seconds as a person says them: "under a minute", "49 min", "1 h 32 min".
================
*/
export function duration( seconds ) {
	const total = Math.max( 0, Math.round( Number( seconds ?? 0 ) ) );
	if ( total < 60 ) return total ? "under a minute" : "none";
	const minutes = Math.round( total / 60 );
	if ( minutes < 60 ) return minutes + " min";
	const hours = Math.floor( minutes / 60 ), rest = minutes % 60;
	return rest ? `${hours} h ${rest} min` : `${hours} h`;
}

/*
================
ago

A timestamp relative to now: "just now", "12 min ago", "3 h ago",
"yesterday, 21:04", then the date. The exact time belongs in a title.
================
*/
export function ago( value, now = Date.now() ) {
	if ( !value ) return "never";
	const seconds = Math.round( (now - value) / 1000 );
	if ( seconds < 45 ) return "just now";
	if ( seconds < 3600 ) return Math.max( 1, Math.round( seconds / 60 ) ) + " min ago";
	if ( seconds < 6 * 3600 ) return Math.round( seconds / 3600 ) + " h ago";
	const at = new Date( value ), today = new Date( now );
	const time = at.toLocaleTimeString( [], { hour: "2-digit", minute: "2-digit" } );
	if ( at.toDateString() === today.toDateString() ) return "today, " + time;
	const yesterday = new Date( now - 86400000 );
	if ( at.toDateString() === yesterday.toDateString() ) return "yesterday, " + time;
	return at.toLocaleDateString( [], { day: "numeric", month: "short" } ) + ", " + time;
}

/*
================
sessionOutcome

How a session ended, in words, with the tone the page colours it by.
================
*/
export function sessionOutcome( row ) {
	if ( row.estimated ) return { label: "Server stopped (time estimated)", tone: "unknown" };
	if ( !row.ended ) {
		return row.attached ?
			{ label: "Playing now", tone: "live" } :
			{ label: "Reconnecting (grace period)", tone: "connection" };
	}
	if ( row.category === "expected" ) {
		return { label: row.code === "logout" ? "Logged out" : "Ended normally", tone: "expected" };
	}
	if ( row.category === "connection" ) return { label: "Lost connection", tone: "connection" };
	if ( row.category === "software" ) {
		return { label: "Ended by an error: " + codeLabel( row.code ), tone: "software" };
	}
	return { label: "Disconnected without a reason", tone: "unknown" };
}

/*
================
shortSession

"#42" for "<boot id>:42": the boot id is the same for every session of one
server process and only matters when following a session across services.
================
*/
export function shortSession( id ) {
	const tail = String( id ?? "" ).split( ":" ).pop();
	return tail ? "#" + tail : "—";
}

/*
================
playerSentence

The player card's headline, from the ledger summary.
================
*/
export function playerSentence( who, summary, now = Date.now() ) {
	const total = duration( summary.connectedSeconds ), today = duration( summary.todaySeconds );
	const week = duration( summary.weekSeconds );
	const played = summary.connectedSeconds ?
		`${who} played ${total} in total: ${today} today and ${week} this week.` :
		`${who} has no recorded time in the world yet.`;
	return `${played} Last signed in ${ago( summary.lastAuthentication, now )}; last in the world ${
		ago( summary.lastSeen, now )
	}.`;
}
