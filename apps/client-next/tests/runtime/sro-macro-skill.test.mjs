/*
===========================================================================

sro-macro-skill.test.mjs - the SRO Macro's auto-skill loop

Port-only, not native (#3907): the v1.696 buff and attack steps
(00686bc0, 00687520), their cursors and cast re-arm, the Weapon / Shield
bag binds (tags 3/4, 0068ede0), and the worker running the loop only with
the Experimental row on and the server's Auto-Skills ON.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { macroReplay } from "../helpers/sro-macro-frames.mjs";

const loop = await import( "../../src/engine/foundation/gameplay/sro-macro-skill.ts" );
const codec = await import( "../../src/engine/foundation/gameplay/sro-macro.ts" );
const { createSroMacroHud } = await import( "../../src/engine/runtime/ui/hud/sro-macro-hud.ts" );
const { experimentalGameplay, experimentalOptions } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const BUFF = 10;
const ATTACK = 11;
const SKILL_PRESS_OPCODE = 0x72cd;
const ITEM_MOVE_OPCODE = 0x706d;
const LOCAL_GID = 1;
const MONSTER_GID = 2;
const WEAPON_REF = 50;
const OTHER_WEAPON_REF = 52;
// TID4 3 sword-class; bare hand counts as TID4 1, so a skill that wants 3
// refuses until this bag weapon is equipped.
const WEAPON_TYPE = 0x2c | (6 << 7) | (3 << 11);
const OTHER_WEAPON_TYPE = 0x2c | (6 << 7) | (2 << 11);
const SHIELD_TYPE = 0x2c | (4 << 7) | (1 << 11);
const CONSUMABLE_TYPE = 0x6c | (1 << 7);
const SWORD_ADMIT = {
	weaponKinds: [ 3, 3 ],
	reqi: undefined
};

/*
================
lists
================
*/
function lists( buff = [ { slot: 0, skill: BUFF } ], attack = [ { slot: 0, skill: ATTACK } ] ) {
	return { buff, attack };
}

test("the buff step walks one entry a tick and rewinds before the attack step runs", () => {
	let state = loop.emptyMacroSkillLoop();
	const skills = lists( [ { slot: 0, skill: BUFF }, { slot: 1, skill: BUFF + 1 } ], [ { slot: 0, skill: ATTACK } ] );

	let pick = loop.pickMacroSkill( state, skills, 0, true );
	assert.ok( "entry" in pick );
	assert.equal( pick.list, "buff" );
	assert.equal( pick.entry.skill, BUFF );
	state = pick.loop;

	pick = loop.pickMacroSkill( state, skills, 0, true );
	assert.ok( "entry" in pick );
	assert.equal( pick.list, "buff" );
	assert.equal( pick.entry.skill, BUFF + 1 );
	state = pick.loop;

	pick = loop.pickMacroSkill( state, skills, 0, true );
	assert.ok( "entry" in pick );
	assert.equal( pick.list, "attack" );
	assert.equal( pick.entry.skill, ATTACK );
	assert.equal( pick.loop.buff, 0, "a finished buff pass rewinds" );
});

test("without a living monster the attack step passes and keeps its cursor", () => {
	let state = loop.emptyMacroSkillLoop();
	const skills = lists( [], [ { slot: 0, skill: ATTACK } ] );
	const idle = loop.pickMacroSkill( state, skills, 0, false );
	assert.equal( "entry" in idle, false );
	assert.equal( idle.loop.attack, 0 );

	state = loop.macroSkillCast( idle.loop, 0, 100 );
	assert.equal( "entry" in loop.pickMacroSkill( state, skills, 499, true ), false, "re-arm holds at least 500 ms" );
	const ready = loop.pickMacroSkill( state, skills, 500, true );
	assert.ok( "entry" in ready );
	assert.equal( ready.entry.skill, ATTACK );
});

test("a cast arms the loop for at least 500 ms", () => {
	const armed = loop.macroSkillCast( loop.emptyMacroSkillLoop(), 1000, 200 );
	assert.equal( armed.readyAt, 1500 );
	const long = loop.macroSkillCast( loop.emptyMacroSkillLoop(), 1000, 900 );
	assert.equal( long.readyAt, 1900 );
});

/*
================
skillRef
================
*/
/**
 * @param {number} id
 * @param {{ targetRequired?: boolean, actionMs?: number, group?: number, admit?: object }} [over]
 */
function skillRef( id, over = {} ) {
	return {
		id,
		group: over.group ?? id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `SKILL_${id}`,
			trainable: true,
			spCost: 1,
			targetRequired: over.targetRequired === true,
			cooldownMs: 0,
			actionMs: over.actionMs ?? 600,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			...(over.admit ? { admit: over.admit } : {})
		}
	};
}

/*
================
weaponBody
================
*/
/** @param {number} id */
function weaponBody( id ) {
	const p = Buffer.alloc( 18 );
	p.writeUInt32LE( id, 0 );
	p.writeUInt32LE( 30, 13 );
	return [ ...p ];
}

/*
================
u32le
================
*/
/** @param {number} n */
function u32le( n ) {
	return [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
}

/*
================
macroWorld

A world with one buff and one attack skill learned, Auto-Skills ON when
asked, and an optional living monster already selected.
================
*/
/**
 * @param {{ row: boolean, skillOn: number, selectMonster?: boolean }} setup
 */
function macroWorld( setup ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const entities = new Map();
	const local = {
		gid: LOCAL_GID,
		refObjId: 1907,
		kind: /** @type {const} */ ("local-player"),
		name: "macro",
		countryByte9c: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	const monster = {
		gid: MONSTER_GID,
		refObjId: 2000,
		kind: /** @type {const} */ ("monster"),
		name: "mob",
		regionId: 257,
		x: 10,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	entities.set( LOCAL_GID, local );
	entities.set( MONSTER_GID, monster );
	const g = createGameplay( f => sent.push( f ), () => {}, () => {}, gid => entities.get( gid ) );
	g.bootstrap( {
		character: {
			hp: 100,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			skills: [ BUFF, ATTACK ],
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refSkillSnapshot: [
			skillRef( BUFF, { actionMs: 100 } ),
			skillRef( ATTACK, { targetRequired: true, actionMs: 100 } )
		]
	} );
	g.worldReady();
	// seed's last entity owns localGid; the monster must not overwrite it.
	g.seed( monster );
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: setup.row } ) ) );
	g.receive(
		macroReplay( {
			skillOn: setup.skillOn,
			buff: [ { slot: 0, skill: BUFF } ],
			attack: [ { slot: 0, skill: ATTACK } ]
		} ),
		0
	);
	if ( setup.selectMonster ) {
		// A select is live only once 0xB45A grants it (skill-press.test.mjs).
		g.command( { kind: "select", gid: MONSTER_GID }, 0, monster, local );
		const grant = new Uint8Array( 11 );
		grant[0] = 1;
		new DataView( grant.buffer ).setUint32( 1, MONSTER_GID, true );
		g.receive( { opcode: 0xb45a, payload: grant }, 0 );
		sent.length = 0;
	}
	return { sent, g, local, monster };
}

test("the worker casts the buff only with the row on and Auto-Skills ON", () => {
	for (
		const [setup, casts] of /** @type {const} */ ([
			[ { row: true, skillOn: 1 }, 1 ],
			[ { row: false, skillOn: 1 }, 0 ],
			[ { row: true, skillOn: 0 }, 0 ]
		])
	) {
		const { sent, g, local } = macroWorld( setup );
		g.step( 0, local );
		const presses = sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE );
		assert.equal( presses.length, casts, JSON.stringify( setup ) );
		if ( casts ) {
			const view = new DataView(
				presses[0].payload.buffer,
				presses[0].payload.byteOffset,
				presses[0].payload.byteLength
			);
			assert.equal( view.getUint32( 2, true ), BUFF );
		}
		g.dispose();
	}
});

test("after the buff list finishes, a selected monster receives the attack skill", () => {
	const { sent, g, local } = macroWorld( { row: true, skillOn: 1, selectMonster: true } );
	g.step( 0, local );
	assert.equal( sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE ).length, 1, "buff first" );
	g.step( 500, local );
	const presses = sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE );
	assert.equal( presses.length, 2, "attack after the buff re-arm" );
	const view = new DataView(
		presses[1].payload.buffer,
		presses[1].payload.byteOffset,
		presses[1].payload.byteLength
	);
	assert.equal( view.getUint32( 2, true ), ATTACK );
	g.dispose();
});

test("a clicked monster is enough for the attack step, even before its select grant", () => {
	const { sent, g, local, monster } = macroWorld( { row: true, skillOn: 1 } );
	// Attack-only list so the buff step does not consume the tick.
	g.receive( macroReplay( { skillOn: 1, buff: [], attack: [ { slot: 0, skill: ATTACK } ] } ), 0 );
	// Pending select only: no 0xB45A. selectionIntent still names the click.
	g.command( { kind: "select", gid: MONSTER_GID }, 0, monster, local );
	sent.length = 0;
	g.step( 0, local );
	const presses = sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE );
	assert.equal( presses.length, 1, "attack fires from selectionIntent" );
	const view = new DataView(
		presses[0].payload.buffer,
		presses[0].payload.byteOffset,
		presses[0].payload.byteLength
	);
	assert.equal( view.getUint32( 2, true ), ATTACK );
	g.dispose();
});

test("macroBuffAlreadyOn matches exact id, skill group, and replace group", () => {
	const replace = { category: 1, group: 40, rank: 1, states: 0, ovl2: 0, cbuf: false, undecided: false };
	const catalog = [
		{ id: BUFF, group: 100, replace },
		{ id: BUFF + 1, group: 100, replace },
		{ id: BUFF + 2, group: 200, replace: { ...replace, group: 41 } }
	];
	assert.equal( loop.macroBuffAlreadyOn( catalog[0], [ { skill: BUFF } ], catalog ), true );
	assert.equal(
		loop.macroBuffAlreadyOn( catalog[0], [ { skill: BUFF + 1 } ], catalog ),
		true,
		"same skilldata group"
	);
	assert.equal(
		loop.macroBuffAlreadyOn( catalog[0], [ { skill: BUFF + 2 } ], catalog ),
		false,
		"other replace group"
	);
	assert.equal(
		loop.macroBuffAlreadyOn(
			{ id: BUFF, group: 999, replace },
			[ { skill: BUFF + 2 } ],
			[ { id: BUFF, group: 999, replace }, { id: BUFF + 2, group: 200, replace } ]
		),
		true,
		"same SkillUiReplace group across different skill ids"
	);
	assert.equal( loop.macroBuffAlreadyOn( catalog[0], [], catalog ), false );
});

test("an already-applied buff is not re-pressed after a monster is selected", () => {
	const { sent, g, local, monster } = macroWorld( { row: true, skillOn: 1, selectMonster: true } );
	g.receive(
		{ opcode: 0xb419, payload: Uint8Array.from( [ ...u32le( LOCAL_GID ), ...u32le( BUFF ), ...u32le( 77 ) ] ) },
		0
	);
	sent.length = 0;
	for ( let t = 0; t <= 4000; t += 100 ) g.step( t, local );
	const presses = sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE );
	assert.ok( presses.length >= 1, "attack still fires" );
	for ( const frame of presses ) {
		const view = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength );
		assert.equal( view.getUint32( 2, true ), ATTACK, "the live buff must not be re-pressed" );
	}
	g.dispose();
});

test("a buff whose attached effect is a sibling skill id is not re-pressed", () => {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const entities = new Map();
	const local = {
		gid: LOCAL_GID,
		refObjId: 1907,
		kind: /** @type {const} */ ("local-player"),
		name: "macro",
		countryByte9c: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	const monster = {
		gid: MONSTER_GID,
		refObjId: 2000,
		kind: /** @type {const} */ ("monster"),
		name: "mob",
		regionId: 257,
		x: 10,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	entities.set( LOCAL_GID, local );
	entities.set( MONSTER_GID, monster );
	const sibling = BUFF + 5;
	const g = createGameplay( f => sent.push( f ), () => {}, () => {}, gid => entities.get( gid ) );
	g.bootstrap( {
		character: {
			hp: 100,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			skills: [ BUFF, sibling, ATTACK ],
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refSkillSnapshot: [
			skillRef( BUFF, { actionMs: 100, group: 100 } ),
			skillRef( sibling, { actionMs: 100, group: 100 } ),
			skillRef( ATTACK, { targetRequired: true, actionMs: 100 } )
		]
	} );
	g.worldReady();
	g.seed( monster );
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: true } ) ) );
	g.receive(
		macroReplay( {
			skillOn: 1,
			buff: [ { slot: 0, skill: BUFF } ],
			attack: [ { slot: 0, skill: ATTACK } ]
		} ),
		0
	);
	g.receive(
		{ opcode: 0xb419, payload: Uint8Array.from( [ ...u32le( LOCAL_GID ), ...u32le( sibling ), ...u32le( 88 ) ] ) },
		0
	);
	g.command( { kind: "select", gid: MONSTER_GID }, 0, monster, local );
	const grant = new Uint8Array( 11 );
	grant[0] = 1;
	new DataView( grant.buffer ).setUint32( 1, MONSTER_GID, true );
	g.receive( { opcode: 0xb45a, payload: grant }, 0 );
	sent.length = 0;
	for ( let t = 0; t <= 4000; t += 100 ) g.step( t, local );
	const presses = sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE );
	assert.ok( presses.length >= 1, "attack still fires" );
	for ( const frame of presses ) {
		const view = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength );
		assert.equal( view.getUint32( 2, true ), ATTACK, "sibling-group buff must not be re-pressed" );
	}
	g.dispose();
});

test("Weapon admits socket-6 gear and Shield admits socket-7; potions never bind", () => {
	assert.equal( loop.macroGearAdmits( "weapon", WEAPON_TYPE ), true );
	assert.equal( loop.macroGearAdmits( "weapon", SHIELD_TYPE ), false );
	assert.equal( loop.macroGearAdmits( "shield", SHIELD_TYPE ), true );
	assert.equal( loop.macroGearAdmits( "shield", WEAPON_TYPE ), false );
	assert.equal( loop.macroGearAdmits( "weapon", CONSUMABLE_TYPE ), false );
});

test("macroSkillWantSwap equips the bound bag weapon then the shield", () => {
	const flags = codec.withMacroGearSlot(
		codec.withMacroGearSlot( codec.defaultMacroSettings().skillFlags, "weapon", 14 ),
		"shield",
		15
	);
	assert.deepEqual(
		loop.macroSkillWantSwap( flags, [
			{ slot: 14, typeFlags: WEAPON_TYPE },
			{ slot: 15, typeFlags: SHIELD_TYPE }
		] ),
		{ kind: "inventory-move", source: 14, destination: 6, quantity: 0, activation: true }
	);
	assert.deepEqual(
		loop.macroSkillWantSwap( flags, [ { slot: 15, typeFlags: SHIELD_TYPE } ] ),
		{ kind: "inventory-move", source: 15, destination: 7, quantity: 0, activation: true }
	);
	assert.equal( loop.macroSkillWantSwap( flags, [] ), null );
	assert.equal(
		loop.macroSkillWantSwap( [ 0, 0, 5, 0 ], [ { slot: 5, typeFlags: WEAPON_TYPE } ] ),
		null,
		"equipment sockets are outside the 0x0d..0x6f bind range"
	);
});

test("scrubMacroGear clears an empty or wrong-class bag bind", () => {
	const base = {
		...codec.defaultMacroSettings(),
		skillFlags: codec.withMacroGearSlot( codec.defaultMacroSettings().skillFlags, "weapon", 14 )
	};
	assert.deepEqual( loop.scrubMacroGear( base, [ { slot: 14, typeFlags: WEAPON_TYPE } ] ), base );
	assert.deepEqual(
		loop.scrubMacroGear( base, [ { slot: 14, typeFlags: CONSUMABLE_TYPE } ] ).skillFlags[2],
		0
	);
	assert.deepEqual( loop.scrubMacroGear( base, [] ).skillFlags[2], 0 );
});

test("the Auto-Skills tab binds and clears Weapon / Shield bag slots", () => {
	const live = { ...codec.defaultMacroSettings(), available: 7 };
	const hud = createSroMacroHud();
	hud.click( "sro-macro:options:1", live );
	assert.equal(
		hud.bindGear( "sro-macro:gear:weapon", { slot: 14, typeFlags: WEAPON_TYPE } ),
		true
	);
	assert.equal( hud.gearBagSlot( "sro-macro:gear:weapon" ), 14 );
	assert.equal(
		hud.bindGear( "sro-macro:gear:weapon", { slot: 14, typeFlags: SHIELD_TYPE } ),
		false
	);
	assert.equal(
		hud.bindGear( "sro-macro:gear:shield", { slot: 15, typeFlags: SHIELD_TYPE } ),
		true
	);
	assert.equal( hud.bindGear( "sro-macro:gear:shield", { slot: 6, typeFlags: SHIELD_TYPE } ), false );
	assert.equal( hud.clearGear( "sro-macro:gear:weapon" ), true );
	assert.equal( hud.gearBagSlot( "sro-macro:gear:weapon" ), 0 );
	assert.equal( hud.isGearItemSlot( "sro-macro:gear:shield" ), true );
	assert.equal( hud.isGearItemSlot( "sro-macro:item:0" ), false );
});

test("macroSkillSwapHelps only for wrong-weapon refusals", () => {
	assert.equal( loop.macroSkillSwapHelps( 0x300d ), true );
	assert.equal( loop.macroSkillSwapHelps( 0x3004 ), false );
});

test("a wrong-weapon attack equips the bound bag weapon once, then casts without oscillating", () => {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const entities = new Map();
	const local = {
		gid: LOCAL_GID,
		refObjId: 1907,
		kind: /** @type {const} */ ("local-player"),
		name: "macro",
		countryByte9c: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	const monster = {
		gid: MONSTER_GID,
		refObjId: 2000,
		kind: /** @type {const} */ ("monster"),
		name: "mob",
		regionId: 257,
		x: 10,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	entities.set( LOCAL_GID, local );
	entities.set( MONSTER_GID, monster );
	const g = createGameplay( f => sent.push( f ), () => {}, () => {}, gid => entities.get( gid ) );
	g.bootstrap( {
		character: {
			hp: 100,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			skills: [ ATTACK ],
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refSkillSnapshot: [
			skillRef( ATTACK, { targetRequired: true, actionMs: 100, admit: SWORD_ADMIT } )
		],
		refItemSnapshot: [
			{ refObjId: WEAPON_REF, typeFlags: WEAPON_TYPE, nativeFields: {} },
			{ refObjId: OTHER_WEAPON_REF, typeFlags: OTHER_WEAPON_TYPE, nativeFields: {} }
		],
		// Wrong weapon worn; the bound bag slot holds the skill's sword.
		equipItems: [
			{ slot: 6, refObjId: OTHER_WEAPON_REF, body: weaponBody( OTHER_WEAPON_REF ) },
			{ slot: 14, refObjId: WEAPON_REF, body: weaponBody( WEAPON_REF ) }
		]
	} );
	g.worldReady();
	g.seed( monster );
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: true } ) ) );
	g.receive(
		macroReplay( {
			skillOn: 1,
			skillFlags: [ 0, 0, 14, 0 ],
			buff: [],
			attack: [ { slot: 0, skill: ATTACK } ]
		} ),
		0
	);
	g.command( { kind: "select", gid: MONSTER_GID }, 0, monster, local );
	const grant = new Uint8Array( 11 );
	grant[0] = 1;
	new DataView( grant.buffer ).setUint32( 1, MONSTER_GID, true );
	g.receive( { opcode: 0xb45a, payload: grant }, 0 );
	sent.length = 0;
	g.step( 0, local );
	assert.equal( sent.filter( frame => frame.opcode === ITEM_MOVE_OPCODE ).length, 1, "one swap" );
	assert.equal( sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE ).length, 0 );
	// Equip confirmed: sword on socket 6, previous weapon back in the bag slot.
	g.receive( { opcode: 0xb06d, payload: Uint8Array.of( 1, 0, 14, 6, 0, 0, 0 ) }, 1 );
	sent.length = 0;
	g.step( 1999, local );
	assert.equal( sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE ).length, 0, "2000 ms re-arm" );
	g.step( 2000, local );
	assert.equal( sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE ).length, 1, "cast after swap" );
	assert.equal( sent.filter( frame => frame.opcode === ITEM_MOVE_OPCODE ).length, 0, "no second swap" );
	sent.length = 0;
	g.step( 2500, local );
	assert.equal( sent.filter( frame => frame.opcode === ITEM_MOVE_OPCODE ).length, 0, "still no oscillation" );
	g.dispose();
});

test("a bound weapon is left alone when the worn weapon already admits the skill", () => {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const entities = new Map();
	const local = {
		gid: LOCAL_GID,
		refObjId: 1907,
		kind: /** @type {const} */ ("local-player"),
		name: "macro",
		countryByte9c: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	const monster = {
		gid: MONSTER_GID,
		refObjId: 2000,
		kind: /** @type {const} */ ("monster"),
		name: "mob",
		regionId: 257,
		x: 10,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	entities.set( LOCAL_GID, local );
	entities.set( MONSTER_GID, monster );
	const g = createGameplay( f => sent.push( f ), () => {}, () => {}, gid => entities.get( gid ) );
	g.bootstrap( {
		character: {
			hp: 100,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			skills: [ ATTACK ],
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refSkillSnapshot: [
			skillRef( ATTACK, { targetRequired: true, actionMs: 100, admit: SWORD_ADMIT } )
		],
		refItemSnapshot: [
			{ refObjId: WEAPON_REF, typeFlags: WEAPON_TYPE, nativeFields: {} },
			{ refObjId: OTHER_WEAPON_REF, typeFlags: OTHER_WEAPON_TYPE, nativeFields: {} }
		],
		equipItems: [
			{ slot: 6, refObjId: WEAPON_REF, body: weaponBody( WEAPON_REF ) },
			{ slot: 14, refObjId: OTHER_WEAPON_REF, body: weaponBody( OTHER_WEAPON_REF ) }
		]
	} );
	g.worldReady();
	g.seed( monster );
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: true } ) ) );
	g.receive(
		macroReplay( {
			skillOn: 1,
			skillFlags: [ 0, 0, 14, 0 ],
			buff: [],
			attack: [ { slot: 0, skill: ATTACK } ]
		} ),
		0
	);
	g.command( { kind: "select", gid: MONSTER_GID }, 0, monster, local );
	const grant = new Uint8Array( 11 );
	grant[0] = 1;
	new DataView( grant.buffer ).setUint32( 1, MONSTER_GID, true );
	g.receive( { opcode: 0xb45a, payload: grant }, 0 );
	sent.length = 0;
	g.step( 0, local );
	assert.equal( sent.filter( frame => frame.opcode === ITEM_MOVE_OPCODE ).length, 0, "no swap" );
	assert.equal( sent.filter( frame => frame.opcode === SKILL_PRESS_OPCODE ).length, 1, "cast with worn weapon" );
	g.dispose();
});
