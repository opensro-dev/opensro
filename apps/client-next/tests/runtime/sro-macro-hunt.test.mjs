/*
===========================================================================

sro-macro-hunt.test.mjs - the SRO Macro's auto-hunt decisions

Port-only, not native (#3907): range reach, return-timer hours, and the
survival / acquire action order from sro-macro-hunt.ts.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const hunt = await import( "../../src/engine/foundation/gameplay/sro-macro-hunt.ts" );
const codec = await import( "../../src/engine/foundation/gameplay/sro-macro.ts" );
const {
	HUNT_BYTE_RANGE,
	HUNT_BYTE_BERSERK,
	HUNT_BYTE_ENERGY,
	HUNT_BYTE_RESURRECT,
	HUNT_BYTE_RETURN,
	HUNT_BYTE_POTION_EMPTY,
	HUNT_BYTE_REPAIR,
	HUNT_BYTE_PICKUP,
	HUNT_BYTE_SPIRIT,
	HUNT_BYTE_PILLAR,
	HUNT_RANGE_IN_PLACE,
	HUNT_IN_PLACE_REACH,
	HUNT_HOUR_MS,
	HUNT_RESURRECT_SCROLL,
	HUNT_RESURRECT_TOWN,
	HUNT_RESURRECT_WAIT,
	HUNT_REPAIR_HAMMER,
	HUNT_REPAIR_TOWN,
	HUNT_BYTE_TIME_TOWN,
	HUNT_BERSERK_ALL,
	huntRangeReach,
	huntReturnDeadlineMs,
	huntItemKind,
	isDimensionPillarName,
	isDimensionPillarTarget,
	isMacroHuntNeverTarget,
	macroUsageTimeEnded,
	pickMacroHuntAction,
	spiritNeedsWake
} = hunt;
const regionCombat = await import( "../../src/engine/foundation/gameplay/region-combat.ts" );
const { macroRegionIsTown, regionAllowsPlayerCombat } = regionCombat;
const { isSpiritItem } = await import( "../../src/engine/foundation/gameplay/avatar-socket.ts" );

/*
================
block

A hunt block with selected bytes overwritten.
================
*/
/**
 * @param {Record<number, number>} [overrides]
 * @returns {number[]}
 */
function block( overrides = {} ) {
	const next = codec.defaultHuntBlock();
	for ( const byte of Object.keys( overrides ) ) {
		const index = Number( byte );
		next[index] = overrides[index] ?? 0;
	}
	return next;
}

/*
================
facts
================
*/
/**
 * @param {Partial<import("../../src/engine/foundation/gameplay/sro-macro-hunt.ts").MacroHuntFacts>} [overrides]
 * @returns {import("../../src/engine/foundation/gameplay/sro-macro-hunt.ts").MacroHuntFacts}
 */
function facts( overrides = {} ) {
	return {
		alive: true,
		startedAtMs: 1_000,
		nowMs: 1_000,
		berserkGauge: 0,
		potionOn: true,
		hpPotionCount: 50,
		mpPotionCount: 50,
		hasReturnScroll: true,
		hasResurrectScroll: true,
		hasRepairHammer: true,
		hasEnergyOfLife: true,
		equipmentBroken: false,
		usageTimeEnded: false,
		hasSpiritToWake: false,
		targetGid: undefined,
		targetLivingMonster: false,
		nearestMonsterGid: 42,
		nearestPickupGid: 99,
		...overrides
	};
}

test("default hunt block keeps byte 1 at 1 (006938a0)", () => {
	assert.deepEqual( codec.defaultHuntBlock(), [ 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 ] );
	assert.deepEqual( codec.defaultMacroSettings().hunt, codec.defaultHuntBlock() );
});

test("range reach: In place is short; 1..4 are metres*10 world units", () => {
	// 0068e700 floats: 300/500/700/1000 (10 units per metre).
	assert.equal( huntRangeReach( HUNT_RANGE_IN_PLACE ), HUNT_IN_PLACE_REACH );
	assert.equal( huntRangeReach( 1 ), 300 );
	assert.equal( huntRangeReach( 2 ), 500 );
	assert.equal( huntRangeReach( 3 ), 700 );
	assert.equal( huntRangeReach( 4 ), 1000 );
	assert.equal( huntRangeReach( 9 ), 0 );
});

test("return deadline multiplies the stored hour count by 0x36ee80", () => {
	assert.equal( huntReturnDeadlineMs( 0, 1000 ), 0 );
	assert.equal( huntReturnDeadlineMs( 1, 1000 ), 1000 + HUNT_HOUR_MS );
	assert.equal( huntReturnDeadlineMs( 3, 1000 ), 1000 + 3 * HUNT_HOUR_MS );
	assert.equal( huntReturnDeadlineMs( 10, 500 ), 500 + 10 * HUNT_HOUR_MS );
});

test("hunt item kinds match the type words used for scrolls and hammers", () => {
	assert.equal( huntItemKind( 0x09ec ), "return" );
	assert.equal( huntItemKind( 0x36ec ), "resurrect" );
	assert.equal( huntItemKind( 0x40ec ), "energy" );
	assert.equal( huntItemKind( 0x3eec ), "repair-hammer" );
	assert.equal( huntItemKind( 0x0dec ), undefined );
});

test("while dead: scroll, town, or wait by resurrect byte", () => {
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_RESURRECT]: HUNT_RESURRECT_SCROLL } ), facts( { alive: false } ) ),
		{ kind: "resurrect-scroll" }
	);
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_RESURRECT]: HUNT_RESURRECT_TOWN } ), facts( { alive: false } ) ),
		{ kind: "resurrect-town" }
	);
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_RESURRECT]: HUNT_RESURRECT_WAIT } ), facts( { alive: false } ) ),
		{ kind: "wait-resurrect" }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_RESURRECT]: HUNT_RESURRECT_SCROLL } ),
			facts( { alive: false, hasResurrectScroll: false } )
		),
		{ kind: "resurrect-town", noScroll: true }
	);
});

test("return timer fires before potion-empty and acquire", () => {
	const action = pickMacroHuntAction(
		block( { [HUNT_BYTE_RETURN]: 1, [HUNT_BYTE_PICKUP]: 1 } ),
		facts( { nowMs: 1000 + HUNT_HOUR_MS } )
	);
	assert.deepEqual( action, { kind: "return-scroll", reason: "timer", endHunt: true } );
});

test("potion-empty return needs potion ON and a found stack of at most 10", () => {
	// 68b4c0: found stack and quantity <= 10 (cmp 0xa; jg). Count 10 fires;
	// count 0 (no stacks found) and count 11 do not.
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_POTION_EMPTY]: 1 } ),
			facts( { hpPotionCount: 5 } )
		),
		{ kind: "return-scroll", reason: "potion-hp", endHunt: true }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_POTION_EMPTY]: 1 } ),
			facts( { hpPotionCount: 10 } )
		),
		{ kind: "return-scroll", reason: "potion-hp", endHunt: true }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_POTION_EMPTY]: 1 } ),
			facts( { hpPotionCount: 0 } )
		),
		{ kind: "select", gid: 42 }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_POTION_EMPTY]: 1 } ),
			facts( { hpPotionCount: 11 } )
		),
		{ kind: "select", gid: 42 }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_POTION_EMPTY]: 1 } ),
			facts( { potionOn: false, hpPotionCount: 5 } )
		),
		{ kind: "select", gid: 42 }
	);
});

test("broken gear uses the hammer or returns to town", () => {
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_REPAIR]: HUNT_REPAIR_HAMMER } ),
			facts( { equipmentBroken: true } )
		),
		{ kind: "repair-hammer" }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_REPAIR]: HUNT_REPAIR_TOWN } ),
			facts( { equipmentBroken: true } )
		),
		{ kind: "return-scroll", reason: "repair", noHammer: false, endHunt: true }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_REPAIR]: HUNT_REPAIR_HAMMER } ),
			facts( { equipmentBroken: true, hasRepairHammer: false } )
		),
		{ kind: "return-scroll", reason: "repair", noHammer: true, endHunt: true }
	);
});

test("VIP usage-time end returns when byte 11 is on", () => {
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_TIME_TOWN]: 1 } ),
			facts( { usageTimeEnded: true } )
		),
		{ kind: "return-scroll", reason: "vip-time", endHunt: true }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_TIME_TOWN]: 1 } ),
			facts( { usageTimeEnded: false } )
		),
		{ kind: "select", gid: 42 }
	);
});

test("macroUsageTimeEnded follows param/count remaining clocks", () => {
	const now = 10_000;
	assert.equal( macroUsageTimeEnded( [], [], now ), false );
	assert.equal(
		macroUsageTimeEnded(
			[ { itemRefObjId: 1, remainingSec: 5, receivedAtMs: now - 6000 } ],
			[],
			now
		),
		true
	);
	assert.equal(
		macroUsageTimeEnded(
			[ { itemRefObjId: 0x7f530001, remainingSec: 0, receivedAtMs: now } ],
			[],
			now
		),
		false,
		"survival arena param jobs do not end VIP time"
	);
	assert.equal(
		macroUsageTimeEnded(
			[],
			[ { packageRefObjId: 1, itemRefObjId: 2, uses: 0, remainingSec: 1, receivedAtMs: now - 2000 } ],
			now
		),
		true
	);
});

test("Spirit wake fires when byte 9 is on and a dormant Spirit is in the bag", () => {
	assert.equal( isSpiritItem( 0x72c ), true );
	assert.equal( isSpiritItem( 0x6ac ), false );
	assert.equal( spiritNeedsWake( 0 ), true );
	assert.equal( spiritNeedsWake( 1 ), false );
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_SPIRIT]: 1 } ),
			facts( { hasSpiritToWake: true } )
		),
		{ kind: "spirit-use" }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_SPIRIT]: 1 } ),
			facts( { hasSpiritToWake: false } )
		),
		{ kind: "select", gid: 42 }
	);
});

test("macroRegionIsTown matches RegionPlayerCombat known denials", () => {
	// regioncombat_test.go pins: 0x62a6 battlefield, 0x62a7..0x62a9 town.
	assert.equal( regionAllowsPlayerCombat( 0x62a6 ), true );
	assert.equal( regionAllowsPlayerCombat( 0x62a7 ), false );
	assert.equal( macroRegionIsTown( 0x62a6 ), false );
	assert.equal( macroRegionIsTown( 0x62a7 ), true );
	assert.equal( macroRegionIsTown( 0 ), false, "unknown region is not town" );
	assert.equal( macroRegionIsTown( 0xffff ), false );
});

test("Dimension pillar skip matches 68c3d0 codenames and English names", () => {
	assert.equal( isDimensionPillarName( "Dimension Pillar" ), true );
	assert.equal( isDimensionPillarName( "Dimensional Pillar" ), true );
	assert.equal( isDimensionPillarName( "Tiger" ), false );
	assert.equal( isDimensionPillarTarget( "Flame", "MOB_GOD_PILLAR_FLAME_01" ), true );
	assert.equal( isDimensionPillarTarget( "Togui", "MOB_GOD_PILLAR_TOGUI_A" ), true );
	assert.equal( isDimensionPillarTarget( "Wreck", "MOB_GOD_PILLAR_WRECK_B" ), true );
	assert.equal( isDimensionPillarTarget( "Tiger", "MOB_TIGER" ), false );
	assert.equal( isMacroHuntNeverTarget( "Flower", "STRUCTURE_SUMMON_FLOWER_01" ), true );
	assert.equal( isMacroHuntNeverTarget( "Tiger", "MOB_TIGER" ), false );
	assert.equal( HUNT_BYTE_PILLAR, 10, "00686720 reads 017b3810" );
	assert.equal( HUNT_BYTE_SPIRIT, 9 );
});

test("Energy of Life and berserk All fire when their gauges want them", () => {
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_ENERGY]: 1 } ), facts( { berserkGauge: 2 } ) ),
		{ kind: "energy-of-life" }
	);
	assert.deepEqual(
		pickMacroHuntAction(
			block( { [HUNT_BYTE_BERSERK]: HUNT_BERSERK_ALL } ),
			facts( { berserkGauge: 5 } )
		),
		{ kind: "berserk" }
	);
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_BERSERK]: 1 } ), facts( { berserkGauge: 5 } ) ),
		{ kind: "select", gid: 42 }
	);
});

test("pickup then select when no living monster is targeted", () => {
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_PICKUP]: 1 } ), facts() ),
		{ kind: "pickup", gid: 99 }
	);
	assert.deepEqual(
		pickMacroHuntAction( block(), facts() ),
		{ kind: "select", gid: 42 }
	);
	assert.deepEqual(
		pickMacroHuntAction( block( { [HUNT_BYTE_PICKUP]: 1 } ), facts( { targetLivingMonster: true } ) ),
		{ kind: "none" }
	);
});

test("hunt group values are not choice indices for range and return", async () => {
	const { MACRO_HUNT_GROUPS } = await import( "../../src/engine/runtime/ui/hud/sro-macro-hud.ts" );
	const range = MACRO_HUNT_GROUPS.find( g => g.byte === HUNT_BYTE_RANGE );
	const ret = MACRO_HUNT_GROUPS.find( g => g.byte === HUNT_BYTE_RETURN );
	assert.ok( range );
	assert.ok( ret );
	assert.deepEqual( range.choices.map( c => c.value ), [ 0, 1, 2, 3, 4 ] );
	assert.equal( range.choices[0]?.label, "In place" );
	assert.deepEqual( ret.choices.map( c => c.value ), [ 0, 1, 3, 5, 10 ] );
});

test("Auto-Hunt select passes the monster entity so Auto-Skills can attack", async () => {
	const { createGameplay } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
	);
	const { experimentalGameplay, experimentalOptions } = await import(
		"../../src/engine/foundation/ui/experimental-options.ts"
	);
	const { macroReplay } = await import( "../helpers/sro-macro-frames.mjs" );
	const SELECT_OPCODE = 0x745a;
	const SKILL_PRESS_OPCODE = 0x72cd;
	const ATTACK = 11;
	const LOCAL_GID = 1;
	const MONSTER_GID = 2;
	const skillRef = ( id, over = {} ) => ({
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
			actionMs: over.actionMs ?? 100,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	});
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const entities = new Map();
	const local = {
		gid: LOCAL_GID,
		refObjId: 1907,
		kind: /** @type {const} */ ("local-player"),
		name: "hunter",
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
		x: 5,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	entities.set( LOCAL_GID, local );
	entities.set( MONSTER_GID, monster );
	const g = createGameplay(
		f => sent.push( f ),
		() => {},
		() => {},
		gid => entities.get( gid ),
		() => {},
		() => [ ...entities.values() ].filter( e => e.kind === "monster" ),
		() => []
	);
	g.bootstrap( {
		character: {
			hp: 100,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			skills: [ ATTACK ],
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refSkillSnapshot: [ skillRef( ATTACK, { targetRequired: true, actionMs: 100 } ) ]
	} );
	g.worldReady();
	g.seed( monster );
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: true } ) ) );
	const hunt = codec.defaultHuntBlock();
	hunt[0] = 1; // 30 m so the mob at 5 units is inside range
	g.receive(
		macroReplay( {
			huntOn: 1,
			skillOn: 1,
			hunt,
			buff: [],
			attack: [ { slot: 0, skill: ATTACK } ]
		} ),
		0
	);
	sent.length = 0;
	g.step( 0, local );
	assert.ok( sent.some( frame => frame.opcode === SELECT_OPCODE ), "hunt must select the monster" );
	assert.equal( g.skillTarget(), MONSTER_GID, "selection intent names the hunt target" );
	// Same tick: hunt ran before skill, so the attack press should also go out.
	assert.ok(
		sent.some( frame => frame.opcode === SKILL_PRESS_OPCODE ),
		"Auto-Skills attacks the hunt target on the same tick"
	);
	g.dispose();
});

test("Auto-Hunt ends on teleportMode, not a region walk, and paces unanswered selects", async () => {
	const { createGameplay } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
	);
	const { experimentalGameplay, experimentalOptions } = await import(
		"../../src/engine/foundation/ui/experimental-options.ts"
	);
	const { macroReplay } = await import( "../helpers/sro-macro-frames.mjs" );
	const { OP_MACRO_REQUEST, MACRO_HUNT, MACRO_ACTION_SWITCH } = codec;
	const SELECT_OPCODE = 0x745a;
	const STATE_REFRESH = 0x3122;
	const LOCAL_GID = 1;
	const MONSTER_GID = 2;
	const skillRef = ( id, over = {} ) => ({
		id,
		group: over.group ?? id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `SKILL_${id}`,
			trainable: true,
			spCost: 1,
			targetRequired: false,
			cooldownMs: 0,
			actionMs: 100,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	});
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const entities = new Map();
	const local = {
		gid: LOCAL_GID,
		refObjId: 1907,
		kind: /** @type {const} */ ("local-player"),
		name: "hunter",
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
		x: 5,
		y: 0,
		z: 0,
		heading: 0,
		appearanceState: [ 1, 0, 0 ]
	};
	entities.set( LOCAL_GID, local );
	entities.set( MONSTER_GID, monster );
	const g = createGameplay(
		f => sent.push( f ),
		() => {},
		() => {},
		gid => entities.get( gid ),
		() => {},
		() => [ ...entities.values() ].filter( e => e.kind === "monster" ),
		() => []
	);
	g.bootstrap( {
		character: {
			hp: 100,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			skills: [],
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refSkillSnapshot: [ skillRef( 11 ) ]
	} );
	g.worldReady();
	g.seed( monster );
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: true } ) ) );
	const hunt = codec.defaultHuntBlock();
	hunt[0] = 1;
	g.receive( macroReplay( { huntOn: 1, hunt, buff: [], attack: [] } ), 0 );
	sent.length = 0;
	g.step( 0, local );
	assert.equal( sent.filter( f => f.opcode === SELECT_OPCODE ).length, 1, "first select" );
	// Selection intent already names the mob, so the next ticks want "none"
	// and must not re-issue select inside the retry window either.
	g.step( 500, local );
	assert.equal(
		sent.filter( f => f.opcode === SELECT_OPCODE ).length,
		1,
		"select is not resent inside MACRO_HUNT_RETRY_MS"
	);

	// Region walk must not end hunt: seed a new region without a teleport latch.
	local.regionId = 258;
	g.seed( local );
	sent.length = 0;
	g.step( 2000, local );
	assert.equal(
		sent.filter( f => f.opcode === OP_MACRO_REQUEST ).length,
		0,
		"a region cell change alone does not send hunt OFF"
	);

	// 0x3122 state 11 value 1 is the teleport latch.
	const teleport = new Uint8Array( 6 );
	new DataView( teleport.buffer ).setUint32( 0, LOCAL_GID, true );
	teleport[4] = 11;
	teleport[5] = 1;
	g.receive( { opcode: STATE_REFRESH, payload: teleport }, 2500 );
	sent.length = 0;
	g.step( 2500, local );
	const offs = sent.filter( f => f.opcode === OP_MACRO_REQUEST );
	assert.ok( offs.length >= 1, "teleportMode ends Auto-Hunt" );
	const huntOff = offs.find(
		f => f.payload[0] === MACRO_ACTION_SWITCH && f.payload[1] === MACRO_HUNT
	);
	assert.ok( huntOff, "hunt OFF request" );
	assert.equal( huntOff.payload[2], 0, "hunt OFF" );
	g.step( 2600, local );
	assert.equal(
		sent.filter(
			f => f.opcode === OP_MACRO_REQUEST && f.payload[0] === MACRO_ACTION_SWITCH &&
				f.payload[1] === MACRO_HUNT
		).length,
		1,
		"teleport OFF is not resent every tick"
	);
	g.dispose();
});
