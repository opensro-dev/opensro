/*
===========================================================================

system-notices.ts - server frames that become a notice or a banner

Decodes the fortress announcements (0x3887), the server notification and
the shard's job-kill and drop/plus notices (0x3667), and the command restriction notice
(0x36EA) into SystemNotice rows the gameplay owner queues for the HUD.

===========================================================================
*/
import { decodeNoticeLinks } from "./notice-links";

/*
================
SystemNotice
================
*/
export interface SystemNotice {
	readonly localizedArguments?: readonly (string | null)[];
	readonly formatKinds?: readonly ("d" | "s")[];
	readonly formatCapacity?: number;
	readonly dialog?: { readonly title: string; readonly lines: readonly string[]; };
	readonly nativeType?: 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7;
	readonly colorArgb?: number;
	readonly sequence?: number;
	readonly key: string;
	readonly value: number;
	readonly text?: string;
	readonly arguments?: readonly string[];
	readonly banner?: true;
	readonly bannerUsesFormattedKey?: true;
	readonly questBanner?: true;
	readonly notificationBanner?: true;
	readonly bannerOnly?: true;
	readonly jobKill?: true;
	readonly dropPlus?: true;
	readonly plusSeven?: true;
	readonly itemBreak?: true;
	readonly sunDrop?: true;
}
/*
================
fortressNotice

76C870, registered as 3887 by 76E850. These six announcement arms read
only their subtype byte; their minutes are constants in the client.
================
*/
export function fortressNotice( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0x3887 || p[0] === undefined || p[0] < 1 || (p[0] > 6 && p[0] !== 9) ) return null;
	if ( p.length !== 1 ) throw Error( "Trailing fortress announcement bytes" );
	const subtype = p[0];
	return {
		key: subtype === 1 ?
			"UIIT_MSG_FORT_WAR_BEFOREBEGIN" :
			subtype === 2 ?
			"UIIT_MSG_FORT_WAR_BEGIN" :
			subtype === 6 ?
			"UIIT_MSG_FORT_WAR_END" :
			"UIIT_MSG_FORT_WAR_BEFOREEND",
		value: subtype === 9 ? 1 : subtype === 1 || subtype === 3 ? 30 : subtype === 4 ? 20 : subtype === 5 ? 10 : 0
	};
}

// Port-only, not native: the shard's job-kill announcement channel (server
// wire.JobKillNoticeChannel). v1.150 (753760) sends any channel outside its
// table, 0x80 included, to its named-sender arm (753D8A), so the channel is
// safe only because the browser client is the only client. Here it shows as
// channel 7 unless the Experimental "Job kill notices" row is off.
export const JOB_KILL_NOTICE_CHANNEL = 0x80;

// Port-only, not native: the shard's seal-drop and plus announcement
// channel (server wire.DropPlusNoticeChannel), shown as channel 7 unless
// one of the notice rows is off. The +7 announcements (owner decision
// 2026-10-19) share this channel as their own bucket; announcedPlusSeven
// picks which Experimental row gates a frame.
export const DROP_PLUS_NOTICE_CHANNEL = 0x81;

// Port-only, not native: the shard's item-break announcement channel
// (server wire.BreakNoticeChannel; SRO_ALCHEMY_BREAK_NOTICE, #1738),
// shown as channel 7 unless the Experimental "Item break notices" row
// is off. The frame is the plain channel-7 layout: a break notice links
// nothing, so the job alias it names never leads to a character.
export const BREAK_NOTICE_CHANNEL = 0x82;

// Port-only, not native: the name-change notice channel (server
// wire.RenameNoticeChannel, #1689). "X is now known as Y" reaches only
// the renamed character's online friends and guild members, so it shows
// as channel 7 with no Experimental row: it is the feature's own
// feedback, not a broadcast a player opts out of.
export const RENAME_NOTICE_CHANNEL = 0x83;

// The announced item's plus rides its encoded body at a fixed byte (the
// server's ItemBody.Encode header, ItemBodySize 18). A shorter or absent
// body names no plus.
const ITEM_BODY_HEADER_BYTES = 18;
const ITEM_BODY_PLUS_BYTE = 4;

/*
================
isServerNoticeChannel

Channel 7 and the two port-only channels laid out like it.
================
*/
export function isServerNoticeChannel( channel: number | undefined ): boolean {
	return channel === 7 || channel === JOB_KILL_NOTICE_CHANNEL || channel === DROP_PLUS_NOTICE_CHANNEL ||
		channel === BREAK_NOTICE_CHANNEL || channel === RENAME_NOTICE_CHANNEL;
}

/*
================
isSunDropNoticeText

Whether a drop notice's text announces a Seal of Sun drop (#1640). The
tiered wording leads with "SEAL OF SUN:"; a server without it writes
the seal in parentheses, and the parenthesis alone also appears in
plus notices, which stay unhighlighted - those never say "has dropped".
================
*/
export function isSunDropNoticeText( text: string ): boolean {
	return text.startsWith( "SEAL OF SUN: " ) ||
		(text.includes( "(Seal Of Sun)" ) && text.includes( " has dropped " ));
}

/*
================
Seal notice colours

Port-only, not native (#1640, owner decision 2026-10-26: "Colour by
tier + Sun prefix"): a drop or plus notice line colours by the linked
item's seal tier - the Star in the rare gold the item tooltip names it
with (item-tooltip.ts), the Moon in pale silver-blue, the Sun in
bright orange-red. Unsealed items keep the channel's own colour. The
colours ride the "Drop & plus notices" Experimental row: row off, the
channel never reaches the log at all.
================
*/
export const SEAL_NOTICE_COLOR_STAR = 0xffffd953;
export const SEAL_NOTICE_COLOR_MOON = 0xffc9d9f2;
export const SEAL_NOTICE_COLOR_SUN = 0xffff4517;

/*
================
sealNoticeColorArgb

The notice colour for an item's seal fields (rarity 2 and the
itemClass whose (itemClass - 1) % 3 is Star, Moon, Sun), or undefined
when the item carries no seal.
================
*/
export function sealNoticeColorArgb(
	rarity: number | undefined,
	itemClass: number | undefined
): number | undefined {
	if ( rarity !== 2 || !itemClass ) return undefined;
	return [ SEAL_NOTICE_COLOR_STAR, SEAL_NOTICE_COLOR_MOON, SEAL_NOTICE_COLOR_SUN ][(itemClass - 1) % 3];
}

/*
================
serverNotification

753A6F -> 752ABD -> 67A140/6B3180: the native type-7 server notification,
and the port-only job-kill, drop/plus and item-break channels laid out the same way
(flagged jobKill, dropPlus, plusSeven or itemBreak, so the gameplay owner can hide them). The chat owner separately retains the
(Notify): line in the main log.
================
*/
export function serverNotification( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0x3667 || !isServerNoticeChannel( p[0] ) ) return null;
	if ( p.length < 3 ) throw Error( "Truncated server notification" );
	const n = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 1, true );
	// Port-only: a drop/plus notice may carry its links after the text
	// (notice-links.ts); the banner shows the text alone.
	const links = p[0] === DROP_PLUS_NOTICE_CHANNEL && p.length > 3 + 2 * n;
	if ( n > 100 || !links && p.length !== 3 + 2 * n || p.length < 3 + 2 * n ) {
		throw Error( "Invalid server notification length" );
	}
	const text = new TextDecoder( "utf-16le", { fatal: true } ).decode( p.subarray( 3, 3 + 2 * n ) );
	const notice: SystemNotice = { key: "", text, value: 0, notificationBanner: true, bannerOnly: true };
	if ( p[0] === JOB_KILL_NOTICE_CHANNEL ) return { ...notice, jobKill: true };
	if ( p[0] === BREAK_NOTICE_CHANNEL ) return { ...notice, itemBreak: true };
	if ( p[0] === DROP_PLUS_NOTICE_CHANNEL ) {
		if ( links && announcedPlusSeven( p, 3 + 2 * n ) ) return { ...notice, plusSeven: true };
		// Port-only, not native: Seal of Sun drop announcements highlight in
		// gold on the top banner and chat log; Star/Moon and plus notices stay unchanged.
		const sun = isSunDropNoticeText( text ) ? ({ sunDrop: true } as const) : {};
		return { ...notice, dropPlus: true, ...sun };
	}
	return notice;
}

/*
================
announcedPlusSeven

Whether a drop/plus frame announces an item at exactly +7, read off the
linked item body's plus byte: the +7 bucket shares the drop/plus
channel (no new wire channel, so every client keeps rendering it), and
this byte alone picks its row. Seal drops (+0), plus of +8 and above,
and frames without a readable body stay with dropPlus.
================
*/
function announcedPlusSeven( p: Uint8Array, linksOffset: number ): boolean {
	const body = decodeNoticeLinks( p, linksOffset )?.body;
	return body !== undefined && body.length >= ITEM_BODY_HEADER_BYTES && body[ITEM_BODY_PLUS_BYTE] === 7;
}

/*
================
restrictionNotice

74BFF0 -> 9C422E -> 67D030 / 67A600. Fields are bit slices, not a JS Date:
zero/overflow calendar values remain numbers; noon is still Forenoon.
================
*/
export function restrictionNotice( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0x36ea ) return null;
	if ( p.length !== 5 ) throw Error( "Invalid command restriction notice" );
	const kind = p[0]!;
	if ( kind !== 0 && kind !== 1 ) return null;
	const date = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
	const hour = (date >>> 15) & 31, afternoon = hour > 12;
	return {
		key: kind === 0 ? "UIIT_MSG_GM_PUNISHMENT_CHAT_BLOCK" : "UIIT_MSG_GM_PUNISHMENT_TRADE_BLOCK",
		value: 0,
		arguments: [
			String( (date & 63) + 2000 ),
			String( (date >>> 6) & 15 ),
			String( (date >>> 10) & 31 ),
			"",
			String( afternoon ? hour - 12 : hour ),
			String( (date >>> 20) & 63 )
		],
		localizedArguments: [
			null,
			null,
			null,
			afternoon ? "UIIT_STT_LETTER_AFTEMOON" : "UIIT_STT_LETTER_FORENOON",
			null,
			null
		],
		formatKinds: [ "d", "d", "d", "s", "d", "d" ],
		formatCapacity: 511,
		nativeType: 5,
		notificationBanner: true
	};
}
