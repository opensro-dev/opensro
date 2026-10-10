/*
===========================================================================

http.ts - the session's requests to the title and agent services

One request helper bounds every response body, chooses credentials per
route and declares the release protocol this build speaks. It is also the
one place that learns the server speaks another: a 426 answer marks the
session's release outdated, which the page shows as the update notice.

===========================================================================
*/
import { readBytes } from "@/engine/foundation/assets/read-bytes";
import {
	RELEASE_OUTDATED_STATUS,
	REFERENCES_CONTRACT,
	RELEASE_PROTOCOL,
	RELEASE_PROTOCOL_HEADER
} from "@/engine/foundation/release/protocol";

const DEFAULT_RESPONSE_BYTES = 65536;
const LIST_RESPONSE_BYTES = 1 << 20;
const REFERENCE_BYTES_LIMIT = 32 * 1024 * 1024;
const REFERENCE_ROWS_LIMIT = 65536;

// Routes that carry the browser session cookie.
const BROWSER_AUTH_ROUTES = new Set( [
	"/title/login",
	"/title/session",
	"/title/logout",
	"/title/character-select",
	"/auth/enterworld-token"
] );

type SessionResponse = { httpOk: boolean; status: number; body: unknown; };

type RequestOptions = {
	base: string;
	route: string;
	signal: AbortSignal;
	body?: unknown;
	token?: string;
	limit?: number;
};

/*
================
createSessionHttp
================
*/
export function createSessionHttp() {
	let outdated = false;

	/*
	================
	request

	One declared, bounded request. A 426 answer still resolves (its body
	names the server's protocol) and marks the release outdated.
	================
	*/
	async function request( options: RequestOptions ): Promise<SessionResponse> {
		const { base, route, signal, body, token, limit = DEFAULT_RESPONSE_BYTES } = options;
		const headers: Record<string, string> = { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) };
		if ( body !== undefined ) headers["Content-Type"] = "application/json";
		if ( token ) headers.Authorization = `Bearer ${token}`;
		const response = await fetch( `${base}${route}`, {
			method: body === undefined ? "GET" : "POST",
			headers,
			credentials: BROWSER_AUTH_ROUTES.has( route ) ? "include" : "omit",
			redirect: "error",
			cache: "no-store",
			...(body !== undefined ? { body: JSON.stringify( body ) } : {}),
			signal
		} );
		if ( response.status === RELEASE_OUTDATED_STATUS ) outdated = true;
		if ( !response.body ) throw new Error( "Session response has no body" );
		const bytes = await readBytes( response.body, limit );
		const text = new TextDecoder( "utf-8", { fatal: true } ).decode( bytes );
		// A refusal can be plain text (the Agent proxy's "shard unavailable"
		// 503); its status still tells the session what happened. An accepted
		// answer must be JSON.
		let parsed: unknown = null;
		try {
			parsed = JSON.parse( text ) as unknown;
		} catch ( error ) {
			if ( response.ok ) throw error;
		}
		return { httpOk: response.ok, status: response.status, body: parsed };
	}

	return {
		references: loadWorldReferences,
		/*
		================
		releaseOutdated

		True once a server answered that it speaks another release protocol.
		================
		*/
		releaseOutdated: () => outdated,
		restore( base: string, signal: AbortSignal ) {
			return request( { base, route: "/title/session", signal, body: {} } );
		},
		logout( base: string, signal: AbortSignal ) {
			return request( { base, route: "/title/logout", signal, body: {} } );
		},
		returnToDock( base: string, signal: AbortSignal ) {
			return request( { base, route: "/title/character-select", signal, body: {} } );
		},
		character( base: string, token: string, route: string, body: unknown, signal: AbortSignal ) {
			return request( { base, route, signal, body, token } );
		},
		mint(
			base: string,
			token: string,
			kind: "transport" | "enterworld",
			characterName: string,
			divisionId: string,
			signal: AbortSignal
		) {
			const body = kind === "transport" ? {} : { characterName, divisionId };
			return request( { base, route: `/auth/${kind}-token`, signal, body, token } );
		},
		login(
			base: string,
			body: { id: string; password: string; serverId: string; divisionId?: string; },
			signal: AbortSignal
		) {
			return request( { base, route: "/title/login", signal, body } );
		},
		servers( base: string, signal: AbortSignal ) {
			return request( { base, route: "/title/servers", signal, limit: LIST_RESPONSE_BYTES } );
		},
		incident( base: string, token: string, body: unknown, signal: AbortSignal ) {
			return request( { base, route: "/client/incident", signal, body, token } );
		},
		roster( base: string, token: string, signal: AbortSignal ) {
			return request( { base, route: "/character/list", signal, token, limit: LIST_RESPONSE_BYTES } );
		}
	};
}

/*
================
loadWorldReferences

The HTTP cache owns reference bytes across reloads; the current world
admission owns the fetch and its abort signal. No module-global promise may
outlive its session. References are content-addressed static files beside
the transport, so they carry no release declaration.

refItemSnapshot here is the static item catalogue (every drop, alchemy
output and gacha reward), and refObjSnapshot every monster the server can
create (#369). The login blob carries only the rows that depend on the
division's players and ground, disjoint from these.
================
*/
async function loadWorldReferences(
	value: unknown,
	base: string,
	signal: AbortSignal
): Promise<{
	refSkillSnapshot: unknown[];
	refItemSnapshot: unknown[];
	refObjSnapshot: unknown[];
	itemCommandReferences?: unknown[];
}> {
	const ref = value as { path?: unknown; sha256?: unknown; bytes?: unknown; };
	if (
		!ref || typeof ref.sha256 !== "string" || !/^[a-f0-9]{64}$/.test( ref.sha256 ) ||
		ref.path !== `/transport/references/${ref.sha256}.json` || typeof ref.bytes !== "number" ||
		!Number.isInteger( ref.bytes ) || ref.bytes < 1 || ref.bytes > REFERENCE_BYTES_LIMIT
	) throw Error( "Invalid world reference identity" );
	// References sit beside the socket under the transport base, including any edge route prefix.
	const url = new URL( base );
	if ( url.protocol !== "https:" && url.protocol !== "http:" ) throw Error( "Invalid transport base" );
	url.pathname = url.pathname.replace( /\/$/, "" ) + ref.path;
	url.search = "";
	url.hash = "";
	const response = await fetch( url, { signal, cache: "force-cache", credentials: "omit", redirect: "error" } );
	if ( !response.ok || !response.body ) throw Error( `World references unavailable (${response.status})` );
	// Bound the decoded stream too: Content-Length describes compressed bytes.
	const bytes = await readBytes( response.body, ref.bytes );
	if ( bytes.length !== ref.bytes ) throw Error( "World references size mismatch" );
	const digest = Array.from(
		new Uint8Array( await crypto.subtle.digest( "SHA-256", bytes ) ),
		n => n.toString( 16 ).padStart( 2, "0" )
	).join( "" );
	if ( digest !== ref.sha256 ) throw Error( "World references digest mismatch" );
	signal.throwIfAborted();
	const parsed = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) ) as {
		referencesVersion?: unknown;
		skillLifecycleVersion?: unknown;
		refSkillSnapshot?: unknown;
		refItemSnapshot?: unknown;
		refObjSnapshot?: unknown;
		itemCommandReferences?: unknown;
	};
	if (
		!parsed ||
		Object.keys( parsed ).some( key =>
			key !== "referencesVersion" && key !== "skillLifecycleVersion" && key !== "refSkillSnapshot" &&
			key !== "refItemSnapshot" && key !== "refObjSnapshot" && key !== "itemCommandReferences"
		) ||
		!Array.isArray( parsed.refSkillSnapshot ) || parsed.refSkillSnapshot.length > REFERENCE_ROWS_LIMIT
	) throw Error( "Invalid world references" );
	if ( parsed.skillLifecycleVersion !== 1 ) {
		throw Error( "World references lack native skill lifecycle metadata; rebuild the server" );
	}
	if ( parsed.referencesVersion !== REFERENCES_CONTRACT ) {
		throw Error(
			`World references contract ${String( parsed.referencesVersion )}, expected ${REFERENCES_CONTRACT}`
		);
	}
	if ( !Array.isArray( parsed.refItemSnapshot ) || parsed.refItemSnapshot.length > REFERENCE_ROWS_LIMIT ) {
		throw Error( "Invalid item references" );
	}
	if ( !Array.isArray( parsed.refObjSnapshot ) || parsed.refObjSnapshot.length > REFERENCE_ROWS_LIMIT ) {
		throw Error( "Invalid object references" );
	}
	if (
		parsed.itemCommandReferences !== undefined &&
		(!Array.isArray( parsed.itemCommandReferences ) || parsed.itemCommandReferences.length > REFERENCE_ROWS_LIMIT)
	) throw Error( "Invalid item command references" );
	return {
		refSkillSnapshot: parsed.refSkillSnapshot,
		refItemSnapshot: parsed.refItemSnapshot,
		refObjSnapshot: parsed.refObjSnapshot,
		...(parsed.itemCommandReferences === undefined ?
			{} :
			{ itemCommandReferences: parsed.itemCommandReferences as unknown[] })
	};
}
