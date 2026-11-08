/*
===========================================================================

experimental-list.ts - the Experimental window's sectioned, scrolling list

Port-only, not native. A tab's list is each section's header followed by
its rows, two per line, left to right. The list scrolls by whole lines, as
the native option scroll managers move whole rows, so no half row ever
shows at an edge; the header of the section whose line is first in view
stays pinned at the top.

===========================================================================
*/

// A section header slot: a 32-pixel bar with 4 pixels above and below.
export const EXPERIMENTAL_HEAD_HEIGHT = 40;
// A line of rows: the label line, then the description line.
export const EXPERIMENTAL_ROW_HEIGHT = 46;
// Rows per line, a row's width and the gap between two rows of a line.
export const EXPERIMENTAL_COLUMNS = 2;
export const EXPERIMENTAL_COLUMN_WIDTH = 312;
export const EXPERIMENTAL_COLUMN_GAP = 8;
// The list's height: one header and seven lines.
export const EXPERIMENTAL_LIST_HEIGHT = 362;
// A row that depends on another row (the pickup filter's kinds) is
// drawn this much to the right.
export const EXPERIMENTAL_INDENT = 20;
// A top-level row's description line; the longest description is 304.
export const EXPERIMENTAL_DESCRIPTION_WIDTH = 304;

/*
================
ExperimentalListItem
================
*/
export type ExperimentalListItem<Row> =
	| { readonly kind: "head"; readonly title: string; }
	| { readonly kind: "rows"; readonly rows: readonly Row[]; readonly section: string; };

/*
================
experimentalListItems

Each section's header, then its rows in lines of EXPERIMENTAL_COLUMNS, in
order; a section's last line may hold fewer.
================
*/
export function experimentalListItems<Row>(
	sections: readonly { readonly title: string; readonly rows: readonly Row[]; }[]
): readonly ExperimentalListItem<Row>[] {
	const items: ExperimentalListItem<Row>[] = [];
	for ( const section of sections ) {
		items.push( { kind: "head", title: section.title } );
		for ( let i = 0; i < section.rows.length; i += EXPERIMENTAL_COLUMNS ) {
			items.push( {
				kind: "rows",
				rows: section.rows.slice( i, i + EXPERIMENTAL_COLUMNS ),
				section: section.title
			} );
		}
	}
	return items;
}

/*
================
itemHeight
================
*/
function itemHeight<Row>( item: ExperimentalListItem<Row> ): number {
	return item.kind === "head" ? EXPERIMENTAL_HEAD_HEIGHT : EXPERIMENTAL_ROW_HEIGHT;
}

/*
================
pinnedHeight

The pinned header's height above a view that starts at this item.
================
*/
function pinnedHeight<Row>( item: ExperimentalListItem<Row> | undefined ): number {
	return item?.kind === "rows" ? EXPERIMENTAL_HEAD_HEIGHT : 0;
}

/*
================
rowStarts

The item indices a view may start at: every line of rows. A view starting
on a header would paint exactly as one starting on its first line with
that header pinned, so a header is never a step of its own.
================
*/
function rowStarts<Row>( items: readonly ExperimentalListItem<Row>[] ): number[] {
	const starts: number[] = [];
	items.forEach( ( item, index ) => {
		if ( item.kind === "rows" ) starts.push( index );
	} );
	return starts;
}

/*
================
experimentalListRange

The last scroll step: the first line step whose remaining items fit under
its pinned header. 0 when the whole list fits.
================
*/
export function experimentalListRange<Row>(
	items: readonly ExperimentalListItem<Row>[],
	height = EXPERIMENTAL_LIST_HEIGHT
): number {
	const starts = rowStarts( items );
	let rest = 0;
	for ( const item of items ) rest += itemHeight( item );
	let consumed = 0, next = 0;
	for ( let step = 0; step < starts.length; step++ ) {
		for ( ; next < starts[step]!; next++ ) consumed += itemHeight( items[next]! );
		if ( EXPERIMENTAL_HEAD_HEIGHT + rest - consumed <= height ) return step;
	}
	return Math.max( 0, starts.length - 1 );
}

/*
================
experimentalListView

The slots the view at a scroll step shows: the pinned header's title and
each item with its top inside the list. A fractional or out-of-range step
is truncated and clamped.
================
*/
export function experimentalListView<Row>(
	items: readonly ExperimentalListItem<Row>[],
	step: number,
	height = EXPERIMENTAL_LIST_HEIGHT
): {
	readonly pinned: string | null;
	readonly slots: readonly { readonly item: ExperimentalListItem<Row>; readonly top: number; }[];
} {
	const starts = rowStarts( items );
	const clamped = Math.max( 0, Math.min( experimentalListRange( items, height ), Math.trunc( step ) || 0 ) );
	const start = starts[clamped] ?? 0, lead = items[start];
	const slots: { item: ExperimentalListItem<Row>; top: number; }[] = [];
	let top = pinnedHeight( lead );
	for ( let i = start; i < items.length; i++ ) {
		const item = items[i]!, bottom = top + itemHeight( item );
		if ( bottom > height ) break;
		slots.push( { item, top } );
		top = bottom;
	}
	return { pinned: lead?.kind === "rows" ? lead.section : null, slots };
}
