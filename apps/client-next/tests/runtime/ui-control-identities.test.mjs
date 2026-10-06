/*
===========================================================================

ui-control-identities.test.mjs - simultaneous frame and content controls

BUG-064: close and cancel perform the same action but remain distinct
controls. Exercise the production HUD, not a list copied from its source.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const { emptyStall } = await import( "../../src/engine/foundation/gameplay/stall.ts" );

for ( const panel of [ "magic-option", "skin", "exchange" ] ) {
	for ( const action of [ "close", "cancel" ] ) {
		test(`${panel}: ${action} remains independently clickable across reopen`, t => {
			const commands = [], f = uiFixture( command => commands.push( command ) );
			t.after( () => f.dispose() );
			Object.assign( f.state.entities[0], { refObjId: 1907, bodyShape: 0x22 } );
			Object.assign( f.state.gameplay, {
				inventorySlotCount: 45,
				equipmentSlotCount: 13,
				playerModels: [ { refObjId: 1907, sex: 1 }, { refObjId: 1920, sex: 0 } ],
				inventory: [ {
					slot: 13,
					refObjId: 1,
					typeFlags: (3 << 2) | (3 << 5) | (13 << 7) | (9 << 11),
					quantity: 1,
					name: "Skin scroll",
					icon: "item/etc/hp_potion_01.ddj"
				} ]
			} );
			let now = 0;
			for ( let cycle = 0; cycle < 2; cycle++ ) {
				if ( panel === "magic-option" ) {
					Object.assign( f.state.gameplay, {
						magicOption: { visible: true, phase: "idle", npc: 2, item: null, error: null, parts: [] }
					} );
				} else if ( panel === "exchange" ) {
					Object.assign( f.state.gameplay, {
						exchange: {
							open: true,
							partner: 2,
							own: [],
							theirs: [],
							ownGold: 0,
							theirGold: 0,
							ownLocked: cycle > 0,
							theirLocked: false,
							approved: false,
							requesting: false
						}
					} );
				}
				let presentation = f.ui.step( f.state, now += 100 );
				if ( panel === "skin" ) {
					f.ui.event( { kind: "key", code: "KeyI" } );
					f.ui.step( f.state, now += 100 );
					f.ui.event( { kind: "double-activate", id: "slot:13" } );
				}
				for ( let step = 0; step < 16; step++ ) presentation = f.ui.step( f.state, now += 100 ) ?? presentation;
				assert.ok( presentation, `${panel} cycle ${cycle}` );
				for ( const suffix of [ "close", "cancel" ] ) {
					const button = presentation.controls.find( control => control.id === `${panel}-${suffix}` );
					assert.ok( button, `${panel}-${suffix} was admitted` );
					assert.equal( !!button.disabled, false );
				}
				commands.length = 0;
				f.ui.event( { kind: "activate", id: `${panel}-${action}` } );
				assert.deepEqual(
					commands,
					panel === "skin" ? [] : [ {
						kind: "gameplay",
						command: { kind: panel === "exchange" ? "exchange-cancel" : "magic-option-close" }
					} ]
				);
				if ( panel === "magic-option" ) Object.assign( f.state.gameplay, { magicOption: undefined } );
				if ( panel === "exchange" ) Object.assign( f.state.gameplay, { exchange: undefined } );
				const closed = f.ui.step( f.state, now += 100 );
				assert.ok( !closed?.controls.some( control => control.id === `${panel}-cancel` ) );
			}
		});
	}
}

for ( const service of [ "exchange", "stall", "stall-net" ] ) {
	test(`${service}: delayed artwork cannot replay the inventory's controls`, t => {
		let held = true;
		const f = uiFixture(
			() => {},
			path =>
				held && path.endsWith(
					service === "exchange" ?
						"/exc_box.png" :
						service === "stall" ?
						"/stl_slot_01.png" :
						"/gil_subj_button16.png"
				)
		);
		t.after( () => f.dispose() );
		f.ui.step( f.state, 0 );
		f.ui.event( { kind: "key", code: "KeyI" } );
		let semantics;
		for ( let i = 1; i < 16; i++ ) semantics = f.ui.step( f.state, i * 100 ) ?? semantics;
		assert.ok( semantics?.controls.some( control => control.id === "main-popup-drag" ) );
		if ( service === "exchange" ) {
			Object.assign( f.state.gameplay, {
				exchange: {
					open: true,
					partner: 2,
					own: [],
					theirs: [],
					ownGold: 0,
					theirGold: 0,
					ownLocked: false,
					theirLocked: false,
					approved: false,
					requesting: false
				}
			} );
		} else {
			const stall = emptyStall();
			Object.assign( f.state.gameplay, {
				stall: service === "stall" ?
					{ ...stall, phase: "owner", owner: 1 } :
					{ ...stall, network: { ...stall.network, open: true } }
			} );
		}
		for ( let i = 16; i < 32; i++ ) semantics = f.ui.step( f.state, i * 100 ) ?? semantics;
		assert.ok( semantics );
		assert.equal( semantics.controls.filter( control => control.id === "main-popup-drag" ).length, 1 );
		assert.ok( !semantics.controls.some( control => control.id === `${service}-close` ), "service remains cold" );
		held = false;
		for ( let i = 32; i < 48; i++ ) semantics = f.ui.step( f.state, i * 100 ) ?? semantics;
		assert.ok( semantics );
		assert.ok(
			semantics.controls.some( control => control.id === `${service}-close` ),
			"service admits independently: " + JSON.stringify( f.ui.stats() )
		);
		assert.equal( semantics.controls.filter( control => control.id === "main-popup-drag" ).length, 1 );
	});
}
