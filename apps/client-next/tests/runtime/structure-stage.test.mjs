/*
===========================================================================

structure-stage.test.mjs - fortress structure stages and damage effects

Pins CICATStruct_UpdateDamageVisualStage (4F7B30), SetVisualStage
(4F78A0) and ApplyDamageVisualLevel (4F79A0) through the manifest reader,
the pure rules and the per-structure owner.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const stage = await import( "../../src/engine/foundation/rendering/structure-stage.ts" );
const { createStructureVisuals } = await import( "../../src/engine/runtime/characters/structure-visuals.ts" );

const effect = ( effectPath, particle, loop = true ) => ({
	effectPath,
	offset: [ 1, 2, 3 ],
	rotation: 0,
	loop,
	particle
});
const visuals = defined( stage.readStructureVisuals( {
	structureStages: {
		0: { glb: "/assets/npc/tower.glb", particleModifiers: [] },
		1: { glb: "/assets/npc/tower_dmg.glb", particleModifiers: [] },
		2: { glb: "/assets/npc/tower_destroy.glb", particleModifiers: [] }
	},
	structureSounds: { 1: { handle: "SND_STRUCT1", shake: true }, 2: { handle: "SND_STRUCT2", shake: true } },
	structureDamageEffects: {
		1: [ effect( "map/fire.efp", 0 ) ],
		5: [ effect( "map/bomb.efp", 1, false ), effect( "map/smoke.efp", 0 ) ]
	}
}, () => [] ) );

test("the stage and level follow lost hit points (4F7B30)", () => {
	assert.deepEqual( stage.structureVisualTarget( 100, 100, 0 ), { stage: 0, level: -1 } );
	assert.deepEqual( stage.structureVisualTarget( 99, 100, 0 ), { stage: 0, level: 0 } );
	assert.deepEqual( stage.structureVisualTarget( 61, 100, 0 ), { stage: 0, level: 1 } );
	// The float ratio 0.600000024 loses 1.99999988 fifths, truncated to 1.
	assert.deepEqual( stage.structureVisualTarget( 60, 100, 0 ), { stage: 1, level: 1 } );
	assert.deepEqual( stage.structureVisualTarget( 59, 100, 0 ), { stage: 1, level: 2 } );
	assert.deepEqual( stage.structureVisualTarget( 1, 100, 0 ), { stage: 1, level: 4 } );
	assert.deepEqual( stage.structureVisualTarget( 0, 100, 0 ), { stage: 1, level: 5 } );
	assert.deepEqual( stage.structureVisualTarget( 100, 100, 1 ), { stage: 2, level: 5 }, "the destroyed state bit" );
	assert.deepEqual( stage.structureVisualTarget( 0, 0, 0 ), { stage: 2, level: 5 }, "no hit points" );
});

test("an effect's particle type matches the transition (4F79A0)", () => {
	assert.equal( stage.structureEffectTransition( -1, 5 ), stage.STRUCTURE_TRANSITION_FIRST );
	assert.equal( stage.structureEffectTransition( 2, 5 ), stage.STRUCTURE_TRANSITION_RISE );
	assert.equal( stage.structureEffectTransition( 5, 2 ), stage.STRUCTURE_TRANSITION_FALL );
	const first = defined( stage.structureLevelParticles( visuals, 5, stage.STRUCTURE_TRANSITION_FIRST ) );
	assert.deepEqual(
		first.map( p => p.effectPath ),
		[ "map/smoke.efp" ],
		"a structure seen destroyed does not explode"
	);
	const rise = defined( stage.structureLevelParticles( visuals, 5, stage.STRUCTURE_TRANSITION_RISE ) );
	assert.deepEqual( rise.map( p => [ p.effectPath, p.loop ] ), [ [ "map/bomb.efp", false ], [
		"map/smoke.efp",
		undefined
	] ] );
	assert.deepEqual( rise[0].offset, [ 1, 2, -3 ], "BSR-space offsets flip z like model particles" );
	assert.equal( stage.structureLevelParticles( visuals, -1, 1 ), undefined );
	assert.deepEqual( stage.structureLevelParticles( visuals, 3, 1 ), [] );
});

test("the owner shows stages once a second and sounds rising damage (4F78A0)", () => {
	const owner = createStructureVisuals();
	let hp = 100;
	const entity = {
		gid: 9,
		refObjId: 19536,
		kind: "structure",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		name: "",
		maxHp: 100,
		structureHp: 100,
		structureState: 0
	};
	const step = nowMs => owner.step( [ { entity, visuals, hp } ], nowMs );
	const shown = () => defined( owner.appearance( 9, "/assets/npc/record.glb", [] ) );
	assert.deepEqual( step( 0 ), [], "the first stage plays no sound" );
	const standing = shown();
	assert.equal( standing.glb, "/assets/npc/tower.glb" );
	assert.equal(
		owner.appearance( 9, "/assets/npc/record.glb", [] ),
		standing,
		"an unchanged appearance keeps its identity"
	);
	hp = 50;
	assert.deepEqual( step( 500 ), [], "the state timer has not fired" );
	assert.deepEqual( step( 1000 ), [ { gid: 9, handle: "SND_STRUCT1", shake: true } ] );
	assert.equal( shown().glb, "/assets/npc/tower_dmg.glb" );
	hp = 0;
	step( 2000 );
	const destroyed = shown();
	assert.deepEqual( destroyed.particles.map( p => p.effectPath ), [ "map/bomb.efp", "map/smoke.efp" ] );
	hp = 100;
	step( 3000 );
	assert.deepEqual( shown().particles, [], "repair clears the effects" );
	owner.step( [], 4000 );
	assert.equal(
		owner.appearance( 9, "/assets/npc/record.glb", [] ),
		undefined,
		"a despawned structure is forgotten"
	);
});
