/*
===========================================================================

settings.js - the Shard rules page: one server's rules and settings

Reads /api/settings for the chosen server (settings.mjs forwards the
GameWorld's settings contract v1) and paints each tunable rule with its
value, bounds and marks (settings-view.js), grouped and filtered, and the
deployment's shard settings below. Read-only: proposals and the second
approval arrive with the contract's second part and staff sign-in.

===========================================================================
*/
import { requestJSON } from "./request.js";
import { boundsText, currentValue, flagState, marks, visibleGroups, changed } from "./settings-view.js";

const byID = id => document.getElementById( id );
const state = { rows: [], flags: [], group: "All", query: "", changedOnly: false };

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
paintSummary
================
*/
function paintSummary() {
	const changes = state.rows.filter( changed ).length, live = state.rows.filter( row => row.class === "live" ).length;
	const tiles = [
		[ state.rows.length, "tunable rules", "" ],
		[ changes, "changed from default", changes ? "amber" : "" ],
		[ live, "change on the next action", "" ],
		[ state.flags.length, "shard settings", "" ]
	];
	byID( "rules-summary" ).replaceChildren(
		...tiles.map( ( [n, label, tone] ) =>
			el( "div", { class: "rules-tile " + tone }, el( "strong", {}, n ), el( "span", {}, label ) )
		)
	);
}

/*
================
paintGroups
================
*/
function paintGroups() {
	const names = [ "All", ...new Set( state.rows.map( row => row.group ) ) ];
	byID( "rules-groups" ).replaceChildren( ...names.map( name =>
		el( "button", {
			type: "button",
			class: "rules-group" + (name === state.group ? " active" : ""),
			"aria-pressed": name === state.group ? "true" : "false",
			onclick: () => {
				state.group = name;
				paint();
			}
		}, name )
	) );
}

/*
================
row
================
*/
function row( setting ) {
	return el(
		"li",
		{ class: "rules-row" + (changed( setting ) ? " changed" : "") },
		el(
			"div",
			{ class: "rules-name" },
			el( "div", { class: "rules-title" }, el( "strong", {}, setting.label ) ),
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
		)
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
				el( "div", {}, el( "div", { class: "eyebrow" }, group.name.toUpperCase() ), el( "h2", {}, group.name ) )
			),
			el( "ul", { class: "rules-rows" }, ...group.rows.map( row ) )
		)
	) );
}

/*
================
paintFlags
================
*/
function paintFlags() {
	byID( "flags-count" ).textContent = String( state.flags.length );
	byID( "flags-body" ).replaceChildren( ...state.flags.map( flag => {
		const shown = flagState( flag );
		return el(
			"li",
			{},
			el( "code", { class: "rules-key" }, flag.env ),
			el( "span", { class: "rules-chip " + shown.tone }, shown.text )
		);
	} ) );
}

/*
================
paint
================
*/
function paint() {
	paintSummary();
	paintGroups();
	paintBoard();
	paintFlags();
}

/*
================
load
================
*/
async function load() {
	const status = byID( "rules-status" );
	status.textContent = "Loading…";
	try {
		const body = await requestJSON( "/api/settings?shard=" + encodeURIComponent( byID( "rules-shard" ).value ) );
		state.rows = body.settings;
		state.flags = body.flags;
		status.textContent = "";
	} catch ( error ) {
		state.rows = [];
		state.flags = [];
		status.textContent = error.message;
	}
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
	byID( "rules-shard" ).replaceChildren( ...config.shards.map( shard => new Option( shard.name, shard.id ) ) );
	if ( !config.settings ) {
		byID( "rules-status" ).textContent = "Shard rules are not configured on this console (operatorTokenFile).";
		return;
	}
	await load();
}

byID( "rules-filter" ).addEventListener( "submit", event => event.preventDefault() );
byID( "rules-shard" ).addEventListener( "change", () => void load() );
byID( "rules-query" ).addEventListener( "input", event => {
	state.query = event.target.value;
	paintBoard();
} );
byID( "rules-changed" ).addEventListener( "change", event => {
	state.changedOnly = event.target.checked;
	paintBoard();
} );
void init();
