/*
===========================================================================

gallery.ts - the UI gallery page: pick a window, see it live

Lists every journey from scenarios.ts by family, loads the selected one in
frame.html (production UI over fake local data) and forwards its action
buttons. The frame reports its status about twice a second. The chosen
window lives in the URL hash, so a link opens the same window.

===========================================================================
*/
import { journeys, type Journey } from "./scenarios";

const list = document.querySelector<HTMLElement>( "#list" )!,
	search = document.querySelector<HTMLInputElement>( "#search" )!,
	frame = document.querySelector<HTMLIFrameElement>( "#frame" )!,
	viewport = document.querySelector<HTMLSelectElement>( "#viewport" )!,
	actions = document.querySelector<HTMLElement>( "#actions" )!,
	status = document.querySelector<HTMLElement>( "#status" )!;
let selected: Journey = journeys.find( row => row.id === decodeURIComponent( location.hash.slice( 1 ) ) ) ??
	journeys[0]!;

/*
================
renderList

Families in first-seen order; the search matches title, id and family.
================
*/
function renderList() {
	const term = search.value.trim().toLowerCase();
	const rows = journeys.filter( row =>
		!term || (row.title + " " + row.id + " " + row.family).toLowerCase().includes( term )
	);
	list.replaceChildren();
	for ( const family of [ ...new Set( rows.map( row => row.family ) ) ] ) {
		const heading = document.createElement( "h3" );
		heading.textContent = family;
		list.append( heading );
		for ( const row of rows.filter( entry => entry.family === family ) ) {
			const button = document.createElement( "button" );
			button.textContent = row.title;
			button.title = row.id;
			button.setAttribute( "aria-current", String( row === selected ) );
			button.addEventListener( "click", () => select( row ) );
			list.append( button );
		}
	}
}

/*
================
setStatus
================
*/
function setStatus( state: "loading" | "ready" | "error", text: string ) {
	status.dataset.state = state;
	status.textContent = text;
}

/*
================
select

Loads the journey into a fresh frame; Reset calls it again.
================
*/
function select( row: Journey ) {
	selected = row;
	history.replaceState( null, "", "#" + encodeURIComponent( row.id ) );
	document.querySelector( "#title" )!.textContent = row.title;
	document.querySelector( "#description" )!.textContent = row.description + " " + row.instructions;
	document.querySelector( "#limits" )!.textContent = row.limits;
	actions.replaceChildren(
		...row.actions.map( ( [action, label] ) => {
			const button = document.createElement( "button" );
			button.textContent = label;
			button.addEventListener(
				"click",
				() => frame.contentWindow?.postMessage( { type: "gallery-action", action }, location.origin )
			);
			return button;
		} )
	);
	setStatus( "loading", "Loading..." );
	frame.src = "./frame.html?scenario=" + encodeURIComponent( row.id );
	renderList();
}

/*
================
resize
================
*/
function resize() {
	const [width, height] = viewport.value.split( "x" ).map( Number );
	frame.width = String( width );
	frame.height = String( height );
}

window.addEventListener( "message", message => {
	if ( message.origin !== location.origin || message.source !== frame.contentWindow ) return;
	if ( message.data?.type !== "gallery-status" ) return;
	const report = message.data.report as { id: string; ready: boolean; error?: string | null; };
	if ( report.id !== selected.id ) return;
	if ( report.error ) setStatus( "error", report.error );
	else setStatus( report.ready ? "ready" : "loading", report.ready ? "Ready" : "Loading..." );
} );
// A pasted link or Back changes only the hash; follow it to that window.
window.addEventListener( "hashchange", () => {
	const row = journeys.find( entry => entry.id === decodeURIComponent( location.hash.slice( 1 ) ) );
	if ( row && row !== selected ) select( row );
} );
search.addEventListener( "input", renderList );
viewport.addEventListener( "change", resize );
document.querySelector( "#reset" )!.addEventListener( "click", () => select( selected ) );
resize();
select( selected );
