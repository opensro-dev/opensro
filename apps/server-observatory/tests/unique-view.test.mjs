/*
===========================================================================

unique-view.test.mjs - authoritative and legacy unique census rendering

An ordinary-monster sample must not masquerade as a complete unique count.
Keep older server snapshots usable while reporting their truncated totals
as lower bounds until the server has the full-population count.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { metrics } from "../public/view.js";

/*
================
uniqueMetric
================
*/
function uniqueMetric( population ) {
	const html = metrics( {
		capacity: 1000,
		data: {
			players: [],
			registeredCharacters: 0,
			transport: { tick_last_ms: 1, tick_overruns: 0 },
			population
		}
	} );
	return html.split( "<article" ).find( card => card.includes( "Uniques alive" ) );
}

test("authoritative living unique count is independent of the capped list", () => {
	const population = {
		resident: 60000,
		regions: 1000,
		truncated: true,
		uniqueAlive: 7,
		monsters: [ { rarity: 3, hp: 100 }, { rarity: 0x13, hp: 100 }, { rarity: 3, hp: 0 } ]
	};
	assert.match( uniqueMetric( population ), /metric-number">7<small>/ );
	assert.match( uniqueMetric( { ...population, uniqueAlive: 0 } ), /metric-number">0<small>/ );
});

test("legacy truncated census is visibly a lower bound", () => {
	const population = {
		resident: 60000,
		regions: 1000,
		truncated: true,
		monsters: [ { rarity: 3, hp: 100 }, { rarity: 0x13, hp: 100 }, { rarity: 3, hp: 0 } ]
	};
	assert.match( uniqueMetric( population ), /metric-number">2\+<small>/ );
	assert.match( uniqueMetric( population ), /at least this many/ );
	assert.match( uniqueMetric( { ...population, truncated: false } ), /metric-number">2<small>/ );
});
