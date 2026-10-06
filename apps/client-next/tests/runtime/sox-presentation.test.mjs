/*
===========================================================================

sox-presentation.test.mjs - seal tooltip style and authored bow effect identity

The requested reference places the seal below the heading. Native style 3
selects bold for the heading, seal and magic attributes. Effect tests compile
the published programs, including their moving BAN paths, without substitutes.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
const { itemTooltip } = await import( "../../src/engine/foundation/ui/item-tooltip.ts" );
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );

/*
================
bow
================
*/
/** @param {number} tier @returns {import('../../src/engine/contracts/gameplay').InventoryItem} */
function bow( tier ) {
	const fields = Object.fromEntries( [
		"varianceIntMin1c0",
		"varianceIntMax1c4",
		"varianceIntMin240",
		"varianceIntMax244",
		"varianceIntMin248",
		"varianceIntMax24c",
		"varianceIntMin254",
		"varianceIntMax258",
		"varianceIntMin25c",
		"varianceIntMax260",
		"varianceFloatPerPlus250",
		"varianceFloatPerPlus264",
		"varianceIntMin288",
		"varianceIntMax28c",
		"varianceFloatPerPlus290",
		"varianceIntMin294",
		"varianceIntMax298",
		"varianceFloatMin268",
		"varianceFloatMax26c",
		"varianceFloatMin270",
		"varianceFloatMax274",
		"varianceFloatMin278",
		"varianceFloatMax27c",
		"varianceFloatMin280",
		"varianceFloatMax284",
		"varianceFloatMin1c8",
		"varianceFloatMax1cc",
		"varianceFloatPerPlus1d0",
		"varianceFloatMin1f4",
		"varianceFloatMax1f8",
		"varianceFloatPerPlus1fc",
		"varianceIntMin1ec",
		"varianceIntMax1f0",
		"varianceIntMin1d4",
		"varianceIntMax1d8",
		"varianceFloatPerPlus1dc",
		"varianceFloatMin20c",
		"varianceFloatMax210",
		"varianceFloatMin214",
		"varianceFloatMax218",
		"varianceFloatMin1e0",
		"varianceFloatMax1e4",
		"varianceFloatPerPlus1e8",
		"varianceFloatMin200",
		"varianceFloatMax204",
		"varianceFloatPerPlus208"
	].map( key => [ key, 0 ] ) );
	return {
		slot: 35,
		refObjId: 4161 + tier,
		name: "Bronz Bow",
		typeFlags: 13100,
		quantity: 1,
		plus: 0,
		durability: 53,
		variance: "0",
		magic: [ String( 3n << 32n | 1n ) ],
		tooltip: { fields: { ...fields, rarity: 2, itemClass: 4 + tier, reqLevelType1: 1, requiredLevel: 8 } },
		magicReferences: [ { paramId: 1, optionName: "MATTR_STR", paramName: "+str", degree: 2 } ]
	};
}

test("all three seals follow the bold heading and precede ordinary equipment details", () => {
	const seals = [ "PARAM_RARE_FIRST", "PARAM_RARE_SECOND", "PARAM_RARE_THIRD" ];
	for ( const [tier, seal] of seals.entries() ) {
		const rows = itemTooltip( bow( tier ), { level: 24, masteries: [] }, symbol => symbol );
		const visible = rows.filter( row => row.value.trim() );
		assert.equal( visible[0].value, "Bronz Bow" );
		assert.equal( visible[0].strong, true );
		assert.equal( visible[1].value, seal );
		assert.equal( visible[1].strong, true );
		assert.equal( visible[1].color, visible[0].color );
		assert.equal( visible.filter( row => row.value === seal ).length, 1 );
		const kind = visible.find( row => row.value.startsWith( "UIIT_STT_WEAPON_TYPE" ) );
		const requirement = visible.find( row => row.value === "PARAM_REQ_LV 8" );
		const magic = visible.find( row => row.value.startsWith( "PARAM_STR 3" ) );
		assert.ok( kind && requirement && magic );
		assert.ok( !kind.strong && !requirement.strong, "ordinary details retain normal weight" );
		assert.equal( magic.strong, true );
	}
});

test("non-rare items do not gain a seal or lose the native bold heading", () => {
	const source = bow( 0 );
	const item = { ...source, tooltip: { fields: { ...source.tooltip?.fields, rarity: 0 } } };
	const rows = itemTooltip( item, { masteries: [] }, symbol => symbol );
	assert.equal( rows[0].strong, true );
	assert.equal( rows.some( row => row.value.startsWith( "PARAM_RARE_" ) ), false );
});

test("all equipment families show capacity and preserve their native description and degree branches", () => {
	for ( const family of [ 1, 2, 3, 4, 5, 6, 7, 9, 10, 11, 12, 13 ] ) {
		const source = bow( 0 );
		const item = {
			...source,
			typeFlags: 44 | family << 7 | 1 << 11,
			tooltip: {
				descriptionSymbol: "DESCRIPTION",
				fields: {
					...source.tooltip?.fields,
					maxMagicOptions51c: 7
				}
			}
		};
		const rows = itemTooltip( item, { level: 24, masteries: [] }, symbol => symbol );
		const capacity = rows.findIndex( r => r.value === "UIIT_STT_AVATAR_MAGICOPTION_MAXCOUNT: 7UIIT_STT_UNIT" );
		assert.ok( capacity > 0, `family ${family}` );
		assert.equal( rows.filter( r => r.value.includes( "MAXCOUNT" ) ).length, 1 );
		assert.ok( !rows[capacity].strong );
		const description = rows.findIndex( r => r.value === "DESCRIPTION" );
		assert.ok( description > 0 );
		assert.equal( description < capacity, family === 7 || family === 13 );
		assert.equal(
			rows.some( r => r.value.includes( "UIIT_TOOLTIP_EQUIPMENT_CLASS" ) ),
			family !== 7 && family !== 13
		);
	}
});

test("Star Moon and Sun bows compile distinct moving effect paths and texture layers", () => {
	const roster = readPublishedAssetJsonSync( "assets/char/roster.json", publicRoot );
	const bytes = readPublishedAssetBytesSync( "assets/effects/programs.json", publicRoot );
	const signatures = [];
	for ( const [tier, suffix] of [ "a", "b", "c" ].entries() ) {
		const path = `system/system_rarebow_${suffix}.efp`;
		assert.equal( roster.dress.specialGlows[4161 + tier][0].effectPath, path );
		const { model, imagePaths } = createEffectPrograms().decode( bytes, path );
		const paths = model.particleGraph?.filter( row => row.positions.length > 0 ) ?? [];
		assert.equal( paths.length, tier + 1, "authored tiers have one, two and three moving paths" );
		for ( const motion of paths ) {
			assert.equal( motion.positions.length, 30 );
			assert.equal( motion.rotations.length, 30 );
			assert.ok(
				motion.positions.some( p => p.some( ( value, axis ) => value !== motion.positions[0][axis] ) ),
				"the bow sparkle follows its path instead of sitting at its origin"
			);
		}
		assert.ok( model.primitives.length > 0 );
		signatures.push( JSON.stringify( imagePaths ) );
	}
	assert.equal( new Set( signatures ).size, 3, "tiers are distinct layers, not one brightness parameter" );
});
