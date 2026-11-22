/*
===========================================================================

inventory-sort-button.test.mjs - the containers' sort button follows the shard

The server's port-only SRO_INVENTORY_SORT, published at world entry as
gameplay.inventorySort, puts a sort button beside the close button of the
warehouse (and of the inventory and a companion's bag). Off, the default,
no window has one, as the original.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";

test("the warehouse carries a sort button only on a shard that turns the rule on", () => {
	for ( const rule of [ false, true ] ) {
		/** @type {any[]} */
		const sent = [],
			f = uiFixture(
				/** @type {() => void} */ (/** @param {any} command */ command => {
					sent.push( command );
				})
			);
		try {
			const game = {
				...f.state.gameplay,
				inventoryPending: false,
				...(rule ? { inventorySort: true } : {}),
				storage: { npc: 7, phase: "open", capacity: 150, gold: "0", items: [] }
			};
			const state = { ...f.state, gameplay: game };
			let now = 0, scene = f.ui.step( state, now );
			scene = f.ui.step( state, ++now ) ?? scene;
			const button = defined( scene ).controls.find( c => c.id === "inventory-sort:storage" );
			assert.equal( !!button, rule, "the button follows the shard's rule" );
			if ( !rule ) continue;
			f.ui.event( { kind: "activate", id: "inventory-sort:storage" } );
			f.ui.step( state, ++now );
			assert.deepEqual( sent.pop(), {
				kind: "gameplay",
				command: { kind: "inventory-sort", container: "storage", gid: undefined }
			} );
			// While a sort runs every sort button is disabled.
			const sorting = { ...state, gameplay: { ...game, inventorySorting: "storage" } };
			scene = f.ui.step( sorting, ++now ) ?? scene;
			assert.equal( defined( scene ).controls.find( c => c.id === "inventory-sort:storage" )?.disabled, true );
		} finally {
			f.dispose();
		}
	}
});
