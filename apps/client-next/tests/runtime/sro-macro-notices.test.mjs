/*
===========================================================================

sro-macro-notices.test.mjs - SRO Macro system chat lines

Port-only, not native (#3907): 00dade90 switch refuses and hunt return /
use notices from sro-macro-notices.ts.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const codec = await import( "../../src/engine/foundation/gameplay/sro-macro.ts" );
const notices = await import( "../../src/engine/foundation/gameplay/sro-macro-notices.ts" );
const {
	MACRO_HUNT,
	MACRO_POTION,
	MACRO_SKILL,
	defaultMacroSettings,
	withPotionSlot
} = codec;
const {
	MACRO_NOTICE_POTION_ON_FAIL,
	MACRO_NOTICE_TOWN_REFUSE,
	MACRO_NOTICE_TOWN_OFF,
	MACRO_NOTICE_HP_POTION_EMPTY,
	MACRO_NOTICE_TIME_OVER,
	MACRO_NOTICE_NOT_HAMMER,
	MACRO_NOTICE_NOT_REVIVAL,
	MACRO_NOTICE_DIE_TOWN,
	MACRO_NOTICE_ENERGY,
	macroPotionHasRegisteredItem,
	macroSwitchRefuseNotice,
	macroHuntNoticeText,
	macroTownDisableKinds
} = notices;

test("potion ON fails when every row is empty; OFF always passes", () => {
	const empty = defaultMacroSettings().potion;
	assert.equal( macroPotionHasRegisteredItem( empty ), false );
	assert.equal(
		macroSwitchRefuseNotice( MACRO_POTION, true, { inTown: false, potionHasItem: false } ),
		MACRO_NOTICE_POTION_ON_FAIL
	);
	assert.equal(
		macroSwitchRefuseNotice( MACRO_POTION, false, { inTown: false, potionHasItem: false } ),
		undefined
	);
	const bound = withPotionSlot( empty, 0, { item: 0x1234, a: 0, b: 1 } );
	assert.equal( macroPotionHasRegisteredItem( bound ), true );
	assert.equal(
		macroSwitchRefuseNotice( MACRO_POTION, true, { inTown: false, potionHasItem: true } ),
		undefined
	);
});

test("skill/hunt ON refuses in town; potion ignores town", () => {
	assert.equal(
		macroSwitchRefuseNotice( MACRO_SKILL, true, { inTown: true, potionHasItem: true } ),
		MACRO_NOTICE_TOWN_REFUSE
	);
	assert.equal(
		macroSwitchRefuseNotice( MACRO_HUNT, true, { inTown: true, potionHasItem: true } ),
		MACRO_NOTICE_TOWN_REFUSE
	);
	assert.equal(
		macroSwitchRefuseNotice( MACRO_POTION, true, { inTown: true, potionHasItem: true } ),
		undefined
	);
});

test("town disable turns skill and hunt off together", () => {
	assert.deepEqual( macroTownDisableKinds( true, true, true ), [ MACRO_HUNT, MACRO_SKILL ] );
	assert.deepEqual( macroTownDisableKinds( false, true, true ), [ MACRO_HUNT ] );
	assert.deepEqual( macroTownDisableKinds( true, true, false ), [] );
});

test("hunt action notices match the English UIIT rows", () => {
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "potion-hp", endHunt: true } ),
		MACRO_NOTICE_HP_POTION_EMPTY
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "timer", endHunt: true } ),
		MACRO_NOTICE_TIME_OVER
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "repair", noHammer: true, endHunt: true } ),
		MACRO_NOTICE_NOT_HAMMER
	);
	assert.equal(
		macroHuntNoticeText( { kind: "resurrect-town", noScroll: true } ),
		MACRO_NOTICE_NOT_REVIVAL
	);
	assert.equal( macroHuntNoticeText( { kind: "resurrect-town" } ), MACRO_NOTICE_DIE_TOWN );
	assert.equal( macroHuntNoticeText( { kind: "energy-of-life" } ), MACRO_NOTICE_ENERGY );
	assert.equal( macroHuntNoticeText( { kind: "select", gid: 1 } ), undefined );
	assert.equal( MACRO_NOTICE_TOWN_OFF.includes( "disabled in towns" ), true );
});
