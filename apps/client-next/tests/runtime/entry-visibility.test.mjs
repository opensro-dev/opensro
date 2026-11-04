/*
===========================================================================

entry-visibility.test.mjs - what a rejected entry reports about the tab

A tab hidden while its entry is in flight reads as a client that went
silent: whether it happened, and for how long, is the difference
between a throttled background tab and a stalled device, so the
rejection report carries the entry's hidden time (#2558).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { entryFixture } from "../helpers/entry-fixture.mjs";

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
