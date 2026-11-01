/*
===========================================================================

mall-notice-memory.ts - which accounts closed the Item Mall notice

Deliberate deviation, approved by the owner (2026-10-28) and on by
default with no switch: the native client shows CIFMallNotifyWnd ("New
item(s) has been stocked in Item Mall") on every login, because 683B40
creates it whenever CGInterface+0x6FC is clear and the CPSMission
constructor (728AB0) rebuilds CGInterface on each world entry. Here a
Close (or Enter) is remembered for that account until the client release
changes, so the notice comes back only when a new release may have
stocked something.

A session restored after a page refresh never typed its account, so the
character that closed the notice is remembered with it and either name
matches. Names compare case-insensitively.

The record is a per-browser convenience: a missing, unreadable or
foreign record shows the notice, as native.

===========================================================================
*/

export const MALL_NOTICE_STORAGE_KEY = "sro:v1150:mall-notice-closed:1";
// Remembered names per release; the oldest are dropped past this.
export const MAX_MALL_NOTICE_NAMES = 64;

/*
================
MallNoticeWho

The login account (empty when the session was restored) and the
character in the world (empty before one is chosen).
================
*/
export interface MallNoticeWho {
	readonly account: string;
	readonly character: string;
}

interface MallNoticeRecord {
	readonly revision: string;
	readonly names: readonly string[];
}

/*
================
mallNoticeNames

The record keys for one player: "a:" accounts and "c:" characters, so an
account and a character that share a name stay apart.
================
*/
function mallNoticeNames( who: MallNoticeWho ): string[] {
	const names: string[] = [];
	const account = who.account.trim().toLowerCase();
	const character = who.character.trim().toLowerCase();
	if ( account ) names.push( "a:" + account );
	if ( character ) names.push( "c:" + character );
	return names;
}

/*
================
readMallNoticeRecord

The stored record when it belongs to this release, else none.
================
*/
function readMallNoticeRecord( storage: Pick<Storage, "getItem">, revision: string ): MallNoticeRecord | null {
	const stored = storage.getItem( MALL_NOTICE_STORAGE_KEY );
	if ( stored === null ) return null;
	const raw: unknown = JSON.parse( stored );
	if ( typeof raw !== "object" || raw === null ) return null;
	const record = raw as { revision?: unknown; names?: unknown; };
	if ( record.revision !== revision || !Array.isArray( record.names ) ) return null;
	return { revision, names: record.names.filter( ( name ): name is string => typeof name === "string" ) };
}

/*
================
mallNoticeClosed

True when this account or character closed the notice under this
release.
================
*/
export function mallNoticeClosed( storage: Pick<Storage, "getItem">, revision: string, who: MallNoticeWho ): boolean {
	const record = readMallNoticeRecord( storage, revision );
	if ( !record ) return false;
	return mallNoticeNames( who ).some( name => record.names.includes( name ) );
}

/*
================
rememberMallNoticeClosed

Adds this account and character to the release's record. A record from
another release is replaced, which is what brings the notice back.
================
*/
export function rememberMallNoticeClosed(
	storage: Pick<Storage, "getItem" | "setItem">,
	revision: string,
	who: MallNoticeWho
): void {
	let names: string[] = [];
	try {
		names = [ ...(readMallNoticeRecord( storage, revision )?.names ?? []) ];
	} catch {}
	for ( const name of mallNoticeNames( who ) ) {
		if ( !names.includes( name ) ) names.push( name );
	}
	const record: MallNoticeRecord = { revision, names: names.slice( -MAX_MALL_NOTICE_NAMES ) };
	storage.setItem( MALL_NOTICE_STORAGE_KEY, JSON.stringify( record ) );
}
