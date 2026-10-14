/*
===========================================================================
config.test.mjs - a bad config stops the service instead of opening it
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { loadConfig } from "../server/main.mjs";

const GOOD = {
	origin: "https://console.example.test",
	roles: { staff: "100", operator: "200", approver: "300" },
	approverIds: [ "7" ],
	discord: { clientId: "42", guildId: "77", clientSecretFile: "secret" }
};

/*
================
reader

A readFile stand-in over named texts.
================
*/
function reader( config, secret = "a-real-looking-client-secret" ) {
	return async name => name === "config" ? JSON.stringify( config ) : secret;
}

test("a good config yields the callback on its own origin and the default limits", async () => {
	const config = await loadConfig( "config", reader( GOOD ) );
	assert.equal( config.app.redirectUri, "https://console.example.test/auth/callback" );
	assert.equal( config.app.clientSecret, "a-real-looking-client-secret" );
	assert.equal( config.limits.sessionMaxMs, 12 * 3600000 );
	assert.equal( config.port, 5191 );
});

test("plain http, bad ids, an empty secret and unknown limits are refused", async () => {
	const bad = [
		{ ...GOOD, origin: "http://console.example.test" },
		{ ...GOOD, origin: "https://console.example.test/sub/" },
		{ ...GOOD, roles: { ...GOOD.roles, approver: "" } },
		{ ...GOOD, approverIds: [ "seven" ] },
		{ ...GOOD, approverIds: undefined },
		{ ...GOOD, discord: { ...GOOD.discord, guildId: "x" } },
		{ ...GOOD, limits: { sessionMaxMs: -1 } },
		{ ...GOOD, limits: { forever: 1 } }
	];
	for ( const config of bad ) {
		await assert.rejects( loadConfig( "config", reader( config ) ), JSON.stringify( config ) );
	}
	await assert.rejects( loadConfig( "config", reader( GOOD, "  \n" ) ), /secret/ );
});
