/*
===========================================================================
history.js - operator searches, correlation, pagination and evidence export

All event text is rendered as text, including client-supplied failure evidence.
===========================================================================
*/
const form = document.querySelector( "#filters" ), status = document.querySelector( "#status" );
let sources = [], current = null, cursor = 0, generation = 0, busy = false;
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
minutes
================
*/
function minutes( seconds ) {
	return (Number( seconds ?? 0 ) / 60).toFixed( 1 ) + " min";
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
	const link = element( "button", source ? "Follow in " + source.name : id );
	link.type = "button";
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
		const details = element( "details" ),
			heading = element(
				"summary",
				`${date( event.at )} · ${event.category} · ${event.kind} · ${
					event.account || event.character || "service"
				} · ${event.code || event.message}`
			);
		heading.className = [ "software", "expected", "connection", "unknown" ].includes( event.category ) ?
			event.category :
			"";
		details.append( heading, element( "pre", JSON.stringify( event, null, 2 ) ) );
		if ( event.session ) { for ( const source of sources ) details.append( sessionLink( event.session, source ) ); }
		const nearby = element( "button", "Logs ±30 seconds" );
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
render
================
*/
function render( data, append ) {
	const health = data.health;
	document.querySelector( "#health" ).textContent =
		`Collection: ${health.service} · build ${health.build} · ${health.pending} queued · ${health.dropped} dropped · ${health.failed} write failures. ${
			health.prunedBefore ? "Some raw events at or before " + date( health.prunedBefore ) + " were pruned. " : ""
		}${health.lastError || ""}`;
	const root = document.querySelector( "#summary" );
	root.replaceChildren();
	for (
		const [label, value] of [
			[ "Connected total", minutes( data.summary.connectedSeconds ) ],
			[ "Today (UTC)", minutes( data.summary.todaySeconds ) ],
			[ "This week (UTC)", minutes( data.summary.weekSeconds ) ],
			[ "Last authentication (Agent)", date( data.summary.lastAuthentication ) ],
			[ "Last world connection", date( data.summary.lastSeen ) ]
		]
	) {
		const card = element( "p", label );
		card.append( element( "strong", value ) );
		root.append( card );
	}
	table(
		"#sessions",
		[ "Session", "Account / character", "Started", "Last observed", "Ended", "Connected", "Outcome" ],
		data.sessions.map(
			row => [
				sessionLink( row.id ),
				row.account + " / " + row.character,
				date( row.started ),
				date( row.seen ),
				date( row.ended ),
				minutes( row.connectedSeconds ),
				row.estimated ?
					"Estimated · process interrupted" :
					row.ended ?
					row.category + " · " + row.code :
					row.attached ?
					"Connected" :
					"Reconnect grace"
			]
		)
	);
	table(
		"#groups",
		[ "Failure", "Count", "Known accounts", "First", "Last", "Builds" ],
		data.groups.map(
			row => [
				row.code || row.message,
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
