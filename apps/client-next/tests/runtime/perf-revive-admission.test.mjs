/*
===========================================================================

perf-revive-admission.test.mjs - bounded recovery admission diagnostics

Exercise the diagnostic allowlist and the existing positive-HP requirement
without launching a browser, changing a character or waiting on real time.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readReviveAdmission, revive } from "../../tools/perf/core/client.mjs";

/*
================
runtime
================
*/
function runtime( game, entities = [] ) {
	return { gameplay: () => game, entities: () => entities };
}

test("missing local HP is distinguished from an explicit zero", () => {
	/** @type {{ localGid: number, vitals: Array<{ gid: number, hp?: number }>, progression: { level: number } }} */
	const game = { localGid: 7, vitals: [], progression: { level: 20 } };
	const target = { __benchRuntime: runtime( game ) };
	assert.equal( readReviveAdmission( target ).state, "missing-local-vital" );
	assert.equal( readReviveAdmission( target ).hp, null );
	game.vitals.push( { gid: 7 } );
	assert.equal( readReviveAdmission( target ).state, "missing-hp" );
	game.vitals[0].hp = 0;
	assert.equal( readReviveAdmission( target ).state, "nonpositive-hp" );
	assert.equal( readReviveAdmission( target ).hp, 0 );
	assert.equal( readReviveAdmission( {} ).state, "missing-runtime" );
	assert.equal( readReviveAdmission( { __benchRuntime: runtime( null ) } ).state, "missing-gameplay" );
});

test("admission output contains only selected numeric and boolean local facts", () => {
	const target = {
		__benchRuntime: runtime( {
			localGid: 7,
			progression: { level: 20 },
			authToken: "secret",
			vitals: [ { gid: 8, hp: 999 }, { gid: 7, hp: 0, maxHp: 100, deathState: true, token: "secret" } ]
		}, [ { gid: 7, name: "private name", appearanceState: [ 2, 3, 4 ], token: "secret" } ] )
	};
	assert.deepEqual( readReviveAdmission( target ), {
		state: "nonpositive-hp",
		localGid: 7,
		localEntityPresent: true,
		localVitalPresent: true,
		hp: 0,
		maxHp: 100,
		deathState: true,
		lifeState: 2,
		level: 20
	} );
});

/*
================
pageFixture
================
*/
function pageFixture( t, game ) {
	const target = /** @type {any} */ (globalThis);
	const original = Object.getOwnPropertyDescriptor( target, "__benchRuntime" );
	const commands = [];
	Object.defineProperty( target, "__benchRuntime", {
		configurable: true,
		value: { ...runtime( game ), session: command => commands.push( command ) }
	} );
	t.after( () => {
		if ( original ) Object.defineProperty( target, "__benchRuntime", original );
		else delete target.__benchRuntime;
	} );
	return {
		commands,
		page: { evaluate: async fn => fn(), waitForTimeout: async () => {} }
	};
}

test("missing HP still fails after the bounded retries with admission evidence", async t => {
	const { page, commands } = pageFixture( t, { localGid: 7, vitals: [], progression: { level: 20 } } );
	await assert.rejects( revive( page ), error => {
		assert.ok( error instanceof Error );
		assert.match( error.message, /could not be revived; admission:/ );
		const admission = JSON.parse( error.message.split( "; admission: " )[1] );
		assert.equal( admission.state, "missing-local-vital" );
		assert.equal( admission.hp, null );
		assert.equal( admission.level, 20 );
		return true;
	} );
	assert.equal( commands.length, 20 );
	assert.ok( commands.every( command => command.command.kind === "rebirth" && command.command.choice === 2 ) );
});

test("positive HP retains the immediate successful admission path", async t => {
	const { page, commands } = pageFixture( t, { localGid: 7, vitals: [ { gid: 7, hp: 1 } ] } );
	await revive( page );
	assert.equal( commands.length, 0 );
});
