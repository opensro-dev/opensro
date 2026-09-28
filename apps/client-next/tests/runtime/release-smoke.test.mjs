/*
===========================================================================

release-smoke.test.mjs - distinguish completed scene cancellation from failure.

Browser request bookkeeping can report an abort after the authenticated owner
has already consumed its response. Only successful phase evidence permits that
cancellation; unrelated errors must continue to block publication.

===========================================================================
*/

import assert from "node:assert/strict";
import test from "node:test";
import { classifyRequestFailures } from "../../tools/beta/release-smoke.mjs";

/*
================
completedTitleCancellation
================
*/
test("title teardown preserves cancellation evidence after successful login", function completedTitleCancellation() {
	const failures = [
		{ path: "/api/title/servers", reason: "net::ERR_ABORTED" },
		{ path: "/api/title/session", reason: "net::ERR_ABORTED" },
		{ path: "/api/title/login", reason: "net::ERR_ABORTED" }
	];
	const result = classifyRequestFailures( failures, { title: "PASS", login: "PASS" } );
	assert.deepEqual( result.errors, [] );
	assert.deepEqual( result.cancelled, failures );
});

/*
================
unrelatedRequestFailure
================
*/
test("a failed login and unrelated asset abort remain release failures", function unrelatedRequestFailure() {
	const result = classifyRequestFailures( [
		{ path: "/api/title/login", reason: "net::ERR_ABORTED" },
		{ path: "/api/title/session", reason: "net::ERR_ABORTED" },
		{ path: "/assets/packs/data.bin", reason: "net::ERR_ABORTED" },
		{ path: "/api/title/servers", reason: "net::ERR_TIMED_OUT" }
	], { title: "PASS" } );
	assert.equal( result.errors.length, 4 );
	assert.deepEqual( result.cancelled, [] );
});

/*
================
intentionalReloadCancellation
================
*/
test("only intentional reload aborts are admitted across all request paths", function intentionalReloadCancellation() {
	const result = classifyRequestFailures( [
		{ path: "/assets/packs/data.bin", reason: "net::ERR_ABORTED", duringReload: true },
		{ path: "/assets/packs/data.bin", reason: "net::ERR_CONNECTION_RESET", duringReload: true }
	], {} );
	assert.equal( result.cancelled.length, 1 );
	assert.equal( result.errors.length, 1 );
});
