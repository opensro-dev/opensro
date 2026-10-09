/*
===========================================================================

item-tooltip-stats.ts - an equipment item's stat rows and its durability maximum

The variance-interpolated stats a tooltip lists (78C5B0 -> 78BD00 -> 555720)
and the durability maximum a repair restores (itemMaxDurability).

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { TooltipRow } from "./tooltip-rows";
// 78C5B0 -> 78BD00 -> 555720. Variance is packed per equipment family,
// not one common sequence of stats. The callee consumes only the low byte
// of the caller's 0xffffff1f mask, leaving exactly five effective bits.
/*
================
magicPercentModifier

A base value moved by the percentages of the item's positive and negative
magic options (the tooltip's +/- rows); MATTR_NOT_REPARABLE also raises the
durability maximum. Never below 1.
================
*/
export function magicPercentModifier( item: InventoryItem, base: number, positive: string, negative: string ): number {
	let pct = 0;
	for ( const encoded of item.magic ) {
		const bits = BigInt( encoded ),
			ref = item.magicReferences?.find( r => r.paramId === Number( bits & 65535n ) ),
			amount = Number( bits >> 32n ) & 0xffffffff;
		if ( !ref ) continue;
		if ( ref.optionName === positive && ref.paramName.includes( "+" ) ) pct += amount;
		if ( ref.optionName === negative && ref.paramName.includes( "-" ) ) pct -= amount;
		if ( positive === "MATTR_DUR" && ref.optionName === "MATTR_NOT_REPARABLE" && ref.paramName.includes( "-" ) ) {
			pct += ref.rangeWords?.[1] ?? 0;
		}
	}
	return Math.max( 1, base + Math.trunc( base * pct / 100 ) );
}
/*
================
itemMaxDurability

The durability maximum the tooltip shows and a repair restores: the
itemdata range picked by variance field 0, moved by the durability
options. 0 for an item without a durability range.
================
*/
export function itemMaxDurability( item: InventoryItem ): number {
	const fields = item.tooltip?.fields;
	if ( !fields ) return 0;
	const lo = fields.varianceIntMin1c0, hi = fields.varianceIntMax1c4;
	if ( lo === undefined || hi === undefined ) {
		throw Error( "Missing item tooltip stat varianceIntMin1c0/varianceIntMax1c4" );
	}
	const base = Math.trunc( (hi - lo) * Number( BigInt( item.variance ) & 31n ) / 31 + lo + .5 );
	return base ? magicPercentModifier( item, base, "MATTR_DUR", "MATTR_DEC_MAXDUR" ) : 0;
}
export function itemTooltipStats( item: InventoryItem, text: ( symbol: string ) => string ): TooltipRow[] {
	const fields = item.tooltip?.fields;
	if ( !fields ) return [];
	const read = ( key: string ) => {
		const n = fields[key];
		if ( n === undefined ) throw Error( "Missing item tooltip stat " + key );
		return n;
	};
	const rows: TooltipRow[] = [], tid3 = (item.typeFlags >>> 7) & 15, bits = BigInt( item.variance );
	const v = ( index: number ) => Number( bits >> BigInt( index * 5 ) & 31n ), plus = item.plus;
	const percent = ( value: number, lo: number, hi: number ) =>
		hi === lo ? 100 : Math.trunc( (value - lo) / (hi - lo) * 100 );
	const add = ( key: string, value: string, color = 0xffffffff ) =>
		rows.push( { value: text( key ) + " " + value, color } );
	const int = ( lo: string, hi: string, index: number, perPlus?: string ) =>
		Math.trunc(
			(read( hi ) - read( lo )) * v( index ) / 31 + read( lo ) +
				(perPlus ? Math.trunc( read( perPlus ) * plus ) : 0) + .5
		);
	const float = ( lo: string, hi: string, index: number, perPlus?: string, special = false ) =>
		Math.fround(
			(read( hi ) - read( lo )) * (special ? Math.fround( v( index ) / 31 ) : v( index ) / 31) + read( lo ) +
				(perPlus ? read( perPlus ) * plus : 0)
		);
	const modifier = ( base: number, positive: string, negative: string ) =>
		magicPercentModifier( item, base, positive, negative );
	const integerRow = (
		key: string,
		lo: string,
		hi: string,
		index: number,
		perPlus?: string,
		magic?: readonly [string, string]
	) => {
		const n = int( lo, hi, index, perPlus );
		if ( n ) add( key, `${magic ? modifier( n, ...magic ) : n} (+${percent( n, read( lo ), read( hi ) )}%)` );
	};
	// 555720 prints the enhanced stat; only its (+%) grade takes the plus
	// back off, so the grade measures the variance alone.
	const floatRow = ( key: string, lo: string, hi: string, index: number, perPlus: string ) => {
		const n = float( lo, hi, index, perPlus );
		if ( n ) {
			const graded = Math.fround( n - plus * read( perPlus ) );
			add( key, `${n.toFixed( 1 )} (+${percent( graded, read( lo ), read( hi ) )}%)` );
		}
	};
	const attack = (
		key: string,
		minLo: string,
		minHi: string,
		maxLo: string,
		maxHi: string,
		index: number,
		perPlus: string
	) => {
		const high = int( maxLo, maxHi, index, perPlus );
		if ( high ) {
			// 555A6C..555A93 print SItemStats' enhanced minimum and maximum;
			// the plus comes off only for the grade.
			const low = int( minLo, minHi, index, perPlus ),
				p = Math.trunc( plus * read( perPlus ) ),
				a = low - p,
				b = high - p,
				frac = ( n: number, lo: string, hi: string ) =>
					read( lo ) === read( hi ) ? 1 : (Math.fround( n >>> 0 ) - read( lo )) / (read( hi ) - read( lo ));
			add(
				key,
				`${low} ~ ${high} (+${Math.trunc( (frac( a, minLo, minHi ) + frac( b, maxLo, maxHi )) * .5 * 100 )}%)`
			);
		}
	};
	const durability = () => {
		const n = itemMaxDurability( item );
		if ( n ) {
			add(
				"PARAM_DUR",
				`${item.durability}/${n} (+${Math.trunc( v( 0 ) / Math.fround( .31 ) )}%)`,
				item.durability ? 0xffffffff : 0xffff4a4a
			);
		}
	};
	const specialize = ( key: string, minLo: string, minHi: string, maxLo: string, maxHi: string, index: number ) => {
		const a = float( minLo, minHi, index, undefined, true ), b = float( maxLo, maxHi, index, undefined, true );
		if ( a || b ) {
			add(
				key,
				`${(a * 100).toFixed( 1 )} % ~ ${(b * 100).toFixed( 1 )} % (+${
					Math.trunc(
						((read( minLo ) === read( minHi ) ? 1 : (a - read( minLo )) / (read( minHi ) - read( minLo ))) +
							(read( maxLo ) === read( maxHi ) ?
								1 :
								(b - read( maxLo )) / (read( maxHi ) - read( maxLo )))) * 50
					)
				}%)`
			);
		}
	};
	const single = ( key: string, lo: string, hi: string, index: number ) => {
		const n = float( lo, hi, index );
		if ( n ) add( key, `${(n * 100).toFixed( 1 )} % (+${percent( n, read( lo ), read( hi ) )}%)` );
	};
	if ( tid3 === 6 ) {
		attack(
			(item.typeFlags & 0xf800) === 0x8000 ? "UIIT_STT_ITEM_FORMAT_FORT_ATTACKP" : "PARAM_PA",
			"varianceIntMin240",
			"varianceIntMax244",
			"varianceIntMin248",
			"varianceIntMax24c",
			4,
			"varianceFloatPerPlus250"
		);
		attack(
			"PARAM_MA",
			"varianceIntMin254",
			"varianceIntMax258",
			"varianceIntMin25c",
			"varianceIntMax260",
			5,
			"varianceFloatPerPlus264"
		);
		durability();
		if ( fields.actionRange23c ) add( "PARAM_RANGE", (fields.actionRange23c / 10).toFixed( 1 ) + " m" );
		integerRow( "PARAM_HR", "varianceIntMin288", "varianceIntMax28c", 3, "varianceFloatPerPlus290", [
			"MATTR_HR",
			""
		] );
		integerRow( "PARAM_CRITICAL", "varianceIntMin294", "varianceIntMax298", 6 );
		specialize(
			"PARAM_PHYSICAL_SPECIALIZE",
			"varianceFloatMin268",
			"varianceFloatMax26c",
			"varianceFloatMin270",
			"varianceFloatMax274",
			1
		);
		specialize(
			"PARAM_MAGICAL_SPECIALIZE",
			"varianceFloatMin278",
			"varianceFloatMax27c",
			"varianceFloatMin280",
			"varianceFloatMax284",
			2
		);
	} else if ( tid3 === 5 || tid3 === 12 ) {
		floatRow( "PARAM_PR", "varianceFloatMin1e0", "varianceFloatMax1e4", 0, "varianceFloatPerPlus1e8" );
		floatRow( "PARAM_MR", "varianceFloatMin200", "varianceFloatMax204", 1, "varianceFloatPerPlus208" );
	} else if ( [ 1, 2, 3, 4, 9, 10, 11 ].includes( tid3 ) ) {
		const shield = tid3 === 4;
		floatRow( "PARAM_PD", "varianceFloatMin1c8", "varianceFloatMax1cc", shield ? 4 : 3, "varianceFloatPerPlus1d0" );
		floatRow( "PARAM_MD", "varianceFloatMin1f4", "varianceFloatMax1f8", shield ? 5 : 4, "varianceFloatPerPlus1fc" );
		durability();
		if ( shield ) integerRow( "PARAM_BLOCKING", "varianceIntMin1ec", "varianceIntMax1f0", 3 );
		else {integerRow( "PARAM_ER", "varianceIntMin1d4", "varianceIntMax1d8", 5, "varianceFloatPerPlus1dc", [
				"MATTR_ER",
				""
			] );}
		single( "PARAM_PHYSICAL_SPECIALIZE", "varianceFloatMin20c", "varianceFloatMax210", 1 );
		single( "PARAM_MAGICAL_SPECIALIZE", "varianceFloatMin214", "varianceFloatMax218", 2 );
	}
	return rows;
}
