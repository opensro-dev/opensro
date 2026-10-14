/*
===========================================================================
edge-identity.test.mjs - who a console request is, through either edge mode
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { allowedRequest, edgeIdentity, LOCAL_OPERATOR, requestOperator } from "../server/security.mjs";

const SECRET = "x".repeat( 40 ), ORIGIN = "https://console.example.test";
const LEGACY = { origin: ORIGIN, secret: SECRET, operator: "operator" };
const DISCORD = { origin: ORIGIN, secret: SECRET, identity: "discord" };

/*
================
edgeRequest
================
*/
function edgeRequest( headers ) {
	return {
		socket: { remoteAddress: "127.0.0.1" },
		headers: { host: "console.example.test", "x-sro-console-auth": SECRET, ...headers }
	};
}

test("the basic-auth edge admits only its single operator account", () => {
	assert.ok( allowedRequest( edgeRequest( { "x-sro-operator": "operator" } ), 5190, LEGACY ) );
	assert.equal( allowedRequest( edgeRequest( { "x-sro-operator": "someone" } ), 5190, LEGACY ), false );
	assert.equal( requestOperator( edgeRequest( { "x-sro-operator": "operator" } ), LEGACY ), "operator" );
});

test("the Discord edge admits a person with a console role, and audits name them", () => {
	const person = {
		"x-sro-operator": "discord:123456789",
		"x-sro-operator-name": "Ann%C3%A9",
		"x-sro-role": "operator"
	};
	assert.ok( allowedRequest( edgeRequest( person ), 5190, DISCORD ) );
	assert.deepEqual( edgeIdentity( edgeRequest( person ), DISCORD ), {
		operator: "discord:123456789",
		name: "Anné",
		role: "operator"
	} );
	assert.equal( requestOperator( edgeRequest( person ), DISCORD ), "discord:123456789" );
	assert.equal( edgeIdentity( edgeRequest( { ...person, "x-sro-role": "approver" } ), DISCORD )?.role, "approver" );
	for (
		const forged of [
			{ ...person, "x-sro-role": "viewer" },
			{ ...person, "x-sro-role": undefined },
			{ ...person, "x-sro-operator": "operator" },
			{ ...person, "x-sro-operator": "discord:12a" },
			{ ...person, "x-sro-console-auth": "wrong" }
		]
	) assert.equal( allowedRequest( edgeRequest( forged ), 5190, DISCORD ), false, JSON.stringify( forged ) );
	const badName = edgeIdentity( edgeRequest( { ...person, "x-sro-operator-name": "%E0%A4%A" } ), DISCORD );
	assert.equal( badName?.name, "discord:123456789", "an undecodable name falls back to the id" );
	assert.equal( requestOperator( { headers: {} }, null ), LOCAL_OPERATOR );
});
