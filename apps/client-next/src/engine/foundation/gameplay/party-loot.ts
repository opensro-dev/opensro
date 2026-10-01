/*
===========================================================================

party-loot.ts - the party loot notice (0x317D)

CPSMission_OnItemLootGoldOrItem0x317D (7511C0): an item-share pickup tells
every party member what the recipient obtained. Gold prints "[%d]gold is
distributed to [%s]"; an item prints "item [%s %d pieces]is distributed to
[%s]", or "item [%s %d pieces]gained." on the recipient's own client.

===========================================================================
*/
import type { SystemNotice } from "./system-notices";

// 7511C0's type tests on the item reference's flag word.
const NON_ITEM_BIT = 2;
const CLASS_MASK = 0x1c;
const EXPENDABLE_CLASS = 0xc;
const CATEGORY_MASK = 0x60;
const EXPENDABLE_CATEGORY = 0x60;
const GROUP_MASK = 0x780;
const GOLD_GROUP = 0x280;
// CGInterface_ChatSystemMessageFormatted channels: 1 gain, 4 party.
const GAIN_CHANNEL = 1;
const PARTY_CHANNEL = 4;

/*
================
PartyLootContext

What the notice needs from its owners: the item's flags and display name
from the world catalog, the recipient's party name, and the local gid.
================
*/
export interface PartyLootContext {
	readonly item: ( refObjId: number ) => { readonly typeFlags: number; readonly name: string; } | undefined;
	readonly memberName: ( gid: number ) => string | undefined;
	readonly localGid: number;
}

/*
================
partyLootNotice

Decode one 0x317D into its status-line notice. The amount's width follows
the item: u32 for gold, u16 for an expendable stack, else one byte.
================
*/
export function partyLootNotice( p: Uint8Array, context: PartyLootContext ): SystemNotice {
	if ( p.length < 9 ) throw Error( "Truncated party loot notice" );
	const view = new DataView( p.buffer, p.byteOffset, p.byteLength );
	const recipient = view.getUint32( 0, true ), refObjId = view.getUint32( 4, true );
	const item = context.item( refObjId );
	if ( !item ) throw Error( "Party loot notice for an unknown item reference" );
	const flags = item.typeFlags;
	const expendable = !(flags & NON_ITEM_BIT) && (flags & CLASS_MASK) === EXPENDABLE_CLASS &&
		(flags & CATEGORY_MASK) === EXPENDABLE_CATEGORY;
	const gold = expendable && (flags & GROUP_MASK) === GOLD_GROUP;
	const width = gold ? 4 : expendable ? 2 : 1;
	if ( p.length !== 8 + width ) throw Error( "Invalid party loot notice length" );
	const amount = width === 4 ? view.getUint32( 8, true ) : width === 2 ? view.getUint16( 8, true ) : p[8]!;
	const member = context.memberName( recipient ) ?? "";
	if ( gold ) {
		return {
			key: "UIIT_MSG_PARTYGET_GOLD",
			value: amount,
			arguments: [ String( amount ), member ],
			nativeType: PARTY_CHANNEL
		};
	}
	if ( recipient === context.localGid ) {
		return {
			key: "UIIT_MSG_STATE_GET_ITEM_EXPENDABLE",
			value: amount,
			arguments: [ item.name, String( amount ) ],
			nativeType: GAIN_CHANNEL
		};
	}
	return {
		key: "UIIT_MSG_PARTYGET_ITEM_EXPENDABLE",
		value: amount,
		arguments: [ item.name, String( amount ), member ],
		nativeType: PARTY_CHANNEL
	};
}
