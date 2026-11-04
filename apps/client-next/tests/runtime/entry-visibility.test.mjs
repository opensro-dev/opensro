/*
===========================================================================

entry-visibility.test.mjs - the entry window's hidden-time accounting

The tracker is pure (the caller owns the clock and the page's hidden
flag): these rows pin the spans a world entry can live through - never
hidden, hidden mid-entry, hidden before the entry started, and a
window that ends while the page is still hidden.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const {
	createEntryVisibility,
	entryVisibilityBegin,
	entryVisibilitySetHidden,
	entryVisibilityTotal,
	entryVisibilityEnd
} = await import( "../../src/engine/foundation/session/entry-visibility.ts" );

test("a visible entry accumulates no hidden time", () => {
	const visibility = createEntryVisibility();
	entryVisibilityBegin( visibility, 1000, false );
	entryVisibilitySetHidden( visibility, false, 6000 );
	assert.equal( entryVisibilityTotal( visibility, 9000 ), 0 );
	assert.equal( entryVisibilityEnd( visibility, 9000 ), 0 );
	assert.equal( visibility.tracking, false );
});

test("a hidden span counts once, from hide to show", () => {
	const visibility = createEntryVisibility();
	entryVisibilityBegin( visibility, 1000, false );
	entryVisibilitySetHidden( visibility, true, 2000 );
	entryVisibilitySetHidden( visibility, true, 3000 );
	entryVisibilitySetHidden( visibility, false, 6500 );
	assert.equal( entryVisibilityTotal( visibility, 9000 ), 4500 );
});

test("an entry that starts hidden counts from its first moment", () => {
	const visibility = createEntryVisibility();
	entryVisibilityBegin( visibility, 1000, true );
	entryVisibilitySetHidden( visibility, false, 4000 );
	assert.equal( entryVisibilityEnd( visibility, 5000 ), 3000 );
});

test("a window ending while hidden counts the open span and closes", () => {
	const visibility = createEntryVisibility();
	entryVisibilityBegin( visibility, 1000, false );
	entryVisibilitySetHidden( visibility, true, 2000 );
	assert.equal( entryVisibilityEnd( visibility, 7000 ), 5000 );
	// Closed: later flips belong to no window.
	entryVisibilitySetHidden( visibility, false, 8000 );
	assert.equal( entryVisibilityTotal( visibility, 9000 ), 0 );
});

test("a new window starts from zero", () => {
	const visibility = createEntryVisibility();
	entryVisibilityBegin( visibility, 1000, false );
	entryVisibilitySetHidden( visibility, true, 2000 );
	entryVisibilityEnd( visibility, 3000 );
	entryVisibilityBegin( visibility, 10000, false );
	assert.equal( entryVisibilityTotal( visibility, 20000 ), 0 );
});
