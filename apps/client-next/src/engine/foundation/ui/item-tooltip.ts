/*
===========================================================================

item-tooltip.ts - ordered item identity, properties and requirement rows

Native 564230 and 558FF0 select bold name and seal text. The seal sits below
its heading by requested presentation policy (referencesox.png); v1.150's
56B099 call instead appends it after requirements.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { Progression } from "../gameplay/progression";
import { itemTooltipMagic } from "./item-tooltip-magic";
import { tooltipDescription } from "./tooltip-description";
import { itemTooltipStats } from "./item-tooltip-stats";
import { itemTooltipRecovery } from "./item-tooltip-recovery";
import { tooltipFormat, type TooltipRow } from "./tooltip-rows";
// 56CDA0/56CB80 family dispatch, 564230 name, 558CB0 degree and
// 568500 requirements. This takes an item instance, never just a ref id.
/*
================
itemTooltip
================
*/
export function itemTooltip(
	item: InventoryItem,
	progression: Progression,
	text: ( symbol: string ) => string,
	identity?: { readonly country?: number; readonly sex?: number; }
): readonly TooltipRow[] {
	if ( item.name === undefined ) return [];
	const f = item.tooltip?.fields,
		t = (item.typeFlags >>> 7) & 15,
		k = (item.typeFlags >>> 11) & 31,
		equipment = (item.typeFlags & 0x7e) === 0x2c;
	const repaired = item.magicReferences?.some( r => r.optionName === "MATTR_REPAIR" ), rare = f?.rarity === 2;
	const nameColor = rare ? (repaired ? 0xff6ce675 : 0xffffd953) : item.magic.length ? 0xff72bfff : 0xffffffff;
	const rows: TooltipRow[] = item.name ?
		[ {
			value: item.name + (item.plus && !((item.typeFlags & 0x7e) === 0x6c && t === 11 && (k === 1 || k === 2)) ?
				` (+${item.plus})` :
				""),
			color: nameColor,
			heading: true,
			strong: true,
			ornament: "item"
		} ] :
		[];
	/*
	================
	add
	================
	*/
	const add = ( value: string, color = 0xffefdaa4 ) => {
		if ( value ) rows.push( { value, color } );
	};
	/*
	================
	space
	================
	*/
	const space = () => rows.push( { value: " ", color: 0 } );
	if ( !f ) return rows;
	space();
	if ( rare && f.itemClass ) {
		rows.push( {
			value: text( [ "PARAM_RARE_FIRST", "PARAM_RARE_SECOND", "PARAM_RARE_THIRD" ][(f.itemClass - 1) % 3]! ),
			color: nameColor,
			strong: true
		} );
	}

	const description = tooltipDescription( text( item.tooltip?.descriptionSymbol ?? "" ) );
	if ( description.length && (!equipment || t === 7 || t === 13) ) {
		rows.push( ...description );
		space();
	}
	if ( equipment ) {
		const armor: Record<number, string> = {
			1: "CLOTHES",
			2: "LIGHT_ARMOR",
			3: "HEAVY_ARMOR",
			9: "EU_ROBE",
			10: "EU_LIGHT_ARMOR",
			11: "EU_HEAVY_ARMOR"
		};
		if ( armor[t] ) {
			add( `${text( "UIIT_STT_ARMOR_TYPE" )}: ${text( "UIO_NEWCHAR_STT_" + armor[t] )}` );
			const position = [ "HEAD", "SHOULDER", "BREAST", "LEG", "HAND", "FOOT" ][k - 1];
			if ( position ) {
				add( `${text( "UIIT_STT_ARMOR_POSITION" )}: ${text( "UIIT_STT_ARMOR_POSITION_" + position )}` );
			}
		} else if ( t === 6 ) {
			const symbol = ({
				2: "PARAM_WEAPON_SWORD",
				3: "PARAM_WEAPON_BLADE",
				4: "PARAM_WEAPON_SPEAR",
				5: "PARAM_WEAPON_TBLADE",
				6: "PARAM_WEAPON_BOW",
				7: "UIO_NEWCHAR_STT_EU_ONEHANDSWORD",
				8: "UIO_NEWCHAR_STT_EU_TWOHANDSWORD",
				9: "UIO_NEWCHAR_STT_EU_DUELAXE",
				10: "UIO_NEWCHAR_STT_EU_DARKSTAFF",
				11: "UIO_NEWCHAR_STT_EU_TWOHANDSTAFF",
				12: "UIO_NEWCHAR_STT_EU_CROSSBOW",
				13: "UIO_NEWCHAR_STT_EU_DAGGER",
				14: "UIO_NEWCHAR_STT_EU_HARP",
				15: "UIO_NEWCHAR_STT_EU_ONEHANDSTAFF",
				16: "UIIT_STT_FORT_ETC_SIEGEWEAPON"
			} as Record<number, string>)[k];
			if ( symbol ) add( `${text( "UIIT_STT_WEAPON_TYPE" )}: ${text( symbol )}` );
		} else if ( t === 4 ) add( text( "UIIT_STT_SHIELD" ) );
		else if ( t === 5 || t === 12 ) {
			const symbol = [ "EARRING", "NECKLACE", "RING" ][k - 1];
			if ( symbol ) add( `${text( "UIIT_STT_ACCESSARY_TYPE" )}: ${text( "UIIT_STT_ACCESSARY_" + symbol )}` );
		} else if ( t === 13 ) {
			const symbol =
				[ "UIIT_STT_SILKMALL_HAT", "UIIT_CTL_DRESS", "UIIT_CTL_WK_AVATAR_ATTACH", "UIIT_CTL_WK_ETC" ][k - 1];
			if ( symbol ) add( `${text( "UIIT_STT_ITEM_TYPE" )}: ${text( symbol )}` );
		}
		if ( t === 13 ) {
			// 56A1E0: only the dress has an attachment-compatibility row.
			if ( k === 2 && f.avatarAttachment51d !== undefined ) {
				add(
					`${text( "UIIT_STT_SILKMALL_ATTACH" )}: ${
						text( f.avatarAttachment51d ? "UIIT_STT_AVATAR_WEAR" : "UIIT_STT_AVATAR_NOT_WEAR" )
					}`
				);
			}
		} else if ( t !== 7 && f.itemClass ) {
			add( tooltipFormat( text( "UIIT_TOOLTIP_EQUIPMENT_CLASS" ), Math.floor( (f.itemClass - 1) / 3 ) + 1 ) );
		}
		// 5570E0 is shared by all six native equipment formatters, not only avatars.
		if ( f.maxMagicOptions51c !== undefined ) {
			add(
				`${text( "UIIT_STT_AVATAR_MAGICOPTION_MAXCOUNT" )}: ${f.maxMagicOptions51c}${text( "UIIT_STT_UNIT" )}`
			);
		}
		space();
		// Armor, accessories, weapons and shields append description after identity
		// and capacity; job suits and avatars append it before their detail rows.
		if ( description.length && t !== 7 && t !== 13 ) {
			rows.push( ...description );
			space();
		}
		rows.push( ...itemTooltipStats( item, text ) );
	}
	rows.push( ...itemTooltipRecovery( item, text ) );
	const requirements: TooltipRow[] = [];
	/*
	================
	requirement
	================
	*/
	const requirement = ( value: string, met: boolean ) =>
		requirements.push( { value, color: met ? 0xffffffff : 0xffff4a4a } );
	const masterySymbols: Record<number, string> = {
		257: "UIIT_STT_MASTERY_VI",
		258: "UIIT_STT_MASTERY_HEUK",
		259: "UIIT_STT_MASTERY_PA",
		273: "UIIT_STT_MASTERY_HAN",
		274: "UIIT_STT_MASTERY_PUNG",
		275: "UIIT_STT_MASTERY_HWA",
		276: "UIIT_STT_MASTERY_GI",
		513: "UIIT_STT_WARRIOR",
		514: "UIIT_STT_WIZARD",
		515: "UIIT_STT_ROG",
		516: "UIIT_STT_WARLOCK",
		517: "UIIT_STT_BARD",
		518: "UIIT_STT_CLERIC"
	};
	for ( let i = 1; i <= 4; i++ ) {
		const type = f["reqLevelType" + i], level = f["requiredLevel" + (i === 1 ? "" : i)] ?? 0;
		if ( type === 1 ) requirement( `${text( "PARAM_REQ_LV" )} ${level}`, (progression.level ?? 0) >= level );
		else if ( type && masterySymbols[type] ) {
			requirement(
				`${text( "PARAM_MASTERY" )}: ${text( masterySymbols[type]! )} ${
					text( "PARAM_MASTERY_LEVEL" )
				} ${level}`,
				(progression.masteries.find( m => m.id === type )?.level ?? 0) >= level
			);
		}
	}
	if ( f.reqStr ) requirement( `${text( "PARAM_STR" )} ${f.reqStr}`, (progression.stats?.strength ?? 0) >= f.reqStr );
	// 569791 reads the STR field for the INT row's printed value.
	if ( f.reqInt ) {
		requirement( `${text( "PARAM_INT" )} ${f.reqStr ?? 0}`, (progression.stats?.intellect ?? 0) >= f.reqInt );
	}
	if ( f.reqGender === 0 || f.reqGender === 1 ) {
		requirement(
			text( f.reqGender === 0 ? "UIO_NEWCHAR_CTL_FEMALE" : "UIO_NEWCHAR_CTL_MALE" ),
			identity?.sex === f.reqGender
		);
	}
	if ( f.country === 0 || f.country === 1 || f.country === 2 ) {
		requirement(
			text( [ "UIO_NEWCHAR_CTL_CHINESE", "UIO_NEWCHAR_CTL_EUROPEAN", "UIO_NEWCHAR_CTL_ARABIAN" ][f.country]! ),
			identity?.country === f.country
		);
	}
	if ( requirements.length ) {
		space();
		rows.push( ...requirements );
	}
	const magic = itemTooltipMagic( item, text );
	if ( magic.length ) {
		space();
		rows.push( ...magic );
	}
	// 55A7B0: the published inventory/shop/COS item slots show stack capacity
	// and the actual instance quantity. Timed COS extension uses quantity as
	// minutes and is deliberately excluded from the ordinary amount row.
	if ( (item.typeFlags & 0x7e) === 0x6c ) {
		if ( f.maxStack !== undefined && f.maxStack !== -1 && f.maxStack !== 0xffffffff ) {
			add( `${text( "PARAM_MAX_CONTAIN" )} ${f.maxStack}`, 0xffffffff );
		}
		if ( !(t === 13 && k === 15 && ((f.itemParam6_2b0 ?? 0) & 2)) ) {
			add( `${text( "UIIT_STT_AMOUNT" )} ${item.quantity}`, 0xffffffff );
		}
	}
	return rows;
}
