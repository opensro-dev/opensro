/*
===========================================================================

item-tooltip-set.test.mjs - the set bonus block of an equipment tooltip

The block lists the item's set, how many pieces are worn and each tier,
marking the one in force. A shard that publishes no sets, or an item that
names none, shows nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { itemTooltipSet, itemTooltipSetName, tooltipSetViewer, wornSetPieces, wornSetSlots } = await import(
	"../../src/engine/foundation/ui/item-tooltip-set.ts"
);
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { sideBubble } = await import( "../../src/engine/foundation/ui/helper-bubble.ts" );
const { publishedEquipmentSets } = await import( "../../src/engine/foundation/gameplay/equipment-sets.ts" );

const SETS = publishedEquipmentSets( [ {
	id: 13,
	name: "SN_ITEM_11_SET_A_DEFENSE",
	pieces: [ { slot: 1, name: "SN_HEAD" }, { slot: 2, name: "SN_SHOULDER" }, { slot: 3, name: "SN_CHEST" } ],
	tiers: [
		{ pieces: 2, options: [ { option: 389, value: 1 } ] },
		{ pieces: 4, options: [ { option: 389, value: 2 }, { option: 390, value: 10 } ] },
		{ pieces: 6, options: [ { option: 389, value: 4 } ] }
	]
} ] );
// A build without the later text: every symbol answers empty, so the English fallbacks show.
const noText = () => "";
const TEXTS = {
	SN_HEAD: "Headgear",
	SN_SHOULDER: "Shell",
	SN_CHEST: "Lamellar",
	SN_ITEM_11_SET_A_DEFENSE: "Destruction"
};
const text = symbol => TEXTS[symbol] ?? "";

/*
================
piece

A worn or carried item as the tooltip reads it: its slot (the equipment slot, below 13 when worn),
its set id and its worn position (TypeID4 in the type flags).
================
*/
/** @returns {any} */
function piece( slot, setId, position = 1 ) {
	return { slot, typeFlags: 0x2c | (3 << 7) | (position << 11), tooltip: { fields: { setId } } };
}

test("worn pieces count by set and ignore the bag", () => {
	const counts = wornSetPieces( [ piece( 1, 13 ), piece( 2, 13 ), piece( 3, 0 ), piece( 13, 13 ), piece( 4, 14 ) ] );
	assert.equal( counts.get( 13 ), 2 );
	assert.equal( counts.get( 14 ), 1 );
	assert.equal( counts.has( 0 ), false );
	assert.deepEqual( [
		...(wornSetSlots( [ piece( 1, 13, 1 ), piece( 2, 13, 3 ), piece( 14, 13, 2 ) ] ).get( 13 ) ?? [])
	], [ 1, 3 ] );
});

test("the block follows the later client's layout: name and count, pieces, tiers with their effects", () => {
	const worn = [ piece( 1, 13, 1 ), piece( 2, 13, 3 ) ];
	const rows = itemTooltipSet( worn[0], SETS, worn, text );
	assert.deepEqual( rows.map( r => r.value ), [
		"Destruction (2/6)",
		"Headgear",
		"Shell",
		"Lamellar",
		" ",
		"Destruction(2)",
		"Set enforcement : 1 enhancement",
		" ",
		"Destruction(4)",
		"Set enforcement : 2 enhancement",
		"Increase all resistance : 10 Increase"
	] );
	// Worn pieces are bright, the others dim; the tier in force is in colour, higher tiers dim.
	const byValue = value => defined( rows.find( r => r.value === value ) );
	assert.equal( byValue( "Headgear" ).color, 0xffffffff );
	assert.equal( byValue( "Lamellar" ).color, 0xffffffff );
	assert.notEqual( byValue( "Shell" ).color, 0xffffffff );
	assert.equal( byValue( "Destruction(2)" ).color, byValue( "Destruction (2/6)" ).color );
	assert.equal( byValue( "Destruction(4)" ).color, byValue( "Shell" ).color );
	assert.equal( byValue( "Set enforcement : 1 enhancement" ).color, 0xff2ee6f0 );
});

test("the tiers shown are those reached and the next one, as the later client's panel", () => {
	const tiersOf = count =>
		itemTooltipSet(
			piece( 1, 13 ),
			SETS,
			Array.from( { length: count }, ( _, n ) => piece( n + 1, 13, n + 1 ) ),
			text
		)
			.map( r => r.value ).filter( value => /^Destruction\(\d\)$/.test( value ) );
	assert.deepEqual( tiersOf( 0 ), [ "Destruction(2)" ] );
	assert.deepEqual( tiersOf( 1 ), [ "Destruction(2)" ] );
	assert.deepEqual( tiersOf( 2 ), [ "Destruction(2)", "Destruction(4)" ] );
	assert.deepEqual( tiersOf( 4 ), [ "Destruction(2)", "Destruction(4)", "Destruction(6)" ] );
	assert.deepEqual( tiersOf( 6 ), [ "Destruction(2)", "Destruction(4)", "Destruction(6)" ] );
});

test("a set without tiers (weapons, shields) shows its name under the title and no block", () => {
	const weapons = publishedEquipmentSets( [ { id: 39, name: "SN_ITEM_11_SET_A_WEAPON", tiers: [] } ] );
	assert.equal( itemTooltipSetName( piece( 1, 39 ), weapons, noText )?.value, "Power" );
	assert.deepEqual( itemTooltipSet( piece( 1, 39 ), weapons, [ piece( 1, 39 ) ], noText ), [] );
});

test("a build without the later text still names the set and the effects in English", () => {
	const rows = itemTooltipSet( piece( 1, 13 ), SETS, [ piece( 1, 13 ) ], noText );
	assert.equal( rows[0].value, "Destruction (1/6)" );
	assert.ok( rows.some( r => r.value === "Set enforcement : 1 enhancement" ) );
	assert.equal( rows.some( r => r.value === "Headgear" ), false, "a piece without text is not listed" );
});

test("below the first tier nothing is in force", () => {
	const rows = itemTooltipSet( piece( 1, 13 ), SETS, [ piece( 1, 13 ) ], text );
	assert.equal( rows.some( r => r.color === 0xff2ee6f0 ), false );
});

test("an item without a published set shows nothing", () => {
	assert.deepEqual( itemTooltipSet( piece( 1, 0 ), SETS, [], text ), [] );
	assert.deepEqual( itemTooltipSet( piece( 1, 99 ), SETS, [], text ), [] );
	assert.deepEqual( itemTooltipSet( piece( 1, 13 ), [], [], text ), [] );
	assert.equal( itemTooltipSetName( piece( 1, 0 ), SETS, text ), undefined );
	assert.equal( itemTooltipSetName( piece( 1, 13 ), SETS, text )?.value, "Destruction" );
});

test("a malformed set list is refused whole and an absent one is empty", () => {
	assert.deepEqual( publishedEquipmentSets( undefined ), [] );
	assert.throws( () => publishedEquipmentSets( [ { id: -1, tiers: [] } ] ) );
	assert.throws( () =>
		publishedEquipmentSets( [ { id: 1, tiers: [ { pieces: 2, options: [ { option: 389 } ] } ] } ] )
	);
	assert.throws( () => publishedEquipmentSets( [ { id: 1, pieces: [ { slot: 99, name: "x" } ], tiers: [] } ] ) );
	assert.throws( () => publishedEquipmentSets( {} ) );
	assert.deepEqual( publishedEquipmentSets( [ { id: 1, tiers: [] } ] )[0].pieces, [] );
});

test("the Experimental row and a published set list decide whether the tooltip has a set block", () => {
	const bag = [ piece( 1, 13 ) ];
	assert.equal( tooltipSetViewer( false, SETS, bag ), undefined, "row off" );
	assert.equal( tooltipSetViewer( true, [], bag ), undefined, "a shard that publishes no sets" );
	assert.equal( tooltipSetViewer( true, undefined, bag ), undefined, "no list at all" );
	const viewer = tooltipSetViewer( true, SETS, bag );
	assert.equal( viewer?.groups, SETS, "the groups are the published ones, for memoising" );
	assert.equal( viewer?.equipment, bag );
	assert.deepEqual( tooltipSetViewer( true, SETS, undefined )?.equipment, [] );
});

test("an armor tooltip carries only the set's name; the pieces and tiers are the side box", () => {
	// Every stat column reads as 1, so the armor rows build; only the set rows are under test.
	const fields = new Proxy( { setId: 13, rarity: 0, itemClass: 1 }, {
		get: ( o, k ) => (k in o ? o[k] : 1),
		has: () => true
	} );
	/** @type {any} */ const item = {
		name: "Piece",
		slot: 1,
		typeFlags: 0x2c | (3 << 7) | (1 << 11),
		quantity: 1,
		plus: 0,
		variance: 0,
		magic: [],
		tooltip: { fields }
	};
	const rowsOf = viewer => itemTooltip( item, { masteries: [] }, text, viewer ).map( row => row.value );
	const plain = rowsOf( undefined );
	assert.equal( plain.some( value => value.includes( "Destruction" ) ), false, "no viewer, no set rows" );
	const shown = rowsOf( { sets: tooltipSetViewer( true, SETS, [ item ] ) } );
	assert.equal( shown[2], "Destruction", "the set's name sits under the title row and its spacer" );
	assert.equal( shown.length, plain.length + 1, "only the name joins the item's own box" );
	assert.equal(
		shown.some( value => value.includes( "Destruction (" ) ),
		false,
		"the block is not in the item's box"
	);
	assert.deepEqual(
		rowsOf( { sets: tooltipSetViewer( false, SETS, [ item ] ) } ),
		plain,
		"row off leaves the native rows"
	);
	const side = itemTooltipSet( item, SETS, [ item ], text );
	assert.equal( side[0].value, "Destruction (1/6)" );
	assert.equal( side[0].ornament, "item", "the side box's heading carries the icon" );
});

test("the side box stands right of the item's box with the borders meeting, else left of it", () => {
	const measure = s => s.length * 7, rows = [ { value: "Destruction (2/6)", color: 0xffffffff } ];
	/** @type {[number, number, number, number]} */ const main = [ 300, 100, 200, 400 ];
	/** @type {[number, number, number, number]} */ const viewport = [ 0, 0, 1200, 900 ];
	const right = sideBubble( rows, main, viewport, measure ).quads[0].rect;
	assert.equal( right[0], 300 + 200 + 16, "8 for the main box's border and 8 for its own" );
	assert.equal( right[1], 112, "12 pixels below the main box's top, as in the later client" );
	// No room on the right: the side box goes to the left, its right border against the main box's left border.
	/** @type {[number, number, number, number]} */ const crowded = [ 900, 100, 200, 400 ];
	const left = sideBubble( rows, crowded, viewport, measure ).quads[0].rect;
	assert.equal( left[0] + left[2] + 8 + 8, 900, "borders meet at the main box's left edge" );
	assert.ok( left[0] >= 0 );
	assert.ok( right[0] > main[0] + main[2] && left[0] + left[2] < crowded[0] );
});
