/*
===========================================================================

tooltip-coverage.test.mjs - tests for tooltip-target.ts, mastery-tooltip.ts,
buff-tooltip.ts, helper-bubble.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { tooltipItems, actionTooltipKey } = await import( "../../src/engine/foundation/ui/tooltip-target.ts" );
const { decodeTooltipMasteries, masteryTooltip } = await import( "../../src/engine/foundation/ui/mastery-tooltip.ts" );
const { abnormalTooltip, buffTooltip } = await import( "../../src/engine/foundation/ui/buff-tooltip.ts" );
const { hudTooltipKey } = await import( "../../src/engine/foundation/ui/helper-bubble.ts" );
const { decodeTooltipSkills } = await import( "../../src/engine/foundation/ui/skill-tooltip-catalog.ts" );
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { shopCatalog } = await import( "../../src/engine/foundation/gameplay/commerce.ts" );
const { itemTooltipRecovery } = await import( "../../src/engine/foundation/ui/item-tooltip-recovery.ts" );
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const json = path => JSON.parse( readFileSync( "../../.generated/client-public/assets/" + path, "utf8" ) );
const strings = { ...json( "text/textdataname.en.json" ).entries, ...json( "text/textuisystem.en.json" ).entries },
	text = key => strings[key] ?? "";
test("avatar help distinguishes dress attachment permission and magic capacity without equipment degree", () => {
	const item = {
		name: "Dress",
		typeFlags: 0x16ac,
		plus: 0,
		magic: [],
		variance: "0",
		durability: 1,
		quantity: 1,
		tooltip: { fields: { itemClass: 1, maxMagicOptions51c: 4, avatarAttachment51d: 1 } }
	};
	const rows = itemTooltip( item, { masteries: [] }, key => key );
	assert.ok( rows.some( r => r.value === "UIIT_STT_SILKMALL_ATTACH: UIIT_STT_AVATAR_WEAR" ) );
	assert.ok( rows.some( r => r.value === "UIIT_STT_AVATAR_MAGICOPTION_MAXCOUNT: 4UIIT_STT_UNIT" ) );
	assert.ok( !rows.some( r => r.value.includes( "UIIT_TOOLTIP_EQUIPMENT_CLASS" ) ) );
	item.tooltip.fields.avatarAttachment51d = 0;
	assert.ok(
		itemTooltip( item, { masteries: [] }, key => key ).some( r =>
			r.value === "UIIT_STT_SILKMALL_ATTACH: UIIT_STT_AVATAR_NOT_WEAR"
		)
	);
	item.typeFlags = 0xeac;
	assert.ok(
		!itemTooltip( item, { masteries: [] }, key => key ).some( r => r.value.includes( "UIIT_STT_SILKMALL_ATTACH" ) )
	);
});
test("native cure items distinguish category masks, individual cures and all-status pills", () => {
	const item = ( kind, values ) => ({
		typeFlags: 0x6c | 2 << 7 | kind << 11,
		tooltip: {
			fields: Object.fromEntries(
				values.map( ( v, i ) => [ "itemParam" + (i + 1) + "_" + (0x29c + i * 4).toString( 16 ), v ] )
			)
		}
	});
	const category = itemTooltipRecovery( item( 1, [ 0x1e2800 | 0x1c0 | 0x1e18600, 7 ] ), text );
	assert.equal( category.length, 3 );
	assert.ok( category[0].value.includes( text( "PARAM_WEAKLY" ) ) );
	assert.ok( category[1].value.includes( text( "PARAM_RESTRICTION" ) ) );
	assert.ok( category[2].value.includes( text( "PARAM_CURSING" ) ) );
	assert.deepEqual( itemTooltipRecovery( item( 2, [ 4, 4, 4, 4, 4, 4 ] ), text ).map( r => r.value ), [
		text( "PARAM_CURE_STATE" ) + " 4"
	] );
	for ( let i = 0; i < 6; i++ ) {
		const values = [ 0, 0, 0, 0, 0, 0 ];
		values[i] = 9;
		const rows = itemTooltipRecovery( item( 2, values ), text );
		assert.equal( rows.length, 1 );
		assert.equal(
			rows[0].value,
			text( "PARAM_CURE_" + [ "FROZEN", "FROSTBITE", "BURN", "ESHOCK", "POISON", "ZOMBIE" ][i] + "_LV" ) + " 9"
		);
	}
	assert.deepEqual( itemTooltipRecovery( item( 1, [ 0, 7 ] ), text ), [] );
});
test("every admitted item-container route resolves its instance, including duplicate references and replacement", () => {
	const bag = { slot: 13, refObjId: 1 },
		equipment = { slot: 6, refObjId: 1 },
		avatar = { slot: 0, typeFlags: 0xeac },
		pet = { slot: 13, refObjId: 1 },
		offer = { refObjId: 1, plus: 7 },
		sold = { refObjId: 1, plus: 9 };
	const game = {
		inventory: [ bag, equipment ],
		avatarInventory: [ avatar ],
		cosRecords: [ { gid: 42, inventory: [ pet ] } ],
		shop: { offers: [ { items: [ offer ] } ], buyback: [ { item: sold } ] },
		quickSlots: [ { slot: 0, kind: 0x46, payload: 0 }, { slot: 1, kind: 0x47, payload: 6 } ]
	};
	for (
		const [id, item] of [
			[ "slot:13", bag ],
			[ "item-mall-slot:13", bag ],
			[ "slot:6", equipment ],
			[ "avatar:1", avatar ],
			[ "cos-player:13", bag ],
			[ "alchemy-slot:13", bag ],
			[ "cos-slot:13", pet ],
			[ "shop-offer:0", offer ],
			[ "shop-buyback:0", sold ],
			[ "hotbar:0", bag ],
			[ "hotbar:1", equipment ]
		]
	) assert.equal( tooltipItems( id, game, 42 )[0], item, id );
	assert.deepEqual( tooltipItems( "cos-slot:13", game, 43 ), [] );
	assert.deepEqual( tooltipItems( "shop-buyback:1", game, 42 ), [] );
	game.shop.buyback[0] = { item: { ...sold, plus: 12 } };
	assert.equal( tooltipItems( "shop-buyback:0", game, 42 )[0].plus, 12 );
});
test("native actions replace sit/run help together with movement mode and preserve packed hotbar payload", () => {
	const game = { quickSlots: [ { slot: 41, kind: 0x4a, payload: 0x010003e8 } ] },
		actions = [ { id: 4000, name: "HELLO" }, { id: 1015, name: "NO_NATIVE_HELP" } ];
	assert.equal( actionTooltipKey( "hotbar:41", game, actions, 4 ), "UIIT_CTL_STAND" );
	assert.equal( actionTooltipKey( "action:1000", game, actions, 0 ), "UIIT_CTL_SIT" );
	assert.equal( actionTooltipKey( "action:1001", game, actions, 2 ), "UIIT_CTL_RUN" );
	assert.equal( actionTooltipKey( "action:1001", game, actions, 3 ), "UIIT_CTL_WALK" );
	assert.equal( actionTooltipKey( "action:1015", game, actions, 0 ), undefined );
	assert.equal( actionTooltipKey( "action:4000", game, actions, 0 ), "HELLO" );
});
test("every published mastery and skill-group description has authored help and images", () => {
	const masteries = decodeTooltipMasteries( json( "data/skillMasteryData.json" ) ), ui = json( "data/skillUi.json" );
	for ( const m of ui.masteries ) {
		const row = masteries.get( m.id );
		assert.ok( row, m.id );
		assert.ok( text( row.name ) );
		assert.ok( text( row.description ), row.description );
		const rows = masteryTooltip(
			row,
			{ level: 1, skillPoints: 0, masteries: [ { id: m.id, level: 1 } ] },
			{ 1: 5 },
			text
		);
		assert.ok( rows.some( r => r.value.includes( text( row.description ) ) ) );
		assert.equal( defined( rows.at( -1 ) ).color, 0xffff4a4a );
		assert.equal( defined( rows.at( -2 ) ).color, 0xffff4a4a );
	}
	for ( const g of ui.groups ) {
		if ( !text( g.name ) ) {
			const native = JSON.parse( readFileSync( "tests/fixtures/tooltip-retail-text.json", "utf8" ) );
			assert.equal( native.texts[g.name], "", g.name + " must have native empty-cell evidence" );
		}
		const path = "../../.generated/client-public/assets/images/Media_extracted/icon/" +
			g.icon.replace( /^icon[\\/]/i, "" ).replaceAll( "\\", "/" ).replace( ".ddj", "_focus.png" );
		assert.ok( existsSync( path ), path );
	}
	const row = masteries.get( 257 );
	assert.ok(
		masteryTooltip( row, { level: 120, masteries: [ { id: 257, level: 120 } ] }, {}, text ).every( r =>
			r.value !== text( "PARAM_CONDITION_OF_NEXT_LEVEL" )
		)
	);
	assert.equal(
		defined(
			masteryTooltip( row, { level: 1, skillPoints: 0, masteries: [ { id: 257, level: 0 } ] }, {}, text ).at( -1 )
		)
			.color,
		0xffefdaa4
	);
});
test("native abnormal labels use all 23 code/category branches, with level and description", () => {
	const bits = [ 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 24 ];
	for ( const bit of bits ) {
		const rows = abnormalTooltip( bit, 7, text );
		assert.ok( rows.length >= 2, "bit " + bit );
		assert.ok( rows[0].value.includes( "7" ) );
		assert.ok( rows[1].value.includes( text( "UIIT_STT_MACROPOTION_ABNORMAL" ) ) );
	}
	for ( const bit of [ 12, 23, 25, 31 ] ) assert.deepEqual( abnormalTooltip( bit, 7, text ), [] );
});
test("buff help uses effect instance lifetime, does not show learning requirements, and retires on teardown", () => {
	const catalog = decodeTooltipSkills( json( "data/skillData.json" ) ),
		row = [ ...catalog.values() ].find( r => r.masteryId && r.reqLearnSp && text( r.nameSymbol ) );
	const source = { kind: "effect", gid: 42, token: 2, skill: defined( row ).id },
		effect = { gid: 42, token: 2, skill: defined( row ).id, phase: 2, remainingMs: 61000, receivedAtMs: 1000 },
		game = { vitals: [], attachedEffects: [ effect ] };
	const rows = buffTooltip( source, game, 1000, catalog, text );
	assert.ok( rows.some( r => r.value.includes( text( "UIIT_STT_REMAIN_TIME" ) ) ) );
	assert.ok( !rows.some( r => r.value.includes( text( "PARAM_CONDITION_OF_LEARN" ) ) ) );
	assert.notDeepEqual( buffTooltip( source, game, 2000, catalog, text ), rows );
	game.attachedEffects = [];
	assert.deepEqual( buffTooltip( source, game, 2000, catalog, text ), [] );
});
test("native helper registry has stateful equipment and blocking help without invented zoom help", () => {
	for (
		const [id, avatar, tab, key] of [
			[ "equipment-view", false, 0, "UIIT_STT_AVATAR_VIEW_AVATARSLOT" ],
			[ "equipment-view", true, 0, "UIIT_STT_AVATAR_VIEW_EQUIPSLOT" ],
			[ "blocking-help", false, 0, "UIIT_STT_CHATING_SHUT_SAVE_SERVER_HELP" ],
			[ "blocking-help", false, 1, "UIIT_STT_CHATING_SHUT_SAVE_PC_HELP" ]
		]
	) {
		assert.equal( hudTooltipKey( id, avatar, tab ), key );
		assert.ok( text( key ) );
	}
	assert.equal( hudTooltipKey( "minimap-in" ), undefined );
	assert.equal( hudTooltipKey( "minimap-out" ), undefined );
});
test("commerce previews validate item identity and byte scalars at packet admission", () => {
	const preview = { refObjId: 1, typeFlags: 0x8ec, name: "Potion", body: [ 1, 0, 0, 0, 5, 0 ] },
		payload = {
			version: 1,
			npc: 1,
			name: "Shop",
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: 1,
				name: "Potion",
				price: "5",
				maxStack: 50,
				previews: [ preview ]
			} ]
		};
	const encode = () => new TextEncoder().encode( JSON.stringify( payload ) );
	assert.equal( defined( shopCatalog( encode() ).offers[0].previews )[0].body[4], 5 );
	preview.body[0] = 2;
	assert.throws( () => shopCatalog( encode() ), /preview/ );
	preview.body[0] = 1;
	preview.body[4] = 256;
	assert.throws( () => shopCatalog( encode() ), /integer/ );
});
test("shop and buyback retain serialized enhancement, variance and magic instead of borrowing bag instances", () => {
	const owner = createInventory( () => {} ),
		body = plus => [ 1, 0, 0, 0, plus, 123, 0, 0, 0, 0, 0, 0, 0, 30, 0, 0, 0, 1, 1, 0, 0, 0, 7, 0, 0, 0 ];
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ { refObjId: 1, typeFlags: 0x132c, name: "Sword", nativeFields: { itemClass: 1 } } ],
		equipItems: [ { slot: 20, refObjId: 1, body: body( 2 ) } ]
	} );
	const preview = plus => ({ refObjId: 1, typeFlags: 0x132c, name: "Sword", body: body( plus ) }),
		encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	owner.openShop( 42, 0 );
	owner.receive(
		11,
		encode( {
			version: 1,
			npc: 42,
			name: "Smith",
			offers: [ {
				tab: 0,
				slot: 0,
				refObjId: 1,
				name: "Sword",
				price: "100",
				maxStack: 1,
				previews: [ preview( 7 ) ]
			} ],
			buyback: [ {
				index: 0,
				id: 9,
				refObjId: 1,
				name: "Sword",
				quantity: 1,
				price: "90",
				plus: 9,
				preview: preview( 9 )
			} ]
		} )
	);
	const state = owner.state(), shop = state.shop;
	assert.equal( defined( defined( shop ).offers[0].items )[0].plus, 7 );
	assert.equal( defined( defined( defined( shop ).buyback )[0].item ).plus, 9 );
	assert.equal( state.inventory[0].plus, 2 );
	assert.equal( defined( defined( defined( shop ).buyback )[0].item ).variance, "123" );
	assert.deepEqual( defined( defined( defined( shop ).buyback )[0].item ).magic, [ "30064771073" ] );
	assert.equal( defined( defined( defined( shop ).offers[0].items )[0].tooltip ).fields.itemClass, 1 );
	assert.equal( owner.state().shop, shop, "presentation is retained" );
	owner.openShop( 42, 10 );
	const bad = {
		version: 1,
		npc: 42,
		name: "Smith",
		offers: [ {
			tab: 0,
			slot: 0,
			refObjId: 1,
			name: "Sword",
			price: "100",
			maxStack: 1,
			previews: [ { ...preview( 7 ), body: body( 7 ).slice( 0, -1 ) } ]
		} ]
	};
	assert.throws( () => owner.receive( 11, encode( bad ) ), /Truncated/ );
	assert.equal( owner.state().shop, shop, "bad preview cannot partially replace catalogue" );
	owner.clear();
	assert.equal( owner.state().shop, undefined );
});
