/*
===========================================================================

item-tooltip-magic.ts - an item tooltip's magic option lines

The tooltip builder (55B540) walks the item's options sorted by param id
(563700) and formats each from its definition's packed bracket words
(definition+0x4C) and the instance's high dword magnitude. Avatar options
go through MagicOption_FormatAvatarParam (553980).

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { TooltipRow } from "./tooltip-rows";
import { avatarMagicOptionText } from "@/engine/foundation/gameplay/avatar-magic-option";

/*
================
itemTooltipMagic
================
*/
export function itemTooltipMagic( item: InventoryItem, text: ( symbol: string ) => string ): TooltipRow[] {
	const rows: TooltipRow[] = [],
		references = item.magicReferences ?? [],
		degree = Math.floor( ((item.tooltip?.fields.itemClass ?? 1) - 1) / 3 ) + 1;
	for (
		const encoded of [ ...item.magic ].sort( ( a, b ) =>
			Number( BigInt( a ) & 65535n ) - Number( BigInt( b ) & 65535n )
		)
	) {
		const bits = BigInt( encoded ), ref = references.find( r => r.paramId === Number( bits & 65535n ) );
		if ( !ref ) continue;
		const source = ref.degree === degree ?
			ref :
			references.find( r => r.optionName === ref.optionName && r.degree === degree );
		const n = ref.optionName, amount = Number( bits >> 32n ), words = source?.rangeWords;
		let min = 0, max = 0;
		if ( words ) {
			min = words[0] >>> 16;
			for ( const word of words ) {
				const hi = word >>> 16, lo = word & 65535;
				if ( !hi ) break;
				max = hi;
				if ( !lo ) break;
				max = lo;
			}
		}
		// 9C2660 uses integer-indefinite for an empty floating-point range.
		const percentage = words ?
			Math.max( 0, max === min ? -2147483648 : Math.trunc( (amount - min) / (max - min) * 100 ) ) :
			undefined;
		const suffix = percentage === undefined ? "" : ` (+${percentage}%)`,
			increase = text( "PARAM_INCREASE" ),
			decrease = text( "PARAM_DECREASE" );
		let value = "", color = 0xff00eaff;
		if ( ref.paramName.includes( "+" ) ) {
			if ( n === "MATTR_STR" || n === "MATTR_INT" ) {
				const shown = amount || ref.degree,
					pct = words ?
						Math.max( 0, max === min ? -2147483648 : Math.trunc( (shown - min) / (max - min) * 100 ) ) :
						undefined;
				value = `${text( n === "MATTR_STR" ? "PARAM_STR" : "PARAM_INT" )} ${shown} ${increase}${
					pct === undefined ? "" : ` (+${pct}%)`
				}`;
			} else if ( [ "MATTR_HP", "MATTR_MP" ].includes( n ) ) {
				value = `${text( "PARAM_" + n.slice( 6 ) )} ${amount} ${increase}${suffix}`;
			} else if ( [ "MATTR_DUR", "MATTR_HR", "MATTR_ER" ].includes( n ) ) {
				value = `${text( "PARAM_" + n.slice( 6 ) )} ${amount}% ${increase}${suffix}`;
			} else if ( n === "MATTR_EVADE_BLOCK" || n === "MATTR_EVADE_CRITICAL" ) {
				value = `${
					text( n === "MATTR_EVADE_BLOCK" ? "PARAM_IGNORE_BLOCKING" : "PARAM_EVADE_CRITICAL" )
				} ${amount}${suffix}`;
			} else if ( n.startsWith( "MATTR_RESIST_" ) ) {
				const symbol = resistSymbol( n.slice( 13 ) );
				if ( symbol ) {
					value = `${text( symbol )}${n === "MATTR_RESIST_FROSTBITE" ? "," + text( "PARAM_FB" ) : ""}${
						text( "PARAM_HOUR" )
					} ${amount}% ${decrease}${suffix}`;
				}
			} else if ( [ "MATTR_ATHANASIA", "MATTR_SOLID", "MATTR_LUCK" ].includes( n ) ) {
				value = `${text( "PARAM_" + n.slice( 6 ) )}(${amount}${text( "UIIT_STT_COUNT" )})`;
			} else if ( n === "MATTR_ASTRAL" ) {
				value = `${text( "PARAM_ASTRAL" )} ${amount} ${text( "UIIT_STT_COUNT" )}`;
			} else if ( n === "MATTR_REPAIR" ) {
				value = `${text( "PARAM_REPAIR" )} (${Math.max( 0, amount - 1 )}${text( "UIIT_STT_COUNT" )})`;
			} else if ( n === "MATTR_STR_3JOB" || n === "MATTR_INT_3JOB" ) {
				value = `${text( n === "MATTR_STR_3JOB" ? "PARAM_STR" : "PARAM_INT" )} ${amount} ${increase}`;
			} else if ( n === "MATTR_REINFORCE_ITEM" && ref.rangeWords ) {
				const duration = amount >= 60000 ?
					`${Math.trunc( amount / 60000 )}${text( "PARAM_MINUTE" )} ${Math.trunc( amount % 60000 / 1000 )}${
						text( "PARAM_SECOND" )
					}` :
					`${Math.trunc( amount / 1000 )}${text( "PARAM_SECOND" )}`;
				value = `+${ref.rangeWords[1]} ${text( "PARAM_REINFORCE" )}(${duration})`;
			} else if ( n.startsWith( "MATTR_AVATAR_" ) ) value = avatarMagicOptionText( n, amount, text );
		}
		if ( ref.paramName.includes( "-" ) ) {
			color = 0xffff4a4a;
			if ( n === "MATTR_DEC_MAXDUR" && ![ 5, 12 ].includes( item.typeFlags >>> 7 & 15 ) ) {
				value = `${text( "PARAM_MAX_DURABILITY" )} ${amount}% ${decrease}`;
			}
			if ( n === "MATTR_NOT_REPARABLE" && ref.rangeWords ) {
				value = `${text( "PARAM_NOT_REPAIRABLE" )} (${text( "PARAM_MAX_DURABILITY" )} ${
					ref.rangeWords[1]
				}% ${increase})`;
			}
		}
		if ( value ) rows.push( { value, color } );
	}
	return rows;
}

/*
================
resistSymbol

The resistance options' parameter names.
================
*/
function resistSymbol( kind: string ): string | undefined {
	switch ( kind ) {
		case "FROSTBITE":
			return "PARAM_FZ";
		case "ESHOCK":
			return "PARAM_ES";
		case "BURN":
			return "PARAM_BU";
		case "POISON":
			return "PARAM_PS";
		case "ZOMBIE":
			return "PARAM_ZB";
	}
	return undefined;
}
