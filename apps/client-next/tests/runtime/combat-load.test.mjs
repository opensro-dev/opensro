/*
===========================================================================

combat-load.test.mjs - scene ownership survives setup drift and transport failure

Execute the real browser callbacks against a small injected runtime. A previously
known monster cannot become a new load merely by entering the measured radius.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import { loadCombat } from "../../tools/perf/bench/scenarios.mjs";

/*
================
monster
================
*/
function monster( gid, x = 0, alive = true ) {
	return { gid, refObjId: 1937, kind: "monster", regionId: 0, x, y: 0, z: 0, appearanceState: [ alive ? 1 : 2 ] };
}

/*
================
fixture
================
*/
function fixture( t, count ) {
	let entities = [ monster( 99, 1000 ) ], polls = 0;
	const oldRoot = globalThis.__benchRuntime, oldFetch = globalThis.fetch;
	globalThis.__benchRuntime = /** @type {any} */ ({
		gameplay: () => ({ pose: { regionId: 0, x: 0, z: 0 } }),
		entities: () => entities,
		characterActors: () => entities.map( e => ({ gid: e.gid }) ),
		/*
		================
		session
		================
		*/
		session( input ) {
			assert.match( input.command.line, /^\/LOADMONSTER / );
			entities = [ monster( 99, 10 ), monster( 1 ) ];
		}
	});
	globalThis.fetch = /** @type {any} */ (async () => ({
		json: async () => ({ models: { test: { codename: "MOB_CH_GYO", kind: "monster", refObjId: 1937 } } })
	}));
	t.after( () => {
		globalThis.__benchRuntime = oldRoot;
		globalThis.fetch = oldFetch;
	} );
	const page = {
		evaluate: ( callback, argument ) => callback( argument ),
		/*
		================
		waitForTimeout
		================
		*/
		waitForTimeout() {
			if ( polls++ ) throw Error( "transport closed during setup" );
			entities = [ monster( 99, 10 ), monster( 1, 500, false ), monster( 2 ) ];
		}
	};
	return { page, scene: { codename: "MOB_CH_GYO", count, type: "NORMAL", vulnerable: true } };
}

test("an old same-reference monster entering the radius is not part of the requested scene", async t => {
	const f = fixture( t, 1 );
	const loaded = await loadCombat( f.page, f.scene );
	assert.deepEqual( loaded.gids, [ 1 ] );
	assert.equal( loaded.ambient, 0 );
});

test("setup failure retains every seen scene GID even after death or departure from the radius", async t => {
	const f = fixture( t, 2 );
	await assert.rejects( loadCombat( f.page, f.scene ), error => {
		assert.ok( error instanceof Error );
		assert.match( error.message, /transport closed during setup/ );
		assert.ok( "scene" in error );
		assert.deepEqual( error.scene, { ...f.scene, refObjId: 1937, gids: [ 1, 2 ], ambient: 0 } );
		return true;
	} );
});
