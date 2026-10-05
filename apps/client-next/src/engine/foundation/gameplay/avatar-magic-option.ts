/*
===========================================================================

avatar-magic-option.ts - the smith's avatar magic option grant

A smith's select word carries 0x80000000 (service 0x20); the menu builder
(5D9100) lists row 0x2F after the storage rows, and its click sends
0x7338 [npc][0x80000000]. B338 lock 0x80000000 opens
CIFGrantMagicAttributeWnd beside the inventory. Dropping an avatar hat,
dress or attachment there (6EB570) lists the options magicoptionassign.txt
assigns to that part (CSOItem_FillAvatarMagicOptionCandidates 78C720), and
the confirm (6EBB10) sends

	0x361A [u8 inventory slot][u16 len][ascii codename]
	    -> 0x32D9 [1][1][u8 slot][item] | [2][u8 code]

The answer's code is a category 0x20 notice. The port ships the
assignment rows in the enter payload (avatarMagicOptions) beside the
magic option definitions they resolve through.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { ItemMagicReference } from "./item-tooltip-reference";

export const AVATAR_MAGIC_OPTION_FUNCTION = 0x80000000;
export const AVATAR_MAGIC_OPTION_REQUEST = 0x361a;
export const AVATAR_MAGIC_OPTION_ANSWER = 0x32d9;
// The notice category of a refused grant (770140).
export const AVATAR_MAGIC_OPTION_NOTICE_CATEGORY = 0x20;

// The avatar type ids (6EB570): TID1 3, TID2 1, TID3 13; TID4 4 is the
// avatar flag, which takes no option (ItemTID_IsAvatarFlag 6EA1B0).
const AVATAR_TYPE_ID1 = 3;
const AVATAR_TYPE_ID2 = 1;
const AVATAR_TYPE_ID3 = 13;
const AVATAR_FLAG_TYPE_ID4 = 4;
const MAX_CODENAME_LENGTH = 128;

/*
================
AvatarMagicOption

One grantable option: its codename and the value the grant gives
(magicoption +0x50, the generator's minimum; avatar rows are single
values).
================
*/
export interface AvatarMagicOption {
	readonly codename: string;
	readonly value: number;
}

/*
================
AvatarMagicOptionPart

The options one avatar part (TID4: 1 hat, 2 dress, 3 attachment) takes.
================
*/
export interface AvatarMagicOptionPart {
	readonly part: number;
	readonly options: readonly AvatarMagicOption[];
}

/*
================
avatarMagicOptionParts

The enter payload's assignment rows resolved through the magic option
definitions, as 78C720 resolves each codename (7D4350). A codename without
a definition is a broken snapshot.
================
*/
export function avatarMagicOptionParts(
	value: unknown,
	references: ReadonlyMap<number, ItemMagicReference>
): readonly AvatarMagicOptionPart[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) || value.length > 31 ) throw Error( "Invalid avatar magic options" );
	const parts: AvatarMagicOptionPart[] = [];
	for ( const row of value as { part?: unknown; options?: unknown; }[] ) {
		const part = row?.part;
		if (
			typeof part !== "number" || !Number.isInteger( part ) || part < 0 || part > 31 ||
			parts.some( p => p.part === part ) || !Array.isArray( row.options ) || row.options.length > 32
		) throw Error( "Invalid avatar magic option row" );
		const options = (row.options as unknown[]).map( codename => {
			if ( typeof codename !== "string" || !codename || codename.length > MAX_CODENAME_LENGTH ) {
				throw Error( "Invalid avatar magic option codename" );
			}
			const reference = [ ...references.values() ].find( r => r.optionName === codename );
			if ( !reference?.rangeWords ) throw Error( "Avatar magic option has no definition: " + codename );
			return { codename, value: reference.rangeWords[1] };
		} );
		parts.push( { part, options } );
	}
	return parts;
}

/*
================
grantableAvatarPart

The TID4 of an avatar part the window accepts, or null (6EB570's
"UIIT_STT_AVATAR_MAGICOPTION_ONLY_AVATAR" refusal).
================
*/
export function grantableAvatarPart( typeFlags: number ): number | null {
	const tid4 = typeFlags >>> 11 & 31;
	if (
		(typeFlags & 2) !== 0 || (typeFlags >>> 2 & 7) !== AVATAR_TYPE_ID1 ||
		(typeFlags >>> 5 & 3) !== AVATAR_TYPE_ID2 || (typeFlags >>> 7 & 15) !== AVATAR_TYPE_ID3 ||
		tid4 === AVATAR_FLAG_TYPE_ID4
	) return null;
	return tid4;
}

/*
================
avatarMagicOptionCount

CSOItem flag +0x85, the item's visible option count: every option whose
definition's param name carries no '-' (78B1B0).
================
*/
export function avatarMagicOptionCount( item: InventoryItem ): number {
	const references = item.magicReferences ?? [];
	return item.magic.filter( encoded => {
		const id = Number( BigInt( encoded ) & 0xffffn );
		return !references.find( r => r.paramId === id )?.paramName.includes( "-" );
	} ).length;
}

/*
================
avatarMagicOptionText

MagicOption_FormatAvatarParam (553980) as both v1.150 callers use it (no
percentage): "%s %d %s" for STR, INT, HP and MP; "%s %d%% %s" for the
rate options. Unknown codenames format to nothing.
================
*/
export function avatarMagicOptionText( codename: string, value: number, text: ( symbol: string ) => string ): string {
	const increase = text( "PARAM_INCREASE" );
	switch ( codename.toUpperCase() ) {
		case "MATTR_AVATAR_STR":
			return `${text( "PARAM_STR" )} ${value} ${increase}`;
		case "MATTR_AVATAR_INT":
			return `${text( "PARAM_INT" )} ${value} ${increase}`;
		case "MATTR_AVATAR_HP":
			return `${text( "PARAM_HP" )} ${value} ${increase}`;
		case "MATTR_AVATAR_MP":
			return `${text( "PARAM_MP" )} ${value} ${increase}`;
		case "MATTR_AVATAR_ER":
			return `${text( "PARAM_ER" )} ${value}% ${increase}`;
		case "MATTR_AVATAR_HR":
			return `${text( "PARAM_HR" )} ${value}% ${increase}`;
		case "MATTR_AVATAR_DRUA":
			return `${text( "PARAM_AVATAR_DRUA" )} ${value}% ${increase}`;
		case "MATTR_AVATAR_DARA":
			return `${text( "PARAM_AVATAR_DARA" )} ${value}% ${increase}`;
		case "MATTR_AVATAR_HPRG":
			return `${text( "PARAM_AVATAR_HPRG" )} ${value}% ${increase}`;
		case "MATTR_AVATAR_MPRG":
			return `${text( "PARAM_AVATAR_MPRG" )} ${value}% ${increase}`;
		case "MATTR_AVATAR_MDIA":
			return `${text( "PARAM_AVATAR_MDIA" )} ${value}% ${text( "UIIT_STT_PROBABILITY" )}`;
	}
	return "";
}

/*
================
avatarPartSymbol

The count line's part name (CIFGrantMagicAttributeWnd_RefreshCountLine
6EA540).
================
*/
export function avatarPartSymbol( part: number ): string | null {
	switch ( part ) {
		case 1:
			return "UIIT_STT_SILKMALL_HAT";
		case 2:
			return "UIIT_STT_SILKMALL_DRESS";
		case 3:
			return "UIIT_STT_SILKMALL_ATTACH";
	}
	return null;
}

/*
================
avatarMagicOptionRequest

0x361A: NetClient_Send361A (703710) writes the bag index + 0x0D, which is
the inventory slot, then the codename as a u16-counted ASCII string.
================
*/
export function avatarMagicOptionRequest( slot: number, codename: string ) {
	if ( !Number.isInteger( slot ) || slot < 13 || slot > 255 ) throw Error( "Invalid avatar slot" );
	if ( !/^[\x20-\x7e]+$/.test( codename ) || codename.length > MAX_CODENAME_LENGTH ) {
		throw Error( "Invalid avatar magic option codename" );
	}
	const payload = new Uint8Array( 3 + codename.length );
	payload[0] = slot;
	payload[1] = codename.length & 0xff;
	payload[2] = codename.length >>> 8;
	for ( let i = 0; i < codename.length; i++ ) payload[3 + i] = codename.charCodeAt( i );
	return { opcode: AVATAR_MAGIC_OPTION_REQUEST, payload };
}
