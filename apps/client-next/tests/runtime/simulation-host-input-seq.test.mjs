/*
===========================================================================

simulation-host-input-seq.test.mjs - the host stamps gameplay inputs (#1441a)

The simulation host is the one place every main-thread session command
passes. A gameplay command leaves with the next input sequence number on
its envelope, and session() returns that number so the render-thread mover
learns its input's sequence. Any other command carries none. A disposed
host posts nothing. Driven through a fake Worker that records each post.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createSimulationHost } = await import( "../../src/engine/runtime/simulation/host.ts" );

/*
================
hostHarness

A simulation host over a fake Worker; sent holds every posted message.
================
*/
function hostHarness( t ) {
	/** @type {any[]} */
	const sent = [];
	const original = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	Object.defineProperty( globalThis, "Worker", {
		configurable: true,
		writable: true,
		value: class {
			/** @param {any} message */
			postMessage( message ) {
				sent.push( message );
			}
			terminate() {}
		}
	} );
	t.after( () => {
		if ( original ) Object.defineProperty( globalThis, "Worker", original );
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	return { host: createSimulationHost(), sent };
}

test("a gameplay command leaves with the next input sequence, which session() returns", t => {
	const { host, sent } = hostHarness( t );
	const destination = { regionId: 0x62a8, x: 10, y: 0, z: 10, angle: 0 };
	const first = host.session( { kind: "gameplay", command: { kind: "move", destination } } );
	const second = host.session( { kind: "gameplay", command: { kind: "select", gid: 7 } } );
	assert.equal( first, 1 );
	assert.equal( second, 2 );
	const posted = sent.filter( message => message.kind === "session" );
	assert.deepEqual( posted.map( message => message.command.inputSeq ), [ 1, 2 ] );
	assert.equal( posted[1].command.command.kind, "select" );
	host.dispose();
});

test("a non-gameplay command carries no sequence and session() returns undefined", t => {
	const { host, sent } = hostHarness( t );
	assert.equal( host.session( { kind: "disconnect" } ), undefined );
	const posted = sent.filter( message => message.kind === "session" );
	assert.equal( posted.length, 1 );
	assert.equal( "inputSeq" in posted[0].command, false );
	// The next gameplay input still starts the sequence at 1.
	assert.equal( host.session( { kind: "gameplay", command: { kind: "select", gid: 7 } } ), 1 );
	host.dispose();
});

test("a disposed host posts nothing and returns undefined", t => {
	const { host, sent } = hostHarness( t );
	host.dispose();
	const before = sent.length;
	assert.equal( host.session( { kind: "gameplay", command: { kind: "select", gid: 7 } } ), undefined );
	assert.equal( sent.length, before );
});
