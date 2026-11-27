/*
===========================================================================

ui-census-refresh.test.mjs - the daily census refresh's decisions

#4337 item 21: the refresh runner (tools/ui-action-census-refresh.mjs)
keeps the committed census from ever being found stale at release time.
These tests pin its decision table on synthetic facts - no census run,
no git, no GitHub: a gate pass does nothing, an open refresh PR
suppresses a new one, a moved census refreshes, and a broken census
tool never becomes a pull request.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	CENSUS_REPO_PATH,
	REFRESH_BRANCH_PREFIX,
	censusRefreshDecision,
	parseRefreshArgs,
	refreshBranchName,
	refreshPrBody,
	refreshPrTitle
} from "../../tools/ui-action-census-refresh.mjs";

test("decision: a gate pass means the census is current, whatever else holds", () => {
	assert.equal(
		censusRefreshDecision( { gatePassed: true, openRefreshPr: false, writeExitCode: 0, writeChanged: false } ),
		"current"
	);
	assert.equal(
		censusRefreshDecision( { gatePassed: true, openRefreshPr: true, writeExitCode: 1, writeChanged: true } ),
		"current"
	);
});

test("decision: a stale census with a refresh PR open is suppressed, not duplicated", () => {
	assert.equal(
		censusRefreshDecision( { gatePassed: false, openRefreshPr: true, writeExitCode: 0, writeChanged: false } ),
		"suppressed"
	);
});

test("decision: a write that moves the census refreshes", () => {
	assert.equal(
		censusRefreshDecision( { gatePassed: false, openRefreshPr: false, writeExitCode: 0, writeChanged: true } ),
		"refresh"
	);
});

test("decision: a broken census tool fails and never becomes a PR", () => {
	// The write failed and changed nothing: staleness was never proven.
	assert.equal(
		censusRefreshDecision( { gatePassed: false, openRefreshPr: false, writeExitCode: 1, writeChanged: false } ),
		"failed"
	);
	// The write failed after moving the file: a truncated census is not published either.
	assert.equal(
		censusRefreshDecision( { gatePassed: false, openRefreshPr: false, writeExitCode: 1, writeChanged: true } ),
		"failed"
	);
	// The gate failed but a clean write changes nothing: not staleness - a tool fault.
	assert.equal(
		censusRefreshDecision( { gatePassed: false, openRefreshPr: false, writeExitCode: 0, writeChanged: false } ),
		"failed"
	);
});

test("args: only --dry-run is known", () => {
	assert.deepEqual( parseRefreshArgs( [] ), { dryRun: false } );
	assert.deepEqual( parseRefreshArgs( [ "--dry-run" ] ), { dryRun: true } );
	assert.throws( () => parseRefreshArgs( [ "--force" ] ), /unknown census refresh option/ );
});

test("branch, title and body name the item and the one file committed", () => {
	assert.equal( refreshBranchName( new Date( "2026-11-27T05:00:00.000Z" ) ), `${REFRESH_BRANCH_PREFIX}-20261127` );
	assert.match( refreshPrTitle(), /#4337/ );
	const body = refreshPrBody( { numstat: "12\t9\tapps/client-next/tests/runtime/ui-action-census.json" } );
	assert.match( body, /#4337/ );
	assert.match( body, /12\t9/ );
	assert.equal( CENSUS_REPO_PATH, "apps/client-next/tests/runtime/ui-action-census.json" );
});
