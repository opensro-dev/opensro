/*
===========================================================================

board.js - the Work board page: one snapshot, re-read on its own

Reads /api/board (board.mjs validates the collector's snapshot) and
paints it through board-view.js. Polls every POLL_MS while the tab is
visible, so the owner can leave it open; the freshness pill keeps
counting between polls and turns amber when the collector falls behind.
Every link is built from the configured repository and a number; every
snapshot string becomes a text node.

===========================================================================
*/
import { requestJSON } from "./request.js";
import {
	agentTone,
	ciTone,
	freshness,
	issueURL,
	priorityTone,
	prTone,
	pullURL,
	since,
	splitPrs,
	stepTone,
	sortThreads,
	summary,
	threadAction
} from "./board-view.js";

const POLL_MS = 30000;
const FRESH_REPAINT_MS = 5000;
const byID = id => document.getElementById( id );
const state = { body: null, answeredAt: 0, timer: 0 };

/*
================
el

One element with attributes and children; strings become text nodes, so
nothing from the snapshot is ever parsed as markup.
================
*/
function el( tag, attrs = {}, ...children ) {
	const node = document.createElement( tag );
	for ( const [name, value] of Object.entries( attrs ) ) {
		if ( value === false || value === undefined || value === null ) continue;
		if ( name === "class" ) node.className = value;
		else node.setAttribute( name, value === true ? "" : value );
	}
	for ( const child of children.flat() ) {
		if ( child === null || child === undefined || child === false ) continue;
		node.append( typeof child === "object" ? child : String( child ) );
	}
	return node;
}

/*
================
pill
================
*/
function pill( text, tone ) {
	return el( "span", { class: "pill " + tone }, text );
}

/*
================
link

A GitHub link this page built from the repository and a number.
================
*/
function link( href, text ) {
	return el( "a", { href, rel: "noopener noreferrer", target: "_blank" }, text );
}

/*
================
numberLinks

Comma-separated PR or issue links, or a word when there are none.
================
*/
function numberLinks( numbers, url, none = "none" ) {
	if ( !numbers.length ) return none;
	return numbers.flatMap( ( n, i ) => [ i ? ", " : "", link( url( state.body.repo, n ), "#" + n ) ] );
}

/*
================
cell
================
*/
function cell( ...children ) {
	return el( "td", {}, ...children );
}

/*
================
fill

One table body, or a single row saying it is empty.
================
*/
function fill( id, rows, columns, empty ) {
	byID( id ).replaceChildren(
		...(rows.length ? rows : [ el( "tr", {}, el( "td", { colspan: columns, class: "board-empty" }, empty ) ) ])
	);
}

/*
================
paintSummary
================
*/
function paintSummary( body ) {
	byID( "board-summary" ).replaceChildren(
		...summary( body ).map( tile =>
			el(
				"div",
				{ class: "board-tile " + tile.tone },
				el( "strong", {}, tile.value ),
				el( "span", {}, tile.label )
			)
		)
	);
}

/*
================
paintRelease
================
*/
function paintRelease( body ) {
	const r = body.release;
	byID( "release-freeze" ).textContent = r?.freeze ? "freeze " + r.freeze.slice( 0, 9 ) : "";
	if ( !r ) {
		byID( "release-body" ).replaceChildren( el( "p", { class: "board-empty" }, "No release in flight." ) );
		return;
	}
	byID( "release-body" ).replaceChildren(
		el( "h3", { class: "board-release" }, r.name ),
		r.why ? el( "p", { class: "board-help" }, r.why ) : null,
		el( "p", {}, el( "strong", {}, "Contains: " ), numberLinks( r.contains, pullURL ) ),
		el(
			"ol",
			{ class: "board-steps" },
			...r.steps.map( s => el( "li", {}, pill( s.state, stepTone( s.state ) ), " ", s.step ) )
		),
		r.next ?
			el(
				"p",
				{ class: "board-help" },
				"After this: " + r.next.name + ". Candidates: ",
				numberLinks( r.next.candidates, pullURL )
			) :
			null,
		r.rules.length ? el( "ul", { class: "board-rules" }, ...r.rules.map( rule => el( "li", {}, rule ) ) ) : null
	);
}

/*
================
paintNeeds
================
*/
function paintNeeds( body ) {
	byID( "needs-count" ).textContent = String( body.needs.length );
	byID( "needs-body" ).replaceChildren(
		...(body.needs.length ?
			body.needs.map( n => el( "li", {}, el( "strong", {}, n.what ), el( "span", {}, n.detail ) ) ) :
			[ el( "li", { class: "board-empty" }, "Nothing waits on the owner." ) ])
	);
}

/*
================
paintAgents
================
*/
function paintAgents( body, now ) {
	byID( "agents-count" ).textContent = String( body.agents.length );
	fill(
		"agents-body",
		body.agents.map( a =>
			el(
				"tr",
				{},
				cell( el( "strong", {}, a.name ) ),
				cell( a.task ),
				cell( a.tree ? el( "code", {}, a.tree ) : "" ),
				cell( pill( a.state || "no status yet", a.state ? agentTone( a.state ) : "warn" ) ),
				cell( a.blockedOn ),
				el( "td", { class: "num" }, since( a.at, now ) )
			)
		),
		6,
		"No agent has reported."
	);
}

/*
================
paintPrs
================
*/
function paintPrs( body, now ) {
	const { agent, fork } = splitPrs( body.prs );
	byID( "prs-count" ).textContent = String( agent.length );
	fill(
		"prs-body",
		agent.map( pr =>
			el(
				"tr",
				{},
				cell( link( pullURL( body.repo, pr.number ), "#" + pr.number ) ),
				cell( pr.title ),
				el( "td", { class: "num" }, pr.approvals + " / " + pr.needed ),
				cell( pill( pr.ci, ciTone( pr.ci ) ) ),
				cell( pill( pr.blocker || (pr.ready ? "ready to merge" : "open"), prTone( pr ) ) ),
				el( "td", { class: "num" }, since( pr.createdAt, now ) )
			)
		),
		6,
		"No agent PR is open."
	);
	byID( "forks-count" ).textContent = String( fork.length );
	fill(
		"forks-body",
		fork.map( pr =>
			el(
				"tr",
				{},
				cell( link( pullURL( body.repo, pr.number ), "#" + pr.number ) ),
				cell( pr.author ),
				cell( pr.title ),
				el( "td", { class: "num" }, since( pr.createdAt, now ) )
			)
		),
		4,
		"No fork PR is open."
	);
}

/*
================
paintThreads
================
*/
function paintThreads( body ) {
	byID( "threads-count" ).textContent = String( body.threads.length );
	fill(
		"threads-body",
		sortThreads( body.threads ).map( t => {
			const action = threadAction( t.action );
			return el(
				"tr",
				{},
				cell( el( "code", {}, t.thread.slice( -6 ) ) ),
				cell( t.title ),
				cell( pill( t.priority || "?", priorityTone( t.priority ) ) ),
				cell( pill( action.text, action.tone ) ),
				cell( numberLinks( t.issues, issueURL ) )
			);
		} ),
		5,
		"No bug-report thread is open."
	);
}

/*
================
paintIssues
================
*/
function paintIssues( body ) {
	const i = body.issues;
	byID( "issues-body" ).replaceChildren(
		el( "p", {}, el( "strong", {}, i.open + " open" ), ", " + i.withoutPr + " without an open PR." ),
		i.labels.length ?
			el(
				"ul",
				{ class: "board-labels" },
				...i.labels.map( l => el( "li", {}, l.name, " ", el( "span", { class: "tag" }, l.count ) ) )
			) :
			null,
		body.merged.length ?
			el(
				"p",
				{ class: "board-help" },
				"Recently merged: ",
				numberLinks( body.merged.map( m => m.number ), pullURL )
			) :
			null
	);
}

/*
================
paintFresh

The freshness pill alone, repainted between polls.
================
*/
function paintFresh() {
	if ( !state.body ) return;
	const fresh = freshness( state.body, state.answeredAt, Date.now() );
	const node = byID( "board-fresh" );
	node.className = "pill " + fresh.tone;
	node.textContent = fresh.text;
}

/*
================
paint
================
*/
function paint() {
	const body = state.body, now = Date.now();
	paintSummary( body );
	paintRelease( body );
	paintNeeds( body );
	paintAgents( body, now );
	paintPrs( body, now );
	paintThreads( body );
	paintIssues( body );
	paintFresh();
}

/*
================
load

One read. A failed read keeps the last good board on screen and says why
above it, so a blip never blanks the page.
================
*/
async function load() {
	const status = byID( "board-status" );
	byID( "board-refresh" ).disabled = true;
	try {
		state.body = await requestJSON( "/api/board" );
		state.answeredAt = Date.now();
		status.textContent = "";
		paint();
	} catch ( error ) {
		status.textContent = error.message + (state.body ? " Showing the last board read." : "");
		if ( !state.body ) byID( "board-fresh" ).textContent = "No board";
	}
	byID( "board-refresh" ).disabled = false;
}

/*
================
schedule

The next poll, only while the tab is visible; a hidden tab reads again
the moment it is shown.
================
*/
function schedule() {
	clearTimeout( state.timer );
	if ( document.hidden ) return;
	state.timer = setTimeout( async () => {
		await load();
		schedule();
	}, POLL_MS );
}

byID( "board-refresh" ).addEventListener( "click", async () => {
	await load();
	schedule();
} );
document.addEventListener( "visibilitychange", async () => {
	if ( document.hidden ) {
		clearTimeout( state.timer );
		return;
	}
	await load();
	schedule();
} );
setInterval( paintFresh, FRESH_REPAINT_MS );
await load();
schedule();
