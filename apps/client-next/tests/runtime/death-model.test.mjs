/*
===========================================================================

death-model.test.mjs - CICharactor_Action_KnockdownDie (8E64F0) branches

Drives presentation-state's death entry the way characters.ts does each
frame: the previous action mask, the down posture, a characterInfo death
model, the body's deathLoop track and GameConfig +0x12E decide whether the
body swaps meshes and whether the motion-4 one-shot plays.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const { createPresentationState } = await import(
	pathToFileURL( "src/engine/runtime/characters/presentation-state.ts" ).href
);

const MASK_CAST = 1 << 2, MASK_BASE = 1 << 3, MASK_SEATED = 1 << 6, MASK_MOVE = 1 << 9;

/*
================
harness

One monster (gid 1) whose body lacks or carries deathLoop, with or without a
death model, stepped through presentation-state like characters.ts does.
================
*/
function harness( { deathLoop = false, deathModel = true, uncensored = false } = {} ) {
	const state = createPresentationState();
	const body = {
		codename: "MOB_TEST",
		refObjId: 1,
		glb: "/assets/npc/test.glb",
		clips: [ "stand", "death", ...(deathLoop ? [ "deathLoop" ] : []) ]
	};
	const published = {
		catalog: new Map( [ [ 1, body ] ] ),
		recoveryByCodename: new Map( [ [ "MOB_TEST", 2000 ] ] ),
		animationStates: new Map(),
		deathModels: new Map(
			deathModel ? [ [ "MOB_TEST", { glb: "/assets/npc/test_die.glb", clips: [ "death", "deathLoop" ] } ] ] : []
		)
	};
	const actionStates = new Map();
	let seconds = 0;
	return {
		/*
		================
		step
		================
		*/
		/** @param {{ dead?: boolean; pending?: boolean; mask?: number; down?: boolean; }} [frame] */
		step( { dead = false, pending = false, mask, down = false } = {} ) {
			if ( mask !== undefined ) actionStates.set( 1, { actionMask: mask } );
			seconds += .1;
			const entity = {
				gid: 1,
				refObjId: 1,
				kind: "monster",
				regionId: 1,
				x: 10,
				y: 0,
				z: 10,
				heading: 0,
				movementMode: 2,
				appearanceState: [ dead ? 2 : 1, 0, 0 ]
			};
			state.step(
				{
					entities: [ entity ],
					seconds,
					simulationMs: seconds * 1000,
					logicalPose: e => ({ regionId: e.regionId, x: e.x, y: e.y, z: e.z, angle: 0 }),
					appearanceRef: e => e.refObjId,
					states: actionStates,
					health: undefined,
					deadGids: new Set(),
					hitByActor: new Map( down ? [ [ 1, { downAt: seconds * 1000 } ] ] : [] ),
					castByActor: new Map(),
					resources: { duration: () => 1 },
					random: { range: () => 0 },
					active: new Set( [ 1 ] ),
					pendingDeaths: new Set( pending ? [ 1 ] : [] ),
					uncensored
				},
				{ failure: null },
				published
			);
			return state.idleStates.get( 1 );
		}
	};
}

test("a body without deathLoop swaps to its death model and plays the one-shot from idle", () => {
	const h = harness();
	h.step( { mask: MASK_BASE | 0x100 } );
	const entry = h.step( { dead: true } );
	assert.equal( entry.deathModel, true );
	assert.equal( entry.deathAction, true );
});

test("the decision is taken once, on entering death, and cleared by revival", () => {
	const h = harness();
	h.step( { mask: MASK_BASE } );
	h.step( { dead: true } );
	// Later frames see actor-motion's dead mask (2): the entry decision stands.
	const held = h.step( { dead: true, mask: 2 } );
	assert.equal( held.deathModel, true );
	assert.equal( held.deathAction, true );
	const revived = h.step( { mask: MASK_BASE } );
	assert.equal( revived.deathModel, false );
	assert.equal( revived.deathAction, false );
});

test("a pending killing hit defers death entry until it lands", () => {
	const h = harness();
	h.step( { mask: MASK_BASE } );
	const pending = h.step( { dead: true, pending: true } );
	assert.equal( pending.deathModel, false );
	h.step( { mask: MASK_CAST | MASK_BASE } );
	const landed = h.step( { dead: true } );
	assert.equal( landed.deathModel, true );
	assert.equal( landed.deathAction, true );
});

test("only states 2 and 3 play the death one-shot", () => {
	for (
		const [mask, played] of /** @type {[number, boolean][]} */ ([
			[ MASK_CAST, true ],
			[ MASK_BASE, true ],
			[ MASK_MOVE, false ],
			[ MASK_SEATED, false ],
			[ 0, false ]
		])
	) {
		const h = harness();
		h.step( { mask } );
		assert.equal( h.step( { dead: true } ).deathAction, played, `mask ${mask}` );
	}
});

test("a body with deathLoop keeps its mesh unless GameConfig +0x12E forces the death model", () => {
	const censored = harness( { deathLoop: true } );
	censored.step( { mask: MASK_BASE } );
	assert.equal( censored.step( { dead: true } ).deathModel, false );
	const forced = harness( { deathLoop: true, uncensored: true } );
	forced.step( { mask: MASK_BASE } );
	assert.equal( forced.step( { dead: true } ).deathModel, true );
});

test("no characterInfo death model, no swap", () => {
	const h = harness( { deathModel: false, uncensored: true } );
	h.step( { mask: MASK_BASE } );
	const entry = h.step( { dead: true } );
	assert.equal( entry.deathModel, false );
	assert.equal( entry.deathAction, true );
});

test("death from the down posture plays downdie on the body", () => {
	const h = harness( { uncensored: true } );
	h.step( { mask: MASK_BASE } );
	h.step( { mask: MASK_BASE, down: true } );
	const entry = h.step( { dead: true, mask: 0x10 } );
	assert.equal( entry.downDeath, true );
	assert.equal( entry.deathModel, false );
	assert.equal( entry.deathAction, false );
});

test("a corpse first seen dead still loads its death model without the one-shot", () => {
	const h = harness();
	const entry = h.step( { dead: true } );
	assert.equal( entry.deathModel, true );
	assert.equal( entry.deathAction, false );
});
