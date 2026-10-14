/*
===========================================================================

system-notices.ts - server frames that become a notice or a banner

Decodes the fortress announcements (0x3887), the server notification and
the shard's job-kill notice (0x3667), and the command restriction notice
(0x36EA) into SystemNotice rows the gameplay owner queues for the HUD.

===========================================================================
*/

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
// wire.JobKillNoticeChannel). v1.150 (753760) routes only channels 1..16, so
// it is the browser's own; it shows as channel 7 unless the Experimental
// "Job kill notices" row is off.
export const JOB_KILL_NOTICE_CHANNEL = 0x80;

/*
================
serverNotification

753A6F -> 752ABD -> 67A140/6B3180: the native type-7 server notification,
and the port-only job-kill channel laid out the same way (flagged jobKill,
so the gameplay owner can hide it). The chat owner separately retains the
(Notify): line in the main log.
================
*/
export function serverNotification( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0x3667 || (p[0] !== 7 && p[0] !== JOB_KILL_NOTICE_CHANNEL) ) return null;
	if ( p.length < 3 ) throw Error( "Truncated server notification" );
	const n = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 1, true );
	if ( n > 100 || p.length !== 3 + 2 * n ) throw Error( "Invalid server notification length" );
	const text = new TextDecoder( "utf-16le", { fatal: true } ).decode( p.subarray( 3 ) );
	return p[0] === JOB_KILL_NOTICE_CHANNEL ?
		{ key: "", text, value: 0, notificationBanner: true, bannerOnly: true, jobKill: true } :
		{ key: "", text, value: 0, notificationBanner: true, bannerOnly: true };
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
