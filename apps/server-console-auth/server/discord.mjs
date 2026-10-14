/*
===========================================================================

discord.mjs - the four Discord OAuth2 calls the console login makes

Code exchange, token refresh, the signed-in user, and that user's member
record in the staff guild (its role ids). Nothing else is asked of Discord:
the scopes are identify and guilds.members.read, no email and no guild
list. Every answer is bounded and checked; a token never appears in an
error message.

===========================================================================
*/

export const DISCORD_API = "https://discord.com/api/v10";
const AUTHORIZE_URL = "https://discord.com/oauth2/authorize";
const SCOPES = "identify guilds.members.read";
const REQUEST_TIMEOUT_MS = 10000;
const MAX_RESPONSE_BYTES = 64 * 1024;
const SNOWFLAKE = /^\d{1,20}$/;

/*
================
DiscordError

A failed Discord call: status is the HTTP status (0 for a network or
format failure). The message names the call, never a credential.
================
*/
export class DiscordError extends Error {
	constructor( call, status ) {
		super( `Discord ${call} failed` + (status ? ` (HTTP ${status})` : "") );
		this.status = status;
	}
}

/*
================
authorizeURL

Where the browser is sent to sign in: state ties the answer to this
browser, the PKCE challenge to this login.
================
*/
export function authorizeURL( app, state, challenge ) {
	const url = new URL( AUTHORIZE_URL );
	url.search = new URLSearchParams( {
		response_type: "code",
		client_id: app.clientId,
		scope: SCOPES,
		redirect_uri: app.redirectUri,
		state,
		code_challenge: challenge,
		code_challenge_method: "S256"
	} ).toString();
	return url.href;
}

/*
================
createDiscord

app is { clientId, clientSecret, redirectUri, guildId }.
================
*/
export function createDiscord( app, fetcher = fetch ) {
	/*
	================
	call
	================
	*/
	async function call( name, path, init ) {
		let response;
		try {
			response = await fetcher( DISCORD_API + path, {
				...init,
				signal: AbortSignal.timeout( REQUEST_TIMEOUT_MS ),
				redirect: "error"
			} );
		} catch {
			throw new DiscordError( name, 0 );
		}
		const text = await boundedText( response );
		if ( !response.ok ) throw new DiscordError( name, response.status );
		try {
			return JSON.parse( text );
		} catch {
			throw new DiscordError( name, 0 );
		}
	}
	/*
	================
	token
	================
	*/
	async function token( name, form ) {
		const basic = Buffer.from( `${app.clientId}:${app.clientSecret}` ).toString( "base64" );
		const body = await call( name, "/oauth2/token", {
			method: "POST",
			headers: { Authorization: "Basic " + basic, "Content-Type": "application/x-www-form-urlencoded" },
			body: new URLSearchParams( form ).toString()
		} );
		if (
			typeof body?.access_token !== "string" || typeof body.refresh_token !== "string" ||
			!Number.isFinite( body.expires_in )
		) throw new DiscordError( name, 0 );
		return { access: body.access_token, refresh: body.refresh_token, expiresIn: body.expires_in };
	}
	return {
		/*
		================
		exchange
		================
		*/
		exchange: ( code, verifier ) =>
			token( "code exchange", {
				grant_type: "authorization_code",
				code,
				redirect_uri: app.redirectUri,
				code_verifier: verifier
			} ),
		/*
		================
		refresh
		================
		*/
		refresh: refreshToken => token( "token refresh", { grant_type: "refresh_token", refresh_token: refreshToken } ),
		/*
		================
		user

		The signed-in user: { id, name }.
		================
		*/
		async user( access ) {
			const body = await call( "user", "/users/@me", { headers: { Authorization: "Bearer " + access } } );
			if ( !SNOWFLAKE.test( body?.id ?? "" ) || typeof body.username !== "string" ) {
				throw new DiscordError( "user", 0 );
			}
			return { id: body.id, name: String( body.global_name ?? body.username ).slice( 0, 64 ) };
		},
		/*
		================
		roles

		The user's role ids in the staff guild; null when they are not a
		member (Discord answers 404).
		================
		*/
		async roles( access ) {
			try {
				const body = await call( "member", `/users/@me/guilds/${app.guildId}/member`, {
					headers: { Authorization: "Bearer " + access }
				} );
				if ( !Array.isArray( body?.roles ) ) throw new DiscordError( "member", 0 );
				return body.roles.filter( role => typeof role === "string" && SNOWFLAKE.test( role ) );
			} catch ( error ) {
				if ( error instanceof DiscordError && error.status === 404 ) return null;
				throw error;
			}
		}
	};
}

/*
================
boundedText
================
*/
async function boundedText( response ) {
	const reader = response.body?.getReader();
	if ( !reader ) return "";
	const chunks = [];
	let size = 0;
	try {
		for ( ;; ) {
			const { done, value } = await reader.read();
			if ( done ) break;
			size += value.byteLength;
			if ( size > MAX_RESPONSE_BYTES ) throw new DiscordError( "response", 0 );
			chunks.push( value );
		}
	} finally {
		await reader.cancel();
	}
	return Buffer.concat( chunks ).toString( "utf8" );
}
