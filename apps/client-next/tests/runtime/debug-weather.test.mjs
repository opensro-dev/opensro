/*
===========================================================================

debug-weather.test.mjs - the worker's debug-weather arm

The console's weather rows (690C40 ids 99/105-107) cross to the worker as a
debug-weather command. The arm is GM-gated and lands in the console weather
slot (event-environment.ts): a GM's rain publishes, a later 3BDE is held
off until /clear, and a session without the privilege drops the row whole.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

/*
================
session

A world gameplay owner entered with the given bootstrap, holding the entry
weather (mode 1, amount 0).
================
*/
function session( bootstrap ) {
	const g = createGameplay( () => {} );
	g.bootstrap( bootstrap );
	g.seed( /** @type {any} */ ({ gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 }) );
	return g;
}

/*
================
weatherOf

The published weather; an untaken frame still shows the entry weather.
================
*/
const weatherOf = g => g.take()?.weather ?? { mode: 1, amount: 0 };

test("a GM's debug weather publishes and holds off 3BDE until /clear", () => {
	const g = session( { character: { gmPrivilege: true } } );
	assert.deepEqual( weatherOf( g ), { mode: 1, amount: 0 } );
	g.command( { kind: "debug-weather", mode: 2, amount: 50 }, 0, undefined );
	assert.deepEqual( weatherOf( g ), { mode: 2, amount: 50 } );
	g.receive( { opcode: 0x3bde, payload: Uint8Array.of( 3, 7 ) }, 0 );
	assert.deepEqual( weatherOf( g ), { mode: 2, amount: 50 }, "a 3BDE displaced the held console weather" );
	g.command( { kind: "debug-weather", mode: 1, amount: 255 }, 0, undefined );
	assert.deepEqual( weatherOf( g ), { mode: 1, amount: 255 } );
	g.receive( { opcode: 0x3bde, payload: Uint8Array.of( 3, 7 ) }, 0 );
	assert.deepEqual( weatherOf( g ), { mode: 3, amount: 7 }, "packets did not land again after /clear" );
});

test("a session without the privilege drops the weather rows whole", () => {
	const g = session( {} );
	g.command( { kind: "debug-weather", mode: 2, amount: 50 }, 0, undefined );
	assert.deepEqual( weatherOf( g ), { mode: 1, amount: 0 }, "a non-GM weather row published" );
});
