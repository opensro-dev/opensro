/*
===========================================================================

settings.js - the Shard rules page: one server's rules and settings

Reads /api/settings for the chosen server (settings.mjs forwards the
GameWorld's settings contract v1) and paints each tunable rule with its
value, bounds, marks and any change on its way (settings-view.js),
grouped and filtered, and the deployment's shard settings below, set
ones first. The filters live in the URL query so a view can be linked.
Read-only: proposing and approving happen on the GameWorld.

===========================================================================
*/
import { requestJSON } from "./request.js";
import {
	boundsText,
	changed,
	currentValue,
	flagState,
	marks,
	pendingText,
	readFilters,
	splitFlags,
	visibleGroups,
	waiting,
	writeFilters
} from "./settings-view.js";

const byID = id => document.getElementById( id );
const state = {
	rows: [],
	flags: [],
	readAt: null,
	...readFilters( new URLSearchParams( location.search ) )
};

/*
================
el

One element with attributes and children; strings become text nodes, so
nothing from the GameWorld is ever parsed as markup.
================
*/
function el( tag, attrs = {}, ...children ) {
	const node = document.createElement( tag );
	for ( const [name, value] of Object.entries( attrs ) ) {
		if ( value === false || value === undefined || value === null ) continue;
		if ( name.startsWith( "on" ) ) node.addEventListener( name.slice( 2 ), value );
		else if ( name === "class" ) node.className = value;
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
saveFilters

Keeps the address in step with the filters, without a history entry per
keystroke.
================
*/
function saveFilters() {
	const query = writeFilters( state );
	history.replaceState( null, "", location.pathname + (query ? "?" + query : "") );
}

/*
================
setFilter

One filter change from any control: the state, the address, the page.
================
*/
function setFilter( change ) {
	Object.assign( state, change );
	saveFilters();
	paint();
}

/*
================
paintSummary

Four plaques. The ones that name a subset are buttons that filter to it;
pressed again they clear it.
================
*/
function paintSummary() {
	const changes = state.rows.filter( changed ).length,
		waits = state.rows.filter( waiting ).length,
		setFlags = state.flags.filter( flag => flag.value ).length;
	const plain = ( n, label ) => el( "div", { class: "rules-tile" }, el( "strong", {}, n ), el( "span", {}, label ) );
	const filter = ( n, label, tone, field ) =>
		el(
			"button",
			{
				type: "button",
				class: "rules-tile " + (n ? tone : ""),
				"aria-pressed": state[field] ? "true" : "false",
				disabled: !n && !state[field],
				onclick: () => setFilter( { [field]: !state[field] } )
			},
			el( "strong", {}, n ),
			el( "span", {}, label )
		);
	byID( "rules-summary" ).replaceChildren(
		plain( state.rows.length, "tunable rules" ),
		filter( changes, "changed from default", "amber", "changedOnly" ),
		filter( waits, "changes waiting", "sky", "pendingOnly" ),
		plain( setFlags + " / " + state.flags.length, "shard settings set" )
	);
}

/*
================
paintFilters

The controls echo the state, so a linked view arrives with them set.
================
*/
function paintFilters() {
	const names = [ "All", ...new Set( state.rows.map( row => row.group ) ) ];
	byID( "rules-groups" ).replaceChildren( ...names.map( name =>
		el( "button", {
			type: "button",
			class: "rules-group" + (name === state.group ? " active" : ""),
			"aria-pressed": name === state.group ? "true" : "false",
			onclick: () => setFilter( { group: name } )
		}, name )
	) );
	if ( byID( "rules-query" ).value !== state.query ) byID( "rules-query" ).value = state.query;
	byID( "rules-changed" ).checked = state.changedOnly;
	byID( "rules-pending" ).checked = state.pendingOnly;
}

/*
================
pendingNote

A waiting proposal under its rule: the value asked for, by whom, when,
and the reason as it was given.
================
*/
function pendingNote( setting ) {
	const p = pendingText( setting );
	if ( !p ) return null;
	return el(
		"div",
		{ class: "rules-pending", role: "note" },
		el( "strong", {}, "Proposed " + p.value ),
		el( "span", {}, " by " + p.by + (p.when ? ", " + p.when : "") + " - awaiting a second approval" ),
		p.reason ? el( "q", {}, p.reason ) : null
	);
}

/*
================
row
================
*/
function row( setting ) {
	return el(
		"li",
		{ class: "rules-row" + (changed( setting ) ? " changed" : "") + (waiting( setting ) ? " waiting" : "") },
		el(
			"div",
			{ class: "rules-name" },
			el( "strong", { class: "rules-title" }, setting.label ),
			setting.help ? el( "p", { class: "rules-help" }, setting.help ) : null,
			setting.native ? el( "p", { class: "rules-native" }, "Native: " + setting.native ) : null,
			el( "code", { class: "rules-key" }, setting.env )
		),
		el(
			"div",
			{ class: "rules-value" },
			el( "span", { class: "rules-now" }, currentValue( setting ) ),
			el( "span", { class: "rules-range" }, boundsText( setting ) ),
			el(
				"div",
				{ class: "rules-chips-row" },
				...marks( setting ).map( mark =>
					el( "span", { class: "rules-chip " + mark.tone, title: mark.title }, mark.text )
				)
			)
		),
		pendingNote( setting )
	);
}

/*
================
paintBoard
================
*/
function paintBoard() {
	const groups = visibleGroups( state.rows, state );
	if ( !groups.length ) {
		byID( "rules-board" ).replaceChildren(
			el(
				"p",
				{ class: "panel rules-empty" },
				state.rows.length ?
					"No rule matches. Clear the search or the filters." :
					"This server reports no tunable rules."
			)
		);
		return;
	}
	byID( "rules-board" ).replaceChildren( ...groups.map( group =>
		el(
			"section",
			{ class: "panel rules-group-panel" },
			el(
				"div",
				{ class: "panel-heading" },
				el( "h2", {}, group.name ),
				el(
					"span",
					{ class: "tag" },
					group.rows.length + (group.rows.length === 1 ? " rule" : " rules") +
						(group.changed ? " · " + group.changed + " changed" : "")
				)
			),
			el( "ul", { class: "rules-rows" }, ...group.rows.map( row ) )
		)
	) );
}

/*
================
flagItem
================
*/
function flagItem( flag ) {
	const shown = flagState( flag );
	return el(
		"li",
		{},
		el( "code", { class: "rules-key" }, flag.env ),
		el( "span", { class: "rules-chip " + shown.tone }, shown.text )
	);
}

/*
================
paintFlags

The settings the deployment sets, then the unset ones folded away. A
search opens the fold when only unset settings match.
================
*/
function paintFlags() {
	const { set, unset } = splitFlags( state.flags, state.query );
	byID( "flags-count" ).textContent = set.length + " set";
	byID( "flags-set" ).replaceChildren(
		...(set.length ? set.map( flagItem ) : [ el( "li", { class: "rules-empty" }, "None set" ) ])
	);
	byID( "flags-unset-title" ).textContent = unset.length + " unset";
	byID( "flags-unset" ).replaceChildren( ...unset.map( flagItem ) );
	byID( "flags-unset-box" ).hidden = !unset.length;
	if ( state.query && !set.length && unset.length ) byID( "flags-unset-box" ).open = true;
}

/*
================
paintRead

When the page last read the shard, as a clock time and its age.
================
*/
function paintRead() {
	if ( !state.readAt ) {
		byID( "rules-read" ).textContent = "";
		return;
	}
	const seconds = Math.round( (Date.now() - state.readAt) / 1000 );
	const age = seconds < 60 ? "just now" : Math.floor( seconds / 60 ) + " min ago";
	byID( "rules-read" ).textContent = "Read " + new Date( state.readAt ).toLocaleTimeString() + " (" + age + ")";
}

/*
================
paint
================
*/
function paint() {
	paintSummary();
	paintFilters();
	paintBoard();
	paintFlags();
	paintRead();
}

/*
================
load
================
*/
async function load() {
	const status = byID( "rules-status" );
	status.textContent = "Loading…";
	byID( "rules-refresh" ).disabled = true;
	try {
		const body = await requestJSON( "/api/settings?shard=" + encodeURIComponent( state.shard ) );
		state.rows = body.settings;
		state.flags = body.flags;
		state.readAt = Date.now();
		status.textContent = "";
	} catch ( error ) {
		state.rows = [];
		state.flags = [];
		state.readAt = null;
		status.textContent = error.message;
	}
	byID( "rules-refresh" ).disabled = false;
	paint();
}

/*
================
init
================
*/
async function init() {
	let config;
	try {
		config = await requestJSON( "/api/console" );
	} catch ( error ) {
		byID( "rules-status" ).textContent = error.message;
		return;
	}
	const select = byID( "rules-shard" );
	select.replaceChildren( ...config.shards.map( shard => new Option( shard.name, shard.id ) ) );
	// A linked shard the console does not know falls back to the first one.
	if ( !config.shards.some( shard => shard.id === state.shard ) ) state.shard = config.shards[0]?.id ?? "";
	select.value = state.shard;
	if ( !config.settings ) {
		byID( "rules-status" ).textContent = "Shard rules are not configured on this console (operatorTokenFile).";
		return;
	}
	await load();
}

// The age beside "Read" goes stale on its own; repaint it each half minute.
const READ_AGE_REPAINT_MS = 30000;

byID( "rules-filter" ).addEventListener( "submit", event => event.preventDefault() );
byID( "rules-shard" ).addEventListener( "change", event => {
	state.shard = event.target.value;
	saveFilters();
	void load();
} );
byID( "rules-refresh" ).addEventListener( "click", () => void load() );
byID( "rules-query" ).addEventListener( "input", event => setFilter( { query: event.target.value } ) );
byID( "rules-changed" ).addEventListener( "change", event => setFilter( { changedOnly: event.target.checked } ) );
byID( "rules-pending" ).addEventListener( "change", event => setFilter( { pendingOnly: event.target.checked } ) );
setInterval( paintRead, READ_AGE_REPAINT_MS );
void init();
