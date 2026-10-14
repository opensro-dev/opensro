/*
===========================================================================
auth.test.mjs - sign-in, sessions and roles against a fake Discord
===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { DiscordError } from "../server/discord.mjs";
import { clientAddress, createAuth, LOGIN_COOKIE, roleFor, safeNext, SESSION_COOKIE } from "../server/auth.mjs";

const ORIGIN = "https://console.example.test";
const ROLES = { staff: "100", operator: "200", approver: "300" };
const MINUTE = 60000;

/*
================
world

An auth owner over a fake Discord whose members are { id: roleIds|null },
with a hand-moved clock and a captured log.
================
*/
function world( members = {}, overrides = {} ) {
	let now = 1_700_000_000_000, serial = 0;
	const calls = { exchange: 0, refresh: 0, roles: 0 }, rows = [];
	const discord = {
		async exchange( code, verifier ) {
			calls.exchange++;
			if ( !verifier ) throw new DiscordError( "code exchange", 400 );
			return { access: "access-" + code, refresh: "refresh-" + code, expiresIn: 604800 };
		},
		async refresh( token ) {
			calls.refresh++;
			return { access: token.replace( "refresh", "access2" ), refresh: token + "+", expiresIn: 604800 };
		},
		async user( access ) {
			const id = access.split( "-" ).at( -1 );
			return { id, name: "user " + id };
		},
		async roles( access ) {
			calls.roles++;
			const id = access.split( "-" ).at( -1 );
			if ( overrides.rolesFail ) throw new DiscordError( "member", 503 );
			return members[id] ?? null;
		}
	};
	const auth = createAuth( {
		config: {
			origin: ORIGIN,
			app: { clientId: "42", redirectUri: ORIGIN + "/auth/callback" },
			roles: ROLES,
			approverIds: [ "7" ],
			limits: overrides.limits ?? {}
		},
		discord,
		clock: () => now,
		random: n => Buffer.alloc( n, ++serial ),
		log: row => rows.push( row )
	} );
	return {
		auth,
		calls,
		rows,
		overrides,
		advance: ms => {
			now += ms;
		}
	};
}

/*
================
request
================
*/
function request( method, target, headers = {} ) {
	const url = new URL( target, ORIGIN );
	return {
		method,
		path: url.pathname,
		query: url.searchParams,
		headers: { "x-forwarded-for": "203.0.113.9", ...headers }
	};
}

/*
================
cookieValue

The value a Set-Cookie answer gives name.
================
*/
function cookieValue( answer, name ) {
	const set = [ answer.headers["Set-Cookie"] ].flat();
	const line = set.find( row => row?.startsWith( name + "=" ) );
	return line ? line.slice( name.length + 1, line.indexOf( ";" ) ) : undefined;
}

/*
================
signIn

A whole sign-in as Discord user id; the session cookie, or the refusal.
================
*/
async function signIn( w, id, next = "/economy.html" ) {
	const login = await w.auth.handle( request( "GET", "/auth/login?next=" + encodeURIComponent( next ) ) );
	const state = new URL( login.headers.Location ).searchParams.get( "state" );
	const loginCookie = cookieValue( login, LOGIN_COOKIE );
	const answer = await w.auth.handle(
		request( "GET", `/auth/callback?code=${id}&state=${state}`, { cookie: `${LOGIN_COOKIE}=${loginCookie}` } )
	);
	return { answer, session: cookieValue( answer, SESSION_COOKIE ) };
}

/*
================
verify
================
*/
function verify( w, session, headers = {} ) {
	return w.auth.handle( request( "GET", "/auth/verify", { cookie: `${SESSION_COOKIE}=${session}`, ...headers } ) );
}

test("a staff member signs in with Discord and Caddy is told who they are", async () => {
	const w = world( { 5: [ "100", "200" ] } );
	const login = await w.auth.handle( request( "GET", "/auth/login?next=/economy.html" ) );
	assert.equal( login.status, 302 );
	const authorize = new URL( login.headers.Location );
	assert.equal( authorize.origin + authorize.pathname, "https://discord.com/oauth2/authorize" );
	assert.equal( authorize.searchParams.get( "scope" ), "identify guilds.members.read" );
	assert.equal( authorize.searchParams.get( "code_challenge_method" ), "S256" );
	assert.equal( authorize.searchParams.get( "redirect_uri" ), ORIGIN + "/auth/callback" );
	assert.match(
		login.headers["Set-Cookie"],
		/^__Host-sro_login=[^;]+; Path=\/; HttpOnly; Secure; SameSite=Lax; Max-Age=600$/
	);
	const { answer, session } = await signIn( w, "5" );
	assert.equal( answer.status, 302 );
	assert.equal( answer.headers.Location, "/economy.html" );
	assert.match(
		answer.headers["Set-Cookie"][0],
		/^__Host-sro_console=[^;]+; Path=\/; HttpOnly; Secure; SameSite=Lax; Max-Age=43200$/
	);
	const ok = await verify( w, session );
	assert.equal( ok.status, 200 );
	assert.equal( ok.headers["X-SRO-Operator"], "discord:5" );
	assert.equal( ok.headers["X-SRO-Role"], "operator" );
	assert.equal( decodeURIComponent( ok.headers["X-SRO-Operator-Name"] ), "user 5" );
});

test("roles: Staff is required, Approver also needs the owner's allowlist", () => {
	const config = { roles: ROLES, approverIds: [ "7" ] };
	assert.equal( roleFor( config, "5", null ), null, "not a guild member" );
	assert.equal( roleFor( config, "5", [ "200", "300" ] ), null, "no Staff role" );
	assert.equal( roleFor( config, "5", [ "100" ] ), "viewer" );
	assert.equal( roleFor( config, "5", [ "100", "200" ] ), "operator" );
	assert.equal( roleFor( config, "5", [ "100", "300" ] ), "viewer", "a self-granted Approver role is not enough" );
	assert.equal( roleFor( config, "5", [ "100", "200", "300" ] ), "operator" );
	assert.equal( roleFor( config, "7", [ "100", "300" ] ), "approver" );
	assert.equal( roleFor( config, "7", [ "100" ] ), "viewer", "the allowlist alone is not enough" );
});

test("non-staff, cancelled, forged, replayed and expired sign-ins get no session", async () => {
	const w = world( { 5: [ "200" ], 6: [ "100" ] } );
	const outsider = await signIn( w, "9" );
	assert.equal( outsider.answer.status, 403 );
	assert.equal( outsider.session, undefined );
	assert.equal( (await signIn( w, "5" )).answer.status, 403, "Operator without Staff" );
	const login = await w.auth.handle( request( "GET", "/auth/login" ) );
	const loginCookie = { cookie: `${LOGIN_COOKIE}=${cookieValue( login, LOGIN_COOKIE )}` };
	const state = new URL( login.headers.Location ).searchParams.get( "state" );
	const forged = await w.auth.handle( request( "GET", `/auth/callback?code=6&state=forged`, loginCookie ) );
	assert.equal( forged.status, 400 );
	const replay = await w.auth.handle( request( "GET", `/auth/callback?code=6&state=${state}`, loginCookie ) );
	assert.equal( replay.status, 400, "a login is spent by its first answer, even a refused one" );
	const late = await w.auth.handle( request( "GET", "/auth/login" ) );
	w.advance( 11 * MINUTE );
	const lateAnswer = await w.auth.handle(
		request( "GET", `/auth/callback?code=6&state=${new URL( late.headers.Location ).searchParams.get( "state" )}`, {
			cookie: `${LOGIN_COOKIE}=${cookieValue( late, LOGIN_COOKIE )}`
		} )
	);
	assert.equal( lateAnswer.status, 400 );
	const cancelled = await w.auth.handle( request( "GET", "/auth/login" ) );
	const cancelAnswer = await w.auth.handle(
		request( "GET", "/auth/callback?error=access_denied", {
			cookie: `${LOGIN_COOKIE}=${cookieValue( cancelled, LOGIN_COOKIE )}`
		} )
	);
	assert.equal( cancelAnswer.status, 403 );
	assert.equal( w.auth.stats().sessions, 0 );
	assert.ok( w.rows.some( row => row.event === "state_mismatch" ) );
});

test("a removed role or any failed re-check ends the session: fail closed", async () => {
	const members = { 5: [ "100", "200" ] };
	const w = world( members );
	const { session } = await signIn( w, "5" );
	w.advance( 9 * MINUTE );
	assert.equal( (await verify( w, session )).status, 200, "inside the re-check interval" );
	members[5] = [ "100" ];
	w.advance( 2 * MINUTE );
	const demoted = await verify( w, session );
	assert.equal( demoted.headers["X-SRO-Role"], "viewer" );
	assert.ok( w.rows.some( row => row.event === "role_change" && row.from === "operator" && row.to === "viewer" ) );
	members[5] = null;
	w.advance( 11 * MINUTE );
	assert.equal( (await verify( w, session )).status, 401, "left the guild" );
	const down = world( { 5: [ "100" ] } );
	const second = await signIn( down, "5" );
	down.overrides.rolesFail = true;
	down.advance( 11 * MINUTE );
	assert.equal( (await verify( down, second.session )).status, 401, "Discord down signs staff out" );
	assert.ok( down.rows.some( row => row.event === "session_end" && row.reason === "recheck_failed_503" ) );
});

test("concurrent requests share one re-check, so a refresh token is never spent twice", async () => {
	const w = world( { 5: [ "100" ] }, { limits: { recheckMs: MINUTE } } );
	const { session } = await signIn( w, "5" );
	w.advance( 8 * 24 * 60 * MINUTE );
	const answers = await Promise.all( [ verify( w, session ), verify( w, session ), verify( w, session ) ] );
	assert.equal( w.calls.refresh, 0, "past the maximum age nothing is refreshed" );
	assert.ok( answers.every( answer => answer.status === 401 ) );
	const fresh = world( { 5: [ "100" ] }, {
		limits: { recheckMs: MINUTE, sessionMaxMs: 9 * 24 * 60 * MINUTE, idleMs: 9 * 24 * 60 * MINUTE }
	} );
	const second = await signIn( fresh, "5" );
	fresh.advance( 8 * 24 * 60 * MINUTE );
	const again = await Promise.all( [ verify( fresh, second.session ), verify( fresh, second.session ) ] );
	assert.ok( again.every( answer => answer.status === 200 ) );
	assert.equal( fresh.calls.refresh, 1 );
});

test("sessions end at their maximum age and after an idle spell", async () => {
	const w = world( { 5: [ "100" ] }, { limits: { recheckMs: 24 * 60 * MINUTE } } );
	const idle = await signIn( w, "5" );
	w.advance( 121 * MINUTE );
	assert.equal( (await verify( w, idle.session )).status, 401 );
	const busy = await signIn( w, "5" );
	for ( let hour = 0; hour < 12; hour++ ) {
		w.advance( 60 * MINUTE );
		const expected = hour < 11 ? 200 : 401;
		assert.equal( (await verify( w, busy.session )).status, expected, `hour ${hour + 1}` );
	}
});

test("a page without a session is sent to sign in and back; a script call is told 401", async () => {
	const w = world();
	const page = await w.auth.handle(
		request( "GET", "/auth/verify", { "sec-fetch-mode": "navigate", "x-forwarded-uri": "/history.html?x=1" } )
	);
	assert.equal( page.status, 302 );
	assert.equal( page.headers.Location, "/auth/login?next=%2Fhistory.html%3Fx%3D1" );
	const api = await w.auth.handle( request( "GET", "/auth/verify", { accept: "application/json" } ) );
	assert.equal( api.status, 401 );
	const forged = await verify( w, "made-up" );
	assert.equal( forged.status, 401 );
});

test("only same-origin paths are returned to after sign-in", () => {
	for (
		const next of [
			"//evil.test",
			"https://evil.test/",
			"/\\evil.test",
			"/auth/login",
			"economy",
			"/a\nb",
			undefined
		]
	) {
		assert.equal( safeNext( next ), "/", String( next ) );
	}
	assert.equal( safeNext( "/economy.html#flags" ), "/economy.html#flags" );
});

test("sign-out needs the console's own origin and ends the session", async () => {
	const w = world( { 5: [ "100" ] } );
	const { session } = await signIn( w, "5" );
	const cookie = `${SESSION_COOKIE}=${session}`;
	assert.equal(
		(await w.auth.handle( request( "POST", "/auth/logout", { cookie, origin: "https://evil.test" } ) )).status,
		403
	);
	assert.equal( (await verify( w, session )).status, 200 );
	const out = await w.auth.handle( request( "POST", "/auth/logout", { cookie, origin: ORIGIN } ) );
	assert.equal( out.status, 303 );
	assert.equal( (await verify( w, session )).status, 401 );
	assert.equal( (await w.auth.handle( request( "GET", "/auth/logout", { cookie, origin: ORIGIN } ) )).status, 404 );
});

test("sign-in attempts are limited per client address, which must come from Caddy", async () => {
	const w = world();
	for ( let i = 0; i < 20; i++ ) assert.equal( (await w.auth.handle( request( "GET", "/auth/login" ) )).status, 302 );
	assert.equal( (await w.auth.handle( request( "GET", "/auth/login" ) )).status, 429 );
	const other = await w.auth.handle( request( "GET", "/auth/login", { "x-forwarded-for": "198.51.100.4" } ) );
	assert.equal( other.status, 302, "another address has its own budget" );
	w.advance( MINUTE );
	assert.equal( (await w.auth.handle( request( "GET", "/auth/login" ) )).status, 302 );
	assert.equal( (await w.auth.handle( request( "GET", "/auth/login", { "x-forwarded-for": "" } ) )).status, 400 );
	assert.equal( clientAddress( { "x-forwarded-for": "not-an-ip" } ), null );
	assert.equal( clientAddress( { "x-forwarded-for": "2001:db8::1, 10.0.0.1" } ), "2001:db8::1" );
});

test("a user holds at most five sessions; the oldest gives way", async () => {
	const w = world( { 5: [ "100" ] } );
	const sessions = [];
	for ( let i = 0; i < 6; i++ ) {
		sessions.push( (await signIn( w, "5" )).session );
		w.advance( 1000 );
	}
	assert.equal( (await verify( w, sessions[0] )).status, 401 );
	for ( const session of sessions.slice( 1 ) ) assert.equal( (await verify( w, session )).status, 200 );
});

test("the service log never carries a token, code, state or cookie", async () => {
	const members = { 5: [ "100", "200" ] };
	const w = world( members );
	const { session } = await signIn( w, "5" );
	members[5] = [ "100" ];
	w.advance( 11 * MINUTE );
	await verify( w, session );
	await w.auth.handle(
		request( "POST", "/auth/logout", { cookie: `${SESSION_COOKIE}=${session}`, origin: ORIGIN } )
	);
	await signIn( w, "9" );
	const text = JSON.stringify( w.rows );
	assert.ok( w.rows.length >= 4 );
	for ( const secret of [ session, "access-5", "refresh-5", "code" ] ) assert.ok( !text.includes( secret ), secret );
});
