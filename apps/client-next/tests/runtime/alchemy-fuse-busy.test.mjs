/*
===========================================================================

alchemy-fuse-busy.test.mjs - Fuse is ignored while a reinforcement runs

CIFAlchemyReinforce keeps +0x378 set from the Fuse press (62AFA0) through
the prepare effect, the answer and the result effect; 62C350's timer 0xA
clears it (625260) only once the result effect has played. 62AFA0 ignores
a press meanwhile and leaves the button up.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
import { defined } from "../helpers/defined.mjs";

// A sword (TypeID 3/1/6/2) and an elixir (3/3/10/1) to fuse onto it.
const SWORD = { slot: 13, refObjId: 3633, typeFlags: 0x132c, quantity: 1, name: "Sword" };
const ELIXIR = { slot: 14, refObjId: 3680, typeFlags: 0xd6c, quantity: 5, name: "Elixir" };
const PREPARE = "/interface/alchemy/alcm_effect_prepare.png";
const STEP_MS = 50;
const SIMULATION_MS = 40;

/*
================
fuseFixture

The reinforce window open with the sword and the elixir placed.
================
*/
function fuseFixture( t ) {
	const sent = [];
	const f = uiFixture( command => {
		if ( command.kind === "gameplay" && command.command.kind === "alchemy-start" ) sent.push( command.command );
	} );
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		inventory: [ SWORD, ELIXIR ],
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		inventoryPending: false,
		alchemy: { visible: true, pending: false, mode: "reinforce", flags: 0, error: null, slot: null }
	} );
	let now = 1000, last = null;
	/*
	================
	step
	================
	*/
	const step = ( frames = 1 ) => {
		for ( let i = 0; i < frames; i++ ) last = f.ui.step( f.state, now += STEP_MS ) ?? last;
		return defined( last );
	};
	step( 20 );
	f.ui.event( { kind: "activate", id: "open-window:Alchemy" } );
	step( 20 );
	for ( const item of [ SWORD, ELIXIR ] ) {
		f.ui.event( { kind: "activate", id: "slot:" + item.slot, ctrl: true } );
		step();
	}
	/*
	================
	press
	================
	*/
	const press = () => {
		f.ui.event( { kind: "activate", id: "alchemy-start" } );
		step();
	};
	return { f, sent, step, press };
}

test("Fuse is ignored from the press until the result effect has played", t => {
	const { f, sent, step, press } = fuseFixture( t );
	press();
	assert.deepEqual( sent.map( c => c.slots ), [ [ 13, 14 ] ] );
	// The worker holds the request through the prepare effect (62AFA0),
	// stamped on the simulation clock, whose origin is not the frame clock's.
	f.state.gameplay.inventoryPending = true;
	f.state.gameplay.alchemy = { ...f.state.gameplay.alchemy, pending: true, preparing: SIMULATION_MS };
	f.state.simulationTimeMs = SIMULATION_MS + STEP_MS;
	step();
	assert.ok(
		defined( f.scenes.at( -1 ) ).quads.some( q => q.texture.endsWith( PREPARE ) ),
		"the prepare effect plays in control 50"
	);
	press();
	assert.equal( sent.length, 1, "a press during the prepare effect is ignored" );
	// The answer: the result effect starts and the worker is idle again.
	f.state.gameplay.inventoryPending = false;
	f.state.gameplay.alchemy = {
		...f.state.gameplay.alchemy,
		pending: false,
		preparing: undefined,
		flags: 0x10,
		outcome: { sequence: 1, flags: 0x10, plus: 1, previousPlus: 0, durability: 1, previousDurability: 1 }
	};
	step();
	assert.ok( step().controls.some( c => c.id === "alchemy-start" ), "Fuse stays up while it runs" );
	press();
	assert.equal( sent.length, 1, "a press during the result effect is ignored" );
	step( 16 );
	press();
	assert.equal( sent.length, 2, "Fuse answers again once the result effect has played" );
});
