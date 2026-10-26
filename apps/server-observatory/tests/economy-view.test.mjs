/*
===========================================================================
economy-view.test.mjs - gold stays exact, reasons stay readable, charts stay bounded
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	alignSeries,
	censusRows,
	compactGold,
	dayLabel,
	flagLabel,
	flowRows,
	groupDigits,
	killStatRows,
	lineChart,
	plusBars,
	rangeWindow,
	rateText,
	reasonLabel,
	recoveryLink,
	seriesTotal
} from "../public/economy-view.js";

test("gold beyond a JS number is summed and printed exactly", () => {
	const huge = "18446744073709551615";
	assert.equal( groupDigits( huge ), "18,446,744,073,709,551,615" );
	assert.equal(
		seriesTotal( { points: [ { t: 0, sum: huge }, { t: 3600, sum: "1" } ] } ),
		18446744073709551616n
	);
	assert.equal( groupDigits( "-1234567" ), "-1,234,567" );
	assert.equal( groupDigits( "not a number" ), "0" );
	assert.equal( compactGold( "1240000000" ), "1.24 B" );
	assert.equal( compactGold( "-830000000" ), "-830 M" );
});

test("every range stays inside the contract's 720-bucket cap and hourly retention", () => {
	for ( const id of [ "24h", "7d", "30d", "90d", "365d" ] ) {
		const window = rangeWindow( id, 1_760_000_000 );
		const buckets = (window.to - window.from) / window.step;
		assert.ok( buckets <= 720, `${id}: ${buckets}` );
		assert.equal( window.from % window.step, 0, "aligned to its bucket" );
		if ( window.bucket === "1h" ) assert.ok( window.to - window.from <= 30 * 86400 );
	}
});

test("flows group mint, burn, transfer, then reasons the console does not know, largest first", () => {
	const meta = {
		reasons: [
			{ name: "monster_gold", kind: "mint", unit: "gold" },
			{ name: "npc_sale", kind: "mint", unit: "gold" },
			{ name: "repair", kind: "burn", unit: "gold" },
			{ name: "exchange", kind: "transfer", unit: "gold" }
		]
	};
	const rows = flowRows( meta, {
		t: [ 0, 3600 ],
		reasons: {
			exchange: [ "5", "5" ],
			repair: [ "100", "0" ],
			brand_new_reason: [ "1", "1" ],
			npc_sale: [ "10", "10" ],
			monster_gold: [ "50", "70" ]
		}
	} );
	assert.deepEqual( rows.map( row => row.name ), [
		"monster_gold",
		"npc_sale",
		"repair",
		"exchange",
		"brand_new_reason"
	] );
	assert.equal( rows[0].total, 120n );
	assert.equal( rows.at( -1 ).kind, "unknown" );
	assert.equal( reasonLabel( "brand_new_reason" ), "Brand new reason" );
	assert.equal( flagLabel( "a_future_rule" ), "A future rule" );
});

test("a level sampled every six hours draws one connected line; a flow gap is zero", () => {
	const window = { from: 0, to: 12 * 3600, step: 3600 };
	const supply = alignSeries(
		{ points: [ { t: 0, sum: "10" }, { t: 6 * 3600, sum: "20" }, { t: 12 * 3600, sum: "30" } ] },
		window,
		null
	);
	assert.equal( supply.filter( p => p.value === null ).length, 10 );
	const svg = lineChart( [ { name: "Gold held", tone: "level", points: supply } ], {
		label: "x",
		step: 3600,
		connect: true
	} );
	const path = /<path class="line tone-level" d="([^"]+)"/.exec( svg )[1];
	assert.equal( (path.match( /M/g ) ?? []).length, 1, "one stroke across the empty buckets" );
	assert.equal( (path.match( /L/g ) ?? []).length, 2 );
	const flow = alignSeries( { points: [ { t: 3600, sum: "4" } ] }, window, 0 );
	assert.equal( flow[0].value, 0 );
	assert.equal( flow[1].value, 4 );
});

test("charts escape server text, keep one hit column per bucket and name two series in a legend", () => {
	const points = Array.from( { length: 720 }, ( _, i ) => ({ t: i * 3600, value: i }) );
	const svg = lineChart( [
		{ name: "<b>Minted</b>", tone: "mint", points },
		{ name: "Burned", tone: "burn", points }
	], { label: 'a "quoted" label', step: 3600 } );
	assert.equal( (svg.match( /class="hit"/g ) ?? []).length, 720 );
	assert.ok( !svg.includes( "<b>" ) );
	assert.ok( svg.includes( "economy-legend" ) );
	assert.ok(
		!lineChart( [ { name: "One", tone: "level", points } ], { label: "x", step: 3600 } ).includes(
			"economy-legend"
		)
	);
	assert.match(
		lineChart( [ { name: "x", tone: "mint", points: [] } ], { label: "x", step: 3600 } ),
		/economy-empty/
	);
});

test("kill stat rows flatten newest day first with bands low to high", () => {
	const rows = killStatRows( {
		contract: 1,
		minCharacters: 3,
		suppressed: 1,
		days: [
			{
				day: 0,
				bands: [
					{
						band: 8,
						label: "81-90",
						characters: 4,
						kills: 60,
						exp: 10000,
						skillExp: 3500,
						onlineHours: 3,
						killsPerOnlineHour: 20,
						expPerOnlineHour: 3333.33,
						skillExpPerOnlineHour: 1166.67
					},
					{
						band: 0,
						label: "1-10",
						characters: 3,
						kills: 9,
						exp: 90,
						skillExp: 30,
						onlineHours: 1.5,
						killsPerOnlineHour: 6,
						expPerOnlineHour: 60,
						skillExpPerOnlineHour: 20
					}
				]
			},
			{
				day: 86400,
				bands: [
					{
						band: 9,
						label: "91+",
						characters: 5,
						kills: 10,
						exp: 500,
						skillExp: 100,
						onlineHours: 2,
						killsPerOnlineHour: 5,
						expPerOnlineHour: 250,
						skillExpPerOnlineHour: 50
					}
				]
			}
		]
	} );
	assert.deepEqual( rows.map( row => [ row.dayText, row.label ] ), [
		[ "1970-01-02", "91+" ],
		[ "1970-01-01", "1-10" ],
		[ "1970-01-01", "81-90" ]
	] );
	assert.equal( rows[2].killsPerHour, 20 );
	assert.equal( rows[2].skillExpPerHour, 1166.67 );
	assert.deepEqual( killStatRows( null ), [] );
	assert.deepEqual( killStatRows( { days: "nope" } ), [] );
	assert.deepEqual( killStatRows( { days: [ { day: 0, bands: null } ] } ), [] );
});

test("rates print grouped with at most two decimals and dashes for gaps", () => {
	assert.equal( rateText( 20 ), "20" );
	assert.equal( rateText( 1166.67 ), "1,166.67" );
	assert.equal( rateText( 3333.333 ), "3,333.33" );
	assert.equal( rateText( 1234567.891 ), "1,234,567.89" );
	assert.equal( rateText( 0.5 ), "0.5" );
	assert.equal( rateText( 2.5 ), "2.5" );
	assert.equal( rateText( 0 ), "0" );
	assert.equal( rateText( undefined ), "—" );
	assert.equal( rateText( NaN ), "—" );
	assert.equal( dayLabel( 0 ), "1970-01-01" );
	assert.equal( dayLabel( 86400 ), "1970-01-02" );
});

test("census rows keep degree order and withheld tiers stay withheld", () => {
	const rows = censusRows( {
		rows: [
			{ degree: 9, star: { items: 6, holders: 4 }, moon: null, sun: { items: 6, holders: 4 } },
			{ degree: 8, star: null, moon: null, sun: null },
			{ degree: 1, star: { items: 3, holders: 3 }, moon: { items: 1, holders: 1 }, sun: null }
		]
	} );
	assert.deepEqual( rows.map( row => row.degree ), [ 1, 8, 9 ] );
	assert.deepEqual( rows[2].star, { items: 6, holders: 4 } );
	assert.equal( rows[2].moon, null, "a withheld bucket never gains numbers here" );
	assert.deepEqual( censusRows( null ), [] );
	assert.deepEqual( censusRows( { rows: "nope" } ), [] );
	assert.deepEqual( censusRows( { rows: [ { star: { items: 1, holders: 1 } } ] } ), [] );
});

test("plus levels sort numerically and flag links only prefill Player recovery", () => {
	assert.deepEqual( plusBars( { "10": 2, "2": 5, "0": 9, junk: 1 } ).map( bar => bar.label ), [ "+0", "+2", "+10" ] );
	assert.equal( recoveryLink( "realm", "A&B" ), "/?shard=realm&character=A%26B#recovery" );
});
