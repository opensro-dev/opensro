/*
===========================================================================

sro-macro-potion.test.mjs - the SRO Macro's character and pet auto-potion

Port-only, not native (#3907): the v1.696 auto-potion rows (character
00692e00..006930a0, pet 00691c70..00691b60), the executor's order and
500 ms re-arm (006931b0), binding a bag item to a row, and the worker
running the loop only with the Experimental row on and the server's
Auto-Potion ON.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { macroReplay } from "../helpers/sro-macro-frames.mjs";

const codec = await import( "../../src/engine/foundation/gameplay/sro-macro.ts" );
const loop = await import( "../../src/engine/foundation/gameplay/sro-macro-potion.ts" );
const { createSroMacroHud } = await import( "../../src/engine/runtime/ui/hud/sro-macro-hud.ts" );
const { macroPotionStock } = await import( "../../src/engine/runtime/ui/hud/sro-macro-window.ts" );
const { experimentalGameplay, experimentalOptions } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const {
	MACRO_ROW_HP,
	MACRO_ROW_MP,
	MACRO_ROW_CURE,
	MACRO_ROW_BUFF_ITEM,
	MACRO_ROW_PET_HP,
	MACRO_ROW_PET_HUNGER,
	MACRO_ROW_PET_CURE,
	MACRO_ROW_PET_RESURRECT,
	MACRO_ROW_PET_SUMMON
} = codec;

const POTION_REF = 7;
const OTHER_REF = 8;
// PackTypeFlags(3,3,1,tid4): HP=1 → 0x8ec, MP=2 → 0x10ec (ITEM_ETC_*_POTION_05).
const CONSUMABLE_TYPE = 0x8ec;
const MP_POTION_TYPE = 0x10ec;
const ITEM_USE_OPCODE = 0x75bd;

/*
================
row

A potion block with one row set.
================
*/
/**
 * @param {number} index
 * @param {{ item: number, a: number, b: number }} slot
 * @param {readonly number[]} [potion]
 */
function row( index, slot, potion = codec.defaultMacroSettings().potion ) {
	return codec.withPotionSlot( potion, index, slot );
}

/*
================
facts
================
*/
/** @param {Partial<import("../../src/engine/foundation/gameplay/auto-potion.ts").AutoPotionFacts>} [over] */
function facts( over = {} ) {
	return { alive: true, hp: 100, mp: 100, maxHp: 100, maxMp: 100, abnormal: 0, ...over };
}

test("a gauge row is on by its second byte and a check-box row by its first", () => {
	const gauge = row( MACRO_ROW_HP, { item: 1, a: 50, b: 0 } );
	assert.equal( codec.potionRowOn( gauge, MACRO_ROW_HP ), false );
	assert.equal( codec.potionRowOn( row( MACRO_ROW_HP, { item: 1, a: 0, b: 1 } ), MACRO_ROW_HP ), true );
	assert.equal( codec.potionRowOn( row( MACRO_ROW_CURE, { item: 1, a: 1, b: 0 } ), MACRO_ROW_CURE ), true );
	assert.equal( codec.potionRowOn( row( MACRO_ROW_CURE, { item: 1, a: 0, b: 1 } ), MACRO_ROW_CURE ), false );

	const on = codec.withPotionRowOn( codec.defaultMacroSettings().potion, MACRO_ROW_MP, true, 50 );
	assert.deepEqual( codec.potionSlot( on, MACRO_ROW_MP ), { item: 0, a: 50, b: 1 } );
	const kept = codec.withPotionRowOn( codec.withPotionRowPercent( on, MACRO_ROW_MP, 30 ), MACRO_ROW_MP, false, 50 );
	assert.deepEqual( codec.potionSlot( kept, MACRO_ROW_MP ), { item: 0, a: 30, b: 0 } );
	const bound = codec.withPotionRowItem( kept, MACRO_ROW_MP, 0x01020304 );
	assert.equal( codec.potionSlot( bound, MACRO_ROW_MP ).item, 0x01020304 );
});

test("HP and MP fire at or under their percent; HP waits out the blocking abnormal bit", () => {
	const potion = row( MACRO_ROW_MP, { item: 2, a: 40, b: 1 }, row( MACRO_ROW_HP, { item: 1, a: 50, b: 1 } ) );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_HP, facts( { hp: 50 } ) ), true );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_HP, facts( { hp: 51 } ) ), false );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_HP, facts( { hp: 10, abnormal: 0x20 } ) ), false );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_MP, facts( { mp: 40, abnormal: 0x20 } ) ), true );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_MP, facts( { mp: 41 } ) ), false );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_HP, facts( { hp: 10, alive: false } ) ), false );
});

test("cure fires on any curable abnormal bit; an off or empty row never fires", () => {
	const cure = row( MACRO_ROW_CURE, { item: 3, a: 1, b: 0 } );
	assert.equal( loop.macroPotionWants( cure, MACRO_ROW_CURE, facts() ), false );
	assert.equal( loop.macroPotionWants( cure, MACRO_ROW_CURE, facts( { abnormal: 1 } ) ), true );
	assert.equal( loop.macroPotionWants( cure, MACRO_ROW_CURE, facts( { abnormal: 0x4000 } ) ), false );
	const off = row( MACRO_ROW_HP, { item: 1, a: 50, b: 0 } );
	assert.equal( loop.macroPotionWants( off, MACRO_ROW_HP, facts( { hp: 1 } ) ), false );
	const empty = row( MACRO_ROW_HP, { item: 0, a: 50, b: 1 } );
	assert.equal( loop.macroPotionWants( empty, MACRO_ROW_HP, facts( { hp: 1 } ) ), false );
});

test("a fired row sits out its re-arm, then rejoins at the back of the order", () => {
	let queue = loop.emptyMacroPotionQueue();
	assert.deepEqual( queue.ready, loop.MACRO_POTION_ORDER );
	queue = loop.macroPotionFired( queue, MACRO_ROW_HP, 0 );
	queue = loop.macroPotionFired( queue, MACRO_ROW_MP, 10 );
	assert.deepEqual(
		queue.ready,
		[
			MACRO_ROW_CURE,
			MACRO_ROW_BUFF_ITEM,
			MACRO_ROW_PET_HP,
			MACRO_ROW_PET_HUNGER,
			MACRO_ROW_PET_CURE,
			MACRO_ROW_PET_RESURRECT,
			MACRO_ROW_PET_SUMMON
		]
	);
	assert.equal( loop.rearmMacroPotion( queue, 499 ), queue, "nothing has re-armed yet" );
	queue = loop.rearmMacroPotion( queue, 600 );
	assert.deepEqual(
		queue.ready,
		[
			MACRO_ROW_CURE,
			MACRO_ROW_BUFF_ITEM,
			MACRO_ROW_PET_HP,
			MACRO_ROW_PET_HUNGER,
			MACRO_ROW_PET_CURE,
			MACRO_ROW_PET_RESURRECT,
			MACRO_ROW_PET_SUMMON,
			MACRO_ROW_HP
		],
		"one row returns per tick"
	);
	queue = loop.rearmMacroPotion( queue, 600 );
	assert.deepEqual( queue.ready.at( -1 ), MACRO_ROW_MP );
	assert.deepEqual( queue.waiting, [] );
});

test("buff-item row fires only while its associated skill is absent (006930a0)", () => {
	const potion = row( MACRO_ROW_BUFF_ITEM, { item: OTHER_REF, a: 1, b: 0 } );
	const skillId = 4242;
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_BUFF_ITEM, facts(), 0 ), false, "no skill id" );
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_BUFF_ITEM, facts(), skillId ),
		true,
		"buff absent"
	);
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_BUFF_ITEM,
			facts( { activeBuffSkills: [ skillId ] } ),
			skillId
		),
		false,
		"buff live"
	);
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_BUFF_ITEM,
			facts( { activeBuffSkills: [ 7 ] } ),
			skillId
		),
		true,
		"other buff does not block"
	);
	const off = row( MACRO_ROW_BUFF_ITEM, { item: OTHER_REF, a: 0, b: 0 } );
	assert.equal( loop.macroPotionWants( off, MACRO_ROW_BUFF_ITEM, facts(), skillId ), false );
});

test("buff-item skill id comes from the bag stack's associatedSkillId", () => {
	const potion = row( MACRO_ROW_BUFF_ITEM, { item: OTHER_REF, a: 1, b: 0 } );
	assert.equal( loop.macroPotionBuffSkillId( potion, [] ), 0 );
	assert.equal(
		loop.macroPotionBuffSkillId( potion, [
			{ slot: 5, refObjId: OTHER_REF, nativeFields: { associatedSkillId: 99 } },
			{ slot: 14, refObjId: OTHER_REF, nativeFields: { associatedSkillId: 55 } }
		] ),
		55,
		"first bag stack, not equipment"
	);
	assert.equal(
		loop.macroPotionBuffSkillId( potion, [
			{ slot: 14, refObjId: OTHER_REF, nativeFields: {} }
		] ),
		0
	);
});

test("a row uses the first bag stack of its item, never an equipment socket", () => {
	const potion = row( MACRO_ROW_HP, { item: POTION_REF, a: 50, b: 1 } );
	const inventory = [
		{ slot: 5, refObjId: POTION_REF },
		{ slot: 20, refObjId: POTION_REF },
		{ slot: 14, refObjId: POTION_REF },
		{ slot: 13, refObjId: OTHER_REF }
	];
	assert.equal( loop.macroPotionBagSlot( potion, MACRO_ROW_HP, inventory ), 14 );
	assert.equal( loop.macroPotionBagSlot( potion, MACRO_ROW_MP, inventory ), undefined );
	assert.equal( loop.macroPotionBagSlot( potion, MACRO_ROW_HP, [ { slot: 3, refObjId: POTION_REF } ] ), undefined );
});

/*
================
optionsHud

The options window open on the potion tab over a fresh replay.
================
*/
function optionsHud() {
	const live = codec.decodeMacroConfig( macroReplay().payload, codec.defaultMacroSettings() );
	const hud = createSroMacroHud();
	hud.toggleMenu( live, true );
	hud.click( "sro-macro:options:0", live );
	return { hud, live };
}

test("HP/MP rows admit by potion mid-byte then TID4 (690f30 / 690b70 / 690bf0)", () => {
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_HP, 0x8ec ), true, "HP recovery (tid4=1)" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_MP, 0x10ec ), true, "MP recovery (tid4=2)" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_HP, 0x10ec ), false, "MP refuses HP row" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_MP, 0x8ec ), false, "HP refuses MP row" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_HP, 0x18ec ), true, "universal tid4=3 on HP" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_MP, 0x18ec ), true, "universal tid4=3 on MP" );
	// Pill family (TID3=2 / 0x16c): mid-byte helper 690ec0, not the potion helper.
	const CURE_PILL_TID4_1 = 0x16c | 1 << 11;
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_HP, CURE_PILL_TID4_1 ), false, "pill refuses HP" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_MP, 0x16c ), false );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_CURE, CURE_PILL_TID4_1 ), true );
});

test("a consumable dropped on a row binds its item; a click on the slot clears it", () => {
	const { hud, live } = optionsHud();
	assert.equal( hud.bindItem( "slot:13", { refObjId: POTION_REF, typeFlags: CONSUMABLE_TYPE } ), false );
	assert.equal( hud.bindItem( "sro-macro:item:0", { refObjId: POTION_REF, typeFlags: CONSUMABLE_TYPE } ), true );
	assert.equal( hud.bindItem( "sro-macro:item:1", { refObjId: OTHER_REF, typeFlags: MP_POTION_TYPE } ), true );
	let draft = hud.state().draft;
	assert.ok( draft );
	assert.equal( codec.potionSlot( draft.potion, MACRO_ROW_HP ).item, POTION_REF );
	assert.equal( codec.potionSlot( draft.potion, MACRO_ROW_MP ).item, OTHER_REF );

	assert.equal( hud.bindItem( "sro-macro:item:0", { refObjId: OTHER_REF, typeFlags: 0x1 } ), false );
	// Equipment shares class 0x0c but sits in band 0x20 (press-admission).
	const EQUIPMENT_TYPE = 0x2c;
	assert.equal( loop.macroPotionAdmits( 2, EQUIPMENT_TYPE ), false );
	assert.equal( hud.bindItem( "sro-macro:item:2", { refObjId: OTHER_REF, typeFlags: EQUIPMENT_TYPE } ), false );
	assert.equal( hud.bindItem( "sro-macro:item:3", { refObjId: OTHER_REF, typeFlags: EQUIPMENT_TYPE } ), false );
	// Alchemy stones / elixirs (TID3 10/11) on the anomaly rows.
	const MAGIC_STONE_TYPE = 0x0dec;
	const ELIXIR_TYPE = 0x0d6c;
	assert.equal( loop.macroPotionAdmits( 2, MAGIC_STONE_TYPE ), false );
	assert.equal( loop.macroPotionAdmits( 3, ELIXIR_TYPE ), false );
	assert.equal( hud.bindItem( "sro-macro:item:2", { refObjId: OTHER_REF, typeFlags: MAGIC_STONE_TYPE } ), false );
	assert.equal( hud.bindItem( "sro-macro:item:3", { refObjId: OTHER_REF, typeFlags: ELIXIR_TYPE } ), false );
	// Universal pill (0x16c, TID4 1) binds the Chinese anomaly row only.
	const CURE_PILL_TYPE = 0x16c | 1 << 11;
	assert.equal( loop.macroPotionAdmits( 2, CURE_PILL_TYPE ), true );
	assert.equal( loop.macroPotionAdmits( 3, CURE_PILL_TYPE ), false );
	const dirty = codec.withPotionRowItem(
		codec.withPotionRowItem( codec.defaultMacroSettings().potion, 2, OTHER_REF ),
		3,
		OTHER_REF
	);
	const scrubbed = loop.scrubMacroPotion(
		{ ...codec.defaultMacroSettings(), potion: dirty },
		[ { slot: 13, refObjId: OTHER_REF, typeFlags: EQUIPMENT_TYPE } ]
	);
	assert.equal( codec.potionSlot( scrubbed.potion, 2 ).item, 0 );
	assert.equal( codec.potionSlot( scrubbed.potion, 3 ).item, 0 );
	assert.equal(
		hud.bindItem( "sro-macro:item:0", { refObjId: OTHER_REF, typeFlags: MP_POTION_TYPE } ),
		false,
		"MP potion refuses the HP row"
	);
	assert.equal(
		hud.bindItem( "sro-macro:item:1", { refObjId: POTION_REF, typeFlags: CONSUMABLE_TYPE } ),
		false,
		"HP potion refuses the MP row"
	);
	draft = hud.state().draft;
	assert.ok( draft );
	assert.equal( codec.potionSlot( draft.potion, MACRO_ROW_HP ).item, POTION_REF, "equipment is refused" );
	assert.equal( hud.isPotionItemSlot( "sro-macro:item:1" ), true );
	assert.equal( hud.isPotionItemSlot( "sro-macro:check:1" ), false );

	hud.click( "sro-macro:item:1", live );
	draft = hud.state().draft;
	assert.ok( draft );
	assert.equal( codec.potionSlot( draft.potion, MACRO_ROW_MP ).item, 0 );

	assert.equal( hud.bindItem( "sro-macro:item:1", { refObjId: OTHER_REF, typeFlags: MP_POTION_TYPE } ), true );
	assert.equal( hud.potionItemRef( "sro-macro:item:1" ), OTHER_REF );
	assert.equal( hud.clearItem( "sro-macro:item:1" ), true );
	assert.equal( hud.potionItemRef( "sro-macro:item:1" ), 0 );
	assert.equal( hud.clearItem( "sro-macro:item:1" ), false );

	hud.click( "sro-macro:tab:2", live );
	assert.equal( hud.bindItem( "sro-macro:item:1", { refObjId: OTHER_REF, typeFlags: MP_POTION_TYPE } ), false );
});

test("the row's stock sums the bag's stacks of one item", () => {
	const stock = macroPotionStock( [
		{ slot: 13, refObjId: POTION_REF, quantity: 10, icon: "a.ddj" },
		{ slot: 20, refObjId: POTION_REF, quantity: 5 },
		{ slot: 2, refObjId: POTION_REF, quantity: 99 }
	], icon => icon ? "/icons/" + icon : null );
	assert.deepEqual( stock.get( POTION_REF ), { icon: "/icons/a.ddj", quantity: 15 } );
});

/*
================
macroWorld

A world where the v1.150 auto-potion is off, one potion stack sits in the
first bag slot and the character is at 40 of 100 HP.
================
*/
/** @param {{ row: boolean, potionOn: number }} setup */
function macroWorld( setup ) {
	/** @type {{ opcode: number, payload: Uint8Array }[]} */
	const sent = [];
	const g = createGameplay( /** @param {{ opcode: number, payload: Uint8Array }} f */ f => sent.push( f ) );
	const local = {
		gid: 1,
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
	g.bootstrap( {
		character: {
			hp: 40,
			mp: 100,
			maxHp: 100,
			maxMp: 100,
			autoPotion: { hp: 0x3211, mp: 0x3212, cure: 0x13, timing: 0x8a }
		},
		refItemSnapshot: [ { refObjId: POTION_REF, typeFlags: CONSUMABLE_TYPE, nativeFields: {} } ],
		// The body leads with the RefItem ID (u32), then the stack count.
		equipItems: [ { slot: 13, refObjId: POTION_REF, body: [ POTION_REF, 0, 0, 0, 10, 0 ] } ]
	} );
	g.worldReady();
	g.seed( local );
	g.experimental( experimentalGameplay( experimentalOptions( { sroMacro: setup.row } ) ) );
	const potion = row( MACRO_ROW_HP, { item: POTION_REF, a: 50, b: 1 } );
	g.receive( macroReplay( { potion, potionOn: setup.potionOn } ), 0 );
	return { sent, g, local };
}

test("the worker drinks a bound potion only with the row on and Auto-Potion ON", () => {
	for (
		const [setup, uses] of /** @type {const} */ ([
			[ { row: true, potionOn: 1 }, 1 ],
			[ { row: false, potionOn: 1 }, 0 ],
			[ { row: true, potionOn: 0 }, 0 ]
		])
	) {
		const { sent, g, local } = macroWorld( setup );
		g.step( 0, local );
		const used = sent.filter( frame => frame.opcode === ITEM_USE_OPCODE );
		assert.equal( used.length, uses, JSON.stringify( setup ) );
		if ( uses ) assert.equal( used[0]?.payload[0], 13 );
		g.dispose();
	}
});

// PackTypeFlags ETC 3/3/1/tid4: pet HP=4, hunger=9, grass=6; pill 0x16c tid4=7.
const PET_HP_TYPE = 0x6c | 1 << 7 | 4 << 11;
const PET_HUNGER_TYPE = 0x6c | 1 << 7 | 9 << 11;
const PET_CURE_TYPE = 0x16c | 7 << 11;
const PET_GRASS_TYPE = 0x6c | 1 << 7 | 6 << 11;
const PET_SUMMONER_TYPE = 0x8cc;

/*
================
pet
================
*/
/** @param {Partial<import("../../src/engine/foundation/gameplay/sro-macro-potion.ts").MacroPetFacts>} [over] */
function pet( over = {} ) {
	return { ...loop.emptyMacroPetFacts(), ...over };
}

test("pet rows admit by mid-byte helpers then TID4 (690f30 / 690ec0 / 6908e0)", () => {
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_HP, PET_HP_TYPE ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_HP, PET_HUNGER_TYPE ), false );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_HUNGER, PET_HUNGER_TYPE ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_CURE, PET_CURE_TYPE ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_CURE, PET_HP_TYPE ), false );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_RESURRECT, PET_GRASS_TYPE ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_SUMMON, PET_SUMMONER_TYPE ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_SUMMON, PET_HP_TYPE ), false );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_HP, PET_HP_TYPE ), false, "pet HP refuses character row" );
	// Pill mid-byte (690ec0) never binds a potion-helper pet row.
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_HP, PET_CURE_TYPE ), false );
	assert.equal(
		loop.macroPotionAdmits( MACRO_ROW_PET_RESURRECT, 0x16c | 6 << 11 ),
		false,
		"pill tid4=6 is not Grass"
	);
	// Summoner helper (6908e0) then high 1/2/3 only (690aa0 / 690880).
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_SUMMON, 0xcc | 2 << 11 ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_SUMMON, 0xcc | 3 << 11 ), true );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_SUMMON, 0xcc | 4 << 11 ), false, "summoner tid4=4 refused" );
	assert.equal( loop.macroPotionAdmits( MACRO_ROW_PET_SUMMON, 0x18ec ), false, "potion tid4=3 is not a summoner" );
});

test("pet HP / hunger / cure fire from the companion's vitals (00691c70 / 00691e20 / 00691a20)", () => {
	const potion = row(
		MACRO_ROW_PET_CURE,
		{ item: 3, a: 1, b: 0 },
		row(
			MACRO_ROW_PET_HUNGER,
			{ item: 2, a: 50, b: 1 },
			row( MACRO_ROW_PET_HP, { item: 1, a: 50, b: 1 } )
		)
	);
	const living = pet( { present: true, hp: 40, maxHp: 100, satiety: 4000 } );
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_PET_HP, facts(), 0, living ), true );
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_PET_HP, facts(), 0, pet( { present: true, hp: 51, maxHp: 100 } ) ),
		false
	);
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_PET_HP,
			facts(),
			0,
			pet( { present: true, hp: 10, maxHp: 100, abnormal: 0x20 } )
		),
		false,
		"pet HP waits out abnormal bit 0x20"
	);
	assert.equal( loop.macroPotionWants( potion, MACRO_ROW_PET_HUNGER, facts(), 0, living ), true );
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_PET_HUNGER,
			facts(),
			0,
			pet( { present: true, hp: 100, maxHp: 100, satiety: 5001 } )
		),
		false
	);
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_PET_CURE,
			facts(),
			0,
			pet( { present: true, hp: 100, maxHp: 100, abnormal: 1 } )
		),
		true
	);
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_PET_HP, facts(), 0, pet() ),
		false,
		"no companion out"
	);
});

test("pet resurrect and summon need the summoner state and no living pet (006921a0 / 00691b60)", () => {
	const potion = row(
		MACRO_ROW_PET_SUMMON,
		{ item: 9, a: 1, b: 0 },
		row( MACRO_ROW_PET_RESURRECT, { item: 8, a: 1, b: 0 } )
	);
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_PET_RESURRECT,
			facts(),
			0,
			pet( { summonState: loop.PET_SUMMON_STATE_DEAD } )
		),
		true
	);
	assert.equal(
		loop.macroPotionWants(
			potion,
			MACRO_ROW_PET_RESURRECT,
			facts(),
			0,
			pet( { present: true, hp: 10, maxHp: 100, summonState: loop.PET_SUMMON_STATE_DEAD } )
		),
		false,
		"pet already out"
	);
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_PET_RESURRECT, facts(), 0, pet( { summonState: 1 } ) ),
		false
	);
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_PET_SUMMON, facts(), 0, pet( { summonState: 1 } ) ),
		true
	);
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_PET_SUMMON, facts(), 0, pet( { summonState: 3 } ) ),
		true
	);
	assert.equal(
		loop.macroPotionWants( potion, MACRO_ROW_PET_SUMMON, facts(), 0, pet( { summonState: 2 } ) ),
		false
	);
});

test("macroPetFacts prefers the selected living companion and falls back to the summoner state", () => {
	const records = [
		{ gid: 1, band: 3, hp: 0, dead: true, satiety: 0 },
		{ gid: 2, band: 3, hp: 40, dead: false, satiety: 2500, inventorySlot: 14 }
	];
	const bag = [
		{ slot: 14, refObjId: 9, typeFlags: PET_SUMMONER_TYPE, summon: { state: 1 } },
		{ slot: 15, refObjId: 9, typeFlags: PET_SUMMONER_TYPE, summon: { state: 4 } }
	];
	const out = loop.macroPetFacts( {
		records,
		selectedGid: 2,
		vitals: [ { gid: 2, hp: 40, maxHp: 100, abnormal: 2, satiety: 2500 } ],
		bag,
		summonRef: 9
	} );
	assert.equal( out.present, true );
	assert.equal( out.hp, 40 );
	assert.equal( out.maxHp, 100 );
	assert.equal( out.satiety, 2500 );
	assert.equal( out.abnormal, 2 );
	assert.equal( out.summonState, 1, "linked inventory slot wins" );

	const fromCatalog = loop.macroPetFacts( {
		records: [ { gid: 2, band: 3, hp: 40, dead: false, maxHp: 200 } ],
		selectedGid: 2,
		vitals: [ { gid: 2, hp: 40 } ],
		bag: [],
		summonRef: 0
	} );
	assert.equal( fromCatalog.maxHp, 200, "COS vitals omit maxHp; the catalog stamp fills it" );

	const idle = loop.macroPetFacts( {
		records: [ { gid: 1, band: 3, hp: 0, dead: true } ],
		selectedGid: 0,
		vitals: [],
		bag,
		summonRef: 9
	} );
	assert.equal( idle.present, false );
	assert.equal( idle.summonState, 1, "first bag stack of the summon ref" );
});
