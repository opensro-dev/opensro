/*
===========================================================================

Probe Agent Session

The shared credentials, shard selection, and strict login contract for both
browser-driven and raw-wire probes. Probes authenticate like players; there
is no development bypass and no implicit first-shard fallback.

===========================================================================
*/
import {
	RELEASE_PROTOCOL,
	RELEASE_PROTOCOL_HEADER
} from "../../apps/client-next/src/engine/foundation/release/protocol.ts";

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parseEnv } from "node:util";
import { assertCharacterAllowed } from "./probeCharacter.mjs";
import { probeAgentUrl } from "./probeEndpoints.mjs";

const DEVELOPMENT_LOGIN = parseEnv(
	readFileSync(
		new URL( "../../apps/server/config/dev-account.env", import.meta.url ),
		"utf8"
	)
);

/*
================
developmentLoginValue
================
*/
function developmentLoginValue( key ) {
	const value = DEVELOPMENT_LOGIN[key];
	if ( typeof value !== "string" || value.trim() === "" ) {
		throw new Error( `apps/server/config/dev-account.env is missing ${key}` );
	}
	return value;
}

const SHARD_CATALOG = process.env.SRO_SHARD_CATALOG?.trim() ||
	fileURLToPath( new URL( "../../apps/server/config/shards.json", import.meta.url ) );

/*
================
resolveShardTransportUrl

Server-local probes dial a GameWorld directly. An advertised edge route
("/shards/<id>") exists only on the web edge that serves browsers, so the
shard's own transport endpoint comes from the catalog the stack deployed.
================
*/
export function resolveShardTransportUrl( divisionId, advertised ) {
	if ( !advertised.startsWith( "/" ) ) {
		return advertised;
	}
	const shard = JSON.parse( readFileSync( SHARD_CATALOG, "utf8" ) ).shards.find(
		( row ) => row.id === divisionId
	);
	if ( typeof shard?.transportUrl !== "string" ) {
		throw new Error( `probe shard "${divisionId}" has no transportUrl in ${SHARD_CATALOG}` );
	}
	return shard.transportUrl;
}

/*
================
resolveProbeCredentials
================
*/
export function resolveProbeCredentials( loginId, loginPassword ) {
	if ( (loginId === undefined) !== (loginPassword === undefined) ) {
		throw new Error( "probe login requires both loginId and loginPassword" );
	}
	if ( loginId !== undefined ) {
		return { loginId, loginPassword };
	}

	const envLoginId = process.env.SRO_PROBE_LOGIN_ID;
	const envLoginPassword = process.env.SRO_PROBE_LOGIN_PASSWORD;
	if ( (envLoginId === undefined) !== (envLoginPassword === undefined) ) {
		throw new Error( "set both SRO_PROBE_LOGIN_ID and SRO_PROBE_LOGIN_PASSWORD" );
	}
	return {
		loginId: envLoginId ?? developmentLoginValue( "SRO_DEV_ACCOUNT_ID" ),
		loginPassword: envLoginPassword ?? developmentLoginValue( "SRO_DEV_ACCOUNT_PASSWORD" )
	};
}

/*
================
resolveProbeDivisionId
================
*/
export function resolveProbeDivisionId( divisionId ) {
	const resolved = divisionId ??
		process.env.SRO_PROBE_SHARD ??
		developmentLoginValue( "SRO_DEV_ACCOUNT_SHARD" );
	if ( typeof resolved !== "string" || resolved.trim() === "" ) {
		throw new Error( "probe login requires a divisionId" );
	}
	return resolved;
}

/*
================
validateProbeSessionRoute
================
*/
export function validateProbeSessionRoute( body, expectedDivisionId ) {
	if (
		body?.ok !== true ||
		typeof body.sessionToken !== "string" ||
		body.sessionToken === "" ||
		body.divisionId !== expectedDivisionId ||
		typeof body.transportUrl !== "string" ||
		body.transportUrl === ""
	) {
		throw new Error(
			`probe login returned an incomplete or wrong-shard route: ${JSON.stringify( body )}`
		);
	}
	return {
		token: body.sessionToken,
		divisionId: body.divisionId,
		transportUrl: body.transportUrl
	};
}

/*
================
declared

Probes authenticate like players, so they declare the release protocol the
client they stand in for was built with (foundation/release/protocol.ts).
================
*/
function declared( headersInit ) {
	const headers = new Headers( headersInit );
	headers.set( RELEASE_PROTOCOL_HEADER, String( RELEASE_PROTOCOL ) );
	return headers;
}

/*
================
fetchJson
================
*/
async function fetchJson( url, init ) {
	const response = await fetch( url, { ...init, headers: declared( init?.headers ) } );
	const body = await response.json().catch( () => null );
	if ( !response.ok ) {
		throw new ProbeSessionHttpError( String( url ), response.status, body, response.headers.get( "retry-after" ) );
	}
	return body;
}

/*
================
openProbeAgentSession
================
*/
export async function openProbeAgentSession( options = {} ) {
	const { loginId, loginPassword } = resolveProbeCredentials(
		options.loginId,
		options.loginPassword
	);
	const divisionId = resolveProbeDivisionId( options.divisionId );
	const servers = await fetchJson( probeAgentUrl( "/title/servers" ) );
	const selected = Array.isArray( servers ) ?
		servers.find( ( server ) => server?.id === divisionId ) :
		undefined;
	if ( !selected || selected.operating !== true ) {
		throw new Error( `probe shard "${divisionId}" is absent or not operating` );
	}

	const body = await fetchJson( probeAgentUrl( "/title/login" ), {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify( {
			id: loginId,
			password: loginPassword,
			serverId: divisionId
		} )
	} );
	const session = validateProbeSessionRoute( body, divisionId );
	return {
		...session,
		transportUrl: resolveShardTransportUrl( divisionId, session.transportUrl )
	};
}

/*
================
probeSessionAuthorizationHeaders
================
*/
export function probeSessionAuthorizationHeaders( session ) {
	if ( !session?.token ) {
		throw new Error( "probe Agent session is missing its bearer" );
	}
	return { Authorization: `Bearer ${session.token}` };
}

export class ProbeSessionHttpError extends Error {
	constructor( pathname, status, body, retryAfter = null ) {
		super( `${pathname} -> HTTP ${status}: ${JSON.stringify( body )}` );
		this.name = "ProbeSessionHttpError";
		this.pathname = pathname;
		this.status = status;
		this.body = body;
		// Seconds the server asked the caller to wait (429), or null.
		this.retryAfter = retryAfter === null ? null : Number( retryAfter );
	}
}

/*
================
fetchProbeSessionJson
================
*/
export async function fetchProbeSessionJson( session, pathname, init = {} ) {
	const headers = declared( init.headers );
	for ( const [name, value] of Object.entries( probeSessionAuthorizationHeaders( session ) ) ) {
		headers.set( name, value );
	}
	const response = await fetch( probeAgentUrl( pathname ), { ...init, headers } );
	const body = await response.json().catch( () => null );
	if ( !response.ok ) {
		throw new ProbeSessionHttpError( pathname, response.status, body );
	}
	return body;
}

/*
================
mintProbeTransportAdmissionToken
================
*/
export async function mintProbeTransportAdmissionToken( session ) {
	const body = await fetchProbeSessionJson( session, "/auth/transport-token", {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: "{}"
	} );
	if ( body?.ok !== true || typeof body.token !== "string" || body.token === "" ) {
		throw new Error( `/auth/transport-token refused: ${body?.code ?? "malformed response"}` );
	}
	return body.token;
}

/*
================
mintProbeEnterWorldToken
================
*/
export async function mintProbeEnterWorldToken( session, characterName ) {
	const body = await fetchProbeSessionJson( session, "/auth/enterworld-token", {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify( {
			characterName,
			divisionId: session.divisionId
		} )
	} );
	if ( body?.ok !== true || typeof body.token !== "string" || body.token === "" ) {
		throw new Error( `/auth/enterworld-token refused: ${body?.code ?? "malformed response"}` );
	}
	return body.token;
}

/**
 * Read a character's persisted world spawn through a short-lived Node-side
 * Agent session. This keeps scratch-position preservation available to the
 * declared loopback development boot lane without coupling it to a page-local
 * bearer minted by the slower retail title flow.
 */
export async function readProbeCharacterSpawn( characterName, options = {} ) {
	assertCharacterAllowed( characterName, {
		context: "readProbeCharacterSpawn()"
	} );
	const session = await openProbeAgentSession( options );
	return readProbeCharacterSpawnFromSession( session, characterName );
}

/*
================
readProbeCharacterSpawnFromSession
================
*/
export async function readProbeCharacterSpawnFromSession( session, characterName ) {
	assertCharacterAllowed( characterName, {
		context: "readProbeCharacterSpawnFromSession()"
	} );
	const body = await fetchProbeSessionJson( session, "/character/list" );
	const character = Array.isArray( body?.characters ) ?
		body.characters.find( ( entry ) => entry?.name === characterName ) :
		undefined;
	return character?.world?.spawn ?? null;
}
