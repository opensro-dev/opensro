/*
===========================================================================

grant-power-hud.ts - the guild page's member rights panel

CIFGuildMember_SetMode(3) (5E3090) swaps the member list for
CIFGuildGrantPower: a row per member with five rights (5EF520: 1 join,
2 withdraw, 4 union chat, 8 storage, 0x10 notice). The master edits a
draft; the union chat right refuses a thirteenth holder (5EF030 case 2,
the tooltip's "%d / 12"); confirm sends the changed rows as 0x744E
(5EE410). The leader's own row is not listed: the leader holds every
right. The UI draws from this owner every frame.

===========================================================================
*/

/*
================
GrantMember
================
*/
export interface GrantMember {
	readonly id: number;
	readonly name: string;
	readonly grade: number;
	readonly permissions: number;
}

export const GRANT_RIGHTS = [ 1, 2, 4, 8, 0x10 ] as const;
const RIGHT_UNION_CHAT = 4;
const UNION_CHAT_HOLDERS = 12;
export const GRANT_VISIBLE_ROWS = 5;

/*
================
createGrantPowerHud
================
*/
export function createGrantPowerHud() {
	let rows: GrantMember[] | null = null;
	const draft = new Map<number, number>();
	let first = 0;
	/*
	================
	unionChatHolders
	================
	*/
	function unionChatHolders() {
		let n = 0;
		for ( const mask of draft.values() ) if ( mask & RIGHT_UNION_CHAT ) n++;
		return n;
	}
	return {
		/*
		================
		open

		Snapshots the roster's rights into the draft.
		================
		*/
		open( members: readonly GrantMember[] ) {
			rows = members.filter( m => m.grade !== 0 ).map( m => ({ ...m, permissions: m.permissions >>> 0 }) );
			draft.clear();
			for ( const row of rows ) draft.set( row.id, row.permissions );
			first = 0;
		},
		/*
		================
		isOpen
		================
		*/
		isOpen(): boolean {
			return rows !== null;
		},
		/*
		================
		close
		================
		*/
		close() {
			rows = null;
			draft.clear();
		},
		/*
		================
		visible

		The rows the scroll window shows, with their drafted rights.
		================
		*/
		visible(): { readonly row: GrantMember; readonly mask: number; }[] {
			if ( !rows ) return [];
			return rows.slice( first, first + GRANT_VISIBLE_ROWS ).map( row => ({
				row,
				mask: draft.get( row.id ) ?? 0
			}) );
		},
		/*
		================
		scroll
		================
		*/
		scroll( delta: number ) {
			if ( !rows ) return;
			first = Math.max( 0, Math.min( Math.max( 0, rows.length - GRANT_VISIBLE_ROWS ), first + delta ) );
		},
		/*
		================
		toggle
		================
		*/
		toggle( id: number, right: number ) {
			const mask = draft.get( id );
			if ( mask === undefined || !GRANT_RIGHTS.includes( right as typeof GRANT_RIGHTS[number] ) ) return;
			const next = (mask ^ right) >>> 0;
			draft.set( id, next );
			if ( right === RIGHT_UNION_CHAT && next & RIGHT_UNION_CHAT && unionChatHolders() > UNION_CHAT_HOLDERS ) {
				draft.set( id, mask );
			}
		},
		/*
		================
		grants

		The rows whose rights changed (5EE1C0 sends only those).
		================
		*/
		grants(): { readonly id: number; readonly permissions: number; }[] {
			if ( !rows ) return [];
			return rows.filter( row => draft.get( row.id ) !== row.permissions ).map( row => ({
				id: row.id,
				permissions: draft.get( row.id )!
			}) );
		}
	};
}
