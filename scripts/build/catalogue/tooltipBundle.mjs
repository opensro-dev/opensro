/*
===========================================================================

tooltipBundle.mjs - the catalogue's tooltip/ folder: the game's item tooltip
for the website

The site's profiles show a worn item's tooltip as the game prints it, so
the catalogue ships the game's own code rather than numbers to re-derive:

	tooltip/item-tooltip.mjs   siteTooltip.mjs bundled with the client's
	                           unmodified tooltip modules (pure ES module)
	tooltip/items/<id>.json    one item's name, type flags, native tooltip
	                           fields and description text
	tooltip/magic.json         every magic-option definition and the text
	                           the tooltip code asks for
	tooltip/golden.json        instances and the exact rows the game prints,
	                           checked here against the shipped bundle

The text table is recorded, not listed: every symbol the tooltip code asks
for while drawing every item, and every magic option on a weapon, an armour
piece, an accessory and an avatar part, goes into magic.json; an item's
own description text goes into its shard.

===========================================================================
*/
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { build } from "esbuild";
import { createMagicTable, itemTooltipRows, TOOLTIP_FORMAT } from "./siteTooltip.mjs";

const ENTRY = fileURLToPath( new URL( "./siteTooltip.mjs", import.meta.url ) );
const CLIENT_TSCONFIG = fileURLToPath( new URL( "../../../apps/client-next/tsconfig.json", import.meta.url ) );
// Every 5-bit variance field at its top value (31): the best roll.
const BEST_VARIANCE = ((1n << 35n) - 1n).toString();
// A reinforcement the golden vectors show.
const GOLDEN_PLUS = 7;
// A magic-option value past every format's thresholds (a timed option
// longer than a minute), so the recorded text covers every branch.
const LONG_OPTION_VALUE = 3661000;
// The item families whose tooltips format magic options differently.
const SAMPLE_FAMILIES = [ "weapon/", "armor/", "accessory/", "avatar/" ];
// The magic-option magnitudes sit in the high dword of the encoded u64.
const MAGNITUDE_SHIFT = 32n;

/*
================
encodeMagic

One option as the wire and the public API carry it: the param id in the
low 16 bits, the value in the high dword.
================
*/
function encodeMagic( paramId, value ) {
	return ((BigInt( value ) << MAGNITUDE_SHIFT) | BigInt( paramId )).toString();
}

/*
================
topValue

An option's highest bracket value (its packed range words), or 1.
================
*/
function topValue( row ) {
	let top = 1;
	for ( const word of row.rangeWords ?? [] ) {
		if ( word >>> 16 ) top = word >>> 16;
		if ( word & 0xffff ) top = word & 0xffff;
	}
	return top;
}

/*
================
itemDegree

The degree an item's options are graded at (55B608: class zero is degree
one by signed division toward zero).
================
*/
function itemDegree( item ) {
	return Math.trunc( ((item.fields.itemClass ?? 1) - 1) / 3 ) + 1;
}

/*
================
shardItem

The per-item input of itemTooltipRows, from the Go stage's reference.
================
*/
function shardItem( reference ) {
	return {
		name: reference.name,
		typeFlags: reference.typeFlags,
		fields: reference.nativeFields,
		...(reference.descriptionSymbol ? { descriptionSymbol: reference.descriptionSymbol } : {})
	};
}

/*
================
goldenInstances

The instances golden.json pins: a plain drop, a +7 best roll, a SoX item
with blues, an armour piece with % / count / red options, an accessory, an
avatar part and a timed reinforcement option. Each names its item by
codename and its options by MATTR name; a missing one fails the build.
================
*/
function goldenInstances() {
	return [
		{ label: "plain drop", codename: "ITEM_CH_SWORD_08_A", plus: 0, variance: "0", options: [] },
		{
			label: "+7 best roll",
			codename: "ITEM_CH_SWORD_08_A",
			plus: GOLDEN_PLUS,
			variance: BEST_VARIANCE,
			options: []
		},
		{
			label: "SoX with blues",
			codename: "ITEM_CH_SWORD_08_A_RARE",
			plus: 9,
			variance: BEST_VARIANCE,
			options: [ "MATTR_STR", "MATTR_INT", "MATTR_HP", "MATTR_DUR", "MATTR_HR", "MATTR_LUCK" ]
		},
		{
			label: "armour, % / count / red options",
			codename: "ITEM_CH_M_HEAVY_08_BA_A",
			plus: 3,
			variance: "123456789",
			durability: 1,
			options: [ "MATTR_ER", "MATTR_RESIST_FROSTBITE", "MATTR_ATHANASIA", "MATTR_DEC_MAXDUR" ]
		},
		{
			label: "accessory",
			codename: "ITEM_CH_RING_08_A",
			plus: 0,
			variance: BEST_VARIANCE,
			options: [ "MATTR_RESIST_POISON", "MATTR_MP" ]
		},
		{
			// The option exists at degrees 1 and 2 only; on a higher degree
			// the game prints no line for it (55B632).
			label: "timed reinforcement",
			codename: "ITEM_CH_SWORD_01_A",
			plus: 2,
			variance: "0",
			options: [ "MATTR_REINFORCE_ITEM" ],
			value: 90500
		},
		{
			label: "avatar part",
			codename: "ITEM_EVENT_SILSAMO_M",
			plus: 0,
			variance: "0",
			options: [ "MATTR_AVATAR_STR" ]
		}
	];
}

/*
================
writeTooltipBundle

Writes out/tooltip. items are items.json's rows (for codenames),
references the Go stage's item references by id, magicRows its magic
definitions, text the client's text lookup.
================
*/
export async function writeTooltipBundle( { out, items, references, magicRows, text } ) {
	const dir = path.join( out, "tooltip" );
	mkdirSync( path.join( dir, "items" ), { recursive: true } );
	const table = createMagicTable( magicRows );
	const used = new Map(), descriptions = new Set();
	const record = symbol => {
		const value = text( symbol );
		if ( symbol ) used.set( symbol, value );
		return value;
	};
	const shards = new Map();
	for ( const reference of references.values() ) {
		if ( !reference.nativeFields ) continue;
		const item = shardItem( reference );
		shards.set( reference.refObjId, item );
		if ( item.descriptionSymbol ) descriptions.add( item.descriptionSymbol );
		itemTooltipRows( { refObjId: reference.refObjId }, item, table, record );
		itemTooltipRows(
			{ refObjId: reference.refObjId, plus: GOLDEN_PLUS, variance: BEST_VARIANCE },
			item,
			table,
			record
		);
	}
	// Every option's text, on an item of each family the formats branch on
	// and of the option's own degree: the game prints a positive option only
	// where a definition of the item's degree exists (55B632).
	const samples = new Map();
	for ( const row of items ) {
		const item = shards.get( row.id ), family = SAMPLE_FAMILIES.find( prefix => row.type?.startsWith( prefix ) );
		if ( !item || !family ) continue;
		const key = family + itemDegree( item );
		if ( !samples.has( key ) ) samples.set( key, row.id );
	}
	for ( const row of magicRows ) {
		for ( const family of SAMPLE_FAMILIES ) {
			const id = samples.get( family + row.degree ) ?? samples.get( family + 1 );
			if ( id === undefined ) continue;
			// Its top value and a long one: a duration past a minute takes the
			// minutes text too.
			for ( const value of [ topValue( row ), LONG_OPTION_VALUE ] ) {
				const instance = { refObjId: id, magic: [ encodeMagic( row.paramId, value ) ] };
				itemTooltipRows( instance, shards.get( id ), table, record );
			}
		}
	}
	for ( const [id, item] of shards ) {
		const own = item.descriptionSymbol ? { [item.descriptionSymbol]: text( item.descriptionSymbol ) } : {};
		writeFileSync( path.join( dir, "items", id + ".json" ), JSON.stringify( { ...item, text: own } ) + "\n" );
	}
	const common = Object.fromEntries( [ ...used ].filter( ( [symbol] ) => !descriptions.has( symbol ) ).sort() );
	writeFileSync(
		path.join( dir, "magic.json" ),
		JSON.stringify( { format: TOOLTIP_FORMAT, references: magicRows, text: common } ) + "\n"
	);
	const golden = goldenVectors( items, shards, table, magicRows, text );
	writeFileSync(
		path.join( dir, "golden.json" ),
		JSON.stringify( { format: TOOLTIP_FORMAT, vectors: golden } ) + "\n"
	);
	await build( {
		entryPoints: [ ENTRY ],
		outfile: path.join( dir, "item-tooltip.mjs" ),
		bundle: true,
		format: "esm",
		platform: "neutral",
		target: "es2020",
		tsconfig: CLIENT_TSCONFIG,
		legalComments: "none",
		logLevel: "warning"
	} );
	await checkBundle( dir, golden, shards, magicRows, common, text );
	return { items: shards.size, symbols: Object.keys( common ).length, vectors: golden.length };
}

/*
================
goldenVectors

Each golden instance and the rows the unbundled client code prints for it.
================
*/
function goldenVectors( items, shards, table, magicRows, text ) {
	const byCodename = new Map( items.map( row => [ row.codename, row ] ) );
	return goldenInstances().map( golden => {
		const row = byCodename.get( golden.codename ), item = row && shards.get( row.id );
		if ( !item ) throw Error( "Golden tooltip item missing: " + golden.codename );
		const degree = itemDegree( item );
		const magic = golden.options.map( name => {
			const option = magicRows.find( m => m.optionName === name && m.degree === degree ) ??
				magicRows.find( m => m.optionName === name );
			if ( !option ) throw Error( "Golden tooltip option missing: " + name );
			return encodeMagic( option.paramId, golden.value ?? topValue( option ) );
		} );
		const instance = {
			refObjId: row.id,
			plus: golden.plus,
			variance: golden.variance,
			magic,
			...(golden.durability === undefined ? {} : { durability: golden.durability })
		};
		return { label: golden.label, instance, rows: itemTooltipRows( instance, item, table, text ) };
	} );
}

/*
================
checkBundle

The shipped bundle, fed only what the site has (an item's shard text, then
magic.json's text), must print every golden vector exactly: a bundling or
text-recording gap fails the build instead of reaching a profile.
================
*/
async function checkBundle( dir, golden, shards, magicRows, common, text ) {
	const bundle = await import( pathToFileURL( path.join( dir, "item-tooltip.mjs" ) ).href );
	if ( bundle.TOOLTIP_FORMAT !== TOOLTIP_FORMAT ) throw Error( "Tooltip bundle format mismatch" );
	const table = bundle.createMagicTable( magicRows );
	for ( const vector of golden ) {
		const item = shards.get( vector.instance.refObjId );
		const own = item.descriptionSymbol ? { [item.descriptionSymbol]: text( item.descriptionSymbol ) } : {};
		const site = symbol => own[symbol] ?? common[symbol] ?? "";
		const rows = bundle.itemTooltipRows( vector.instance, item, table, site );
		if ( JSON.stringify( rows ) !== JSON.stringify( vector.rows ) ) {
			throw Error( "Tooltip bundle drifted on golden vector: " + vector.label );
		}
	}
}
