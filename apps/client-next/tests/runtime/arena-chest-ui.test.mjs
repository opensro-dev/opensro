/*
===========================================================================
arena-chest-ui.test.mjs - the reward chest and the bag share the HUD.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";

test("a visible world waits for the server chest seed without posting probes", () => {
	const commands = [];
	const f = uiFixture( command => commands.push( command ) );
	try {
		for ( let i = 0; i < 32; i++ ) f.ui.step( f.state, i * 100 );
		assert.equal( commands.some( c => c.kind === "gameplay" && c.command.kind === "arena-decision" ), false );
	} finally {
		f.dispose();
	}
});

test("the bag stays usable beside the chest and a sparse reward slot drags into it", () => {
	const commands = [];
	const f = uiFixture( command => commands.push( command ) );
	const reward = {
		slot: 41,
		refObjId: 11459,
		typeFlags: 0x132c,
		name: "Reward",
		quantity: 1,
		plus: 0,
		durability: 100,
		variance: "0",
		magic: []
	};
	const checkpoint = {
		id: "run-1",
		stage: 4,
		kills: 100,
		phase: /** @type {const} */ ("checkpoint"),
		remainingMs: 900000,
		decisionEndsAt: 300000,
		gold: "130000000",
		items: [ reward ]
	};
	const gameplay = {
		...f.state.gameplay,
		arenaChest: checkpoint,
		equipmentSlotCount: 13,
		inventorySlotCount: 45,
		inventory: [ { ...reward, slot: 14 } ]
	};
	const state = { ...f.state, gameplay };
	/** @param {string} id */
	const control = id => f.presented()?.controls.find( row => row.id === id );
	try {
		f.ui.step( state, 100 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.ui.step( state, 200 );
		const destination = control( "slot:13" )?.rect;
		assert.ok( destination, "inventory remains accessible at a checkpoint" );
		assert.equal( control( "slot:14" )?.draggable, true );
		f.ui.event( { kind: "drag", id: "slot:14", dx: -36, dy: 0 } );
		f.ui.event( { kind: "drag-end", id: "slot:14", x: destination[0] + 16, y: destination[1] + 16 } );
		assert.deepEqual( commands.at( -1 )?.command, {
			kind: "inventory-move",
			source: 14,
			destination: 13,
			quantity: 1
		} );
		assert.equal( control( "arena-chest-item:41" )?.draggable, false );
		assert.equal( control( "arena-choose" ), undefined );
		assert.equal( control( "arena-claim" )?.label, "Take all & end run" );
		assert.equal( control( "arena-continue" )?.label, "Next stage" );
		f.ui.event( { kind: "activate", id: "arena-claim" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "arena-decision", operation: 1, id: "run-1" } );

		f.ui.step( { ...state, gameplay: { ...gameplay, arenaChest: { ...checkpoint, stage: 5 } } }, 250 );
		assert.equal( control( "arena-claim" )?.label, "Take all & end run" );
		assert.equal( control( "arena-continue" ), undefined );

		f.ui.step( { ...state, gameplay: { ...gameplay, arenaChest: { ...checkpoint, id: "run-2" } } }, 275 );
		f.ui.event( { kind: "activate", id: "arena-continue" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "arena-decision", operation: 2, id: "run-2" } );

		const pending = {
			...checkpoint,
			phase: /** @type {const} */ ("claimable"),
			error: "Make bag space or drag rewards to your inventory one at a time."
		};
		f.ui.step( { ...state, gameplay: { ...gameplay, arenaChest: pending } }, 300 );
		assert.equal( control( "arena-claim" )?.label, "Take all" );
		assert.equal( control( "arena-continue" ), undefined );
		assert.equal( control( "arena-chest-item:41" )?.draggable, true );
		const claimDestination = control( "slot:13" )?.rect;
		assert.ok( claimDestination );
		f.ui.event( { kind: "drag", id: "arena-chest-item:41", dx: 10, dy: 0 } );
		f.ui.event( {
			kind: "drag-end",
			id: "arena-chest-item:41",
			x: claimDestination[0] + 16,
			y: claimDestination[1] + 16
		} );
		assert.deepEqual( commands.at( -1 )?.command, {
			kind: "arena-decision",
			operation: 4,
			id: "run-1",
			source: 41,
			destination: 13
		} );
		const before = commands.length;
		f.ui.event( { kind: "activate", id: "arena-discard" } );
		assert.equal( commands.length, before, "discard needs confirmation" );
		f.ui.event( { kind: "activate", id: "arena-discard" } );
		assert.deepEqual( commands.at( -1 )?.command, { kind: "arena-decision", operation: 5, id: "run-1" } );
	} finally {
		f.dispose();
	}
});
