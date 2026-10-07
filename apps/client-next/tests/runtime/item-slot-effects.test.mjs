/*
===========================================================================

item-slot-effects.test.mjs - native item-slot overlays and their timing

Pins the CIFSlotWithHelp sheets: the rare shine's 8x4 frames at 40 ms
(54FA60/555110/565850), the summoned glow's 9 frames at 50 ms, the one-shot
revival (8 x 80 ms) and item-changed (4x4 x 50 ms, 48 px) flashes, the dead
companion wash, and which 0x3645 updates raise a flash.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const fx = await import( "../../src/engine/foundation/ui/item-slot-effects.ts" );
/** @type {[number, number, number, number]} */
const RECT = [ 100, 200, 32, 32 ];
const RARE = { tooltip: { fields: { rarity: 2 } } };

test("rare items shine through 32 frames of an 8x4 sheet, 40 ms apart", () => {
	assert.deepEqual( fx.itemSlotOverlays( { tooltip: { fields: { rarity: 0 } } }, RECT, 0, 0 ), [] );
	const at = ms => fx.itemSlotOverlays( RARE, RECT, 0, ms )[0];
	assert.ok( at( 0 ).path.endsWith( "icon/item/etc/icon_edge_rare.png" ) );
	assert.deepEqual( at( 0 ).uv, [ 0, 0, 0.125, 0.25 ] );
	assert.deepEqual( at( 40 ).uv, [ 0.125, 0, 0.125, 0.25 ] );
	assert.deepEqual( at( 9 * 40 ).uv, [ 0.125, 0.25, 0.125, 0.25 ] );
	assert.deepEqual( at( 32 * 40 ).uv, at( 0 ).uv, "the counter wraps" );
	assert.deepEqual( at( 0 ).rect, RECT );
	// The bind-time phase rand() & 31 shifts the cycle.
	assert.deepEqual( fx.itemSlotOverlays( RARE, RECT, 31, 0 )[0].uv, [ 0.125, 0, 0.125, 0.25 ] );
});

test("a summoned companion's item glows; a dead one's is washed", () => {
	const glow = fx.itemSlotOverlays( { summon: { state: 2 } }, RECT, 0, 50 )[0];
	assert.ok( glow.path.endsWith( "interface/pet/pt_edge_effect.png" ) );
	assert.deepEqual( glow.uv, [ 1 / 9, 0, 1 / 9, 1 ] );
	assert.deepEqual( fx.itemSlotOverlays( { summon: { state: 3 } }, RECT, 0, 50 ), [] );
	assert.deepEqual( fx.itemSlotWash( { summon: { state: 4 } } ), [ 0, 0x4b / 255, 0x7e / 255, 0x80 / 255 ] );
	assert.equal( fx.itemSlotWash( { summon: { state: 2 } } ), null );
});

test("one-shot flashes run their frames once", () => {
	const life = ms => fx.itemSlotOverlays( {}, RECT, 0, ms, [ { kind: "life", atMs: 1000 } ] );
	assert.deepEqual( life( 1000 )[0].uv, [ 0, 0, 1 / 8, 1 ] );
	assert.deepEqual( life( 1000 + 7 * 80 )[0].uv, [ 7 / 8, 0, 1 / 8, 1 ] );
	assert.deepEqual( life( 1000 + 8 * 80 ), [] );
	const changed = ms => fx.itemSlotOverlays( {}, RECT, 0, ms, [ { kind: "changed", atMs: 0 } ] );
	assert.deepEqual( changed( 5 * 50 )[0].uv, [ 0.25, 0.25, 0.25, 0.25 ] );
	assert.deepEqual( changed( 0 )[0].rect, [ 92, 192, 48, 48 ] );
	assert.deepEqual( changed( 16 * 50 ), [] );
});

test("0x3645 raises the changed flash for a new type and the revival flash for 4 -> 3", () => {
	assert.deepEqual( fx.itemSlotFlashKinds( 0x01, undefined, undefined ), [ "changed" ] );
	assert.deepEqual( fx.itemSlotFlashKinds( 0x40, 4, 3 ), [ "life" ] );
	assert.deepEqual( fx.itemSlotFlashKinds( 0x40, 2, 3 ), [] );
	assert.deepEqual( fx.itemSlotFlashKinds( 0x41, 4, 3 ), [ "changed", "life" ] );
	assert.deepEqual( fx.itemSlotFlashKinds( 0x08, 4, 3 ), [] );
});

test("a repair flash runs 20 frames of the 8x4 sheet over 72 px, before the changed flash", () => {
	const repair = ms => fx.itemSlotOverlays( {}, RECT, 0, ms, [ { kind: "repair", atMs: 1000 } ] );
	assert.ok( repair( 1000 )[0].path.endsWith( "icon/icon_mall_repair.png" ) );
	assert.deepEqual( repair( 1000 )[0].uv, [ 0, 0, 1 / 8, 1 / 4 ] );
	assert.deepEqual( repair( 1000 + 9 * 50 )[0].uv, [ 1 / 8, 1 / 4, 1 / 8, 1 / 4 ] );
	assert.deepEqual( repair( 1000 + 19 * 50 )[0].uv, [ 3 / 8, 2 / 4, 1 / 8, 1 / 4 ] );
	assert.deepEqual( repair( 1000 + 20 * 50 ), [] );
	assert.deepEqual( repair( 999 ), [] );
	// 5669B3: 72 px from 20 px above and left of the slot.
	assert.deepEqual( repair( 1000 )[0].rect, [ 80, 180, 72, 72 ] );
	const both = fx.itemSlotOverlays( {}, RECT, 0, 0, [ { kind: "changed", atMs: 0 }, { kind: "repair", atMs: 0 } ] );
	assert.deepEqual( both.map( o => o.path.split( "/" ).at( -1 ) ), [
		"icon_mall_repair.png",
		"icon_mall_transgender.png"
	] );
});
