/*
===========================================================================

keep-awake.test.mjs - the background-tab Web Lock follows the world session

Edge's sleeping tabs freeze a background tab unless it holds a Web Lock;
a frozen tab cannot answer keepalives and the server ends its session.
These tests pin that the lock is held exactly while asked for, once, and
released on dispose, against an injected navigator.locks.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { createKeepAwake } = await import(
	sourceFileUrl( path.resolve( root, "src/engine/runtime/platform/keep-awake.ts" ) ).href
);

/*
================
installLocks

Replaces navigator with one whose locks record every request and report
whether its callback's promise is still pending (the lock is held).
================
*/
function installLocks( t, locks ) {
	const previous = Object.getOwnPropertyDescriptor( globalThis, "navigator" );
	Object.defineProperty( globalThis, "navigator", { value: { locks }, configurable: true } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "navigator", previous );
		else Reflect.deleteProperty( globalThis, "navigator" );
	} );
}

/*
================
recordingLocks
================
*/
function recordingLocks() {
	const held = new Set(), requests = [];
	return {
		held,
		requests,
		request( name, callback ) {
			requests.push( name );
			held.add( name );
			return Promise.resolve( callback() ).finally( () => held.delete( name ) );
		}
	};
}

/*
================
settle
================
*/
function settle() {
	return new Promise( resolve => setImmediate( resolve ) );
}

test("holds one lock while active and releases it when inactive", async t => {
	const locks = recordingLocks();
	installLocks( t, locks );
	const keepAwake = createKeepAwake();
	keepAwake.set( true );
	keepAwake.set( true );
	await settle();
	assert.equal( locks.requests.length, 1 );
	assert.equal( locks.held.size, 1 );
	keepAwake.set( false );
	await settle();
	assert.equal( locks.held.size, 0 );
	keepAwake.set( true );
	await settle();
	assert.equal( locks.requests.length, 2 );
	assert.equal( locks.held.size, 1 );
	keepAwake.dispose();
	await settle();
	assert.equal( locks.held.size, 0 );
	keepAwake.set( true );
	assert.equal( locks.requests.length, 2, "a disposed owner never takes the lock again" );
});

test("two tabs never share a lock name", async t => {
	const locks = recordingLocks();
	installLocks( t, locks );
	const first = createKeepAwake(), second = createKeepAwake();
	first.set( true );
	second.set( true );
	await settle();
	assert.notEqual( locks.requests[0], locks.requests[1] );
	first.dispose();
	second.dispose();
});

test("a browser without Web Locks and a refused request are harmless", async t => {
	installLocks( t, undefined );
	const keepAwake = createKeepAwake();
	assert.doesNotThrow( () => keepAwake.set( true ) );
	keepAwake.dispose();
	installLocks( t, { request: () => Promise.reject( new Error( "denied" ) ) } );
	const refused = createKeepAwake();
	refused.set( true );
	await settle();
	refused.dispose();
});
