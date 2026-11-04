/*
===========================================================================
settings-view.test.mjs - the Shard rules page's words for each rule
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	boundsText,
	changed,
	currentValue,
	flagState,
	groupedNumber,
	marks,
	pendingText,
	rawValue,
	readFilters,
	splitFlags,
	visibleGroups,
	waiting,
	writeFilters
} from "../public/settings-view.js";

const BOUNTY = {
	key: "most_wanted_test_bounty",
	env: "SRO_MOST_WANTED_TEST_BOUNTY",
	group: "Most Wanted",
	label: "Test pacing: bounty per level",
	help: "Gold a wanted level is worth.",
	kind: "int",
	unit: "gold",
	value: 1000000,
	enabled: true,
	default: 1000000,
	native: null,
	min: 0,
	max: 100000000,
	allowed: null,
	step: 1,
	class: "live",
	source: "env"
};
const KILLS = {
	...BOUNTY,
	key: "most_wanted_test_kills",
	env: "SRO_MOST_WANTED_TEST_KILLS",
	label: "Test pacing: kills per level",
	unit: "count",
	value: 1,
	default: 5,
	min: 1,
	max: 5,
	allowed: [ 1, 5 ],
	class: "restart"
};
const RIDE = {
	...BOUNTY,
	key: "mall_ride_speed",
	env: "SRO_MALL_RIDE_SPEED",
	group: "Movement",
	label: "Mall vehicle and pet speed",
	kind: "factor",
	unit: "factor",
	value: 1,
	enabled: false,
	default: 1.3,
	native: "off: mall vehicles and pets move at their listed speed",
	min: 1,
	max: 3,
	class: "restart"
};
const REPEAT = {
	...BOUNTY,
	key: "job_kill_repeat_minutes",
	group: "Notices",
	unit: "minutes",
	value: 1,
	default: 10,
	min: 1,
	max: 60
};

test("values read in their unit", () => {
	assert.equal( groupedNumber( 100000000 ), "100,000,000" );
	assert.equal( currentValue( BOUNTY ), "1,000,000 gold" );
	assert.equal( currentValue( KILLS ), "1" );
	assert.equal( currentValue( REPEAT ), "1 minute" );
	assert.equal( currentValue( { ...REPEAT, value: 10 } ), "10 minutes" );
	assert.equal( currentValue( { ...BOUNTY, unit: "ms", value: 10000 } ), "10,000 ms" );
	assert.equal( currentValue( { ...KILLS, unit: "percent", value: 300 } ), "300%" );
	assert.equal( currentValue( { ...RIDE, enabled: true, value: 1.3 } ), "×1.30" );
	assert.equal( currentValue( RIDE ), "Off (native)", "a switched-off factor runs native whatever its number" );
});

test("bounds name the exact list, the range, or the factor's off", () => {
	assert.equal( boundsText( KILLS ), "one of 1, 5" );
	assert.equal( boundsText( BOUNTY ), "0 gold – 100,000,000 gold" );
	assert.equal( boundsText( RIDE ), "off, or ×1.00 – ×3.00" );
});

test("a rule is changed when it runs other than its default", () => {
	assert.equal( changed( BOUNTY ), false, "env at the default is not a change" );
	assert.equal( changed( KILLS ), true );
	assert.equal( changed( RIDE ), true, "a factor switched off runs native, not its default" );
	assert.deepEqual( marks( KILLS ).map( m => m.text ), [ "Restart", "Default 5" ] );
	assert.deepEqual( marks( BOUNTY ).map( m => m.text ), [ "Live" ] );
	assert.deepEqual( marks( { ...BOUNTY, source: "store" } ).map( m => m.text ), [ "Live", "Set here" ] );
	assert.deepEqual( marks( { ...BOUNTY, class: "entry" } ).map( m => m.text ), [ "Next world entry" ] );
});

test("filters keep matching rules in their groups, in first-seen order", () => {
	const rows = [ BOUNTY, RIDE, KILLS, REPEAT ];
	assert.deepEqual( visibleGroups( rows ).map( g => [ g.name, g.rows.map( r => r.key ) ] ), [
		[ "Most Wanted", [ "most_wanted_test_bounty", "most_wanted_test_kills" ] ],
		[ "Movement", [ "mall_ride_speed" ] ],
		[ "Notices", [ "job_kill_repeat_minutes" ] ]
	] );
	assert.deepEqual( visibleGroups( rows, { group: "Notices" } ).map( g => g.name ), [ "Notices" ] );
	assert.deepEqual(
		visibleGroups( rows, { changedOnly: true } ).flatMap( g => g.rows.map( r => r.key ) ),
		[ "mall_ride_speed", "most_wanted_test_kills", "job_kill_repeat_minutes" ]
	);
	assert.deepEqual(
		visibleGroups( rows, { query: "sro_mall" } ).flatMap( g => g.rows.map( r => r.key ) ),
		[ "mall_ride_speed" ]
	);
	assert.deepEqual( visibleGroups( rows, { query: "nothing like this" } ), [] );
});

test("a shard setting reads as on, off or its value", () => {
	assert.deepEqual( flagState( { env: "SRO_A", value: "on", on: true } ), { tone: "on", text: "On" } );
	assert.deepEqual( flagState( { env: "SRO_A", value: "", on: false } ), { tone: "off", text: "Off (unset)" } );
	assert.deepEqual( flagState( { env: "SRO_A", value: "off", on: false } ), { tone: "off", text: "Off" } );
	assert.deepEqual( flagState( { env: "SRO_A", value: "1.5", on: null } ), { tone: "value", text: "1.5" } );
});

test("a change on its way reads in the rule's unit and filters", () => {
	const proposed = {
		...REPEAT,
		pending: { id: "p1", to: "8", by: "s1", byName: "Staff A", at: 1793675000, reason: "weekend" }
	};
	const queued = { ...RIDE, restartQueued: "1.5" };
	assert.equal( rawValue( RIDE, "off" ), "Off (native)" );
	assert.equal( rawValue( RIDE, "1.5" ), "×1.50" );
	assert.equal( rawValue( BOUNTY, "2000" ), "2,000 gold" );
	assert.equal( rawValue( BOUNTY, "" ), "", "an empty raw value is not a zero" );
	assert.deepEqual( pendingText( proposed ), {
		value: "8 minutes",
		by: "Staff A",
		when: "2026-11-03 03:03 UTC",
		reason: "weekend"
	} );
	assert.equal( pendingText( { ...proposed, pending: { ...proposed.pending, byName: "" } } ).by, "s1" );
	assert.equal( pendingText( REPEAT ), null );
	assert.deepEqual( marks( queued ).map( m => m.text ), [ "Restart", "Default ×1.30", "After restart ×1.50" ] );
	assert.equal( waiting( proposed ), true );
	assert.equal( waiting( queued ), true );
	assert.equal( waiting( { ...BOUNTY, pending: null, restartQueued: null } ), false );
	assert.deepEqual(
		visibleGroups( [ BOUNTY, proposed, queued ], { pendingOnly: true } ).flatMap( g => g.rows.map( r => r.key ) ),
		[ "job_kill_repeat_minutes", "mall_ride_speed" ]
	);
});

test("each group counts its changed rules", () => {
	const groups = visibleGroups( [ BOUNTY, KILLS, RIDE ] );
	assert.deepEqual( groups.map( g => [ g.name, g.rows.length, g.changed ] ), [
		[ "Most Wanted", 2, 1 ],
		[ "Movement", 1, 1 ]
	] );
});

test("shard settings split into set and unset, and the search reaches them", () => {
	const flags = [
		{ env: "SRO_A_ON", value: "on", on: true },
		{ env: "SRO_B_UNSET", value: "", on: false },
		{ env: "SRO_C_RATE", value: "1.5", on: null }
	];
	const all = splitFlags( flags );
	assert.deepEqual( all.set.map( f => f.env ), [ "SRO_A_ON", "SRO_C_RATE" ] );
	assert.deepEqual( all.unset.map( f => f.env ), [ "SRO_B_UNSET" ] );
	assert.deepEqual( splitFlags( flags, "rate" ).set.map( f => f.env ), [ "SRO_C_RATE" ] );
	assert.deepEqual( splitFlags( flags, "1.5" ).set.map( f => f.env ), [ "SRO_C_RATE" ], "a value matches too" );
	assert.deepEqual( splitFlags( flags, "unset" ), { set: [], unset: [ flags[1] ] } );
});

test("filters round-trip through the address, defaults left out", () => {
	const filters = { shard: "realm", group: "Trade", query: "gold", changedOnly: true, pendingOnly: false };
	assert.equal( writeFilters( filters ), "shard=realm&group=Trade&q=gold&changed=1" );
	assert.deepEqual( readFilters( new URLSearchParams( writeFilters( filters ) ) ), filters );
	assert.equal( writeFilters( { shard: "", group: "All", query: "", changedOnly: false, pendingOnly: false } ), "" );
	assert.deepEqual( readFilters( new URLSearchParams( "" ) ), {
		shard: "",
		group: "All",
		query: "",
		changedOnly: false,
		pendingOnly: false
	} );
});
