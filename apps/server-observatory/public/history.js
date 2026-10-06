/*
===========================================================================
history.js - operator searches, correlation, pagination and evidence export

All event text is rendered as text, including client-supplied failure evidence.
===========================================================================
*/
import {
	ago,
	categoryLabel,
	codeLabel,
	duration,
	kindLabel,
	playerSentence,
	sessionOutcome,
	shortSession
} from "./history-view.js";

const form = document.querySelector( "#filters" ), status = document.querySelector( "#status" );
let sources = [], current = null, cursor = 0, generation = 0, busy = false, roster = [];
/*
================
element
================
*/
function element( tag, text = "" ) {
	const node = document.createElement( tag );
	node.textContent = text;
	return node;
}
/*
================
date
================
*/
function date( value ) {
	return value ? new Date( value ).toLocaleString() : "—";
}
/*
================
query
================
*/
function query() {
	const params = new URLSearchParams();
	for ( const [key, value] of new FormData( form ) ) {
		if ( value ) {
			params.set( key, key === "from" || key === "to" ? String( new Date( value ).getTime() ) : value );
		}
	}
	return params;
}
/*
================
table
================
*/
function table( target, headings, rows ) {
	const root = document.querySelector( target );
	root.replaceChildren();
	if ( !rows.length ) {
		root.append( element( "p", "No matching records." ) );
		return;
	}
	const grid = element( "table" ), head = element( "tr" );
	for ( const heading of headings ) head.append( element( "th", heading ) );
	grid.append( head );
	for ( const values of rows ) {
		const row = element( "tr" );
		for ( const value of values ) {
			const cell = element( "td" );
			cell.append( value instanceof Node ? value : document.createTextNode( String( value ) ) );
			row.append( cell );
		}
		grid.append( row );
	}
	root.append( grid );
}
/*
================
sessionLink
================
*/
function sessionLink( id, source ) {
	const link = element( "button", source ? "Follow in " + source.name : shortSession( id ) );
	link.type = "button";
	link.className = source ? "button" : "session-id";
	link.title = source ? "Show this session's records in " + source.name : "Session " + id + ": show its records";
	link.addEventListener( "click", () => {
		const account = form.elements.account.value;
		form.reset();
		form.elements.account.value = account;
		form.elements.source.value = source?.id ?? current.source;
		form.elements.session.value = id;
		search();
	} );
	return link;
}
/*
================
renderEvents
================
*/
function renderEvents( rows, append ) {
	const root = document.querySelector( "#events" );
	if ( !append ) root.replaceChildren();
	for ( const event of rows ) {
		const details = element( "details" ), heading = element( "summary", "" );
		heading.className = [ "software", "expected", "connection", "unknown" ].includes( event.category ) ?
			event.category :
			"";
		// One part per fact, so a row reads at a glance: when, what kind of
		// outcome, which event, whose, and the code or message.
		for (
			const [part, text, title] of [
				[ "event-time", ago( event.at ), date( event.at ) ],
				[ "event-category", categoryLabel( event.category ), event.category ],
				[ "event-kind", kindLabel( event.kind ), event.kind ],
				[ "event-who", event.character || event.account || "server", event.account ],
				[ "event-code", codeLabel( event.code ) || event.message || "", event.code || event.message ]
			]
		) {
			const span = element( "span", text );
			span.className = part;
			if ( title ) span.title = title;
			heading.append( span );
		}
		details.append( heading, element( "pre", JSON.stringify( event, null, 2 ) ) );
		if ( event.session ) { for ( const source of sources ) details.append( sessionLink( event.session, source ) ); }
		const nearby = element( "button", "Logs ±30 seconds" );
		nearby.className = "button";
		nearby.addEventListener( "click", () => {
			form.elements.incident.value = "";
			form.elements.kind.value = "log";
			form.elements.category.value = "";
			const params = query();
			params.set( "from", String( event.at - 30000 ) );
			params.set( "to", String( event.at + 30000 ) );
			search( false, params );
		} );
		details.append( nearby );
		root.append( details );
	}
	if ( !rows.length && !append ) {
		root.append(
			element(
				"p",
				"No matching events. This is not proof that no failure occurred; check collection health and the other service."
			)
		);
	}
}
/*
================
renderPlayer

The player card: a sentence for the account or character searched, then
the same numbers as plaques. Without an identity there is no personal time
to show, so the card asks for one.
================
*/
function renderPlayer( summary, sessions ) {
	const root = document.querySelector( "#summary" );
	root.replaceChildren();
	const who = form.elements.character.value.trim() || form.elements.account.value.trim();
	if ( !who ) {
		root.append( element( "p", "Pick a player from the roster to see their play time and sessions." ) );
		root.className = "ledger-summary prompt";
		return;
	}
	root.className = "ledger-summary";
	const card = element( "div" );
	card.className = "player-card";
	const name = element( "h3", who );
	if ( sessions.some( row => !row.ended && row.attached ) ) {
		const badge = element( "span", "Playing now" );
		badge.className = "live-badge";
		name.append( badge );
	}
	card.append( name, element( "p", playerSentence( who, summary ) ) );
	const drops = sessions.filter( row => row.ended && row.category !== "expected" ).length;
	if ( drops ) {
		const warn = element( "p", `${drops} of the sessions shown ended without a normal logout.` );
		warn.className = "player-warning";
		card.append( warn );
	}
	root.append( card );
	const plaques = element( "div" );
	plaques.className = "plaques";
	for (
		const [label, value, title] of [
			[ "Played in total", duration( summary.connectedSeconds ) ],
			[ "Today", duration( summary.todaySeconds ), "UTC day" ],
			[ "This week", duration( summary.weekSeconds ), "UTC week" ],
			[ "Last signed in", ago( summary.lastAuthentication ), date( summary.lastAuthentication ) ],
			[ "Last in the world", ago( summary.lastSeen ), date( summary.lastSeen ) ]
		]
	) {
		const plaque = element( "p", label );
		if ( title ) plaque.title = title;
		plaque.append( element( "strong", value ) );
		plaques.append( plaque );
	}
	root.append( plaques );
}
/*
================
renderSessions

One card per session: who, when, how long (against the longest shown) and
how it ended, in words. The session number opens its records.
================
*/
function renderSessions( rows ) {
	const root = document.querySelector( "#sessions" );
	root.replaceChildren();
	if ( !rows.length ) {
		root.append( element( "p", "No sessions match these filters." ) );
		return;
	}
	const longest = Math.max( 1, ...rows.map( row => Number( row.connectedSeconds ?? 0 ) ) );
	for ( const row of rows ) {
		const outcome = sessionOutcome( row ), card = element( "article" );
		card.className = "session-card tone-" + outcome.tone;
		const avatar = element( "span", (row.character || row.account || "?").slice( 0, 1 ).toUpperCase() );
		avatar.className = "session-avatar";
		const body = element( "div" );
		body.className = "session-body";
		const title = element( "div" );
		title.className = "session-title";
		title.append(
			element( "strong", row.character || "—" ),
			element( "span", row.account ? "account " + row.account : "" )
		);
		const meta = element(
			"div",
			"Started " + ago( row.started ) + " · played " + duration( row.connectedSeconds ) +
				(row.ended ? " · ended " + ago( row.ended ) : "")
		);
		meta.className = "session-meta";
		meta.title = "Started " + date( row.started ) + " · last observed " + date( row.seen ) + " · ended " +
			date( row.ended );
		const bar = element( "div" ), fill = element( "span" );
		bar.className = "session-bar";
		fill.style.width = Math.max( 2, Math.round( 100 * Number( row.connectedSeconds ?? 0 ) / longest ) ) + "%";
		bar.append( fill );
		body.append( title, meta, bar );
		const end = element( "div" );
		end.className = "session-end";
		const chip = element( "span", outcome.label );
		chip.className = "outcome";
		if ( row.ended ) chip.title = (row.category ?? "") + " · " + (row.code ?? "");
		end.append( chip, sessionLink( row.id ) );
		card.append( avatar, body, end );
		root.append( card );
	}
}
/*
================
failureCell

A failure group's name in words, with its raw code under it.
================
*/
function failureCell( row ) {
	const cell = element( "div" );
	cell.append( element( "strong", codeLabel( row.code ) || row.message ) );
	if ( row.code ) {
		const raw = element( "small", row.code );
		raw.className = "raw-code";
		cell.append( raw );
	}
	return cell;
}
/*
================
choosePlayer

Fills the identity filters from a roster row, or clears them for everyone,
and searches: the operator picks a player, never types an id.
================
*/
function choosePlayer( row ) {
	form.elements.account.value = row?.account ?? "";
	form.elements.character.value = row?.character ?? "";
	search();
}
/*
================
rosterRow

One player in the roster: initial, name, account, when last seen, and a
count of sessions that ended badly. A null row is "Everyone".
================
*/
function rosterRow( row, selected ) {
	const button = element( "button" ), name = row ? row.character || row.account : "Everyone";
	button.type = "button";
	button.className = "roster-row" + (row ? (row.online ? " online" : "") : " everyone") +
		(selected ? " selected" : "");
	if ( selected ) button.setAttribute( "aria-current", "true" );
	const avatar = element( "span", row ? name.slice( 0, 1 ).toUpperCase() : "全" );
	avatar.className = "roster-avatar";
	const label = element( "span" );
	label.className = "roster-name";
	label.append(
		element( "strong", name ),
		element(
			"small",
			!row ? "every player's records" : row.character ? "account " + row.account : "signed in to the Agent"
		)
	);
	const side = element( "span" );
	side.className = "roster-side";
	if ( row ) {
		side.append( element( "span", row.online ? "playing now" : ago( row.lastSeen ) ) );
		if ( row.problems ) {
			const drops = element( "span", String( row.problems ) );
			drops.className = "roster-drops";
			drops.title = row.problems + " sessions ended without a normal logout";
			side.append( drops );
		}
		button.title = row.sessions ?
			`${row.sessions} sessions · played ${duration( row.connectedSeconds )} · last seen ${
				date( row.lastSeen )
			}` :
			"Last signed in " + date( row.lastSeen );
	}
	button.append( avatar, label, side );
	button.addEventListener( "click", () => choosePlayer( row ) );
	return button;
}
/*
================
renderRoster

The roster of known players for the selected service, narrowed by the
roster's own name filter (a local filter, not a ledger search).
================
*/
function renderRoster() {
	const root = document.querySelector( "#roster" );
	const needle = document.querySelector( "#roster-filter" ).value.trim().toLowerCase();
	const account = form.elements.account.value.trim().toLowerCase();
	const character = form.elements.character.value.trim().toLowerCase();
	document.querySelector( "#roster-count" ).textContent = roster.length +
		(roster.length === 1 ? " player" : " players");
	root.replaceChildren( rosterRow( null, !account && !character ) );
	const rows = roster.filter(
		row => !needle || ((row.character ?? "") + " " + row.account).toLowerCase().includes( needle )
	);
	for ( const row of rows ) {
		const chosen = row.account.toLowerCase() === account && (row.character ?? "").toLowerCase() === character;
		root.append( rosterRow( row, chosen ) );
	}
	if ( !rows.length ) {
		root.append(
			element( "p", roster.length ? "No player matches that name." : "This service has no players yet." )
		);
	}
}
/*
================
render
================
*/
function render( data, append ) {
	const health = data.health;
	document.querySelector( "#health" ).textContent =
		`Collection: ${health.service} · build ${health.build} · ${health.pending} queued · ${health.dropped} dropped · ${health.failed} write failures. ${
			health.prunedBefore ? "Some raw events at or before " + date( health.prunedBefore ) + " were pruned. " : ""
		}${health.lastError || ""}`;
	if ( !append ) {
		roster = data.players ?? [];
		renderRoster();
	}
	renderPlayer( data.summary, data.sessions );
	renderSessions( data.sessions );
	table(
		"#groups",
		[ "What went wrong", "Times", "Players", "First", "Last", "Builds" ],
		data.groups.map(
			row => [
				failureCell( row ),
				row.count,
				row.players,
				date( row.first ),
				date( row.last ),
				Object.entries( row.buildCounts ?? {} ).map( ( [build, count] ) => `${build}: ${count}` ).join( ", " )
			]
		)
	);
	renderEvents( data.events, append );
	cursor = data.next;
	document.querySelector( "#older" ).hidden = !cursor;
}
/*
================
search
================
*/
async function search( older = false, supplied ) {
	const epoch = ++generation, params = supplied ?? (older ? new URLSearchParams( current.query ) : query());
	if ( older ) params.set( "before", String( cursor ) );
	busy = true;
	status.textContent = "Reading durable history…";
	try {
		const response = await fetch( "/api/history?" + params, { signal: AbortSignal.timeout( 15000 ) } );
		const data = await response.json();
		if ( epoch !== generation ) return;
		if ( !response.ok ) throw Error( data.error ?? "History request failed" );
		current = {
			source: params.get( "source" ),
			query: params.toString(),
			...data,
			events: older ? [ ...current.events, ...data.events ] : data.events
		};
		render( data, older );
		status.textContent = `${date( Date.now() )} · ${current.events.length} events loaded · ${
			data.groups.reduce( ( sum, row ) => sum + row.count, 0 )
		} software/error records in displayed groups. Unknown disconnects remain unclassified.`;
	} catch ( error ) {
		if ( epoch === generation ) status.textContent = error.message + " Previous results, if any, are stale.";
	} finally {
		if ( epoch === generation ) busy = false;
	}
}
form.addEventListener( "submit", event => {
	event.preventDefault();
	search();
} );
document.querySelector( "#older" ).addEventListener( "click", () => search( true ) );
document.querySelector( "#roster-filter" ).addEventListener( "input", renderRoster );
form.elements.source.addEventListener( "change", () => search() );
document.querySelector( "#export" ).addEventListener( "click", () => {
	if ( !current ) return;
	const url = URL.createObjectURL( new Blob( [ JSON.stringify( current, null, 2 ) ], { type: "application/json" } ) );
	const link = element( "a" );
	link.href = url;
	link.download = "session-evidence.json";
	link.click();
	URL.revokeObjectURL( url );
} );
setInterval( () => {
	if ( !busy && document.querySelector( "#auto" ).checked ) search();
}, 15000 );
try {
	const response = await fetch( "/api/console" );
	if ( !response.ok ) throw Error( "Operator access refused" );
	sources = (await response.json()).historySources;
	for ( const source of sources ) {
		const option = element( "option", source.name );
		option.value = source.id;
		form.elements.source.append( option );
	}
	await search();
} catch ( error ) {
	status.textContent = error.message;
}
