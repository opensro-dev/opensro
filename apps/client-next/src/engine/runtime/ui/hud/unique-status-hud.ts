/*
===========================================================================

unique-status-hud.ts - the Unique Status window's state (port-only)

Port-only, not native (#3531). The window lists the uniques of the
worker's feed (contracts/unique-status.ts) on four tabs and lets the
player mark uniques to be alerted about. Edits are a draft until Save:
the marks, whether deaths alert too, and whether the browser may speak.
Saving is also the only moment browser permission is asked for (ui.ts
asks, right there, after the player opted in). Alerts follow the saved
list, never the draft.

This owner carries the window's whole behaviour: the click routing,
the list's wheel and thumb state, and the server clock run forward
between feed frames. ui.ts keeps only the window admission, the
draw-operation loop, and the two platform calls a Save can ask for
(persisting the list, requesting the browser's permission).

Alert changes are decided here, on the feed's revision: the first feed
after entering the world is the baseline and alerts on nothing, so a
player is never told of a death that predates their session.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import type { UniqueStatusFeed } from "@/engine/contracts/unique-status";
import {
	emptyUniqueNotify,
	uniqueNotifySettings,
	uniqueStatusAlertText,
	uniqueStatusFlips,
	type UniqueNotifySettings,
	type UniqueStatusFlip,
	type UniqueStatusTab
} from "@/engine/foundation/gameplay/unique-status";
import { containsPoint } from "@/engine/foundation/ui/hit-test";

/*
================
UniqueStatusDraft

The window's unsaved edits: the marked references, and the two toggles.
================
*/
export interface UniqueStatusDraft {
	readonly refs: readonly number[];
	readonly deathsToo: boolean;
	readonly browser: boolean;
}

/*
================
UniqueStatusAction

What a click asks the page to do beyond the state it already changed:
persist this saved list, and (only for a Save that turned browser
notifications on) ask the browser's permission - on Save, and only
there. Both are platform calls, so they stay in ui.ts.
================
*/
export interface UniqueStatusAction {
	readonly save: UniqueNotifySettings | null;
	readonly askPermission: boolean;
}

/*
================
UniqueStatusPointerEvent

The pointer shapes the list routes: a wheel turn over it, and its
scrollbar thumb's drags.
================
*/
export interface UniqueStatusPointerEvent {
	readonly kind: string;
	readonly id?: string;
	readonly x?: number;
	readonly y?: number;
	readonly delta?: number;
	readonly dy?: number;
}

/*
================
notifyBrowserUnique

The browser's own notification for a marked unique's flip - only when
the saved list allows it and the browser's permission (granted at
Save, never asked here) is in place. A browser without the
Notification API simply stays quiet.
================
*/
export function notifyBrowserUnique( flip: UniqueStatusFlip, allowed: boolean ): void {
	if ( !allowed || typeof Notification === "undefined" || Notification.permission !== "granted" ) {
		return;
	}
	try {
		new Notification( flip.row.name, { body: uniqueStatusAlertText( flip ) } );
	} catch {
		// A refused notification is the browser's business, not a frame's.
	}
}

/*
================
createUniqueStatusHud
================
*/
export function createUniqueStatusHud( book?: () => Readonly<Record<string, UniqueNotifySettings>> ) {
	let tab: UniqueStatusTab = "all";
	let firstRow = 0;
	// The character the settings belong to, its saved list, and the draft.
	let character = "";
	let saved: UniqueNotifySettings = emptyUniqueNotify();
	let draft: UniqueStatusDraft = emptyUniqueNotify();
	// The last feed the alerts were decided against (null = no baseline).
	let lastRows: UniqueStatusFeed["rows"] | null = null;
	// The list the last frame drew (its rect, rows, page) and how far
	// its scrollbar thumb travels: the pointer routes against these.
	let drawnList: { readonly rect: UiRect; readonly total: number; readonly page: number; } | null = null;
	let thumbTravel = 0;
	// The server's clock at the last feed frame, run forward locally.
	let clockRevision = -1;
	let clockBase = { serverMs: 0, atMs: 0 };
	/*
================
dirty

Unsaved edits exist (the Save button's state).
================
	*/
	function dirty(): boolean {
		return draft.deathsToo !== saved.deathsToo ||
			draft.browser !== saved.browser ||
			draft.refs.length !== saved.refs.length ||
			draft.refs.some( ( ref, i ) => ref !== saved.refs[i] );
	}
	/*
================
scroll

Moves the first visible row by whole rows, clamped to the list.
================
	*/
	function scroll( delta: number, total: number, page: number ) {
		firstRow = Math.min( Math.max( 0, total - page ), Math.max( 0, firstRow + delta ) );
	}
	/*
================
dragThumb

The scrollbar thumb dragged by dy pixels over its travel.
================
	*/
	function dragThumb( dy: number, total: number, page: number ) {
		if ( thumbTravel <= 0 ) return;
		const range = Math.max( 0, total - page );
		firstRow = Math.min( range, Math.max( 0, Math.round( firstRow + dy * range / thumbTravel ) ) );
	}
	return {
		/*
================
state

What the window draws: the tab and the first visible row. (The window
itself is the "Unique Status" panel; ui.ts owns its opening.)
================
		*/
		state: () => ({ tab, firstRow }),
		/*
================
draft
================
		*/
		draft: () => draft,
		/*
================
savedSettings

The list alerts follow for the attached character.
================
		*/
		savedSettings: () => saved,
		/*
================
character
================
		*/
		character: () => character,
		/*
================
attach

Points the window at a character and its saved list (the book from
platform storage). A different character re-reads the book and drops
any unsaved edits; the same one keeps them.
================
		*/
		attach( name: string ) {
			if ( name === character ) return;
			character = name;
			saved = name ? uniqueNotifySettings( book?.()[name] ) : emptyUniqueNotify();
			draft = saved;
			firstRow = 0;
		},
		/*
================
click

One control of the window, by its id: switches the tab, toggles a
mark or an option, steps the list, or takes the Save. Returns what
the page must still do (see UniqueStatusAction), or null when the id
is not this window's.
================
		*/
		click( id: string ): UniqueStatusAction | null {
			if ( id.startsWith( "unique-status-tab:" ) ) {
				tab = id.slice( "unique-status-tab:".length ) as UniqueStatusTab;
				firstRow = 0;
			} else if ( id.startsWith( "unique-status-mark:" ) ) {
				const refObjId = Number( id.slice( "unique-status-mark:".length ) );
				const refs = draft.refs.includes( refObjId ) ?
					draft.refs.filter( ( ref ) => ref !== refObjId ) :
					[ ...draft.refs, refObjId ].sort( ( a, b ) => a - b );
				draft = { ...draft, refs };
			} else if ( id === "unique-status-deaths" ) {
				draft = { ...draft, deathsToo: !draft.deathsToo };
			} else if ( id === "unique-status-browser" ) {
				draft = { ...draft, browser: !draft.browser };
			} else if ( id === "unique-status-save" ) {
				if ( !dirty() ) return { save: null, askPermission: false };
				saved = { refs: [ ...draft.refs ], deathsToo: draft.deathsToo, browser: draft.browser };
				draft = saved;
				return { save: saved, askPermission: saved.browser };
			} else if ( id === "unique-status-scroll-up" || id === "unique-status-scroll-down" ) {
				scroll( id === "unique-status-scroll-up" ? -1 : 1, drawnList?.total ?? 0, drawnList?.page ?? 0 );
			} else {
				return null;
			}
			return { save: null, askPermission: false };
		},
		/*
================
pointer

The wheel over the drawn list and its thumb's drags, against the
geometry the last frame drew. True when the event was the list's
(whether or not the position moved).
================
		*/
		pointer( event: UniqueStatusPointerEvent ): boolean {
			if ( event.kind === "scroll" ) {
				if ( !drawnList || !containsPoint( drawnList.rect, event.x ?? -1, event.y ?? -1 ) ) {
					return false;
				}
				scroll( Math.sign( event.delta ?? 0 ), drawnList.total, drawnList.page );
				return true;
			}
			if ( event.id === "unique-status-thumb" && drawnList ) {
				if ( event.kind === "drag" ) dragThumb( event.dy ?? 0, drawnList.total, drawnList.page );
				return true;
			}
			return false;
		},
		/*
================
noteDrawn

Records what the last frame drew of the list: its geometry, and its
scrollbar thumb's travel when a bar was drawn.
================
		*/
		noteDrawn(
			list: { readonly rect: UiRect; readonly total: number; readonly page: number; },
			travel?: number
		) {
			drawnList = list;
			if ( travel !== undefined ) thumbTravel = travel;
		},
		/*
================
serverNowMs

The server's clock at `atMs`: the last feed's time run forward by the
local one, so "50 min ago" keeps moving between feed frames. A feed
revision rebases it; no feed reads as 0.
================
		*/
		serverNowMs( feed: UniqueStatusFeed | null, atMs: number ): number {
			if ( !feed ) return 0;
			if ( feed.revision !== clockRevision ) {
				clockRevision = feed.revision;
				clockBase = { serverMs: feed.serverNowMs, atMs };
			}
			return clockBase.serverMs + Math.max( 0, atMs - clockBase.atMs );
		},
		dirty,
		/*
================
reconcile

The flips the saved list alerts on in this feed: a marked unique that
spawned, and (only with deathsToo) one that died. The first feed is
the baseline and alerts on nothing. With the Experimental row off the
flips are still consumed, so turning it back on never alerts on an
old change.
================
		*/
		reconcile( feed: UniqueStatusFeed | null | undefined, enabled: boolean ): readonly UniqueStatusFlip[] {
			if ( !feed ) return [];
			const previous = lastRows;
			lastRows = feed.rows;
			if ( !enabled || previous === null ) return [];
			return uniqueStatusFlips( previous, feed.rows ).filter( ( flip ) =>
				saved.refs.includes( flip.row.refObjId ) && (flip.spawned || saved.deathsToo)
			);
		},
		/*
================
reset

Leaving the world forgets the baseline, the drawn list and the
clock: the next world's first feed starts a fresh one.
================
		*/
		reset() {
			lastRows = null;
			firstRow = 0;
			drawnList = null;
			thumbTravel = 0;
			clockRevision = -1;
			clockBase = { serverMs: 0, atMs: 0 };
		}
	};
}
