/*
===========================================================================

stall-presentation.test.mjs - native trading-state branches and warm admission

The real HUD must replace the invalid authored icon before admission, draw
the native open/closed resource, and preserve owner/visitor control gates.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { emptyStall } = await import( "../../src/engine/foundation/gameplay/stall.ts" );
const { stallTradingPresentation } = await import( "../../src/engine/runtime/ui/hud/stall-hud.ts" );

const ICON_ROOT = "/assets/images/Media_extracted/interface/stall/";
const ITEM_ICON = "/assets/images/Media_extracted/icon/item/etc/hp_potion_01.png";

for ( const owner of [ true, false ] ) {
	test(`${owner ? "owner" : "visitor"} stall admits and updates every trading-state branch`, t => {
		const f = uiFixture();
		t.after( () => f.dispose() );
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		let now = 100;
		for ( const open of [ false, true, false, true ] ) {
			const stall = {
				...emptyStall(),
				phase: owner ? "owner" : "visitor",
				owner: owner ? 1 : 2,
				open,
				offers: [ {
					slot: 0,
					bagSlot: 13,
					quantity: 2,
					price: 12345,
					item: { slot: 13, refObjId: 1, quantity: 2, name: "Potion", icon: "item/etc/hp_potion_01.ddj" }
				} ]
			};
			Object.assign( f.state.gameplay, { stall } );
			let frame;
			for ( let i = 0; i < 20; i++ ) frame = f.ui.step( f.state, now += 100 ) ?? frame;
			assert.ok( frame );
			const control = id => frame.controls.find( row => row.id === id );
			assert.ok( control( owner ? "stall-close" : "stall-leave" ), "stall admits with retail artwork" );
			assert.equal( frame.controls.filter( row => row.id === "main-popup-drag" ).length, 1 );
			assert.equal( control( "stall-modify:0" )?.disabled, !owner || open );
			assert.equal( control( "stall-modify:1" ), undefined, "empty offers have no price button" );
			assert.equal( control( "stall-slot:0" )?.disabled, owner ? open : !open );
			assert.equal( control( "stall-slot:1" )?.disabled, owner ? open : true );
			assert.equal( !!control( "stall-trading" ), owner );
			assert.equal( control( "stall-change-title" )?.disabled, owner ? open : undefined );
			const quads = f.scenes.at( -1 )?.quads ?? [];
			assert.equal( quads.filter( row => row.texture === ICON_ROOT + "stl_slot_02.png" ).length, 1 );
			assert.equal( quads.filter( row => row.texture === ICON_ROOT + "stl_slot_05.png" ).length, 9 );
			assert.ok( f.hasText( "12,345" ), "price uses native three-digit grouping" );
			const expected = ICON_ROOT + `stl_condition_icon_${open ? "01" : "02"}.png`;
			assert.ok( quads.some( row => row.texture === expected ), expected );
			assert.ok(
				!quads.some( row => row.texture === ICON_ROOT + `stl_condition_icon_${open ? "02" : "01"}.png` )
			);
			assert.ok(
				quads.some( row => row.texture === ITEM_ICON && row.color[3] === (!owner && !open ? 128 / 255 : 1) )
			);
			assert.ok(
				!f.requested.includes( ICON_ROOT + "stl_condition_icon_1.png" ),
				"authored placeholder is never requested"
			);
			const presentation = stallTradingPresentation( owner, open );
			assert.equal( presentation.status, open ? "UIIT_STT_TRADING_NOW" : "UIIT_STT_STALL_MODIFYING" );
			assert.equal( presentation.toggle, open ? "UIIT_STT_END_STALL" : "UIIT_STT_START_STALL" );
		}
	});
}
