/*
===========================================================================

grant-silk-security.test.mjs - an unauthenticated silk grant never leaves
the dashboard

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { allowedMutation } from "../server/security.mjs";

const PORT = 8790;
const edge = { origin: "https://ops.example.test", operator: "operator-1", secret: "s".repeat( 48 ) };

/*
================
request

A same-origin console POST of a silk grant, with the given header changes.
================
*/
function request( headers, remoteAddress = "127.0.0.1" ) {
	return {
		socket: { remoteAddress },
		headers: {
			host: "ops.example.test",
			origin: edge.origin,
			"x-sro-operator": edge.operator,
			"x-sro-console-auth": edge.secret,
			"x-sro-console": "1",
			"content-type": "application/json",
			...headers
		}
	};
}

/*
================
edge authentication
================
*/
test("a silk grant without the operator's edge credential is refused", () => {
	assert.equal( allowedMutation( request( {} ), PORT, edge ), true );
	assert.equal( allowedMutation( request( { "x-sro-console-auth": "" } ), PORT, edge ), false );
	assert.equal( allowedMutation( request( { "x-sro-console-auth": "wrong" } ), PORT, edge ), false );
	assert.equal( allowedMutation( request( { "x-sro-operator": "someone-else" } ), PORT, edge ), false );
	assert.equal( allowedMutation( request( {}, "203.0.113.9" ), PORT, edge ), false );
});

/*
================
console request shape
================
*/
test("a cross-origin or non-console silk grant is refused", () => {
	assert.equal( allowedMutation( request( { origin: "https://evil.example" } ), PORT, edge ), false );
	assert.equal( allowedMutation( request( { "x-sro-console": undefined } ), PORT, edge ), false );
	assert.equal( allowedMutation( request( { "content-type": "text/plain" } ), PORT, edge ), false );
	assert.equal( allowedMutation( request( { "sec-fetch-site": "cross-site" } ), PORT, edge ), false );
});
