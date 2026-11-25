/*
===========================================================================

unique-status.ts - the Unique Status window's text and per-character marks

Port-only, not native (#3531). The pure half of the Unique Status window
(unique-status-hud.ts owns the state, ui.ts the drawing): which rows a
tab shows and in what order, how a death reads in server time and how
long ago it was, and the player's per-character notify list - which
uniques to be told about, whether deaths count too, and whether the
browser may speak. Nothing here knows the wire or the canvas.

===========================================================================
*/
import type { UniqueStatusRow } from "@/engine/contracts/unique-status";

/*
================
UniqueStatusTab

The window's four tabs. The Notify tab shows the marked uniques.
================
*/
export type UniqueStatusTab = "all" | "alive" | "dead" | "notify";

/*
================
UniqueNotifySettings

One character's notify list: the marked uniques' reference ids, whether
deaths alert as well as spawns, and whether browser notifications are
allowed (their permission is asked only when this is saved on).
================
*/
export interface UniqueNotifySettings {
	readonly refs: readonly number[];
	readonly deathsToo: boolean;
	readonly browser: boolean;
}

const MAX_NOTIFY_REFS = 256;

/*
================
emptyUniqueNotify

A fresh empty notify list, so callers never share one mutable default.
================
*/
export function emptyUniqueNotify(): UniqueNotifySettings {
	return { refs: [], deathsToo: false, browser: false };
}

/*
================
uniqueNotifySettings

A stored notify entry, read back: anything malformed is the empty list,
so a corrupt record never alerts and never throws.
================
*/
export function uniqueNotifySettings( value: unknown ): UniqueNotifySettings {
	if ( !value || typeof value !== "object" ) return emptyUniqueNotify();
	const record = value as Record<string, unknown>;
	const refs: number[] = [];
	if ( Array.isArray( record.refs ) ) {
		for ( const ref of record.refs ) {
			if ( typeof ref === "number" && Number.isFinite( ref ) && ref > 0 && !refs.includes( ref ) ) {
				refs.push( Math.floor( ref ) );
			}
		}
	}
	refs.sort( ( a, b ) => a - b );
	return {
		refs: refs.slice( 0, MAX_NOTIFY_REFS ),
		deathsToo: record.deathsToo === true,
		browser: record.browser === true
	};
}

/*
================
uniqueNotifyBook

The stored book: character name to its notify settings.
================
*/
export function uniqueNotifyBook( value: unknown ): Readonly<Record<string, UniqueNotifySettings>> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) return {};
	const out: Record<string, UniqueNotifySettings> = {};
	for ( const [name, entry] of Object.entries( value as Record<string, unknown> ) ) {
		if ( name ) out[name] = uniqueNotifySettings( entry );
	}
	return out;
}

/*
================
uniqueStatusRows

The rows one tab shows: the marked ones on the Notify tab, the living
or the dead on theirs. Alive first (a hunter's question), then the
highest level, then the name.
================
*/
export function uniqueStatusRows(
	rows: readonly UniqueStatusRow[],
	tab: UniqueStatusTab,
	marked: readonly number[]
): readonly UniqueStatusRow[] {
	const shown = rows.filter( ( row ) =>
		tab === "all" ||
		(tab === "alive" && row.alive) ||
		(tab === "dead" && !row.alive) ||
		(tab === "notify" && marked.includes( row.refObjId ))
	);
	return [ ...shown ].sort( ( a, b ) =>
		Number( b.alive ) - Number( a.alive ) || b.level - a.level || a.name.localeCompare( b.name )
	);
}

/*
================
uniqueStatusAgo

How long ago a server time was, in the window's short form. A death in
the future (the two clocks mid-handshake) reads as now.
================
*/
export function uniqueStatusAgo( thenMs: number, serverNowMs: number ): string {
	const seconds = Math.max( 0, Math.floor( (serverNowMs - thenMs) / 1000 ) );
	if ( seconds < 60 ) return "just now";
	const minutes = Math.floor( seconds / 60 );
	if ( minutes < 60 ) return `${minutes} min ago`;
	const hours = Math.floor( minutes / 60 );
	if ( hours < 24 ) return minutes % 60 === 0 ? `${hours} h ago` : `${hours} h ${minutes % 60} min ago`;
	return `${Math.floor( hours / 24 )} d ago`;
}

/*
================
uniqueStatusClock

A server time of day (HH:MM), for the Died column; the footer reminds
the player whose clock it is.
================
*/
export function uniqueStatusClock( ms: number ): string {
	const date = new Date( ms );
	const pad = ( n: number ) => String( n ).padStart( 2, "0" );
	return `${pad( date.getUTCHours() )}:${pad( date.getUTCMinutes() )}`;
}

/*
================
UniqueStatusFlip

One unique whose living state changed between two feeds.
================
*/
export interface UniqueStatusFlip {
	readonly row: UniqueStatusRow;
	readonly spawned: boolean;
}

/*
================
uniqueStatusFlips

The rows that spawned (dead to alive) or died (alive to dead) between
the previous feed and this one. A row first learned already alive is
not a spawn: it may have been up for hours.
================
*/
export function uniqueStatusFlips(
	previous: readonly UniqueStatusRow[],
	next: readonly UniqueStatusRow[]
): readonly UniqueStatusFlip[] {
	const before = new Map( previous.map( ( row ) => [ row.refObjId, row.alive ] ) );
	const flips: UniqueStatusFlip[] = [];
	for ( const row of next ) {
		const was = before.get( row.refObjId );
		if ( was !== undefined && was !== row.alive ) {
			flips.push( { row, spawned: row.alive } );
		}
	}
	return flips;
}

/*
================
uniqueStatusAlertText

The alert's one line, in game and in the browser alike.
================
*/
export function uniqueStatusAlertText( flip: UniqueStatusFlip ): string {
	if ( flip.spawned ) return `${flip.row.name} has spawned!`;
	return flip.row.killer ? `${flip.row.name} was killed by ${flip.row.killer}.` : `${flip.row.name} has died.`;
}
