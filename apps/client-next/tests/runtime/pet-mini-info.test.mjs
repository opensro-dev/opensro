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
});

test("the HP caution blinks while 0 < HP <= 30%", () => {
	const caution = hp => petMiniInfo( [ { ...pet, hp } ], references )?.caution;
	assert.equal( caution( 300 ), true, "exactly 30% blinks" );
	assert.equal( caution( 301 ), false );
	assert.equal( caution( 1 ), true );
	assert.equal( caution( 0 ), false, "a dead pet does not blink" );
});
