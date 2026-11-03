/*
===========================================================================

triage.js - the Bug reports page: read, filter, approve, reject, copy

Reads /api/triage, shows each thread's drafted reply as text, and sends a
decision to /api/triage/decision. Every render builds elements with
textContent; draft text never becomes markup. The filter lives in the URL.

===========================================================================
*/
import { postJSON, requestJSON } from "/request.js";
import {
	actionText,
	counts,
	decisionText,
	FILTERS,
	priorityTone,
	readFilter,
	visible,
	writeFilter
} from "/triage-view.js";

const REPO_ISSUES = "https://github.com/opensro-dev/opensro/issues/";
const POLL_MS = 30000;
const COPIED_MS = 2000;

const state = { threads: [], filter: readFilter( location.search ), canDecide: false, busy: new Set() };

function el( tag, props = {}, ...children ) {
	const node = document.createElement( tag );
	for ( const [key, value] of Object.entries( props ) ) {
		if ( key === "class" ) node.className = value;
		else if ( key === "text" ) node.textContent = value;
		else node.setAttribute( key, value );
	}
	for ( const child of children ) if ( child ) node.append( child );
	return node;
}

function pill( text, tone ) {
	return el( "span", { class: `pill ${tone}`, text } );
}

function status( text ) {
	document.getElementById( "triage-status" ).textContent = text;
}

/*
================
renderSummary

One tile per filter; the active one is pressed.
================
*/
function renderSummary() {
	const n = counts( state.threads );
	const box = document.getElementById( "triage-summary" );
	box.replaceChildren( ...FILTERS.map( f => {
		const tile = el(
			"button",
			{
				class: "board-tile triage-tile" + (f.key === "pending" && n.pending ? " warn" : ""),
				type: "button",
				"aria-pressed": String( state.filter === f.key )
			},
			el( "strong", { text: String( n[f.key] ) } ),
			el( "span", { text: f.label } )
		);
		tile.addEventListener( "click", () => {
			state.filter = f.key;
			history.replaceState( null, "", location.pathname + writeFilter( location.search, f.key ) );
			render();
		} );
		return tile;
	} ) );
}

/*
================
draftItem
================
*/
function draftItem( t ) {
	const d = decisionText( t.decision );
	const head = el(
		"div",
		{ class: "triage-head" },
		el( "h3", { text: t.title || "(untitled thread)" } ),
		el(
			"div",
			{ class: "triage-marks" },
			pill( t.priority || "no priority", priorityTone( t.priority ) ),
			pill( actionText( t.action ), "idle" ),
			pill( d.text, d.tone )
		)
	);
	const meta = el(
		"p",
		{ class: "triage-meta" },
		el( "span", { text: "Thread " } ),
		el( "code", { text: t.thread } )
	);
	if ( t.issues.length ) {
		meta.append( el( "span", { text: " · issues " } ) );
		t.issues.forEach( ( n, i ) => {
			if ( i ) meta.append( ", " );
			meta.append( el( "a", { href: REPO_ISSUES + n, target: "_blank", rel: "noopener", text: "#" + n } ) );
		} );
	}
	const draft = el( "pre", { class: "triage-draft", text: t.draft || "(no draft text)" } );
	const item = el( "li", { class: `panel triage-item ${t.decision.state}` }, head, meta, draft );
	if ( t.decision.note ) item.append( el( "p", { class: "triage-note", text: "Note: " + t.decision.note } ) );
	item.append( actions( t ) );
	return item;
}

/*
================
actions

Approve, reject (with the reason box) and copy. Decisions are offered only
to staff who may make them; copy is for approved drafts.
================
*/
function actions( t ) {
	const bar = el( "div", { class: "triage-actions" } );
	if ( t.decision.state === "approved" ) {
		const copy = el( "button", { class: "button", type: "button", text: "Copy reply" } );
		copy.addEventListener( "click", async () => {
			try {
				await navigator.clipboard.writeText( t.draft );
				copy.textContent = "Copied";
				setTimeout( () => (copy.textContent = "Copy reply"), COPIED_MS );
			} catch {
				status( "The browser refused the clipboard; select the text and copy it by hand." );
			}
		} );
		bar.append( copy );
	}
	if ( !state.canDecide ) return bar;
	const busy = state.busy.has( t.thread );
	const note = el( "input", {
		class: "triage-reason",
		type: "text",
		maxlength: "500",
		placeholder: "Why reject? (needed to reject; optional to approve)",
		"aria-label": "Reason for " + t.thread
	} );
	const approve = el( "button", {
		class: "button primary",
		type: "button",
		text: t.decision.state === "approved" ? "Approved" : "Approve"
	} );
	const reject = el( "button", { class: "button", type: "button", text: "Reject" } );
	if ( busy ) { for ( const b of [ approve, reject ] ) b.setAttribute( "disabled", "" ); }
	approve.addEventListener( "click", () => decide( t.thread, "approve", note.value ) );
	reject.addEventListener( "click", () => {
		if ( !note.value.trim() ) {
			note.focus();
			status( "Write why the draft is rejected first, so it can be redrafted." );
			return;
		}
		decide( t.thread, "reject", note.value );
	} );
	bar.append( note, approve, reject );
	return bar;
}

/*
================
render
================
*/
function render() {
	renderSummary();
	const search = document.getElementById( "triage-search" ).value;
	const priority = document.getElementById( "triage-priority" ).value;
	const rows = visible( state.threads, state.filter, search, priority );
	document.getElementById( "drafts-count" ).textContent = String( rows.length );
	const list = document.getElementById( "drafts-body" );
	list.replaceChildren(
		...(rows.length ? rows.map( draftItem ) : [ el( "li", { class: "board-empty", text: "Nothing here." } ) ])
	);
}

/*
================
decide
================
*/
async function decide( thread, decision, note ) {
	state.busy.add( thread );
	render();
	try {
		const body = await postJSON( "/api/triage/decision", { thread, decision, note } );
		state.threads = body.threads;
		status(
			decision === "approve" ?
				"Approved. Copy the reply from the Approved tab to post it." :
				"Rejected; it goes back for a redraft."
		);
	} catch ( err ) {
		status( "Not saved: " + err.message );
	} finally {
		state.busy.delete( thread );
		render();
	}
}

/*
================
load
================
*/
async function load() {
	const fresh = document.getElementById( "triage-fresh" );
	try {
		const body = await requestJSON( "/api/triage" );
		state.threads = body.threads;
		const age = Math.max( 0, Math.round( (Date.now() - Date.parse( body.generatedAt )) / 60000 ) );
		fresh.textContent = age < 1 ? "Drafts current" : `Drafts from ${age} min ago`;
		fresh.className = "pill " + (age > 10 ? "warn" : "ok");
		status( "" );
	} catch ( err ) {
		fresh.textContent = "Unavailable";
		fresh.className = "pill bad";
		status( err.message );
	}
	render();
}

async function start() {
	try {
		state.canDecide = Boolean( (await requestJSON( "/api/console" )).canDecide );
	} catch {
		state.canDecide = false;
	}
	for ( const id of [ "triage-search", "triage-priority" ] ) {
		document.getElementById( id ).addEventListener( "input", render );
	}
	document.getElementById( "triage-refresh" ).addEventListener( "click", load );
	await load();
	setInterval( () => {
		if ( document.visibilityState === "visible" && !state.busy.size ) load();
	}, POLL_MS );
}

start();
