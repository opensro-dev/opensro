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

Alert changes are decided here, on the feed's revision: the first feed
after entering the world is the baseline and alerts on nothing, so a
player is never told of a death that predates their session.

===========================================================================
*/
import type { UniqueStatusFeed } from "@/engine/contracts/unique-status";
import {
	emptyUniqueNotify,
	uniqueNotifySettings,
	uniqueStatusFlips,
	type UniqueNotifySettings,
	type UniqueStatusFlip,
	type UniqueStatusTab
} from "@/engine/foundation/gameplay/unique-status";

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
createUniqueStatusHud
================
*/
export function createUniqueStatusHud( book: Readonly<Record<string, UniqueNotifySettings>> ) {
	let open = false;
	let tab: UniqueStatusTab = "all";
	let firstRow = 0;
	// The character the settings belong to, its saved list, and the draft.
	let character = "";
	let saved: UniqueNotifySettings = emptyUniqueNotify();
	let draft: UniqueStatusDraft = emptyUniqueNotify();
	// The last feed the alerts were decided against (null = no baseline).
	let lastRows: UniqueStatusFeed["rows"] | null = null;
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
	return {
		/*
================
state

What the window draws: open or not, the tab, the first visible row.
================
		*/
		state: () => ({ open, tab, firstRow }),
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
			saved = name ? uniqueNotifySettings( book[name] ) : emptyUniqueNotify();
			draft = saved;
			firstRow = 0;
		},
		/*
================
openWindow
================
		*/
		openWindow() {
			open = true;
		},
		/*
================
close
================
		*/
		close() {
			open = false;
		},
		/*
================
selectTab
================
		*/
		selectTab( next: UniqueStatusTab ) {
			tab = next;
			firstRow = 0;
		},
		/*
================
scroll

Moves the first visible row by whole rows, clamped by the caller-given
row count and page size.
================
		*/
		scroll( delta: number, total: number, page: number ) {
			firstRow = Math.min( Math.max( 0, total - page ), Math.max( 0, firstRow + delta ) );
		},
		/*
================
dragThumb

The scrollbar thumb dragged by dy pixels over its travel.
================
		*/
		dragThumb( dy: number, travel: number, total: number, page: number ) {
			if ( travel <= 0 ) return;
			const range = Math.max( 0, total - page );
			firstRow = Math.min( range, Math.max( 0, Math.round( firstRow + dy * range / travel ) ) );
		},
		/*
================
toggleMark
================
		*/
		toggleMark( refObjId: number ) {
			const refs = draft.refs.includes( refObjId ) ?
				draft.refs.filter( ( ref ) => ref !== refObjId ) :
				[ ...draft.refs, refObjId ].sort( ( a, b ) => a - b );
			draft = { ...draft, refs };
		},
		/*
================
toggleDeaths
================
		*/
		toggleDeaths() {
			draft = { ...draft, deathsToo: !draft.deathsToo };
		},
		/*
================
toggleBrowser
================
		*/
		toggleBrowser() {
			draft = { ...draft, browser: !draft.browser };
		},
		dirty,
		/*
================
takeSave

Takes the draft as the saved list and returns it for the page to
persist (null when there is nothing to save). The caller asks the
browser's permission when the taken list allows it - on Save, and
only there.
================
		*/
		takeSave(): UniqueNotifySettings | null {
			if ( !dirty() ) return null;
			saved = { refs: [ ...draft.refs ], deathsToo: draft.deathsToo, browser: draft.browser };
			draft = saved;
			return saved;
		},
		/*
================
reconcile

The flips the saved list alerts on in this feed: a marked unique that
spawned, and (only with deathsToo) one that died. The first feed is
the baseline and alerts on nothing. With the Experimental row off the
window closes and flips are still consumed, so turning it back on
never alerts on an old change.
================
		*/
		reconcile( feed: UniqueStatusFeed | null | undefined, enabled: boolean ): readonly UniqueStatusFlip[] {
			if ( !enabled ) open = false;
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

Leaving the world closes the window and forgets the baseline: the
next world's first feed starts a fresh one.
================
		*/
		reset() {
			open = false;
			lastRows = null;
			firstRow = 0;
		}
	};
}
