/*
===========================================================================

sro-macro-notices.test.mjs - SRO Macro system chat UIIT keys

00dade90 switch refuses and hunt return / use notices queue
UIIT_STT_SRO_MACRO_* keys (sro-macro-notices.ts); English lives in the
text catalog completion.

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
	MACRO_NOTICE_TELEPORT_OFF,
	MACRO_NOTICE_HP_POTION_EMPTY,
	MACRO_NOTICE_MP_POTION_EMPTY,
	MACRO_NOTICE_ITEM_DURA,
	MACRO_NOTICE_NOT_HAMMER,
	MACRO_NOTICE_TIME_OVER,
	MACRO_NOTICE_NOT_REVIVAL,
	MACRO_NOTICE_DIE_TOWN,
	MACRO_NOTICE_RETURN,
	MACRO_NOTICE_ENERGY,
	MACRO_NOTICE_HAMMER,
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

test("skill/hunt ON refuses in town; potion ignores town; OFF and field pass", () => {
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
	assert.equal(
		macroSwitchRefuseNotice( MACRO_SKILL, true, { inTown: false, potionHasItem: true } ),
		undefined,
		"skill ON outside town proceeds"
	);
	assert.equal(
		macroSwitchRefuseNotice( MACRO_HUNT, true, { inTown: false, potionHasItem: true } ),
		undefined,
		"hunt ON outside town proceeds"
	);
	assert.equal(
		macroSwitchRefuseNotice( MACRO_SKILL, false, { inTown: true, potionHasItem: true } ),
		undefined,
		"skill OFF in town always passes"
	);
	assert.equal(
		macroSwitchRefuseNotice( MACRO_HUNT, false, { inTown: true, potionHasItem: true } ),
		undefined,
		"hunt OFF in town always passes"
	);
});

test("town refuse key matches 00dade90 CANUSEMACRO (not Media CANTUSEMACRO)", () => {
	assert.equal(
		MACRO_NOTICE_TOWN_REFUSE,
		"UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_CANUSEMACRO"
	);
});

test("town disable turns skill and hunt off together", () => {
	assert.deepEqual( macroTownDisableKinds( true, true, true ), [ MACRO_HUNT, MACRO_SKILL ] );
	assert.deepEqual( macroTownDisableKinds( false, true, true ), [ MACRO_HUNT ] );
	assert.deepEqual( macroTownDisableKinds( true, false, true ), [ MACRO_SKILL ] );
	assert.deepEqual( macroTownDisableKinds( false, false, true ), [] );
	assert.deepEqual( macroTownDisableKinds( true, true, false ), [] );
});

test("hunt action notices queue every return / use UIIT key", () => {
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "potion-hp", endHunt: true } ),
		MACRO_NOTICE_HP_POTION_EMPTY
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "potion-mp", endHunt: true } ),
		MACRO_NOTICE_MP_POTION_EMPTY
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "timer", endHunt: true } ),
		MACRO_NOTICE_TIME_OVER
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "vip-time", endHunt: true } ),
		MACRO_NOTICE_TIME_OVER,
		"vip-time shares TIME_OVER with the hour timer"
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "repair", noHammer: true, endHunt: true } ),
		MACRO_NOTICE_NOT_HAMMER
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "repair", noHammer: false, endHunt: true } ),
		MACRO_NOTICE_ITEM_DURA
	);
	assert.equal(
		macroHuntNoticeText( { kind: "return-scroll", reason: "repair", endHunt: true } ),
		MACRO_NOTICE_ITEM_DURA,
		"missing noHammer is durability return"
	);
	assert.equal(
		macroHuntNoticeText( { kind: "resurrect-town", noScroll: true } ),
		MACRO_NOTICE_NOT_REVIVAL
	);
	assert.equal( macroHuntNoticeText( { kind: "resurrect-town" } ), MACRO_NOTICE_DIE_TOWN );
	assert.equal( macroHuntNoticeText( { kind: "energy-of-life" } ), MACRO_NOTICE_ENERGY );
	assert.equal( macroHuntNoticeText( { kind: "repair-hammer" } ), MACRO_NOTICE_HAMMER );
	assert.equal( macroHuntNoticeText( { kind: "select", gid: 1 } ), undefined );
	assert.equal( macroHuntNoticeText( { kind: "pickup", gid: 1 } ), undefined );
	assert.equal( macroHuntNoticeText( { kind: "berserk" } ), undefined );
	assert.equal( macroHuntNoticeText( { kind: "none" } ), undefined );
	assert.equal( macroHuntNoticeText( { kind: "wait-resurrect" } ), undefined );
	assert.equal( macroHuntNoticeText( { kind: "resurrect-scroll" } ), undefined );
	assert.equal( macroHuntNoticeText( { kind: "spirit-use" } ), undefined );
	assert.equal( MACRO_NOTICE_TOWN_OFF, "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_MACRO_OFF" );
	assert.equal( MACRO_NOTICE_TELEPORT_OFF, "UIIT_STT_SRO_MACRO_AUTO_HUNTING_TELEPORT_OFF" );
	assert.equal( MACRO_NOTICE_RETURN, "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_ERORR" );
	assert.equal( MACRO_NOTICE_POTION_ON_FAIL.startsWith( "UIIT_STT_SRO_MACRO_" ), true );
});

test("every queued MACRO_NOTICE_* key has a catalog completion", async () => {
	const { MACRO_NOTICE_UIIT_COMPLETIONS, completeUiText } = await import(
		"../../../../scripts/build/shared/textResources.mjs"
	);
	// The namespace mixes key strings with functions; String() gives the
	// filtered keys a string type, so the catalog lookup below type-checks.
	const queued = Object.entries( notices )
		.filter( ( [name, value] ) => name.startsWith( "MACRO_NOTICE_" ) && typeof value === "string" )
		.map( ( [, value] ) => String( value ) );
	assert.equal( queued.length, 14, "fourteen system-chat keys" );
	assert.deepEqual(
		[ ...queued ].sort(),
		Object.keys( MACRO_NOTICE_UIIT_COMPLETIONS ).sort(),
		"worker keys match MACRO_NOTICE_UIIT_COMPLETIONS"
	);
	/** @type {Record<string, string | undefined>} */
	const catalog = completeUiText( {} );
	for ( const key of queued ) {
		assert.ok( catalog[key]?.trim(), `${key} must resolve to English` );
	}
	assert.equal(
		catalog[MACRO_NOTICE_TOWN_REFUSE],
		"You cannot start the skill/hunting macro in town."
	);
});
