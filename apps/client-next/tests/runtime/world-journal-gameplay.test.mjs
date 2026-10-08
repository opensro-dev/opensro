/*
===========================================================================

world-journal-gameplay.test.mjs - tests for entities.ts supersedeGameplay

A gameplay snapshot published while an older one is still queued merges
into it (#339). The merged batch must present exactly what the unmerged
sequence presents, and a world reset stays a barrier.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createPresentation } = await import( "../../src/engine/runtime/presentation/presentation.ts" );

// The shop and social values are opaque here: only their identity is checked.
const CATALOG = [ { id: 1, name: "skill" } ];
const SOCIAL = /** @type {any} */ (Object.freeze( { party: [] } ));
const SHOP = /** @type {any} */ (Object.freeze( { npc: 1 } ));

// What the worker publishes over three ticks (core.ts omits unchanged parts).
/** @type {any[]} */
const SNAPSHOTS = [
	{ localGid: 1, skillCatalog: CATALOG, shop: SHOP },
	{ localGid: 2, social: SOCIAL },
	{ localGid: 3, shop: undefined }
];

/*
================
gameplayEvent
================
*/
/** @returns {import("../../src/engine/contracts/world").WorldEvent} */
function gameplayEvent( /** @type {any} */ state ) {
	return { kind: "gameplay", state };
}

/*
================
presented

The gameplay state presentation shows after one batch.
================
*/
function presented( /** @type {readonly import("../../src/engine/contracts/world").WorldEvent[]} */ events ) {
	const presentation = createPresentation();
	presentation.apply( { sequence: 1, events } );
	return defined( presentation.gameplay(), "presented gameplay" );
}

/*
================
queuedGids

The localGid of each gameplay event in a batch, or the kind of any other.
================
*/
function queuedGids( /** @type {import("../../src/engine/contracts/world").WorldBatch | null} */ batch ) {
	return defined( batch, "batch" ).events.map( event =>
		event.kind === "gameplay" ? event.state.localGid : event.kind
	);
}

test("queued gameplay snapshots merge into one that presents the same state", () => {
	const entities = createEntities();
	for ( const state of SNAPSHOTS ) entities.publish( gameplayEvent( state ) );
	const batch = defined( entities.take(), "batch" );
	assert.equal( batch.events.length, 1 );
	const merged = presented( batch.events );
	assert.deepEqual( merged, presented( SNAPSHOTS.map( gameplayEvent ) ) );
	assert.equal( merged.localGid, 3 );
	assert.equal( merged.skillCatalog, CATALOG );
	assert.equal( merged.social, SOCIAL );
	assert.ok( "shop" in merged );
	assert.equal( merged.shop, undefined );
});

test("an earlier shop survives a merge that does not name one", () => {
	const entities = createEntities();
	entities.publish( gameplayEvent( { localGid: 1, shop: SHOP } ) );
	entities.publish( gameplayEvent( { localGid: 2 } ) );
	assert.equal( presented( defined( entities.take(), "batch" ).events ).shop, SHOP );
});

test("an offered batch is never rewritten and a reset is a merge barrier", () => {
	const entities = createEntities();
	entities.publish( gameplayEvent( { localGid: 1 } ) );
	const first = defined( entities.take(), "first batch" );
	entities.publish( gameplayEvent( { localGid: 2 } ) );
	assert.deepEqual( queuedGids( first ), [ 1 ] );
	entities.clear();
	entities.publish( gameplayEvent( { localGid: 3 } ) );
	entities.ack( first.sequence );
	assert.deepEqual( queuedGids( entities.take() ), [ 2, "reset", 3 ] );
});
