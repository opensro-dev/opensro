/*
===========================================================================

unique-notices.ts - the 0x3058 event notice: unique appearances and kills

v1.150 74D451 registers 0x3058 -> 7508C0. Unlike the newer server's 0x300C
envelope, v1.150 starts with one kind byte; no live entity or GID is
required. Kinds 5 and 6 announce a unique's appearance and death; kind 7
is the 2005 badge event (no live server sends it); kinds 8, 9 and 10 are
the specialty-goods announcements, and every other kind shows the
category 0x11 notice whose code is the kind (kind 1: a thief may not
enter a town with its transport).

Kinds 0x14 and 0x15 are the Roc's countdowns, which v1.150 lacks: the
v1.696 client's 0x300C handler (0xD9E880) reads them as kinds 0x0C14
and 0x0C15, and this port's server sends their low bytes here as kinds
5 and 6 are 0x0C05's and 0x0C06's (action/roc.go). Each carries one
minute byte; 0x0C14's 0 shows _START and 0xFF _AGAIN.

===========================================================================
*/
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";
import { EVENT_NOTICE_OPCODE } from "@/engine/contracts/opcodes";
import { viewOf } from "@/engine/foundation/binary/byte-view";

// 7508C0's kinds: the unique announcements, the badge event, and the
// category its other kinds' notices come from.
const KIND_UNIQUE_APPEARED = 5;
const KIND_UNIQUE_KILLED = 6;
const KIND_BADGE_EVENT = 7;
const KIND_SELL_READY = 8;
const KIND_LAST_SPECIAL = 10;
const EVENT_NOTICE_CATEGORY = 0x11;
// The Roc's countdowns (v1.696 0xD9E91F and 0xD9EAF2) and the berserk
// byte's two lines without a count.
const KIND_ROC_BERSERK = 0x14;
const KIND_ROC_DEPARTURE = 0x15;
const ROC_BERSERK_START = 0;
const ROC_BERSERK_AGAIN = 0xff;

// The badge byte's key (750FF7/751018/75105B/7510A0 via the table at
// 7511A4): the 2005-10-21 event lines. No live server sends kind 7;
// the arm lands dark and prints the day one does.
const BADGE_KEYS: Readonly<Record<number, string>> = {
	1: "UIIT_MSG_EVENT_051021_BADGEGAIN_BLUEDRAGON",
	2: "UIIT_MSG_EVENT_051021_BADGEGAIN_PHOENIX",
	3: "UIIT_MSG_EVENT_051021_BADGEGAIN_WHITETIGER",
	4: "UIIT_MSG_EVENT_051021_BADGEGAIN_TURTLE"
} as const;

/*
================
UniqueReference
================
*/
interface UniqueReference {
	readonly symbol: string;
	readonly name: string;
}

/*
================
nativeWording

These five branches are literal comparisons in v1.150 750949..750A77; all
other authored references use the generic template, including new ones.
================
*/
function nativeWording( symbol: string ): string | undefined {
	switch ( symbol ) {
		case "SN_MOB_CH_TIGERWOMAN":
			return "TIGER_GIRL";
		case "SN_MOB_KK_ISYUTARU":
			return "IYUTARU";
		case "SN_MOB_OA_URUCHI":
			return "URRUCHI";
		case "SN_MOB_TK_BONELORD":
			return "BONELORD";
		case "SN_MOB_RM_TAHOMET":
			return "TAHOMET";
	}
	return undefined;
}

/*
================
uniqueReferences

The monster references a unique notice may name, from the bootstrap.
================
*/
export function uniqueReferences( value: unknown ): ReadonlyMap<number, UniqueReference> {
	const rows = (value as { refObjSnapshot?: unknown; })?.refObjSnapshot ?? [];
	if ( !Array.isArray( rows ) ) throw Error( "Invalid announcement references" );
	const refs = new Map<number, UniqueReference>();
	for ( const row of rows ) {
		if ( row.kind !== "monster" ) continue;
		if (
			!Number.isInteger( row.refObjId ) || row.refObjId <= 0 || row.refObjId > 0xffffffff ||
			typeof row.name !== "string" || typeof row.nameStrId !== "string"
		) {
			throw Error( "Invalid unique reference" );
		}
		refs.set( row.refObjId, { symbol: row.nameStrId, name: row.name } );
	}
	return refs;
}

/*
================
eventNotice

7508C0's other kinds: outside 5..10, and 8, 9 and 10 inside it, the
category 0x11 notice of that code prints on the centre banner (751155).
Kind 10 (751114) is the specialty sell-end: with a shop interaction
live (751127) the handler tests the store's specialty-page field
(75113D) and hides the shop (751150, CGInterface_SetNpcShopVisible),
then falls through to the banner print (751155) either way. This
decoder returns only the banner; gameplay works the close half
through isSpecialtySellEnd before the notice queues. Kind 7 has its
own branch.
================
*/
function eventNotice( kind: number ): SystemNotice | null {
	if ( kind < KIND_UNIQUE_APPEARED || kind > KIND_LAST_SPECIAL || kind >= KIND_SELL_READY ) {
		return constantNativeNotice( EVENT_NOTICE_CATEGORY, kind );
	}
	return null;
}

/*
================
isSpecialtySellEnd

Kind 10 (751114) is the one notice kind with a side effect: closing an
open specialty-goods shop (751150) before its banner. The kind byte
alone decides; 7508C0 reads nothing after it for this kind.
================
*/
export function isSpecialtySellEnd( opcode: number, p: Uint8Array ): boolean {
	return opcode === EVENT_NOTICE_OPCODE && p[0] === KIND_LAST_SPECIAL;
}

/*
================
badgeNotice

Kind 7 (750F57): a badge byte and a guild name. Bytes 1..4 select the
event key (table at 7511A4); the formatted line shows on the centre
banner (67D030 at 751106). Another byte formats nothing natively
(the tail shows an unformatted buffer), so the port prints nothing.
================
*/
function badgeNotice( p: Uint8Array ): SystemNotice | null {
	if ( p.length < 4 ) throw Error( "Truncated badge announcement" );
	const view = viewOf( p ), length = view.getUint16( 2, true );
	if ( p.length !== 4 + length ) throw Error( "Invalid badge name length" );
	const key = BADGE_KEYS[p[1]!];
	if ( !key ) return null;
	const name = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 4 ) );
	return { key, value: 0, nativeType: 0, arguments: [ name ], banner: true };
}

/*
================
rocCountdownNotice

Kinds 0x14 and 0x15: [kind][minutes]. The v1.696 handler formats the
key's %d with the byte and shows it in the chat and on the banner
(0x869900, 0x86BEB0), as the unique lines do.
================
*/
function rocCountdownNotice( p: Uint8Array ): SystemNotice {
	if ( p.length !== 2 ) throw Error( "Invalid Roc countdown" );
	const kind = p[0]!, minutes = p[1]!;
	if ( kind === KIND_ROC_DEPARTURE ) {
		return { key: "UIIT_MSG_QUEST_ROC_BYEBYE", value: minutes, nativeType: 0, banner: true };
	}
	if ( minutes === ROC_BERSERK_START ) {
		return { key: "UIIT_MSG_QUEST_ROC_BERSERKER_START", value: 0, nativeType: 0, banner: true };
	}
	if ( minutes === ROC_BERSERK_AGAIN ) {
		return { key: "UIIT_MSG_QUEST_ROC_BERSERKER_AGAIN", value: 0, nativeType: 0, banner: true };
	}
	return { key: "UIIT_MSG_QUEST_ROC_BERSERKER", value: minutes, nativeType: 0, banner: true };
}

/*
================
uniqueNotice

The notice a 0x3058 frame shows, or null for another opcode or a kind this
port does not present.
================
*/
export function uniqueNotice(
	opcode: number,
	p: Uint8Array,
	refs: ReadonlyMap<number, UniqueReference>
): SystemNotice | null {
	if ( opcode !== EVENT_NOTICE_OPCODE ) return null;
	if ( p.length === 0 ) throw Error( "Missing announcement subtype" );
	const kind = p[0]!;
	if ( kind === KIND_BADGE_EVENT ) return badgeNotice( p );
	if ( kind === KIND_ROC_BERSERK || kind === KIND_ROC_DEPARTURE ) return rocCountdownNotice( p );
	// 7508C0 reads only the kind for these; anything after it is ignored.
	if ( kind !== KIND_UNIQUE_APPEARED && kind !== KIND_UNIQUE_KILLED ) return eventNotice( kind );
	if ( p.length < 5 ) throw Error( "Truncated unique reference" );
	const view = viewOf( p ), id = view.getUint32( 1, true );
	let killer: string | undefined;
	if ( kind === KIND_UNIQUE_APPEARED ) {
		if ( p.length !== 5 ) throw Error( "Trailing unique appearance bytes" );
	} else {
		if ( p.length < 7 ) throw Error( "Truncated unique killer" );
		const length = view.getUint16( 5, true );
		if ( p.length !== 7 + length ) throw Error( "Invalid unique killer length" );
		killer = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( 7 ) );
	}
	const ref = refs.get( id );
	if ( !ref ) return null;
	// 750C0E et al. compare against the UTF-16 literal "???" at C0B1DC.
	const suffix = nativeWording( ref.symbol ), dead = kind === KIND_UNIQUE_KILLED, anonymous = killer === "???";
	const prefix = !dead ? "APPEAR" : anonymous ? "DEAD" : "ANYONE_DEAD";
	const args = !dead || anonymous ? (suffix ? [] : [ ref.name ]) : suffix ? [ killer! ] : [ killer!, ref.name ];
	return { key: `UIIT_MSG_${prefix}_${suffix ?? "UNIC"}`, value: 0, nativeType: 0, arguments: args, banner: true };
}
