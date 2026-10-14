/*
===========================================================================

auth.mjs - console staff sign-in: Discord login, sessions and roles

The console (apps/server-observatory) trusts only who Caddy says the
operator is. Caddy asks this owner on every console request
(forward_auth to /auth/verify) and copies the identity headers it answers
with: X-SRO-Operator (discord:<user id>, what audits store),
X-SRO-Operator-Name (display only) and X-SRO-Role (viewer, operator or
approver). Caddy deletes client copies of those headers first and adds
the console's edge secret itself; this owner never knows that secret.

Sessions live in memory: a restart signs everyone out, and no Discord
token is ever written to disk. A session ends at its maximum age, after
an idle spell, or when a role re-check fails for any reason (fail
closed: Discord being down signs staff out rather than keeping a stale
role).

Approver needs the Approver role AND a user id on the owner's allowlist:
anyone with Discord's Manage Roles could otherwise grant it to
themselves.

===========================================================================
*/
import { createHash, randomBytes, timingSafeEqual } from "node:crypto";
import { isIP } from "node:net";
import { authorizeURL, DiscordError } from "./discord.mjs";

export const SESSION_COOKIE = "__Host-sro_console";
export const LOGIN_COOKIE = "__Host-sro_login";
const SECRET_BYTES = 32;
const MAX_NEXT_LENGTH = 512;
// Pending sign-ins are a shared pool: one address keeps at most a few, and
// a full pool gives way oldest first, so a flood delays sign-ins it cannot
// block (a refusal at the cap would let ~500 addresses lock staff out).
const MAX_PENDING_LOGINS = 10000;
const PENDING_LOGINS_PER_ADDRESS = 3;
const MAX_RATE_ENTRIES = 10000;
const RATE_WINDOW_MS = 60000;
// Refresh an access token this long before Discord says it expires.
const ACCESS_MARGIN_MS = 60000;

/*
================
AuthConfig

What the owner's root-owned config sets (main.mjs reads and checks it).
================
*/
export const DEFAULT_LIMITS = Object.freeze( {
	sessionMaxMs: 12 * 3600000,
	idleMs: 2 * 3600000,
	recheckMs: 10 * 60000,
	loginTtlMs: 10 * 60000,
	sessionsPerUser: 5,
	attemptsPerMinute: 20
} );

/*
================
roleFor

The console role of a guild member: none without the Staff role; the
highest of operator and approver they hold, approver only when their id
is on the allowlist.
================
*/
export function roleFor( config, userId, roleIds ) {
	if ( !roleIds || !roleIds.includes( config.roles.staff ) ) return null;
	if ( roleIds.includes( config.roles.approver ) && config.approverIds.includes( userId ) ) return "approver";
	if ( roleIds.includes( config.roles.operator ) ) return "operator";
	return "viewer";
}

/*
================
safeNext

A same-origin path to return to after sign-in, or "/".
================
*/
export function safeNext( value ) {
	if (
		typeof value !== "string" || value.length > MAX_NEXT_LENGTH || !value.startsWith( "/" ) ||
		value.startsWith( "//" ) || value.startsWith( "/auth/" ) || /[\\\u0000-\u001f\u007f]/.test( value )
	) return "/";
	return value;
}

/*
================
clientAddress

The browser's address as Caddy reports it. Every socket peer is Caddy on
loopback; Caddy overwrites X-Forwarded-For with the client it saw (any
client copy is deleted before forward_auth). Null when absent or malformed.
================
*/
export function clientAddress( headers ) {
	const first = String( headers["x-forwarded-for"] ?? "" ).split( "," )[0].trim();
	return isIP( first ) ? first : null;
}

/*
================
cookies
================
*/
function cookies( header ) {
	const out = new Map();
	for ( const part of String( header ?? "" ).split( ";" ) ) {
		const at = part.indexOf( "=" );
		if ( at > 0 ) out.set( part.slice( 0, at ).trim(), part.slice( at + 1 ).trim() );
	}
	return out;
}

/*
================
digest

Sessions are keyed by a hash of their cookie, so a memory dump of the
table does not hand out live cookies.
================
*/
function digest( value ) {
	return createHash( "sha256" ).update( value ).digest( "hex" );
}

/*
================
sameSecret
================
*/
function sameSecret( left, right ) {
	const a = Buffer.from( String( left ) ), b = Buffer.from( String( right ) );
	return a.length === b.length && timingSafeEqual( a, b );
}

/*
================
cookie
================
*/
function cookie( name, value, maxAgeSeconds ) {
	return `${name}=${value}; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=${maxAgeSeconds}`;
}

/*
================
page

A minimal HTML answer for the browser; text is escaped.
================
*/
function page( status, title, text, extraHeaders = {} ) {
	const escape = value => String( value ).replace( /[&<>"']/g, c => `&#${c.charCodeAt( 0 )};` );
	return {
		status,
		headers: {
			"Content-Type": "text/html; charset=utf-8",
			"Cache-Control": "no-store",
			"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'",
			...extraHeaders
		},
		body:
			`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${
				escape( title )
			}</title></head><body style="font:16px/1.5 Georgia,serif;max-width:32rem;margin:15vh auto;padding:0 16px;background:#f4e8cb;color:#2e1d10"><h1 style="color:#5c170d;font-size:24px">${
				escape( title )
			}</h1><p>${
				escape( text )
			}</p><p><a href="/auth/login" style="color:#5c170d">Sign in with Discord</a></p></body></html>`
	};
}

/*
================
redirect
================
*/
function redirect( location, extraHeaders = {} ) {
	return { status: 302, headers: { Location: location, "Cache-Control": "no-store", ...extraHeaders }, body: "" };
}

/*
================
createAuth

config: { origin, app (authorizeURL's { clientId, redirectUri }), roles:
{ staff, operator, approver }, approverIds, limits }. discord is
createDiscord's owner; clock, random and log are injected for tests.
================
*/
export function createAuth( deps ) {
	const { config, discord } = deps;
	const clock = deps.clock ?? Date.now, random = deps.random ?? randomBytes, log = deps.log ?? (() => {});
	const limits = { ...DEFAULT_LIMITS, ...config.limits };
	const sessions = new Map(), logins = new Map(), attempts = new Map();

	/*
	================
	secret
	================
	*/
	const secret = () => random( SECRET_BYTES ).toString( "base64url" );

	/*
	================
	limited

	Whether address has used its sign-in attempts for this minute.
	================
	*/
	function limited( address ) {
		const now = clock();
		if ( attempts.size > MAX_RATE_ENTRIES ) {
			for ( const [key, row] of attempts ) if ( now - row.start >= RATE_WINDOW_MS ) attempts.delete( key );
		}
		let row = attempts.get( address );
		if ( !row || now - row.start >= RATE_WINDOW_MS ) {
			row = { start: now, count: 0 };
			attempts.set( address, row );
		}
		row.count++;
		return row.count > limits.attemptsPerMinute;
	}

	/*
	================
	end
	================
	*/
	function end( key, session, reason ) {
		sessions.delete( key );
		log( { event: "session_end", reason, user: session.userId } );
	}

	/*
	================
	recheck

	The session's role, asked of Discord again. Any failure ends the
	session; a change is logged. One re-check runs per session at a time,
	so concurrent requests never spend the same refresh token twice.
	================
	*/
	function recheck( key, session ) {
		session.pending ??= (async () => {
			try {
				if ( clock() >= session.accessExpires - ACCESS_MARGIN_MS ) {
					const tokens = await discord.refresh( session.refresh );
					session.access = tokens.access;
					session.refresh = tokens.refresh;
					session.accessExpires = clock() + tokens.expiresIn * 1000;
				}
				const role = roleFor( config, session.userId, await discord.roles( session.access ) );
				if ( role !== session.role ) {
					log( { event: "role_change", user: session.userId, from: session.role, to: role } );
				}
				if ( !role ) {
					end( key, session, "role_removed" );
					return false;
				}
				session.role = role;
				session.checked = clock();
				return true;
			} catch ( error ) {
				end(
					key,
					session,
					error instanceof DiscordError ? `recheck_failed_${error.status}` : "recheck_failed"
				);
				return false;
			} finally {
				session.pending = null;
			}
		})();
		return session.pending;
	}

	/*
	================
	refuse

	A console request without a usable session: a page navigation is sent
	to sign in and back, anything else is told to sign in.
	================
	*/
	function refuse( headers ) {
		const navigation = headers["sec-fetch-mode"] === "navigate" ||
			String( headers.accept ?? "" ).includes( "text/html" );
		if ( navigation ) {
			const next = safeNext( headers["x-forwarded-uri"] );
			return redirect( "/auth/login?" + new URLSearchParams( { next } ) );
		}
		return {
			status: 401,
			headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
			body: JSON.stringify( { error: "Sign in to the console again" } )
		};
	}

	/*
	================
	verify

	Caddy's forward_auth: 200 with the identity headers, or a refusal.
	================
	*/
	async function verify( headers ) {
		const id = cookies( headers.cookie ).get( SESSION_COOKIE );
		const key = id ? digest( id ) : "", session = key ? sessions.get( key ) : undefined;
		if ( !session ) return refuse( headers );
		const now = clock();
		if ( now - session.created >= limits.sessionMaxMs ) {
			end( key, session, "expired" );
			return refuse( headers );
		}
		if ( now - session.seen >= limits.idleMs ) {
			end( key, session, "idle" );
			return refuse( headers );
		}
		if ( now - session.checked >= limits.recheckMs || session.pending ) {
			if ( !await recheck( key, session ) ) return refuse( headers );
		}
		session.seen = clock();
		return {
			status: 200,
			headers: {
				"Cache-Control": "no-store",
				"X-SRO-Operator": "discord:" + session.userId,
				"X-SRO-Operator-Name": encodeURIComponent( session.name ),
				"X-SRO-Role": session.role
			},
			body: ""
		};
	}

	/*
	================
	login
	================
	*/
	function login( headers, query ) {
		const address = clientAddress( headers );
		if ( !address ) {
			return page( 400, "Sign-in refused", "The request did not come through the console's front door." );
		}
		if ( limited( address ) ) {
			log( { event: "rate_limited", ip: address } );
			return page( 429, "Too many attempts", "Wait a minute, then sign in again." );
		}
		const now = clock();
		// Maps keep insertion order, so the first rows are the oldest.
		const own = [];
		for ( const [key, row] of logins ) {
			if ( row.expires <= now ) logins.delete( key );
			else if ( row.address === address ) own.push( key );
		}
		while ( own.length >= PENDING_LOGINS_PER_ADDRESS ) logins.delete( own.shift() );
		if ( logins.size >= MAX_PENDING_LOGINS ) logins.delete( logins.keys().next().value );
		const loginId = secret(), state = secret(), verifier = secret();
		logins.set( digest( loginId ), {
			address,
			state,
			verifier,
			next: safeNext( query.get( "next" ) ),
			expires: now + limits.loginTtlMs
		} );
		const challenge = createHash( "sha256" ).update( verifier ).digest( "base64url" );
		return redirect( authorizeURL( config.app, state, challenge ), {
			"Set-Cookie": cookie( LOGIN_COOKIE, loginId, Math.floor( limits.loginTtlMs / 1000 ) )
		} );
	}

	/*
	================
	callback
	================
	*/
	async function callback( headers, query ) {
		const address = clientAddress( headers );
		if ( !address ) {
			return page( 400, "Sign-in refused", "The request did not come through the console's front door." );
		}
		if ( limited( address ) ) {
			log( { event: "rate_limited", ip: address } );
			return page( 429, "Too many attempts", "Wait a minute, then sign in again." );
		}
		const clearLogin = { "Set-Cookie": cookie( LOGIN_COOKIE, "", 0 ) };
		const loginId = cookies( headers.cookie ).get( LOGIN_COOKIE );
		const loginKey = loginId ? digest( loginId ) : "", pending = loginKey ? logins.get( loginKey ) : undefined;
		if ( loginKey ) logins.delete( loginKey );
		if ( !pending || pending.expires <= clock() ) {
			return page( 400, "Sign-in expired", "That sign-in took too long or was already used.", clearLogin );
		}
		if ( query.get( "error" ) ) return page( 403, "Sign-in cancelled", "Discord did not sign you in.", clearLogin );
		if ( !sameSecret( query.get( "state" ) ?? "", pending.state ) || !query.get( "code" ) ) {
			log( { event: "state_mismatch", ip: address } );
			return page( 400, "Sign-in refused", "That answer does not belong to this sign-in.", clearLogin );
		}
		let tokens, user, roleIds;
		try {
			tokens = await discord.exchange( query.get( "code" ), pending.verifier );
			user = await discord.user( tokens.access );
			roleIds = await discord.roles( tokens.access );
		} catch {
			return page( 502, "Discord did not answer", "Try signing in again in a moment.", clearLogin );
		}
		const role = roleFor( config, user.id, roleIds );
		if ( !role ) {
			log( { event: "login_denied", user: user.id, ip: address, member: roleIds !== null } );
			return page( 403, "Not console staff", "This Discord account has no console role.", clearLogin );
		}
		const now = clock();
		const own = [ ...sessions ].filter( ( [, row] ) => row.userId === user.id ).sort( ( a, b ) =>
			a[1].created - b[1].created
		);
		while ( own.length >= limits.sessionsPerUser ) {
			const [key, row] = own.shift();
			end( key, row, "replaced" );
		}
		const id = secret();
		sessions.set( digest( id ), {
			userId: user.id,
			name: user.name,
			role,
			created: now,
			seen: now,
			checked: now,
			access: tokens.access,
			refresh: tokens.refresh,
			accessExpires: now + tokens.expiresIn * 1000,
			pending: null
		} );
		log( { event: "login", user: user.id, role, ip: address } );
		return {
			status: 302,
			headers: {
				Location: pending.next,
				"Cache-Control": "no-store",
				"Set-Cookie": [
					cookie( SESSION_COOKIE, id, Math.floor( limits.sessionMaxMs / 1000 ) ),
					cookie( LOGIN_COOKIE, "", 0 )
				]
			},
			body: ""
		};
	}

	/*
	================
	logout

	A same-origin POST ends the session.
	================
	*/
	function logout( headers ) {
		if ( headers.origin !== config.origin ) {
			return page( 403, "Sign-out refused", "Sign out from the console itself." );
		}
		const id = cookies( headers.cookie ).get( SESSION_COOKIE );
		const key = id ? digest( id ) : "", session = key ? sessions.get( key ) : undefined;
		if ( session ) end( key, session, "signed_out" );
		return {
			status: 303,
			headers: {
				Location: "/auth/signed-out",
				"Cache-Control": "no-store",
				"Set-Cookie": cookie( SESSION_COOKIE, "", 0 )
			},
			body: ""
		};
	}

	return {
		/*
		================
		handle

		request is { method, path, query (URLSearchParams), headers (lower-case
		names) }; the answer is { status, headers, body }.
		================
		*/
		async handle( request ) {
			const { method, path, query, headers } = request;
			if ( path === "/auth/verify" && method === "GET" ) return verify( headers );
			if ( path === "/auth/login" && method === "GET" ) return login( headers, query );
			if ( path === "/auth/callback" && method === "GET" ) return callback( headers, query );
			if ( path === "/auth/logout" && method === "POST" ) return logout( headers );
			if ( path === "/auth/signed-out" && method === "GET" ) {
				return page( 200, "Signed out", "You have signed out of the console." );
			}
			return { status: 404, headers: { "Cache-Control": "no-store" }, body: "" };
		},
		/*
		================
		stats

		Counts for the service log; no identities.
		================
		*/
		stats: () => ({ sessions: sessions.size, pendingLogins: logins.size })
	};
}
