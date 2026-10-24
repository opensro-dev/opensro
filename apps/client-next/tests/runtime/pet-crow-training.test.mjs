/*
===========================================================================
pet-crow-training.test.mjs - crow support counters and stored-cast UI state.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { PET_DESIGN } = await import( "../../src/engine/foundation/ui/pet-design-data.ts" );
const { createPetTraining } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/pet-training/pet-training.ts"
);
const { petSkillDesign, petEffect, petRowCooldown } = await import( "../../src/engine/foundation/ui/pet-training.ts" );

test("Tailwind private state validates atomically and clears on session reset", () => {
	const owner = createPetTraining( () => null );
	const bytes = new Uint8Array( 8 ), v = new DataView( bytes.buffer );
	v.setUint32( 0, 100002, true );
	bytes[4] = 57;
	bytes[5] = 10;
	v.setUint16( 6, 300, true );
	owner.receive( { opcode: 0x3fb7, payload: bytes }, 0 );
	assert.deepEqual( owner.tailwind(), { gid: 100002, book: 57, stacks: 10, speedPercent: 30 } );
	for ( const [offset, value] of [ [ 4, 58 ], [ 5, 11 ] ] ) {
		const bad = bytes.slice();
		bad[offset] = value;
		assert.throws( () => owner.receive( { opcode: 0x3fb7, payload: bad }, 1 ) );
		assert.equal( owner.tailwind()?.stacks, 10 );
	}
	assert.throws( () => owner.receive( { opcode: 0x3fb7, payload: bytes.slice( 1 ) }, 2 ) );
	owner.clear();
	assert.equal( owner.tailwind(), undefined );
	for ( const design of PET_DESIGN.treeSkills.filter( row => row.family === "Tailwind" ) ) {
		assert.match( petEffect( design, design.value, 1 ), /5 s/ );
		assert.doesNotMatch( petEffect( design, design.value, 1 ), /Attack speed|Planned/ );
	}
});

test("penguin books explain the complete frost caster kit", () => {
	for ( const d of PET_DESIGN.treeSkills.filter( s => s.pet === "Penguin" ) ) {
		const effect = petEffect( d, d.value, 1 );
		assert.ok( effect.length > 10 );
		assert.doesNotMatch( effect, /Planned|Static Feathers|Mana Tide/ );
	}
	assert.match( petEffect( defined( PET_DESIGN.treeSkills.find( s => s.id === 45 ) ), 2, 1 ), /2 s longer/ );
});

/*
================
Guiding Feathers private state is atomic and capped
================
*/
test("Guiding Feathers validates recipient stacks and clears on session end", () => {
	const owner = createPetTraining( () => null );
	const bytes = new Uint8Array( 8 ), view = new DataView( bytes.buffer );
	view.setUint32( 0, 100002, true );
	bytes[4] = 60;
	bytes[5] = 5;
	view.setUint16( 6, 100, true );
	owner.receive( { opcode: 0x3fb9, payload: bytes }, 100 );
	assert.deepEqual( owner.guidance(), { gid: 100002, book: 60, stacks: 5, hitPercent: 10 } );
	const previous = owner.guidance();
	bytes[5] = 6;
	assert.throws( () => owner.receive( { opcode: 0x3fb9, payload: bytes }, 101 ), /Invalid Guiding/ );
	assert.strictEqual( owner.guidance(), previous );
	bytes[4] = 0;
	bytes[5] = 0;
	view.setUint16( 6, 0, true );
	owner.receive( { opcode: 0x3fb9, payload: bytes }, 102 );
	assert.equal( owner.guidance(), undefined );
	owner.clear();
	assert.equal( owner.guidance(), undefined );
});

/*
================
stored casts
================
*/
test("Stormbrood shows charges while a sequential recharge is running", async () => {
	const { petSkillStack } = await import( "../../src/engine/foundation/ui/pet-skill-icons.ts" );
	const skill = petSkillDesign( 51 );
	assert.ok( skill );
	assert.match( petEffect( skill, 30, 10 ), /Maximum 10 eggs\/clones.*wind burst/ );
	const state = {
		gid: 1,
		points: 0,
		attackPercent: 100,
		defensePercent: 100,
		slotted: [],
		skills: [],
		gear: [],
		rows: [ { timerMs: 0, left: 0, kind: "" }, { timerMs: 300000, left: 2, kind: "Charges" } ],
		receivedAtMs: 1000
	};
	assert.equal( petRowCooldown( state, 1, 1000 ), "" );
	assert.equal(
		petSkillStack( state, skill, {
			id: 51,
			live: true,
			level: 1,
			effectiveLevel: 1,
			nextPetLevel: 0,
			action: true,
			queued: false,
			learnable: false,
			slotted: false,
			value: 0,
			nextValue: 0
		} ),
		2
	);
	state.rows[1].left = 0;
	assert.equal( petRowCooldown( state, 1, 1000 ), "300s" );
});
