/*
===========================================================================

settings-view.js - what the Shard rules page shows for each rule

Pure functions from the settings contract (v1 rows) to the page's words:
a value in its unit, its bounds, the marks that set it apart (switched
off, changed from its default) and the filtered, grouped list. No DOM
here, so node tests read exactly what the page renders.

===========================================================================
*/

/*
================
groupedNumber

A whole number with comma thousands separators.
================
*/
export function groupedNumber( value ) {
	const [whole, fraction] = String( value ).split( "." );
	const grouped = whole.replace( /\B(?=(\d{3})+(?!\d))/g, "," );
	return fraction ? grouped + "." + fraction : grouped;
}

/*
================
showValue

One number in a rule's unit: count, gold, minutes, ms, percent or factor.
================
*/
export function showValue( row, value ) {
	switch ( row.unit ) {
		case "gold":
			return groupedNumber( value ) + " gold";
		case "minutes":
			return value + (value === 1 ? " minute" : " minutes");
		case "ms":
			return groupedNumber( value ) + " ms";
		case "percent":
			return groupedNumber( value ) + "%";
		case "factor":
			return "×" + Number( value ).toFixed( 2 );
		default:
			return groupedNumber( value );
	}
}

/*
================
currentValue

The rule's value as the shard runs it. A factor switched off runs the
native behaviour, whatever number it carries.
================
*/
export function currentValue( row ) {
	if ( row.kind === "factor" && !row.enabled ) return "Off (native)";
	return showValue( row, row.value );
}

/*
================
boundsText

The values the rule accepts: its exact list, or its range, and for a
factor that it can also be switched off.
================
*/
export function boundsText( row ) {
	const values = row.allowed?.length ?
		"one of " + row.allowed.map( value => showValue( row, value ) ).join( ", " ) :
		showValue( row, row.min ) + " – " + showValue( row, row.max );
	return row.kind === "factor" ? "off, or " + values : values;
}

/*
================
changed

Whether the shard runs the rule other than its default (a factor switched
off counts as changed when its default is on).
================
*/
export function changed( row ) {
	if ( row.kind === "factor" && !row.enabled ) return true;
	return row.value !== row.default;
}

// What each class promises: when an approved change takes effect.
const CLASS_MARKS = Object.freeze( {
	live: { tone: "live", text: "Live", title: "A change applies on the next action" },
	entry: { tone: "entry", text: "Next world entry", title: "A change applies at each player's next world entry" },
	restart: { tone: "restart", text: "Restart", title: "A change applies at the next restart" }
} );

/*
================
rawValue

A contract value carried as a string (a proposal's to, a queued restart
value): a factor's "off" or a number, shown in the rule's unit; anything
else as it came.
================
*/
export function rawValue( row, raw ) {
	if ( row.kind === "factor" && String( raw ).trim().toLowerCase() === "off" ) return "Off (native)";
	const n = Number( raw );
	return raw !== "" && Number.isFinite( n ) ? showValue( row, n ) : String( raw );
}

/*
================
marks

The chips beside a rule, in reading order, each with its words.
================
*/
export function marks( row ) {
	const out = [ CLASS_MARKS[row.class] ?? CLASS_MARKS.restart ];
	if ( changed( row ) ) out.push( { tone: "changed", text: "Default " + showValue( row, row.default ) } );
	if ( row.source === "store" ) out.push( { tone: "store", text: "Set here" } );
	if ( row.restartQueued !== null && row.restartQueued !== undefined ) {
		out.push( {
			tone: "queued",
			text: "After restart " + rawValue( row, row.restartQueued ),
			title: "Approved; the shard runs it from its next restart"
		} );
	}
	return out;
}

/*
================
pendingText

A waiting proposal in words, or null: the value it asks for, who asked,
when (at is Unix seconds) and why.
================
*/
export function pendingText( row ) {
	const p = row.pending;
	if ( !p ) return null;
	const when = p.at ? new Date( p.at * 1000 ).toISOString().slice( 0, 16 ).replace( "T", " " ) + " UTC" : "";
	return { value: rawValue( row, p.to ), by: p.byName || p.by || "staff", when, reason: p.reason };
}

/*
================
waiting

Whether a change to the rule is on its way: a proposal awaiting its
second approval, or an approved value queued for the next restart.
================
*/
export function waiting( row ) {
	return Boolean( row.pending ) || (row.restartQueued !== null && row.restartQueued !== undefined);
}

/*
================
matches

Whether a lowercase needle is in any of the given strings.
================
*/
function matches( needle, ...fields ) {
	return !needle || fields.join( " " ).toLowerCase().includes( needle );
}

/*
================
visibleGroups

The rules a filter keeps, grouped in their first-seen order, each group
with how many of its rules are changed. query matches the label, key,
variable and help; changedOnly keeps rules run other than their default;
pendingOnly keeps rules with a change on its way.
================
*/
export function visibleGroups( rows, { group = "All", query = "", changedOnly = false, pendingOnly = false } = {} ) {
	const needle = query.trim().toLowerCase(), groups = new Map();
	for ( const row of rows ) {
		if ( group !== "All" && row.group !== group ) continue;
		if ( changedOnly && !changed( row ) ) continue;
		if ( pendingOnly && !waiting( row ) ) continue;
		if ( !matches( needle, row.label, row.key, row.env, row.help ) ) continue;
		if ( !groups.has( row.group ) ) groups.set( row.group, [] );
		groups.get( row.group ).push( row );
	}
	return [ ...groups ].map( ( [name, members] ) => ({
		name,
		rows: members,
		changed: members.filter( changed ).length
	}) );
}

/*
================
splitFlags

The shard settings a query keeps, split into those the deployment sets
(any value) and those it leaves unset, each in contract order.
================
*/
export function splitFlags( flags, query = "" ) {
	const needle = query.trim().toLowerCase(), set = [], unset = [];
	for ( const flag of flags ) {
		if ( !matches( needle, flag.env, flag.value ) ) continue;
		(flag.value ? set : unset).push( flag );
	}
	return { set, unset };
}

/*
================
readFilters

The page's filters from a URL query (URLSearchParams), defaults for
anything missing.
================
*/
export function readFilters( params ) {
	return {
		shard: params.get( "shard" ) ?? "",
		group: params.get( "group" ) ?? "All",
		query: params.get( "q" ) ?? "",
		changedOnly: params.get( "changed" ) === "1",
		pendingOnly: params.get( "pending" ) === "1"
	};
}

/*
================
writeFilters

The URL query for the page's filters. Defaults stay out, so a plain view
keeps a plain address.
================
*/
export function writeFilters( filters ) {
	const params = new URLSearchParams();
	if ( filters.shard ) params.set( "shard", filters.shard );
	if ( filters.group && filters.group !== "All" ) params.set( "group", filters.group );
	if ( filters.query ) params.set( "q", filters.query );
	if ( filters.changedOnly ) params.set( "changed", "1" );
	if ( filters.pendingOnly ) params.set( "pending", "1" );
	return params.toString();
}

/*
================
flagState

A shard setting's switch state in words: on, off, or its raw value when it
is not a switch (a rate, a list).
================
*/
export function flagState( flag ) {
	if ( flag.on === true ) return { tone: "on", text: "On" };
	if ( flag.on === false ) return { tone: "off", text: flag.value ? "Off" : "Off (unset)" };
	return { tone: "value", text: flag.value };
}
