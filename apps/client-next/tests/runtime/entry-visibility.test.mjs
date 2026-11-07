/*
===========================================================================

entry-visibility.test.mjs - what a rejected entry reports about the tab

A tab hidden while its entry is in flight reads as a client that went
silent: whether it happened, and for how long, is the difference
between a throttled background tab and a stalled device, so the
rejection report carries the entry's hidden time (#2558). The pure
tracker underneath is pinned beside the report test: never hidden,
hidden mid-entry, hidden before the entry started, a window ending
while the page is still hidden, and a fresh window from zero.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { entryFixture } from "../helpers/entry-fixture.mjs";
const {
	createEntryVisibility,
	entryVisibilityBegin,
	entryVisibilitySetHidden,
	entryVisibilityTotal,
	entryVisibilityEnd
} = await import( "../../src/engine/foundation/session/entry-visibility.ts" );

/*
================
entryTabHiddenTime

The tab hides for one second of the entry; the rejection that follows
must report that second, not omit the span and not invent one.
================
*/
test("a rejected entry reports how long the tab was hidden during it", t => {
	const page = { hidden: false };
	Object.defineProperty( globalThis, "document", { value: page, configurable: true } );
	t.after( () => Reflect.deleteProperty( globalThis, "document" ) );
	const sent = [];
	const f = entryFixture( false, sent );
	f.toDock();
	f.frontend.select( "fixture" );
	for ( let i = 0; i < 30; i++ ) f.step();
	assert.equal( f.frontend.start(), true );
	f.bind( "connecting" );
	// Hidden for one second of the entry (ten 100 ms steps), then back.
	page.hidden = true;
	for ( let i = 0; i < 10; i++ ) f.step();
	page.hidden = false;
	f.step();
	f.session = { ...f.session, phase: "character-select", revision: f.session.revision + 1, error: "offline" };
	f.step();
	const reports = sent.filter( command => command.kind === "client-report" );
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].code, "world_entry_rejected" );
	assert.ok(
		reports[0].tabHiddenMs >= 900 && reports[0].tabHiddenMs <= 1100,
		`tabHiddenMs = ${reports[0].tabHiddenMs}`
	);
	f.frontend.dispose();
});

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
