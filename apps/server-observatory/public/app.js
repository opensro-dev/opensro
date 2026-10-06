/*
===========================================================================
app.js - dashboard navigation, sampled shard state and visible polling lifecycle
One owner retains snapshots and view selection; feature views own their forms.
===========================================================================
*/
import * as operations from "./operations.js";
const journal = operations.createJournal();
import { escape, fmt, census, historySample, coordinates } from "./model.js";
import * as view from "./view.js";
/*
================
$
================
*/
function $( id ) {
	return document.getElementById( id );
}
const state = {
	snapshot: null,
	shard: "",
	tab: "overview",
	paused: false,
	page: 0,
	histories: new Map(),
	last: new Map(),
	selected: null
};
let timer, controller, disposed = false;
/*
================
shard
================
*/
function shard() {
	return state.snapshot?.shards.find( s => s.id === state.shard );
}
/*
================
data
================
*/
function data() {
	const s = shard();
	return s?.data ?? state.last.get( state.shard );
}
/*
================
render
================
*/
function render() {
	const s = shard(), d = data();
	$( "realm-strip" ).innerHTML = operations.realmStrip( state.snapshot?.shards ?? [], state.shard );
	$( "operator-alerts" ).innerHTML = operations.alerts( s, d );
	$( "operations" ).hidden = state.tab !== "overview";
	$( "metrics" ).innerHTML = view.metrics( d ? { ...s, data: d } : s );
	$( "warning" ).hidden = !!s?.connected;
	$( "warning" ).textContent = !s?.connected ?
		`${s?.name ?? "Server"} is unavailable. ${s?.error ?? "Waiting for a snapshot."}${
			d ? " Showing the last successful capture." : ""
		}` :
		"Population exceeds the 50,000-row capture limit. The census is partial; resident total remains authoritative.";
	$( "connection" ).textContent = state.paused ? "Paused" : s?.connected ? "Live · 2s updates" : "Disconnected";
	$( "connection" ).classList.toggle( "offline", !s?.connected );
	$( "subtitle" ).textContent = s ?
		`${s.name} · ${s.test ? "Test realm" : "Global realm"} · Live server observation` :
		"A live window into the people, creatures and systems of Silkroad.";
	if ( !d ) {
		for (
			const id of [
				"atlas",
				"uniques",
				"chart",
				"health-summary",
				"player-table",
				"monster-table",
				"runtime-grid",
				"hotspots",
				"journal"
			]
		) $( id ).innerHTML = view.empty( "Awaiting server", "The dashboard will reconnect automatically." );
		return;
	}
	const h = state.histories.get( state.shard ) ?? [];
	$( "timestamp" ).textContent = `CAPTURED ${new Date( d.capturedAt ).toLocaleTimeString()} · ${
		s?.connected ? "CONNECTED" : "LAST KNOWN STATE"
	}`;
	if ( state.tab === "overview" ) {
		$( "hotspots" ).innerHTML = operations.hotspotView( d );
		$( "journal" ).innerHTML = operations.journalView( journal.rows( state.shard ) );
		$( "atlas" ).innerHTML = view.atlas( d.population.monsters, d.players );
		$( "uniques" ).innerHTML = view.encounters( d.population.monsters );
		$( "region-count" ).textContent = fmt( d.population.regions ) + " REGIONS";
		$( "chart" ).innerHTML = view.chart( h );
		$( "tick-label" ).textContent = d.transport.tick_last_ms.toFixed( 2 ) + " MS";
		$( "history-label" ).textContent = h.length > 1 ?
			`${Math.round( (h.at( -1 ).at - h[0].at) / 1000 )}s observed · latest tick samples` :
			"Collecting samples";
		$( "health-summary" ).innerHTML = view.healthSummary( d );
		$( "health-mark" ).innerHTML = d.storage.LastError ? "!" : '<span class="health-check">✓</span>';
	}
	if ( state.tab === "players" ) {
		$( "player-table" ).innerHTML = view.playerTable(
			d.players.filter( p => p.name.toLowerCase().includes( $( "player-search" ).value.toLowerCase() ) )
		);
	}
	if ( state.tab === "world" ) {
		const rows = census( d.population.monsters, $( "monster-search" ).value, $( "rarity" ).value ).filter( m =>
			!$( "sector-filter" ).value || m.region === Number( $( "sector-filter" ).value )
		);
		const sort = $( "monster-sort" ).value;
		rows.sort( ( a, b ) =>
			(sort === "level" ?
				b.level - a.level :
				sort === "health" ?
				(a.maxHP ? a.hp / a.maxHP : 1) - (b.maxHP ? b.hp / b.maxHP : 1) :
				sort === "name" ?
				a.name.localeCompare( b.name ) :
				0) || a.gid - b.gid
		);
		state.page = Math.min( state.page, Math.max( 0, Math.ceil( rows.length / 30 ) - 1 ) );
		$( "monster-table" ).innerHTML = view.monsterTable( rows.slice( state.page * 30, (state.page + 1) * 30 ) );
		$( "census-summary" ).textContent = `${fmt( rows.length )} matching / ${
			fmt( d.population.resident )
		} resident · ${
			fmt( d.population.respawns )
		} queued respawns · Regions activate through gameplay, not observation.`;
		$( "page-label" ).textContent = `Page ${state.page + 1} of ${Math.max( 1, Math.ceil( rows.length / 30 ) )}`;
		$( "previous" ).disabled = state.page === 0;
		$( "next" ).disabled = (state.page + 1) * 30 >= rows.length;
	}
	if ( state.tab === "health" ) $( "runtime-grid" ).innerHTML = view.runtimePanels( d, h.at( -1 ) );
	if ( $( "detail" ).open ) {
		const m = d.population.monsters.find( m => m.gid === state.selected );
		$( "detail-content" ).innerHTML = m ?
			view.detail( m ) :
			view.empty( "Entity no longer resident", "It left the latest authoritative snapshot." );
		$( "copy" ).disabled = !m;
	}
}
/*
================
poll
================
*/
async function poll() {
	clearTimeout( timer );
	if ( disposed || state.paused || document.hidden ) return;
	controller = new AbortController();
	const timeout = setTimeout( () => controller.abort(), 9000 );
	try {
		const response = await fetch( "/api/snapshot", { signal: controller.signal, cache: "no-store" } );
		if ( !response.ok ) throw Error( `Dashboard returned HTTP ${response.status}` );
		const result = await response.json();
		if ( disposed ) return;
		state.snapshot = result;
		const signature = result.shards.map( s => s.id + ":" + s.name ).join( "|" );
		if ( $( "shard" ).dataset.signature !== signature ) {
			$( "shard" ).innerHTML = result.shards.map( s =>
				`<option value="${escape( s.id )}">${escape( s.name )}</option>`
			).join( "" );
			$( "shard" ).dataset.signature = signature;
		}
		if ( !result.shards.some( s => s.id === state.shard ) ) state.shard = result.shards[0]?.id ?? "";
		$( "shard" ).value = state.shard;
		for ( const s of result.shards ) {
			if ( !s.connected ) continue;
			journal.observe( s.id, s.data );
			state.last.set( s.id, s.data );
			let h = state.histories.get( s.id ) ?? [];
			const sample = historySample( s.data, h.at( -1 ) );
			if ( sample.uptime < (h.at( -1 )?.uptime ?? 0) ) h = [];
			if ( sample.at !== h.at( -1 )?.at ) {
				h.push( sample );
				while ( h.length > 120 || h.length > 1 && h[0].at < sample.at - 240000 ) h.shift();
			}
			state.histories.set( s.id, h );
		}
		render();
	} catch ( error ) {
		if ( disposed ) return;
		if ( state.snapshot ) {
			state.snapshot.shards = state.snapshot.shards.map( s => ({
				...s,
				connected: false,
				error: error.message
			}) );
		}
		render();
		$( "warning" ).hidden = false;
		$( "warning" ).textContent =
			"Dashboard connection lost. Retrying automatically; displayed values are from the last capture.";
	} finally {
		clearTimeout( timeout );
		controller = null;
		if ( !disposed && !state.paused && !document.hidden ) timer = setTimeout( poll, 2000 );
	}
}
/*
================
selectView

Hash navigation keeps browser Back and direct links on the same view owner.
================
*/
function selectView( tab ) {
	const titles = {
		overview: "World overview",
		players: "Players online",
		world: "World census",
		health: "Runtime health",
		items: "Item archive",
		recovery: "Player recovery"
	};
	if ( !Object.hasOwn( titles, tab ) ) tab = "overview";
	state.tab = tab;
	document.querySelectorAll( ".view" ).forEach( n => n.hidden = n.id !== tab );
	document.querySelectorAll( "[data-view]" ).forEach( n => {
		n.classList.toggle( "active", n.dataset.view === tab );
		n.setAttribute( "aria-current", n.dataset.view === tab ? "page" : "false" );
	} );
	$( "view-title" ).textContent = titles[tab];
	$( "metrics" ).hidden = tab === "recovery";
	$( "realm-strip" ).hidden = tab === "recovery";
	document.querySelector( ".heading-actions" ).hidden = tab === "recovery";
	render();
}
document.querySelector( "nav" ).addEventListener( "click", event => {
	const tab = event.target.closest( "[data-view]" )?.dataset.view;
	if ( !tab ) return;
	selectView( tab );
	if ( location.hash !== "#" + tab ) history.pushState( null, "", "#" + tab );
} );
window.addEventListener( "popstate", () => selectView( location.hash.slice( 1 ) ) );
window.addEventListener( "hashchange", () => selectView( location.hash.slice( 1 ) ) );
$( "shard" ).addEventListener( "change", () => {
	state.shard = $( "shard" ).value;
	state.page = 0;
	$( "detail" ).close();
	render();
} );
for ( const id of [ "player-search", "monster-search", "rarity", "sector-filter", "monster-sort" ] ) {
	$( id ).addEventListener( "input", () => {
		state.page = 0;
		render();
	} );
}
for ( const [id, delta] of [ [ "previous", -1 ], [ "next", 1 ] ] ) {
	$( id ).addEventListener( "click", () => {
		state.page += delta;
		render();
	} );
}
$( "pause" ).addEventListener( "click", () => {
	state.paused = !state.paused;
	$( "pause" ).textContent = state.paused ? "▶ Resume" : "Ⅱ Pause";
	clearTimeout( timer );
	render();
	if ( !state.paused && !controller ) poll();
} );
document.addEventListener( "visibilitychange", () => {
	clearTimeout( timer );
	if ( !document.hidden && !controller ) poll();
} );
document.addEventListener( "click", event => {
	const id = event.target.closest( "[data-entity]" )?.dataset.entity;
	if ( !id ) return;
	state.selected = Number( id );
	$( "copy-status" ).textContent = "";
	$( "detail" ).showModal();
	render();
} );
$( "close-detail" ).addEventListener( "click", () => $( "detail" ).close() );
$( "copy" ).addEventListener( "click", async () => {
	const m = data()?.population.monsters.find( m => m.gid === state.selected );
	if ( !m ) return;
	const p = coordinates( m );
	try {
		await navigator.clipboard.writeText(
			`${m.name}: X ${p.x.toFixed( 0 )} Y ${p.y.toFixed( 0 )}; region ${m.region}; local ${m.x.toFixed( 2 )} ${
				m.y.toFixed( 2 )
			} ${m.z.toFixed( 2 )}`
		);
		$( "copy-status" ).textContent = "Coordinates copied";
	} catch {
		$( "copy-status" ).textContent = "Clipboard unavailable";
	}
} );
$( "export" ).addEventListener( "click", () => {
	if ( !data() ) return;
	const url = URL.createObjectURL( new Blob( [ JSON.stringify( data(), null, 2 ) ], { type: "application/json" } ) ),
		a = document.createElement( "a" );
	a.href = url;
	a.download = `silkroad-${state.shard}-${Date.now()}.json`;
	a.click();
	setTimeout( () => URL.revokeObjectURL( url ), 1000 );
} );
window.addEventListener( "pagehide", () => {
	disposed = true;
	clearTimeout( timer );
	controller?.abort();
} );
selectView( location.hash.slice( 1 ) );
poll();

$( "realm-strip" ).addEventListener( "click", event => {
	const id = event.target.closest( "[data-shard]" )?.dataset.shard;
	if ( !id ) return;
	state.shard = id;
	$( "shard" ).value = id;
	state.page = 0;
	$( "detail" ).close();
	render();
} );
$( "hotspots" ).addEventListener( "click", event => {
	const region = event.target.closest( "[data-region]" )?.dataset.region;
	if ( !region ) return;
	$( "sector-filter" ).value = region;
	$( "monster-search" ).value = "";
	$( "rarity" ).value = "all";
	state.page = 0;
	document.querySelector( "[data-view=world]" ).click();
} );
$( "clear-filters" ).addEventListener( "click", () => {
	for ( const id of [ "monster-search", "sector-filter" ] ) $( id ).value = "";
	$( "rarity" ).value = "all";
	$( "monster-sort" ).value = "gid";
	state.page = 0;
	render();
} );

// Item data stays lazy, including direct links and browser history navigation.
window.addEventListener( "DOMContentLoaded", () => {
	if ( state.tab === "items" ) document.querySelector( '[data-view="items"]' ).click();
} );
