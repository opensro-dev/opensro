/*
===========================================================================

pet-mini-info.test.mjs - the attack pet's mini window

CIFCOSManager_AddCompanion binds the window to an attack pet only;
CIFPetMiniInfo_OnUpdate (6B34C0) fills HP from the reference's maximum and
HGP from satiety / 10000, and blinks the caution while 0 < HP <= 30%.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { petMiniInfo } = await import( "../../src/engine/foundation/ui/pet-mini-info.ts" );

/** @type {ReadonlyMap<number, import("../../src/engine/foundation/ui/cos-command.ts").CosReference>} */
const references = new Map( [ [ 9, {
	icon: "pet/wolf.ddj",
	maxHp: 1000,
	rideable: false,
	physicalDefence: 0,
	magicalDefence: 0,
	parry: 0,
	hit: 0,
	skills: []
} ] ] );
const pet = { gid: 7, refObjId: 9, band: 3, hp: 500, mp: 0, status: 0, dead: false, level: 12, satiety: 5000 };

test("the window binds to the attack pet only", () => {
	assert.equal( petMiniInfo( [ { ...pet, band: 1 }, { ...pet, band: 4 } ], references ), null );
	assert.equal( petMiniInfo( undefined, references ), null );
	const info = petMiniInfo( [ { ...pet, band: 1, gid: 3 }, pet ], references );
	assert.equal( info?.gid, 7 );
	assert.equal( info?.level, 12 );
	assert.equal( info?.icon, "pet/wolf.ddj" );
	assert.equal( info?.name, undefined, "an unnamed pet takes the window's default title" );
});

test("gauges follow 6B34C0", () => {
	const info = petMiniInfo( [ pet ], references );
	assert.equal( info?.hp, 0.5 );
	assert.equal( info?.hgp, 0.5 );
	assert.equal( petMiniInfo( [ pet ], new Map() )?.hp, null, "no reference, no HP authority" );
	const resolved = petMiniInfo(
		[ { ...pet, hp: 128 } ],
		references,
		undefined,
		{ stacks: 10, tier: 3, untilMs: 5000 },
		1000
	);
	// A level-12 bear has 197 base HP; ten Resolve stacks project 256 HP.
	assert.equal( resolved?.hp, Math.fround( 128 / 256 ) );
});

test("the HP caution blinks while 0 < HP <= 30%", () => {
	const caution = hp => petMiniInfo( [ { ...pet, hp } ], references )?.caution;
	assert.equal( caution( 300 ), true, "exactly 30% blinks" );
	assert.equal( caution( 301 ), false );
	assert.equal( caution( 1 ), true );
	assert.equal( caution( 0 ), false, "a dead pet does not blink" );
});

test("6B3A00 / 6B3920 GDR_PET_MINI_BUFF renders attack pet buffs and unlevelled abnormals while the pet entity is present", () => {
	const fixture = uiFixture();
	try {
		/** @type {any} */
		const state = fixture.state;
		state.gameplay = {
			...state.gameplay,
			cosRecords: [ { ...pet, gid: 77, name: "Wolf" } ],
			skillCatalog: [ { id: 10, name: "Fire Shield", icon: "skill/china/fire_shield_a.ddj" } ],
			attachedEffects: [ { gid: 77, skill: 10, token: 1, phase: 2 } ],
			vitals: [ { gid: 77, hp: 500, mp: 0, abnormal: 0x800, abnormalLevels: [ { bit: 0x800, level: 3 } ] } ]
		};
		state.entities = [
			...state.entities,
			{ gid: 77, kind: "cos", name: "Wolf", regionId: 257, x: 0, y: 0, z: 0, heading: 0, mountedOn: 0 }
		];
		let frame = null;
		for ( let i = 0; i < 6; i++ ) {
			frame = fixture.ui.step( state, (i + 1) * 100 ) ?? frame;
		}
		assert.ok( frame );
		const buffControl = frame.controls.find( c => c.id === "pet-buff:0:buff:10" );
		const abnormalControl = frame.controls.find( c => c.id === "pet-buff:1:abnormal:11" );
		assert.ok( buffControl, "expected primary buff cell in GDR_PET_MINI_BUFF" );
		assert.ok( abnormalControl, "expected secondary abnormal cell in GDR_PET_MINI_BUFF" );
		assert.deepEqual( buffControl.rect, [ 89, 101, 14, 14 ] );
		assert.deepEqual( abnormalControl.rect, [ 89, 120, 14, 14 ] );
		assert.deepEqual( abnormalControl.helpSource, {
			kind: "abnormal",
			gid: 77,
			bit: 11,
			viewer: true,
			unlevelled: true
		} );

		// 6B3920 clears the viewer when GidObjectRegistry_FindEntityByGid returns 0.
		state.entities = state.entities.filter( ( /** @type {any} */ e ) => e.gid !== 77 );
		frame = fixture.ui.step( state, 2000 ) ?? frame;
		assert.equal( frame.controls.some( c => c.id.startsWith( "pet-buff:" ) ), false );
	} finally {
		fixture.dispose();
	}
});
