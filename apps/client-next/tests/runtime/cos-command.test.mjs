/*
===========================================================================

cos-command.test.mjs - the native COS HUD rules per record class

Pins the CIFCOSManager branches cos-command.ts ports: 830EC0's band to class
map, 6A3DF0's button sets, 6A1BE0's enabled arms and icons, 6A2350's stance
toggle and the info page text formats.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const cos = await import( "../../src/engine/foundation/ui/cos-command.ts" );

const HORSE = { gid: 7, refObjId: 2191, band: 1, hp: 500, mp: 0, status: 0, dead: false };
const HORSE_REF = {
	icon: "cos\\cos_c_horse1.ddj",
	maxHp: 983,
	rideable: false,
	physicalDefence: 0,
	magicalDefence: 0,
	parry: 65,
	hit: 65,
	skills: []
};
const TRANSPORT = { gid: 8, refObjId: 3914, band: 2, hp: 100, mp: 0, status: 0, dead: false };
const TRANSPORT_REF = { icon: "cos\\cos_t_dhorse3.ddj", maxHp: 87829, rideable: true };
const PET = {
	gid: 9,
	refObjId: 100,
	band: 3,
	hp: 50,
	mp: 0,
	status: 0,
	dead: false,
	satiety: 2500,
	commandMode: 1
};

/*
================
context
================
*/
function context( record, reference, extra = {} ) {
	return { record, reference, ownerDead: false, mounted: false, ...extra };
}

test("830EC0 maps bands to record classes; quest companions retain default class zero", () => {
	assert.deepEqual( [ 1, 2, 3, 4, 5, 6 ].map( cos.cosClass ), [ 0, 1, 3, 2, 4, 0 ] );
});

test("6A3DF0 builds each class's command row in native order", () => {
	assert.deepEqual( cos.cosCommandButtons( 0 ), [ 0, 1, 5 ] );
	assert.deepEqual( cos.cosCommandButtons( 1 ), [ 0, 1, 5 ] );
	assert.deepEqual( cos.cosCommandButtons( 2 ), [ 0, 1, 4, 3 ] );
	assert.deepEqual( cos.cosCommandButtons( 3 ), [ 0, 1, 4, 3, 2, 6 ] );
	assert.deepEqual( cos.cosCommandButtons( 4 ), [ 5 ] );
});

test("a riding horse cannot be boarded again and leaves through Clean", () => {
	const horse = context( HORSE, HORSE_REF, { mounted: true } );
	assert.equal( cos.cosCommandEnabled( 1, horse ), false );
	assert.equal(
		cos.cosCommandIcon( 1, horse ),
		"/assets/images/Media_extracted/icon/action/cos_cmd_embark_disable.png"
	);
	assert.equal( cos.cosCommandEnabled( 5, horse ), true );
	assert.equal(
		cos.cosCommandIcon( 5, horse ),
		"/assets/images/Media_extracted/icon/action/cos_cmd_ai_destruction.png"
	);
	assert.equal( cos.cosCommandLabel( 5, horse ), "UIIT_STT_COS_CLEAN" );
});

test("a transport toggles boarding by the rider's state", () => {
	assert.equal(
		cos.cosCommandIcon( 1, context( TRANSPORT, TRANSPORT_REF ) ),
		"/assets/images/Media_extracted/icon/action/cos_cmd_embark.png"
	);
	assert.equal(
		cos.cosCommandIcon( 1, context( TRANSPORT, TRANSPORT_REF, { mounted: true } ) ),
		"/assets/images/Media_extracted/icon/action/cos_cmd_disembark.png"
	);
	assert.equal(
		cos.cosCommandLabel( 1, context( TRANSPORT, TRANSPORT_REF, { mounted: true } ) ),
		"UIIT_STT_COS_DISEMBARK"
	);
});

test("a dead owner keeps only Info", () => {
	const dead = context( PET, undefined, { ownerDead: true } );
	assert.deepEqual( cos.cosCommandButtons( 3 ).map( c => cos.cosCommandEnabled( c, dead ) ), [
		true,
		false,
		false,
		false,
		false,
		false
	] );
});

test("the attack pet's stance shows and toggles its command mode", () => {
	assert.equal(
		cos.cosCommandIcon( 6, context( PET ) ),
		"/assets/images/Media_extracted/icon/action/cos_cmd_aggressive.png"
	);
	assert.equal(
		cos.cosCommandIcon( 6, context( { ...PET, commandMode: 0 } ) ),
		"/assets/images/Media_extracted/icon/action/cos_cmd_defensive.png"
	);
	assert.deepEqual( [ 0, 1, 7, undefined ].map( cos.cosStanceToggle ), [ 1, 0, 1, 1 ] );
});

test("status chrome and gauges follow 6AA290 and 6A9C50", () => {
	assert.deepEqual( cos.cosStatusChrome( 3 ).size, [ 44, 68 ] );
	assert.equal( cos.cosStatusChrome( 3 ).showHgp, true );
	assert.deepEqual( cos.cosStatusChrome( 0 ).size, [ 44, 56 ] );
	assert.equal( cos.cosStatusChrome( 4 ).showHp, false );
	assert.deepEqual( cos.cosStatusRect( 1024, 1, 0 ), [ 1024 - 172 - 48, 2, 44, 56 ] );
	assert.deepEqual( cos.cosStatusRatios( PET, { ...HORSE_REF, maxHp: 200 } ), { hp: 0.25, hgp: 0.25 } );
	assert.deepEqual( cos.cosStatusRatios( HORSE, undefined ), { hp: null, hgp: null } );
});

test("the command row lays out leftward from the under bar", () => {
	const layout = cos.cosCommandLayout( 100, 700, 3 );
	assert.deepEqual( layout.rows.map( r => r.at ), [ [ 660, 666 ], [ 691, 666 ], [ 722, 666 ] ] );
	assert.deepEqual( layout.rows[0].slot, [ 664, 671, 32, 32 ] );
	assert.ok( layout.rows[0].frame.endsWith( "am_ctrl_window_front.png" ) );
	assert.ok( layout.rows[2].frame.endsWith( "am_ctrl_window_end.png" ) );
	assert.deepEqual( layout.toggle, [ 758, 682 ] );
});

test("info page texts use the native formats", () => {
	assert.equal( cos.cosHpText( HORSE, HORSE_REF ), "500/983 (50%)" );
	assert.equal( cos.cosHpText( HORSE, undefined ), "100%" );
	// 1 - ftol(gauge * -100): one above the truncated percent, capped at 100.
	assert.equal( cos.cosSatietyText( 2500 ), "26% (2500)" );
	assert.equal( cos.cosSatietyText( 10000 ), "100% (10000)" );
	assert.equal( cos.cosSatietyText( 0 ), "1% (0)" );
	assert.equal( cos.cosExperienceText( 50n, 200n ).text, "50/200 (25.00%)" );
	assert.equal( cos.cosExperienceText( 200n, 200n ).text, "200/200 (99.99%)" );
	assert.equal( cos.cosRentText( 90061000, "Day", "Hour", "Minute" ), "1Day 1Hour 1Minute" );
	assert.equal( cos.cosRentText( 0, "Day", "Hour", "Minute" ), "0Day 0Hour 0Minute" );
	assert.deepEqual( cos.cosInfoSections( 2 ), { hp: false, rentTime: true, growth: false } );
	assert.deepEqual( cos.cosInfoSections( 3 ), { hp: true, rentTime: false, growth: true } );
});

test("the presentation catalog rejects malformed rows", () => {
	const references = cos.decodeCosReferences( {
		format: "sro-cos-presentation",
		rows: { 2191: [ "cos\\cos_c_horse1.ddj", 983, false, 0, 0, 65, 65, [] ] }
	} );
	assert.deepEqual( references.get( 2191 ), HORSE_REF );
	assert.throws( () =>
		cos.decodeCosReferences( { format: "sro-cos-presentation", rows: { 1: [ "x", -1, false ] } } )
	);
	assert.throws( () => cos.decodeCosReferences( { format: "other", rows: {} } ) );
});

test("8301A0 derives the attack pet's abilities from its reference", () => {
	const wolf = {
		icon: "",
		maxHp: 6858,
		rideable: false,
		physicalDefence: 199,
		magicalDefence: 318,
		parry: 105,
		hit: 107,
		skills: [ 1, 2, 3 ]
	};
	const blocks = new Map( [
		[ 1, { flags: 4, minimum: 40, maximum: 61 } ],
		[ 2, { flags: 8, minimum: 30, maximum: 45 } ],
		[ 3, { flags: 4, minimum: 99, maximum: 99 } ]
	] );
	const fed = cos.cosAbilities( { ...PET, satiety: 3001 }, wolf, [ 1, 2, 3 ].map( skill => blocks.get( skill ) ) );
	assert.deepEqual( fed, {
		low: false,
		physical: [ 40, 61 ],
		magical: [ 30, 45 ],
		physicalDefence: 199,
		magicalDefence: 318,
		hit: 107,
		parry: 105
	}, "the walk stops once both kinds are set" );
	const hungry = cos.cosAbilities( { ...PET, satiety: 3000 }, wolf, [ 1, 2, 3 ].map( skill => blocks.get( skill ) ) );
	assert.deepEqual( [ hungry.low, hungry.physical, hungry.magical, hungry.physicalDefence, hungry.hit ], [
		true,
		[ 20, 30 ],
		[ 15, 22 ],
		99,
		53
	] );
	const physicalOnly = cos.cosAbilities(
		PET,
		{ ...wolf, skills: [ 1, 3 ] },
		[ 1, 3 ].map( skill => blocks.get( skill ) )
	);
	assert.deepEqual( physicalOnly.physical, [ 49, 49 ], "a later block of the same kind overwrites" );
	assert.equal( cos.cosAttackText( [ 0, 0 ] ), "0" );
	assert.equal( cos.cosAttackText( [ 40, 61 ] ), "40 ~ 61" );
});
