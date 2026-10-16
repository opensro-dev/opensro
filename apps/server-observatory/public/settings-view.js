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

One number in a rule's unit: count, gold, minutes, ms or factor.
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
marks

The chips beside a rule, in reading order, each with its words.
================
*/
export function marks( row ) {
	const out = [ CLASS_MARKS[row.class] ?? CLASS_MARKS.restart ];
	if ( changed( row ) ) out.push( { tone: "changed", text: "Default " + showValue( row, row.default ) } );
	if ( row.source === "store" ) out.push( { tone: "store", text: "Set here" } );
	return out;
}

/*
================
visibleGroups

The rules a filter keeps, grouped in their first-seen order. query
matches the label, key, variable and help; changedOnly keeps rules run
other than their default.
================
*/
export function visibleGroups( rows, { group = "All", query = "", changedOnly = false } = {} ) {
	const needle = query.trim().toLowerCase(), groups = new Map();
	for ( const row of rows ) {
		if ( group !== "All" && row.group !== group ) continue;
		if ( changedOnly && !changed( row ) ) continue;
		if (
			needle && !(row.label + " " + row.key + " " + row.env + " " + row.help).toLowerCase().includes( needle )
		) {
			continue;
		}
		if ( !groups.has( row.group ) ) groups.set( row.group, [] );
		groups.get( row.group ).push( row );
	}
	return [ ...groups ].map( ( [name, members] ) => ({ name, rows: members }) );
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
