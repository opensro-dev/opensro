/*
===========================================================================
arena-chest.test.mjs - authoritative previews, bounded parsing and dismissal.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createArenaChest } = await import( "../../src/engine/foundation/gameplay/arena-chest.ts" );
const { tooltipItems } = await import( "../../src/engine/foundation/ui/tooltip-target.ts" );
const encode = p => new TextEncoder().encode( JSON.stringify( p ) );
const projection = {
	version: 1,
	id: "run-1",
	phase: "checkpoint",
	stage: 1,
	kills: 100,
	remainingMs: 900000,
	decisionMs: 30000,
	gold: "100000",
	items: [ { slot: 0, refObjId: 5, typeFlags: 1, name: "Reward", body: [ 1 ] } ]
};
/** @returns {any} */
function preview( row ) {
	return { refObjId: row.refObjId, name: row.name };
}
test("checkpoint item help uses its own container and remains server owned", () => {
	const owner = createArenaChest( preview );
	owner.receive( encode( projection ), 100 );
	assert.equal( owner.state()?.decisionEndsAt, 30100 );
	assert.equal( owner.state()?.items[0].slot, 0 );
	owner.dismiss();
	assert.equal( owner.state()?.phase, "checkpoint" );
	/** @type {any} */ const game = {
		arenaChest: owner.state(),
		inventory: [ { refObjId: 5, name: "Wrong bag item" } ]
	};
	assert.equal( tooltipItems( "arena-chest-item:0", game, 0 )[0].name, "Reward" );
	owner.receive( encode( { ...projection, phase: "claimable" } ), 200 );
	owner.dismiss();
	assert.equal( owner.state()?.phase, "closed" );
	owner.receive( encode( { ...projection, phase: "recoverable" } ), 300 );
	owner.dismiss();
	assert.equal( owner.state()?.phase, "closed" );
	owner.clear();
	assert.equal( owner.state(), undefined );
});
test("malformed chest snapshots cannot partially replace the previous reward", () => {
	for (
		const bad of [
			{ version: 2 },
			{ items: Array( 201 ).fill( projection.items[0] ) },
			{ decisionMs: 300001 },
			{ stage: 6 },
			{ skillCooldowns: [ { skill: 1, remainingMs: -1, durationMs: 1000 } ] },
			{
				skillCooldowns: [ { skill: 1, remainingMs: 1, durationMs: 1000 }, {
					skill: 1,
					remainingMs: 1,
					durationMs: 1000
				} ]
			},
			{ gold: "-1" },
			{ items: [ ...projection.items, ...projection.items ] },
			{ items: [ { ...projection.items[0], body: [ 256 ] } ] }
		]
	) {
		const owner = createArenaChest( preview );
		owner.receive( encode( projection ), 0 );
		assert.throws( () => owner.receive( encode( { ...projection, ...bad } ), 100 ) );
		assert.equal( owner.state()?.id, "run-1" );
	}
});
