/*
===========================================================================
pet-public-buffs.test.mjs - observer buff isolation and paid Bond feedback
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { createPetTraining } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/pet-training/pet-training.ts"
);
const { targetPetBuffs } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { damageText } = await import( "../../src/engine/foundation/ui/damage-text.ts" );
test("public recipient buffs preserve local state and clear on expiry/removal", () => {
	const owner = createPetTraining( () => null );
	function stacks( op, gid, book, count, value ) {
		const p = new Uint8Array( 8 ), v = new DataView( p.buffer );
		v.setUint32( 0, gid, true );
		p[4] = book;
		p[5] = count;
		v.setUint16( 6, value, true );
		owner.receive( { opcode: op, payload: p }, 100, 1 );
	}
	stacks( 0x3fb7, 1, 57, 2, 100 );
	stacks( 0x3fb7, 2, 57, 10, 500 );
	stacks( 0x3fb9, 2, 60, 5, 150 );
	const p = new Uint8Array( 11 ), v = new DataView( p.buffer );
	v.setUint32( 0, 2, true );
	p[4] = 3;
	v.setUint16( 5, 5000, true );
	v.setUint32( 7, 8000, true );
	owner.receive( { opcode: 0x3fbd, payload: p }, 100, 1 );
	assert.equal( defined( owner.tailwind() ).stacks, 2 );
	const game = { petRecipientBuffs: owner.recipients() };
	assert.deepEqual( targetPetBuffs( game, 2, 200 ).map( r => r.id ).sort(), [
		"pet-guidance",
		"pet-howl",
		"pet-tailwind"
	] );
	assert.equal(
		defined( targetPetBuffs( game, 2, 200 ).find( r => r.id === "pet-tailwind" ) ).label,
		"Tailwind: 10/10 - Stun ready"
	);
	assert.equal( targetPetBuffs( game, 999, 200 ).length, 0 );
	assert.equal( targetPetBuffs( game, 2, 8100 ).some( r => r.id === "pet-howl" ), false );
	stacks( 0x3fb7, 2, 0, 0, 0 );
	stacks( 0x3fb9, 2, 0, 0, 0 );
	p.fill( 0, 4 );
	owner.receive( { opcode: 0x3fbd, payload: p }, 300, 1 );
	assert.equal( targetPetBuffs( { petRecipientBuffs: owner.recipients() }, 2, 300 ).length, 0 );
	owner.removeRecipient( 1 );
	assert.equal( owner.recipients().length, 0 );
	owner.clear();
	assert.equal( owner.recipients().length, 0 );
});
test("paid Bond damage is yellow feedback without another HP debit", () => {
	const bear = {
		gid: 2,
		refObjId: 23729,
		name: "TestBear",
		heading: 0,
		kind: "cos",
		regionId: 24744,
		x: 1,
		y: 2,
		z: 3
	};
	const combat = createCombat( gid => gid === 2 ? bear : undefined );
	combat.seed( 2, { hp: 80, mp: 0, maxHp: 100, maxMp: 0 } );
	const p = new Uint8Array( 13 ), v = new DataView( p.buffer );
	v.setUint32( 0, 1, true );
	v.setUint32( 4, 2, true );
	p[8] = 1;
	v.setUint32( 9, 20, true );
	assert.equal( combat.receive( 0x3fb4, p, 100 ), true );
	const event = combat.state().environmentalDamage[0];
	assert.equal( event.damage, 20 );
	assert.equal( event.linked, true );
	assert.equal( defined( combat.state().vitals.find( v => v.gid === 2 ) ).hp, 80 );
	assert.deepEqual(
		damageText( bear, { type: 0, flags: 1, damage: 20, fatal: false, secondaryAmount: 0 }, 0, true, event.linked )
			.color,
		[
			1,
			211 / 255,
			58 / 255
		]
	);
	combat.receive( 0x3fb4, p.slice( 0, 9 ), 200 );
	assert.equal( combat.state().environmentalDamage.length, 1 );
	p[8] = 2;
	assert.throws( () => combat.receive( 0x3fb4, p, 300 ) );
});

test("selected-player Howl uses server metadata without the removed experimental toggle", t => {
	const f = uiFixture();
	t.after( () => f.dispose() );
	f.state.entities.push( { ...f.state.entities[0], gid: 2, name: "Ally" } );
	Object.assign( f.state.gameplay, {
		target: 2,
		petRecipientBuffs: [ { gid: 2, howl: { gid: 2, book: 3, speedPercent: 500, expiresAtMs: 8000 } } ]
	} );
	const visible = defined( f.ui.step( f.state, 100 ) );
	assert.ok( visible.controls.some( row => row.id === "target-pet-howl" ) );
	Object.assign( f.state.gameplay, { petRecipientBuffs: [] } );
	const cleared = defined( f.ui.step( f.state, 200 ) );
	assert.equal( cleared.controls.some( row => row.id === "target-pet-howl" ), false );
});
