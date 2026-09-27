/*
===========================================================================

tooltips.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { readLocalizedTextDataRowsSync } from "../../../../scripts/build/shared/textDataIo.mjs";
import { retailTextdataRoot } from "../../../../scripts/build/world/paths.mjs";
import {
	ITEM_TEXT_COMPLETIONS,
	completeItemText,
	completeItemTextProjection,
	completeItemReferenceProjection
} from "../../../../scripts/build/shared/itemTextCompletions.mjs";
import { assertItemNameCoverage } from "../../../../scripts/build/shared/itemNameCoverage.mjs";
import nameCompletions from "../../../../scripts/build/shared/itemNameCompletions.json" with { type: "json" };
import { readPublishedAssetJsonSync, readPackedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";
async function load( file ) {
	return import( sourceFileUrl( file ).href );
}
const { decodeTooltipSkills } = await load( "src/engine/foundation/ui/skill-tooltip-catalog.ts" );
const { skillTooltip } = await load( "src/engine/foundation/ui/skill-tooltip.ts" );
const { itemTooltipStats } = await load( "src/engine/foundation/ui/item-tooltip-stats.ts" );
const { tooltipBubble } = await load( "src/engine/foundation/ui/helper-bubble.ts" );
const source = JSON.parse( readFileSync( "../../.generated/client-public/assets/data/skillData.json", "utf8" ) );
const catalog = decodeTooltipSkills( source );
const strings = {
	...JSON.parse( readFileSync( "../../.generated/client-public/assets/text/textuisystem.en.json", "utf8" ) ).entries,
	...JSON.parse( readFileSync( "../../.generated/client-public/assets/text/textdataname.en.json", "utf8" ) ).entries
};
const text = s => strings[s] ?? "";
test("published native catalogue admits every skill and rejects shifted columns", () => {
	assert.equal( catalog.size, 27835 );
	assert.throws( () => decodeTooltipSkills( { ...source, columns: source.columns.slice( 1 ) } ), /catalogue/ );
});
test("all published skill tooltips compose without unresolved prerequisite or parameter crashes", () => {
	const progression = { level: 1, skillPoints: 0, masteries: [] };
	let descriptions = 0;
	for ( const row of catalog.values() ) {
		if ( !row.masteryId || !row.reqLearnSp ) continue;
		const rows = skillTooltip( row.id, catalog, [], progression, text, id => String( id ) );
		assert.ok( rows.length > 1 );
		if ( row.tooltipDescriptionSymbol && text( row.tooltipDescriptionSymbol ) ) descriptions++;
	}
	assert.ok( descriptions > 100 );
});
test("learning requirements change color at the authoritative threshold; learned skill omits current learn heading", () => {
	const row = [ ...catalog.values() ].find( r =>
		r.reqLearnSp > 0 && r.basicActivity === 1 && r.masteryId && r.basicLevel === 1 &&
		!r.reqGroups.some( q => q.groupId && q.groupId !== r.groupId )
	);
	assert.ok( row );
	const low = skillTooltip( row.id, catalog, [], { skillPoints: 0, masteries: [] }, text, () => "Mastery" );
	assert.ok( low.some( r => r.color === 0xffff4a4a ) );
	assert.ok( low.some( r => r.value === text( "PARAM_CONDITION_OF_LEARN" ) ) );
	const high = skillTooltip(
		row.id,
		catalog,
		[ row.id ],
		{ skillPoints: 1000000, masteries: [ { id: row.masteryId, level: 120 } ] },
		text,
		() => "Mastery"
	);
	assert.ok( !high.some( r => r.value === text( "PARAM_CONDITION_OF_LEARN" ) ) );
	assert.ok( high.some( r => r.value === text( "PARAM_CONDITION_OF_NEXT_LEVEL" ) ) );
});
test("weapon variance, opt level and broken durability use native display rules", () => {
	const fields = {};
	for (
		const name of [
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
			"varianceFloatMax284"
		]
	) fields[name] = 0;
	Object.assign( fields, {
		varianceIntMin1c0: 20,
		varianceIntMax1c4: 40,
		varianceIntMin240: 10,
		varianceIntMax244: 41,
		varianceIntMin248: 20,
		varianceIntMax24c: 51,
		varianceFloatPerPlus250: 3
	} );
	const item = {
		typeFlags: 0x132c,
		plus: 5,
		variance: String( 31n << 20n ),
		magic: [],
		durability: 0,
		tooltip: { fields }
	};
	const rows = itemTooltipStats( item, s => s );
	assert.equal( rows.find( r => r.value.startsWith( "PARAM_PA" ) ).value, "PARAM_PA 41 ~ 51 (+100%)" );
	assert.equal( rows.find( r => r.value.startsWith( "PARAM_DUR" ) ).color, 0xffff4a4a );
});
test("tooltip wrapping retains row colors and blank separators across viewport edge placement", () => {
	const result = tooltipBubble(
		[ { value: "First", color: 0xffffffff }, { value: " ", color: 0 }, {
			value: "Unmet requirement",
			color: 0xffff4a4a
		} ],
		[ 1150, 850, 32, 32 ],
		[ 0, 0, 1200, 900 ],
		s => s.length * 7
	);
	assert.equal( result.lines.length, 3 );
	assert.equal( result.lines[2].color, 0xffff4a4a );
	assert.ok( result.lines[0].rect[0] < 1150 );
	assert.ok( result.lines.at( -1 ).rect[1] + 18 <= 885 );
});

test("native tooltip border opacity is independent of fill, text and child ornaments", () => {
	const result = tooltipBubble(
		[ { value: "Skill", color: 0xffffffff }, { value: "Attack", color: 0xffff4a4a, ornament: "diamond" } ],
		[ 300, 100, 32, 32 ],
		[ 0, 0, 1200, 900 ],
		s => s.length * 7
	);
	assert.deepEqual(
		result.quads[0].color,
		[ 26 / 255, 35 / 255, 69 / 255, 181 / 255 ],
		"679190 fill uses 0xB51A2345"
	);
	const border = result.quads.filter( q => /com_tooltip_(corner|edge)\.png$/.test( q.texture ) );
	assert.equal( border.length, 8 );
	assert.ok( border.every( q => q.sampling === "nearest" ), "native stretch border uses point filtering" );
	for ( const q of border ) {
		assert.deepEqual(
			q.color,
			[ 1, 1, 1, 196 / 255 ],
			"6791C0 display alpha overrides construction before 6FB030"
		);
	}
	assert.equal(
		result.quads.find( q => q.texture.endsWith( "com_diamond.png" ) ).color[3],
		1,
		"child ornament does not inherit parent border alpha"
	);
	assert.deepEqual( result.lines.map( row => row.color ), [ 0xffffffff, 0xffff4a4a ] );
	assert.deepEqual( border.slice( 4 ).map( q => q.uvTurn ), [ 0, 1, 0, 3 ] );
});

const { itemTooltip } = await load( "src/engine/foundation/ui/item-tooltip.ts" );
const { itemTooltipMagic } = await load( "src/engine/foundation/ui/item-tooltip-magic.ts" );
const { tooltipDescription } = await load( "src/engine/foundation/ui/tooltip-description.ts" );
const { itemTooltipReference, itemMagicReferences } = await load(
	"src/engine/foundation/gameplay/item-tooltip-reference.ts"
);
test("native reference names survive admission; malformed numeric fields and bounds fail closed", () => {
	const fields = { itemParam1_29c: 120, itemParam2_2a0: 25 };
	const ref = itemTooltipReference( fields, "SN_POTION_DESC" );
	fields.itemParam1_29c = 9;
	assert.equal( ref.fields.itemParam1_29c, 120 );
	assert.equal( ref.descriptionSymbol, "SN_POTION_DESC" );
	assert.throws( () => itemTooltipReference( { itemParam1_29c: NaN } ) );
	assert.throws( () =>
		itemMagicReferences( [ {
			paramId: 1,
			degree: 1,
			optionName: "MATTR_STR",
			paramName: "+str",
			rangeWords: [ 1, -1, 0 ]
		} ] )
	);
});
// 175 retail English descriptions plus 71 from the English completion layer
// (englishCompletions/textdataname.json).
test("all 246 English published rich item descriptions preserve native color and emphasis without leaking markup", () => {
	let count = 0;
	for ( const [key, value] of Object.entries( strings ) ) {
		if ( !key.includes( "ITEM" ) || !value.startsWith( "<sml2>" ) ) continue;
		count++;
		const rows = tooltipDescription( value );
		assert.ok( rows.length );
		assert.ok( rows.every( row => !/<[^>]+>/.test( row.value ) ) );
	}
	assert.equal( count, 246 );
	const rows = tooltipDescription(
		'<sml2><strong><font color="255,236,219,156">Heading</font></strong><br>Body</sml2>'
	);
	assert.equal( rows[0].strong, true );
	assert.equal( rows[0].color, 0xffecdb9c );
	assert.equal( rows[1].strong, false );
});
test("magic brackets resolve item degree, zero STR follows native fallback, repair count excludes consumed charge", () => {
	const refs = [
		{ paramId: 1, optionName: "MATTR_STR", paramName: "+str", degree: 1, rangeWords: [ 65538, 196608, 0 ] },
		{ paramId: 2, optionName: "MATTR_STR", paramName: "+str", degree: 2, rangeWords: [ 131075, 262144, 0 ] },
		{ paramId: 3, optionName: "MATTR_REPAIR", paramName: "+repair", degree: 1 }
	];
	const item = {
		magic: [ String( 3n << 32n | 1n ) ],
		magicReferences: refs,
		tooltip: { fields: { itemClass: 4 } },
		typeFlags: 0x132c
	};
	assert.equal( itemTooltipMagic( item, s => s )[0].value, "PARAM_STR 3 PARAM_INCREASE (+50%)" );
	item.tooltip.fields.itemClass = 1;
	item.magic = [ "1" ];
	assert.equal( itemTooltipMagic( item, s => s )[0].value, "PARAM_STR 1 PARAM_INCREASE (+0%)" );
	item.magic = [ String( 1n << 32n | 3n ) ];
	assert.equal( itemTooltipMagic( item, s => s )[0].value, "PARAM_REPAIR (0UIIT_STT_COUNT)" );
});
test("consumable tooltip keeps flat and ratio recovery separate; equipment enhancement suffix is not suppressed for EU armor", () => {
	const item = {
		name: "Potion",
		typeFlags: 0x8ec,
		plus: 0,
		magic: [],
		tooltip: { fields: { itemParam1_29c: 120, itemParam2_2a0: 25, itemParam3_2a4: 80, itemParam4_2a8: 10 } }
	};
	const rows = itemTooltip( item, { masteries: [] }, s => s );
	assert.ok( rows.some( r => r.value === "PARAM_HEAL_HP[120]" ) );
	assert.ok( rows.some( r => r.value === "PARAM_HEAL_HP[25]%" ) );
	assert.ok( rows.some( r => r.value === "PARAM_HEAL_MP[80]" ) );
	const armor = { name: "EU armor", typeFlags: 0xdac, plus: 3, magic: [] };
	assert.equal( itemTooltip( armor, { masteries: [] }, s => s )[0].value, "EU armor (+3)" );
});

// Read-only retail capture: English cells for these developer-spawnable items
// are empty. A Korean fallback is not retail English and renders as ???? in
// the Latin atlas. An empty heading must not suppress the valid amount rows.
test("retail empty item text preserves quantity without invented names or foreign fallback", async () => {
	const { itemTooltip } = await load( "src/engine/foundation/ui/item-tooltip.ts" );
	const raw = Object.fromEntries(
		readLocalizedTextDataRowsSync( path.join( retailTextdataRoot, "textdataname.txt" ) ).map(
			row => [ row[1], row[8] ]
		)
	);
	for ( const symbol of Object.keys( ITEM_TEXT_COMPLETIONS ) ) {
		assert.ok( raw[symbol] === undefined || /^(?:-|0)?$/.test( raw[symbol] ), symbol );
	}
	const retailText = s => Object.hasOwn( raw, s ) ? raw[s] : text( s );
	const rows = itemTooltip(
		{
			name: "",
			quantity: 1,
			typeFlags: 0xeec,
			magic: [],
			tooltip: { descriptionSymbol: "SN_ITEM_ETC_SPEED_UP_BASIC_TT_DESC", fields: { maxStack: 20 } }
		},
		{ masteries: [] },
		retailText
	);
	assert.ok( rows.some( r => r.value === text( "PARAM_MAX_CONTAIN" ) + " 20" ) );
	assert.ok( rows.some( r => r.value === text( "UIIT_STT_AMOUNT" ) + " 1" ) );
	assert.ok( !rows.some( r => r.heading || r.value.includes( "ITEM_" ) || /[\uac00-\ud7af]/.test( r.value ) ) );
});

const { createUiPreparation } = await load( "src/engine/foundation/ui/ui.ts" );
test("sampling changes invalidate retained commands, survive admission and reject invalid modes", () => {
	const owner = createUiPreparation(),
		q = {
			rect: [ 0, 0, 8, 8 ],
			clip: [ 0, 0, 100, 100 ],
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ],
			texture: "frame"
		},
		scene = { revision: 1, width: 100, height: 100, quads: [ q ] };
	const first = owner.prepare( scene );
	const next = owner.prepare( { ...scene, revision: 2, quads: [ { ...q, sampling: "nearest" } ] } );
	assert.equal( next.scene.quads[0].sampling, "nearest" );
	assert.notEqual( next.scene.quads[0], first.scene.quads[0] );
	assert.throws( () => owner.prepare( { ...scene, quads: [ { ...q, sampling: "invalid" } ] } ), /sampling/ );
	assert.equal(
		owner.prepare( { ...scene, revision: 3, quads: [ { ...q, sampling: "nearest" } ] } ).scene.quads[0],
		next.scene.quads[0]
	);
});

const { buffTooltip } = await load( "src/engine/foundation/ui/buff-tooltip.ts" );
test("reported beginner speed scroll uses native effect level and measured tooltip bounds", () => {
	const row = catalog.get( 31107 );
	assert.ok( row );
	assert.equal( row.basicLevel, 1 );
	assert.equal( text( row.nameSymbol ), ITEM_TEXT_COMPLETIONS.SN_ITEM_ETC_SPEED_UP_BASIC );
	const game = {
		localGid: 1,
		attachedEffects: [ { gid: 1, skill: 31107, token: 7, phase: 2, remainingMs: 2516000, receivedAtMs: 1000 } ]
	};
	const rows = buffTooltip( { kind: "buff", gid: 1, skill: 31107, token: 7 }, game, 1000, catalog, text );
	assert.equal( rows[0].value, ITEM_TEXT_COMPLETIONS.SN_ITEM_ETC_SPEED_UP_BASIC + " Lv 1\n " );
	const bubble = tooltipBubble( rows, [ 300, 100, 20, 20 ], [ 0, 0, 1200, 900 ], s => s.length * 7 );
	const [x, y, w, h] = bubble.quads[0].rect;
	assert.equal( w, Math.max( ...bubble.lines.map( r => r.value.length * 7 ) ) );
	assert.equal( h, bubble.lines.length * 18 );
	assert.deepEqual( bubble.quads[1].rect, [ x - 8, y - 8, 8, 8 ] );
	assert.deepEqual( bubble.quads[3].rect, [ x + w, y + h, 8, 8 ] );
	assert.equal( bubble.lines[0].rect[0], x );
});

const { createInventory } = await load(
	"src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
test("item localization is shared by published descriptions and server inventory references", () => {
	const source = readFileSync( path.join( retailTextdataRoot, "textdataname.txt" ) );
	const projected = completeItemTextProjection( source ), again = completeItemTextProjection( projected );
	assert.deepEqual(
		readFileSync( "../../.generated/game-data/1.150/server/textdata/textdataname.txt" ),
		projected,
		"installed server projection uses the same completion policy"
	);
	assert.deepEqual( again, projected, "rebuilding must be idempotent" );
	const before = source.toString( "utf16le" ).split( /\r\n?/ ),
		after = projected.toString( "utf16le" ).split( /\r\n?/ );
	assert.equal( after.length - before.length, 17, "only the absent name records are appended" );
	let changed = 0;
	for ( let i = 0; i < before.length - 1; i++ ) {
		if ( before[i] !== after[i] ) {
			const a = before[i].split( "\t" ), b = after[i].split( "\t" );
			assert.equal( b[8], ITEM_TEXT_COMPLETIONS[a[1]] );
			b[8] = a[8];
			assert.deepEqual( b, a );
			changed++;
		}
	}
	assert.equal( changed, 557 );
	const official = completeItemText( {
		SN_ITEM_MALL_MOVE_SPEED_UP_100: "Official future translation",
		unrelated: ""
	} );
	assert.equal( official.SN_ITEM_MALL_MOVE_SPEED_UP_100, "Official future translation" );
	assert.equal( official.unrelated, "" );
	const rawName = Object.fromEntries( after.map( r => r.split( "\t" ) ).map( r => [ r[1], r[8] ] ) );
	const published = readPublishedAssetJsonSync(
		"/assets/text/textdataname.en.json",
		path.resolve( "../../.generated/client-public" )
	).entries;
	const packed = JSON.parse(
		readPackedAssetBytesSync(
			"/assets/text/textdataname.en.json",
			path.resolve( "../../.generated/client-public" )
		).toString( "utf8" )
	).entries;
	for ( const [key, value] of Object.entries( ITEM_TEXT_COMPLETIONS ) ) {
		assert.equal( rawName[key], value );
		assert.equal( published[key], value );
		assert.equal( packed[key], value );
	}
	assert.deepEqual( assertItemNameCoverage( retailTextdataRoot, published ), {
		activeItems: 8439,
		itemTitles: 7394
	} );
	const broken = { ...published, SN_ITEM_ETC_DETECT_01: "-" };
	assert.throws( () => assertItemNameCoverage( retailTextdataRoot, broken ), /SN_ITEM_ETC_DETECT_01/ );
	const inventory = createInventory( () => {} );
	for (
		const [refObjId, symbol] of [ [ 9263, "SN_ITEM_MALL_MOVE_SPEED_UP_50" ], [
			9264,
			"SN_ITEM_MALL_MOVE_SPEED_UP_100"
		], [ 24198, "SN_ITEM_ETC_SPEED_UP_BASIC" ] ]
	) {
		const reference = {
			refObjId,
			typeFlags: 0xeec,
			name: rawName[symbol],
			icon: "item/etc/mall_move_speed_up_100.ddj",
			nativeFields: { maxStack: 10 },
			descriptionSymbol: symbol + "_TT_DESC"
		};
		inventory.bootstrap( { refItemSnapshot: [ reference ] } );
		const item = inventory.present( { slot: 13, refObjId, typeFlags: 0xeec, quantity: 10, magic: [] } );
		const rows = itemTooltip( item, { masteries: [] }, s => published[s] ?? text( s ) );
		assert.equal( rows[0].value, ITEM_TEXT_COMPLETIONS[symbol] );
		assert.ok( rows[0].heading );
		assert.ok( rows.some( r => r.value === ITEM_TEXT_COMPLETIONS[symbol + "_TT_DESC"] ) );
		assert.ok( rows.some( r => r.value === text( "UIIT_STT_AMOUNT" ) + " 10" ) );
	}
});

test("item completions replace only catalogued blank or dash cells and preserve record bytes", () => {
	const symbol = "SN_ITEM_ETC_MP_POTION_05", expected = "MP recovery potion (X-large)";
	for ( const value of [ "", "  ", "-", " - ", "0", "xxx", "NULL" ] ) {
		assert.equal( completeItemText( { [symbol]: value } )[symbol], expected );
	}
	const untouched = { [symbol]: "Official translation", SN_UNLISTED: "-", SN_ITEM_ETC_HP_POTION_05: undefined };
	const completed = completeItemText( { ...untouched } );
	for ( const [key, value] of Object.entries( untouched ) ) assert.equal( completed[key], value );
	const added = completeItemText( {} );
	assert.equal( added[symbol], undefined );
	assert.deepEqual(
		Object.keys( added ).sort(),
		Object.entries( nameCompletions ).filter( ( [, r] ) => r.absent ).map( ( [s] ) => s ).sort()
	);
	const row = value =>
		[ "1", symbol, "Korean\ncontinued", "", "", "Chinese", "", "Japanese", value, "Other" ].join( "\t" );
	const tail = `\r\n//${row( "-" )}\r\n1\t${symbol}\tshort\r\n1\tSN_UNLISTED\t0\t0\t0\t0\t0\t0\t-\r\n${
		row( "Official translation" )
	}\r\n`;
	for ( const encoding of [ "utf8", "utf16le" ] ) {
		const before = Buffer.from( "\ufeff" + row( " - " ) + tail, encoding );
		const after = completeItemTextProjection( before );
		const prefix = Buffer.from( "\ufeff" + row( expected ) + tail, encoding );
		assert.deepEqual( after.subarray( 0, prefix.length ), prefix );
		assert.deepEqual( completeItemTextProjection( after ), after );
	}
});

test("all catalog completions carry readable English and source labels; currency repairs change only the name key", () => {
	for ( const [symbol, entry] of Object.entries( nameCompletions ) ) {
		assert.match( symbol, /^SN_ITEM_/ );
		assert.ok( entry.source && entry.basis, symbol );
		assert.match( entry.english, /^[\x20-\x7e]+$/ );
		assert.ok( !/undefined|^[-0]$/.test( entry.english ), symbol );
	}
	const row = "1\t1\tITEM_ETC_GOLD_01\tGold\txxx\txxx\txxx_TT_DESC\t0\t0";
	const untouched = "1\t42\tITEM_UNKNOWN\tUnknown\txxx\txxx\txxx_TT_DESC\t0\t0";
	for ( const encoding of [ "utf8", "utf16le" ] ) {
		const source = Buffer.from( "\ufeff" + row + "\r\n" + untouched + "\r\n//" + row + "\r\n", encoding );
		const result = completeItemReferenceProjection( source );
		assert.deepEqual(
			result,
			Buffer.from(
				"\ufeff" + row.replace( "\txxx\txxx\t", "\txxx\tSN_ITEM_ETC_GOLD\t" ) + "\r\n" + untouched + "\r\n//" +
					row + "\r\n",
				encoding
			)
		);
		assert.deepEqual( completeItemReferenceProjection( result ), result );
	}
});

test("X-large potions and all purification pills retain their title, ornament and stack rows", () => {
	const projected = readFileSync( "../../.generated/game-data/1.150/server/textdata/textdataname.txt", "utf16le" );
	const names = Object.fromEntries(
		projected.split( /\r\n?/ ).map( r => r.split( "\t" ) ).map( r => [ r[1], r[8]?.trim() ] )
	);
	const inventory = createInventory( () => {} );
	for (
		const [refObjId, suffix, expected] of [
			[ 8, "HP_POTION_05", "HP recovery potion (X-large)" ],
			[ 15, "MP_POTION_05", "MP recovery potion (X-large)" ],
			[ 10368, "CURE_RANDOM_01", "Purification pill (small)" ],
			[ 10369, "CURE_RANDOM_02", "Purification pill (medium)" ],
			[ 10370, "CURE_RANDOM_03", "Purification pill (large)" ],
			[ 10371, "CURE_RANDOM_04", "Purification pill (X-large)" ]
		]
	) {
		const symbol = "SN_ITEM_ETC_" + suffix, typeFlags = suffix.startsWith( "CURE" ) ? 0x96c : 0x8ec;
		assert.equal( names[symbol], expected );
		assert.equal( text( symbol ), expected );
		inventory.bootstrap( {
			refItemSnapshot: [ { refObjId, name: names[symbol], typeFlags, nativeFields: { maxStack: 50 } } ]
		} );
		const item = inventory.present( { slot: 13, refObjId, typeFlags, quantity: 3, magic: [] } );
		const rows = itemTooltip( item, { masteries: [] }, text );
		assert.equal( rows[0].value, expected );
		assert.equal( rows[0].heading, true );
		assert.equal( rows[0].ornament, "item" );
		assert.ok( rows.some( r => r.value === text( "PARAM_MAX_CONTAIN" ) + " 50" ) );
		assert.ok( rows.some( r => r.value === text( "UIIT_STT_AMOUNT" ) + " 3" ) );
		const bubble = tooltipBubble( rows, [ 300, 100, 32, 32 ], [ 0, 0, 1200, 900 ], s => s.length * 7 );
		assert.equal( bubble.lines.filter( r => r.heading ).map( r => r.value.trim() ).join( " " ), expected );
		assert.ok( bubble.quads.some( q => q.texture.endsWith( "com_itemsign.png" ) ) );
	}
});

// ASM 84C900/84C911/84C9BA: argument words must never become instructions.
test("native tooltip parameter cursor consumes complete setv saps and stns blocks", () => {
	for (
		const [tail, offset, values] of [
			[ [ 0x73657476, 0x45325341, 20, 0x64757261, 0x64757261, 123 ], null, null ],
			[ [ 0x73617073, 11, 22, 0x64757261, 123 ], 0x30, [ 11, 22 ] ],
			[ [ 0x73746e73, 11, 22, 33, 0x64757261, 123 ], 0x70, [ 11, 22, 33 ] ]
		]
	) {
		const cells = source.rows[0].split( "\t" );
		for ( let i = 69; i < 118; i++ ) cells[source.columns.indexOf( i )] = String( defined( tail )[i - 69] ?? 0 );
		cells[source.columns.indexOf( 9 )] = "0";
		const row = [ ...decodeTooltipSkills( { ...source, rows: [ cells.join( "\t" ) ] } ).values() ][0];
		assert.equal( row.directTooltipParams.durationMs, 123 );
		if ( offset !== null ) {
			assert.deepEqual(
				row.directTooltipParams.nativeParamBlocks.find( b => b.offset === offset )?.values,
				values
			);
		} else assert.deepEqual( row.directTooltipParams.setValues, [ { code: 0x45325341, value: 20 } ] );
	}
});
