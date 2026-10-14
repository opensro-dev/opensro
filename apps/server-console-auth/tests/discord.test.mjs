/*
===========================================================================
discord.test.mjs - the Discord calls send what OAuth2 needs and leak nothing
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createDiscord, DISCORD_API } from "../server/discord.mjs";

const APP = {
	clientId: "42",
	clientSecret: "secret-value-123456",
	redirectUri: "https://c.test/auth/callback",
	guildId: "77"
};

/*
================
recording
================
*/
function recording( answer ) {
	const calls = [];
	return {
		calls,
		fetcher: async ( url, init ) => {
			calls.push( { url, init } );
			return answer( url, init );
		}
	};
}

test("the code exchange is a confidential, PKCE-bound form post", async () => {
	const { calls, fetcher } = recording( () =>
		Response.json( { access_token: "a", refresh_token: "r", expires_in: 604800 } )
	);
	const tokens = await createDiscord( APP, fetcher ).exchange( "the-code", "the-verifier" );
	assert.deepEqual( tokens, { access: "a", refresh: "r", expiresIn: 604800 } );
	assert.equal( calls[0].url, DISCORD_API + "/oauth2/token" );
	assert.equal( calls[0].init.method, "POST" );
	assert.equal( calls[0].init.redirect, "error" );
	assert.equal(
		calls[0].init.headers.Authorization,
		"Basic " + Buffer.from( "42:secret-value-123456" ).toString( "base64" )
	);
	const form = new URLSearchParams( calls[0].init.body );
	assert.equal( form.get( "grant_type" ), "authorization_code" );
	assert.equal( form.get( "code_verifier" ), "the-verifier" );
	assert.equal( form.get( "redirect_uri" ), APP.redirectUri );
});

test("a non-member is null, the user is checked, and failures name no credential", async () => {
	const answers = {
		"/users/@me": () => Response.json( { id: "5", username: "five", global_name: "Five" } ),
		"/users/@me/guilds/77/member": () => new Response( "{}", { status: 404 } )
	};
	const discord = createDiscord( APP, recording( url => answers[url.slice( DISCORD_API.length )]() ).fetcher );
	assert.deepEqual( await discord.user( "tok" ), { id: "5", name: "Five" } );
	assert.equal( await discord.roles( "tok" ), null );
	answers["/users/@me/guilds/77/member"] = () => new Response( "down", { status: 503 } );
	await assert.rejects( discord.roles( "tok" ), error => error.status === 503 && !error.message.includes( "tok" ) );
	answers["/users/@me"] = () => Response.json( { id: "../5", username: "x" } );
	await assert.rejects( discord.user( "tok" ) );
	const broken = createDiscord( APP, async () => {
		throw Error( "connect failed: secret-value-123456" );
	} );
	await assert.rejects( broken.refresh( "r" ), error => error.status === 0 && !error.message.includes( "secret" ) );
	const huge = createDiscord( APP, async () => new Response( "x".repeat( 70 * 1024 ) ) );
	await assert.rejects( huge.user( "tok" ) );
});
