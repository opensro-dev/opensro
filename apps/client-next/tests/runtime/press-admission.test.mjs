/*
===========================================================================

press-admission.test.mjs - the local press replays the server's 58D8F0

One row per gate the client can read: the gate's native refusal, and a
control that differs only in the state the gate reads, so a removed or
inverted gate fails its row. Rows the server alone can judge are unknown.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { pressAdmission, parsePressAdmit, equipmentBroken } = await import(
	"../../src/engine/foundation/gameplay/press-admission.ts"
);
const { skillCatalog } = await import( "../../src/engine/foundation/gameplay/skill-catalog.ts" );

const ANY = [ 255, 255 ];
// Item TIDs as typeFlags: TID1 bits 2..4, TID2 5..6, TID3 7..10, TID4 11..15.
const tid = ( t1, t2, t3, t4 ) => t1 << 2 | t2 << 5 | t3 << 7 | t4 << 11;
const WEAPON = 6, SWORD = 2, BOW = 6, SHIELD_KIND = 1;
const sword = { typeFlags: tid( 3, 1, WEAPON, SWORD ), durability: 30, quantity: 1 };
const bow = { typeFlags: tid( 3, 1, WEAPON, BOW ), durability: 30, quantity: 1 };
const arrows = { typeFlags: tid( 3, 3, 4, 1 ), durability: 0, quantity: 50 };
const bolts = { typeFlags: tid( 3, 3, 4, 2 ), durability: 0, quantity: 50 };
const shield = { typeFlags: tid( 3, 1, 4, SHIELD_KIND ), durability: 30, quantity: 1 };
// Armour family 1 (TID3 1), head piece (index 1, socket 0).
const helm = { typeFlags: tid( 3, 1, 1, 1 ), durability: 30, quantity: 1 };

/*
================
caster
================
*/
function caster( overrides = {}, sockets = {} ) {
	return {
		abnormal: 0,
		hp: 1000,
		maxHp: 1000,
		mp: 500,
		maxMp: 500,
		body: 0,
		mounted: false,
		seated: false,
		equipped: Object.entries( sockets ).map( ( [slot, item] ) => ({ ...item, slot: Number( slot ) }) ),
		...overrides
	};
}

/*
================
press
================
*/
function press( admit, extra = {} ) {
	return { admit: { weaponKinds: ANY, ...admit }, mpCost: 0, ...extra };
}

// [name, skill, refused caster, admitted control, native code]
/** @typedef {import("../../src/engine/foundation/gameplay/press-admission.ts").PressSkill} PressSkill */
/** @typedef {import("../../src/engine/foundation/gameplay/press-admission.ts").PressCaster} PressCaster */
/** @type {[string, PressSkill, PressCaster, PressCaster, number][]} */
const GATES = [
	[ "stunned", press( {} ), caster( { abnormal: 0x4000 } ), caster(), 0x3009 ],
	[ "frozen", press( {} ), caster( { abnormal: 0x1 } ), caster( { abnormal: 0x2 } ), 0x3009 ],
	[ "asleep", press( {} ), caster( { abnormal: 0x40 } ), caster(), 0x3009 ],
	[ "berserk hide", press( { berserk: true } ), caster( { body: 1 } ), caster( { body: 6 } ), 0x3031 ],
	[ "above 30 % HP", press( { lowHp: true } ), caster( { hp: 301 } ), caster( { hp: 300 } ), 0x3036 ],
	[ "not stealthed", press( { stealthStrike: true } ), caster( { body: 0 } ), caster( { body: 6 } ), 0x3034 ],
	[ "rooted teleport", press( { teleports: true } ), caster( { abnormal: 0x80 } ), caster(), 0x3009 ],
	[ "riding ao/pw", press( {}, { needsFooting: true } ), caster( { mounted: true } ), caster(), 0x3009 ],
	[ "seated ao/pw", press( {}, { needsFooting: true } ), caster( { seated: true } ), caster(), 0x3009 ],
	[
		"wrong weapon",
		press( { weaponKinds: [ BOW, BOW ] } ),
		caster( {}, { 6: sword } ),
		caster( {}, { 6: bow, 7: arrows } ),
		0x300d
	],
	[ "bare hand", press( { weaponKinds: [ SWORD, SWORD ] } ), caster(), caster( {}, { 6: sword } ), 0x300d ],
	[
		"fortress weapon",
		press( { weaponKinds: [ 16, SWORD ] } ),
		caster( {}, { 6: { ...sword, typeFlags: tid( 3, 1, WEAPON, 16 ) } } ),
		caster( {}, { 6: sword } ),
		0x3047
	],
	[
		"broken weapon",
		press( { weaponKinds: [ SWORD, SWORD ] } ),
		caster( {}, { 6: { ...sword, durability: 0 } } ),
		caster( {}, { 6: sword } ),
		0x300f
	],
	[
		"reqi shield",
		press( { reqi: { all: false, pairs: [ [ 4, SHIELD_KIND ] ] } } ),
		caster( {}, { 6: sword } ),
		caster( {}, { 6: sword, 7: shield } ),
		0x300d
	],
	[
		"reqi broken shield",
		press( { reqi: { all: false, pairs: [ [ 4, SHIELD_KIND ] ] } } ),
		caster( {}, { 7: { ...shield, durability: 0 } } ),
		caster( {}, { 7: shield } ),
		0x300d
	],
	[
		"reqi armour set",
		press( { reqi: { all: false, pairs: [ [ 1, 0 ] ] } } ),
		caster( {}, { 0: { ...helm, typeFlags: tid( 3, 1, 2, 1 ) } } ),
		caster( {}, { 0: helm } ),
		0x300d
	],
	[ "HP cost", press( { hp: 200, hpPercent: 10 } ), caster( { hp: 299 } ), caster( { hp: 300 } ), 0x3013 ],
	[ "MP cost", press( {}, { mpCost: 120 } ), caster( { mp: 119 } ), caster( { mp: 120 } ), 0x3004 ],
	[
		"no arrows",
		press( { weaponKinds: [ BOW, BOW ], ammunition: true } ),
		caster( {}, { 6: bow, 7: bolts } ),
		caster( {}, { 6: bow, 7: arrows } ),
		0x300e
	],
	[
		"empty quiver",
		press( { weaponKinds: [ BOW, BOW ], ammunition: true } ),
		caster( {}, { 6: bow, 7: { ...arrows, quantity: 0 } } ),
		caster( {}, { 6: bow, 7: arrows } ),
		0x300e
	]
];

test("each caster gate refuses with its native code and admits its control", () => {
	for ( const [name, skill, refused, admitted, code] of GATES ) {
		assert.deepEqual( pressAdmission( skill, refused ), { kind: "refuse", code }, name );
		assert.deepEqual( pressAdmission( skill, admitted ), { kind: "admit" }, `${name} control` );
	}
});

test("every refusal the client classifies is produced by a gate row", () => {
	// Server refusals of 58D8F0 (skilladmit.go contextSkillAdmission), each
	// either replayed here or left to the server (unknown, never predicted).
	const replayed = [ 0x3009, 0x3031, 0x3036, 0x3034, 0x300d, 0x3047, 0x300f, 0x3013, 0x3004, 0x300e ];
	const serverOnly = {
		0x3028: "battle state (hide modes 1, 2)",
		0x3032: "dance selector",
		0x3038: "qest area",
		0x3039: "msch job suit",
		0x3010: "line of sight",
		0x3005: "cooldown (decidePress)",
		0x3006: "target (skillAdmitsPredictedTarget)"
	};
	const produced = new Set( GATES.map( row => row[4] ) );
	assert.deepEqual( [ ...produced ].sort(), [ ...replayed ].sort() );
	for ( const code of replayed ) assert.equal( serverOnly[code], undefined );
});

test("the frozen-asleep-stunned gate yields to nmf, and it runs first", () => {
	const stunned = caster( { abnormal: 0x4000, hp: 1 } );
	assert.deepEqual( pressAdmission( press( { nmf: true } ), stunned ), { kind: "admit" } );
	// Stun outranks the HP cost the same press would fail.
	assert.deepEqual( pressAdmission( press( { hp: 50 } ), stunned ), { kind: "refuse", code: 0x3009 } );
});

test("rows the server alone can judge are unknown, never predicted", () => {
	assert.equal( pressAdmission( { mpCost: 0 }, caster() ).kind, "unknown" );
	assert.equal( pressAdmission( press( { serverOnly: true } ), caster() ).kind, "unknown" );
	// The disabled gate precedes the server-only ones, as 58DA3A does.
	assert.deepEqual( pressAdmission( press( { serverOnly: true } ), caster( { abnormal: 1 } ) ), {
		kind: "refuse",
		code: 0x3009
	} );
});

test("low HP compares against the widened float 0.3", () => {
	// 1001 * 0.3f is 300.30000447; 300 HP is at or below it, 301 is above.
	const skill = press( { lowHp: true } );
	assert.equal( pressAdmission( skill, caster( { maxHp: 1001, hp: 300 } ) ).kind, "admit" );
	assert.equal( pressAdmission( skill, caster( { maxHp: 1001, hp: 301 } ) ).kind, "refuse" );
	// 10 * 0.3f is 3.0000001192: 3 HP is not above it.
	assert.equal( pressAdmission( skill, caster( { maxHp: 10, hp: 3 } ) ).kind, "admit" );
});

test("paid skills need known vitals while genuinely free skills need no maxima", () => {
	const unknown = caster( { maxHp: 0, hp: 0, maxMp: 0, mp: 0 } );
	assert.deepEqual( pressAdmission( press( {}, { mpCost: 120 } ), unknown ), {
		kind: "unknown",
		gate: "unknown MP"
	} );
	for ( const admit of [ { hp: 1 }, { hpPercent: 1 }, { lowHp: true } ] ) {
		assert.deepEqual( pressAdmission( press( admit ), unknown ), { kind: "unknown", gate: "unknown HP" } );
	}
	assert.deepEqual( pressAdmission( press( {} ), unknown ), { kind: "admit" } );
	assert.deepEqual( pressAdmission( press( {}, { mpCost: 120 } ), caster( { mp: 0 } ) ), {
		kind: "refuse",
		code: 0x3004
	} );
	assert.deepEqual( pressAdmission( press( { hp: 1 } ), caster( { hp: 0 } ) ), {
		kind: "refuse",
		code: 0x3013
	} );
	assert.deepEqual( pressAdmission( press( { hp: 1 } ), { ...unknown, abnormal: 0x4000 } ), {
		kind: "refuse",
		code: 0x3009
	}, "known earlier refusals retain their precedence" );
});

test("HP percentages match the server's signed crtFtol conversion", () => {
	// 7 % of 1015 is 71.05: the cost is 71.
	const skill = press( { hpPercent: 7 } );
	assert.equal( pressAdmission( skill, caster( { maxHp: 1015, hp: 71 } ) ).kind, "admit" );
	assert.equal( pressAdmission( skill, caster( { maxHp: 1015, hp: 70 } ) ).kind, "refuse" );
	const boundary = press( { hpPercent: 200 } );
	assert.deepEqual( pressAdmission( boundary, caster( { maxHp: 0x3fffffff, hp: 1 } ) ), {
		kind: "refuse",
		code: 0x3013
	} );
	assert.equal(
		pressAdmission( boundary, caster( { maxHp: 0x40000000, hp: 1 } ) ).kind,
		"admit",
		"2^31 converts to INT32_MIN, not a positive cost"
	);
	assert.equal( pressAdmission( press( { hpPercent: 65535 } ), caster( { maxHp: 4000000, hp: 1 } ) ).kind, "admit" );
	assert.deepEqual(
		pressAdmission( press( { hp: 0xffffffff, hpPercent: 200 } ), caster( { maxHp: 0x80000001, hp: 1 } ) ),
		{ kind: "refuse", code: 0x3013 },
		"a signed-input product below INT32_MIN also converts to INT32_MIN before flat cost is added"
	);
});

test("reqn needs every pair, and five matched pairs still fail (58D681)", () => {
	const sockets = { 6: sword, 7: shield };
	const pair = [ 6, SWORD ], shieldPair = [ 4, SHIELD_KIND ];
	const all = pairs => press( { reqi: { all: true, pairs } } );
	assert.equal( pressAdmission( all( [ pair, shieldPair ] ), caster( {}, sockets ) ).kind, "admit" );
	assert.deepEqual( pressAdmission( all( [ pair, shieldPair ] ), caster( {}, { 6: sword } ) ), {
		kind: "refuse",
		code: 0x300d
	} );
	assert.equal( pressAdmission( all( [ pair, pair, pair, pair, pair ] ), caster( {}, sockets ) ).kind, "refuse" );
	// Without reqn the first match ends the walk.
	const any = press( { reqi: { all: false, pairs: [ [ 6, BOW ], pair ] } } );
	assert.equal( pressAdmission( any, caster( {}, sockets ) ).kind, "admit" );
});

test("a broken item breaks unless it is exempt equipment (495980)", () => {
	assert.equal( equipmentBroken( sword.typeFlags, 0 ), true );
	assert.equal( equipmentBroken( sword.typeFlags, 1 ), false );
	// Equipment family 5 is exempt; the same family off the equipment TIDs is not.
	assert.equal( equipmentBroken( tid( 3, 1, 5, 1 ), 0 ), false );
	assert.equal( equipmentBroken( tid( 3, 2, 5, 1 ), 0 ), true );
});

test("the catalog parses admission inputs strictly", () => {
	assert.deepEqual( parsePressAdmit( { weaponKinds: [ 2, 255 ], lowHp: true, reqi: { pairs: [ [ 6, 2 ] ] } } ), {
		nmf: false,
		serverOnly: false,
		berserk: false,
		lowHp: true,
		stealthStrike: false,
		teleports: false,
		weaponKinds: [ 2, 255 ],
		reqi: { all: false, pairs: [ [ 6, 2 ] ] },
		hp: 0,
		hpPercent: 0,
		ammunition: false
	} );
	for (
		const bad of [
			{},
			{ weaponKinds: [ 2 ] },
			{ weaponKinds: [ 2, 256 ] },
			{ weaponKinds: ANY, nmf: 1 },
			{ weaponKinds: ANY, hp: -1 },
			{ weaponKinds: ANY, reqi: { pairs: [ [ 1 ] ] } },
			{ weaponKinds: ANY, reqi: { pairs: [ [ 6, undefined ] ] } },
			{ weaponKinds: ANY, reqi: { pairs: [ [ undefined, 0 ] ] } },
			{ weaponKinds: ANY, reqi: { pairs: [ [ 6, null ] ] } },
			{ weaponKinds: ANY, reqi: { pairs: new Array( 1 ) } },
			{ weaponKinds: ANY, reqi: { pairs: [ [ 6, 0x100000000 ] ] } },
			{ weaponKinds: ANY, reqi: { pairs: Array.from( { length: 6 }, () => [ 6, 2 ] ) } }
		]
	) {
		assert.throws( () => parsePressAdmit( bad ), Error, JSON.stringify( bad ) );
	}
	assert.deepEqual(
		parsePressAdmit( { weaponKinds: ANY, reqi: { pairs: [ [ 6, 0 ] ] } } ).reqi?.pairs,
		[ [ 6, 0 ] ],
		"an explicit zero remains a valid required operand"
	);
	const none = { ID: 0, Level: 0 };
	const row = admit => ({
		id: 9,
		group: 9,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: "SKILL_9",
			trainable: true,
			spCost: 1,
			targetRequired: false,
			cooldownMs: 0,
			masteries: [ none, none ],
			prerequisites: [ none, none, none ],
			...admit
		}
	});
	assert.equal( skillCatalog( { refSkillSnapshot: [ row( {} ) ] } )[0].admit, undefined );
	assert.deepEqual(
		skillCatalog( { refSkillSnapshot: [ row( { admit: { weaponKinds: ANY } } ) ] } )[0].admit?.weaponKinds,
		ANY
	);
	assert.throws( () => skillCatalog( { refSkillSnapshot: [ row( { admit: { weaponKinds: "any" } } ) ] } ) );
});
