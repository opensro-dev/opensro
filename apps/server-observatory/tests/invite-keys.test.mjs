/*
===========================================================================
invite-keys.test.mjs - the console's calls to the website's invite-key API

The console reaches only the configured loopback site, always with its
own secret and the authenticated operator, passes the site's answers on
as they are, and refuses malformed ids before calling anything.
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createInviteKeys, siteAdminURL } from "../server/invite-keys.mjs";

const SITE = new URL( "http://127.0.0.1:3000" );
const SECRET = "console-site-secret-0123456789abcdef";
const MEMBER = "123456789012345678";

/*
================
recording

A fetcher that records each call and answers `status` with `body`.
================
*/
function recording( status, body ) {
	const calls = [];
	const fetcher = async ( url, options ) => {
		calls.push( { url: String( url ), options } );
		return new Response( typeof body === "string" ? body : JSON.stringify( body ), { status } );
	};
	return { calls, fetcher };
}

test("only a loopback http site may be configured", () => {
	assert.equal( siteAdminURL( "http://127.0.0.1:3000" ).port, "3000" );
	for ( const bad of [ "https://127.0.0.1:3000", "http://opensro.online", "http://10.0.0.5:3000" ] ) {
		assert.throws( () => siteAdminURL( bad ), /loopback/ );
	}
});

test("without a secret the page is off and nothing is called", async () => {
	const { calls, fetcher } = recording( 200, {} );
	const keys = createInviteKeys( SITE, "", fetcher );
	assert.equal( (await keys.list( "operator", "" )).status, 503 );
	assert.equal( calls.length, 0 );
});

test("creating keys sends the console secret, the operator and the request", async () => {
	const { calls, fetcher } = recording( 200, { keys: [ { code: "SRO-AAAA-BBBB-CCCC" } ], linked: [] } );
	const keys = createInviteKeys( SITE, SECRET, fetcher );
	const result = await keys.create( "operator", {
		count: "1",
		maxUses: 1,
		expiresInDays: 7,
		note: "forum tester",
		discordUserId: MEMBER,
		discordName: "caravan_fan"
	} );
	assert.equal( result.status, 200 );
	assert.equal( calls.length, 1 );
	const [call] = calls;
	assert.equal( call.url, "http://127.0.0.1:3000/internal/admin/invites" );
	assert.equal( call.options.method, "POST" );
	assert.equal( call.options.headers.Authorization, "Bearer " + SECRET );
	assert.equal( call.options.headers["X-SRO-Operator"], "operator" );
	assert.equal( call.options.redirect, "error" );
	assert.deepEqual( JSON.parse( call.options.body ), {
		count: 1,
		maxUses: 1,
		expiresInDays: 7,
		note: "forum tester",
		discordUserId: MEMBER,
		discordName: "caravan_fan"
	} );
});

test("creating refuses a malformed Discord user id before calling the site", async () => {
	const { calls, fetcher } = recording( 200, {} );
	const keys = createInviteKeys( SITE, SECRET, fetcher );
	for ( const bad of [ "12", "12345678901234567x", "1".repeat( 23 ) ] ) {
		const answer = await keys.create( "operator", { count: 1, maxUses: 1, expiresInDays: 1, discordUserId: bad } );
		assert.equal( answer.status, 400, bad );
	}
	assert.equal( calls.length, 0 );
});

test("listing filters by a Discord member and refuses a malformed id", async () => {
	const { calls, fetcher } = recording( 200, { invites: [] } );
	const keys = createInviteKeys( SITE, SECRET, fetcher );
	await keys.list( "operator", { discordUserId: MEMBER } );
	assert.equal( calls[0].url, `http://127.0.0.1:3000/internal/admin/invites?discordUserId=${MEMBER}` );
	assert.equal( (await keys.list( "operator", { discordUserId: "12&x=1" } )).status, 400 );
	assert.equal( calls.length, 1 );
});

test("listing sends every filter to the site and refuses a malformed one", async () => {
	const { calls, fetcher } = recording( 200, { invites: [], counts: { total: 0 } } );
	const keys = createInviteKeys( SITE, SECRET, fetcher );
	await keys.list( "operator", {
		status: "unused",
		source: "reddit",
		createdFrom: "2026-10-01T00:00:00.000Z",
		createdTo: "2026-11-01T00:00:00.000Z",
		expiresFrom: "2026-11-01T00:00:00.000Z",
		expiresTo: "2026-12-01T00:00:00.000Z"
	} );
	assert.equal(
		calls[0].url,
		"http://127.0.0.1:3000/internal/admin/invites?status=unused&source=reddit" +
			"&createdFrom=2026-10-01T00%3A00%3A00.000Z&createdTo=2026-11-01T00%3A00%3A00.000Z" +
			"&expiresFrom=2026-11-01T00%3A00%3A00.000Z&expiresTo=2026-12-01T00%3A00%3A00.000Z"
	);
	for (
		const filters of [
			{ status: "stale" },
			{ createdFrom: "next week" },
			{ source: "x".repeat( 201 ) }
		]
	) {
		assert.equal( (await keys.list( "operator", filters )).status, 400, JSON.stringify( filters ) );
	}
	assert.equal( calls.length, 1 );
});

test("revealing asks the site for one key's code and refuses a malformed id", async () => {
	const { calls, fetcher } = recording( 200, { code: "SRO-AAAA-BBBB-CCCC" } );
	const keys = createInviteKeys( SITE, SECRET, fetcher );
	const id = "0b0e1c4e-6f2d-4d6b-9a3f-2b7c9d1e5a10";
	const answer = await keys.reveal( "operator", id );
	assert.equal( answer.status, 200 );
	assert.deepEqual( answer.body, { code: "SRO-AAAA-BBBB-CCCC" } );
	assert.equal( calls[0].url, `http://127.0.0.1:3000/internal/admin/invites/${id}/reveal` );
	assert.equal( calls[0].options.method, "POST" );
	assert.equal( calls[0].options.headers.Authorization, "Bearer " + SECRET );
	for ( const bad of [ "../discord-invite", id + "/../../x", "" ] ) {
		assert.equal( (await keys.reveal( "operator", bad )).status, 400 );
	}
	assert.equal( calls.length, 1 );
});

test("revoke takes only a key id, never a path", async () => {
	const { calls, fetcher } = recording( 200, { revoked: true } );
	const keys = createInviteKeys( SITE, SECRET, fetcher );
	const id = "0b0e1c4e-6f2d-4d6b-9a3f-2b7c9d1e5a10";
	assert.equal( (await keys.revoke( "operator", id )).status, 200 );
	assert.equal( calls[0].url, `http://127.0.0.1:3000/internal/admin/invites/${id}/revoke` );
	for ( const bad of [ "../discord-invite", id + "/../../x", "" ] ) {
		assert.equal( (await keys.revoke( "operator", bad )).status, 400 );
	}
	assert.equal( calls.length, 1 );
});

test("the site's refusals reach the operator in plain words", async () => {
	const refused = createInviteKeys( SITE, SECRET, recording( 404, "Not Found" ).fetcher );
	const answer = await refused.list( "operator", "" );
	assert.equal( answer.status, 503 );
	assert.match( answer.body.error, /CONSOLE_ADMIN_TOKEN_FILE/ );

	const invalid = createInviteKeys( SITE, SECRET, recording( 400, { error: "count: too small" } ).fetcher );
	assert.deepEqual( await invalid.create( "operator", { count: 0 } ), {
		status: 400,
		body: { error: "count: too small" }
	} );

	const garbled = createInviteKeys( SITE, SECRET, recording( 200, "<html>" ).fetcher );
	assert.equal( (await garbled.list( "operator", "" )).status, 502 );
});
