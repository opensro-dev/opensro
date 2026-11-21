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
	pickMacroHuntAction
} = hunt;

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

test("range reach: In place is short; 1..4 are 30/50/70/100 m", () => {
	assert.equal( huntRangeReach( HUNT_RANGE_IN_PLACE ), HUNT_IN_PLACE_REACH );
	assert.equal( huntRangeReach( 1 ), 30 );
	assert.equal( huntRangeReach( 2 ), 50 );
	assert.equal( huntRangeReach( 3 ), 70 );
	assert.equal( huntRangeReach( 4 ), 100 );
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

test("potion-empty return needs potion ON and a registered stack under 10", () => {
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

test("Dimension pillar names match the hunt skip heuristic", () => {
	assert.equal( isDimensionPillarName( "Dimension Pillar" ), true );
	assert.equal( isDimensionPillarName( "Dimensional Pillar" ), true );
	assert.equal( isDimensionPillarName( "Tiger" ), false );
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
