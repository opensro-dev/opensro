/*
===========================================================================

cos-hud.test.mjs - asset-independent item-target cursor and confirmation lifecycle

The HUD nominates an occupied slot; the worker validates it on confirmation.
Targeting survives a closed inventory but not world exit or source replacement.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCosHud } = await import( "../../src/engine/runtime/ui/hud/cos-hud.ts" );

/*
================
clockItem
================
*/
function clockItem() {
	return {
		slot: 13,
		refObjId: 8985,
		typeFlags: 0x66ec,
		quantity: 1,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: []
	};
}

test("clock targeting confirms any occupied slot exactly once", () => {
	for ( const slot of [ 0, 13, 14, 44 ] ) {
		const hud = createCosHud(), clock = clockItem();
		hud.armItemTarget( clock );
		assert.equal( hud.itemTargetCursor(), 0xa6 );
		assert.equal( hud.reconcileItemTarget( [ clock ], true ), false );
		assert.equal( hud.chooseItemTarget( undefined ), false );
		assert.equal( hud.itemTargetCursor(), 0xa6 );
		assert.equal( hud.chooseItemTarget( { ...clock, slot } ), true );
		assert.equal( hud.itemTargetCursor(), null );
		assert.equal( hud.chooseItemTarget( { ...clock, slot: 45 } ), false );
		assert.deepEqual( hud.takeTargetUse(), { kind: "item-use", slot: 13, summonerSlot: slot } );
		assert.equal( hud.takeTargetUse(), null );
		assert.equal( hud.itemTargetCursor(), null );
	}
});

test("clock cancellation and world/source changes clear both cursor and confirmation", () => {
	const clock = clockItem();
	for ( const confirmed of [ false, true ] ) {
		for ( const change of [ "cancel", "reset", "world", "removed", "replaced", "depleted" ] ) {
			const hud = createCosHud();
			hud.armItemTarget( clock );
			if ( confirmed ) hud.chooseItemTarget( { ...clock, slot: 14 } );
			if ( change === "cancel" ) hud.takeTargetUse();
			else if ( change === "reset" ) hud.reset();
			else {
				const items = change === "removed" ? [] : [ {
					...clock,
					refObjId: change === "replaced" ? 10 : clock.refObjId,
					quantity: change === "depleted" ? 0 : 1
				} ];
				assert.equal( hud.reconcileItemTarget( items, change !== "world" ), true );
			}
			assert.equal( hud.itemTargetCursor(), null, change );
			assert.equal( hud.targetUse(), null, change );
			assert.equal( hud.chooseItemTarget( clock ), false, change );
		}
	}
});
