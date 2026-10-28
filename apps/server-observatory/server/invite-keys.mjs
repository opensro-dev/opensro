/*
===========================================================================
invite-keys.mjs - the website's invite keys, managed from the console

Invite keys live in the website's database and its signup redeems them;
the console manages them through the site's loopback admin API
(opensro-web /internal/admin/invites). The site trusts this console only
by its own secret (config siteAdmin.tokenFile, never the Discord bot's)
and records the operator Caddy authenticated in every audit entry. The
browser never sees the secret: it talks to /api/invites here.
===========================================================================
*/
import { readResponseText } from "./player-operations.mjs";

const MAX_RESPONSE_BYTES = 2 * 1024 * 1024;
const TIMEOUT_MS = 15000;
const DISCORD_SNOWFLAKE = /^[0-9]{15,22}$/;
const KEY_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/*
================
siteAdminURL

The site's admin API base from the console config: plain HTTP on this
host only, as every other upstream the console calls.
================
*/
export function siteAdminURL( value ) {
	const url = new URL( value );
	if ( url.protocol !== "http:" || ![ "127.0.0.1", "[::1]", "localhost" ].includes( url.hostname ) ) {
		throw Error( "siteAdmin.url must be a loopback http URL" );
	}
	return url;
}

const STATUS_FILTERS = [ "unused", "partial", "used", "revoked", "expired" ];
const DATE_FILTERS = [ "createdFrom", "createdTo", "expiresFrom", "expiresTo" ];

/*
================
inviteListQuery

The list's filters as the query string the site understands, validated
here so a malformed filter is refused before anything is called. Dates
travel as the instants the page computed. Returns { text } with the
leading "?", or { error } in plain words.
================
*/
export function inviteListQuery( filters ) {
	const params = new URLSearchParams();
	const member = String( filters.discordUserId ?? "" ).trim();
	if ( member ) {
		if ( !DISCORD_SNOWFLAKE.test( member ) ) return { error: "A Discord user ID is 15 to 22 digits" };
		params.set( "discordUserId", member );
	}
	const status = String( filters.status ?? "" ).trim();
	if ( status ) {
		if ( !STATUS_FILTERS.includes( status ) ) {
			return { error: `Status must be one of ${STATUS_FILTERS.join( ", " )}` };
		}
		params.set( "status", status );
	}
	const source = String( filters.source ?? "" ).trim();
	if ( source ) {
		if ( source.length > 200 ) return { error: "Source is too long" };
		params.set( "source", source );
	}
	for ( const name of DATE_FILTERS ) {
		const value = String( filters[name] ?? "" ).trim();
		if ( !value ) continue;
		if ( Number.isNaN( Date.parse( value ) ) ) return { error: `${name} is not a date` };
		params.set( name, value );
	}
	const text = params.toString();
	return { text: text ? "?" + text : "" };
}

/*
================
createInviteKeys
================
*/
export function createInviteKeys( base, token, fetcher = fetch ) {
	/*
	================
	call

	One request to the site; its JSON answer and status are passed on.
	================
	*/
	async function call( path, operator, body ) {
		if ( !token || !base ) {
			return { status: 503, body: { error: "Invite keys are not configured on this console" } };
		}
		const response = await fetcher( new URL( path, base ), {
			method: body ? "POST" : "GET",
			headers: {
				Authorization: "Bearer " + token,
				"X-SRO-Operator": operator,
				"Content-Type": "application/json"
			},
			...(body ? { body: JSON.stringify( body ) } : {}),
			signal: AbortSignal.timeout( TIMEOUT_MS ),
			redirect: "error"
		} );
		const text = await readResponseText( response, MAX_RESPONSE_BYTES, "Website answer exceeded limit" );
		if ( response.status === 404 && text.trim() === "Not Found" ) {
			return {
				status: 503,
				body: {
					error:
						"The website refused the console: check CONSOLE_ADMIN_TOKEN_FILE and siteAdmin.tokenFile match."
				}
			};
		}
		let parsed;
		try {
			parsed = JSON.parse( text );
		} catch {
			return { status: 502, body: { error: "The website answered something that is not JSON" } };
		}
		return { status: response.status, body: parsed };
	}

	return {
		list( operator, filters = {} ) {
			const query = inviteListQuery( filters );
			if ( query.error ) return { status: 400, body: { error: query.error } };
			return call( "/internal/admin/invites" + query.text, operator );
		},
		reveal( operator, id ) {
			if ( !KEY_ID.test( id ) ) return { status: 400, body: { error: "Unknown key" } };
			return call( `/internal/admin/invites/${id}/reveal`, operator, {} );
		},
		create( operator, request ) {
			if ( request.discordUserId && !DISCORD_SNOWFLAKE.test( String( request.discordUserId ) ) ) {
				return { status: 400, body: { error: "A Discord user ID is 15 to 22 digits" } };
			}
			const body = {
				count: Number( request.count ),
				maxUses: Number( request.maxUses ),
				expiresInDays: Number( request.expiresInDays ),
				note: String( request.note ?? "" ),
				...(request.discordUserId ?
					{
						discordUserId: String( request.discordUserId ),
						discordName: String( request.discordName ?? "" )
					} :
					{})
			};
			return call( "/internal/admin/invites", operator, body );
		},
		revoke( operator, id ) {
			if ( !KEY_ID.test( id ) ) return { status: 400, body: { error: "Unknown key" } };
			return call( `/internal/admin/invites/${id}/revoke`, operator, {} );
		}
	};
}
