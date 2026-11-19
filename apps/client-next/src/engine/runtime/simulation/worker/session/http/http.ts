/*
===========================================================================

http.ts - the session's requests to the title and agent services

One request helper bounds every response body, chooses credentials per
route and declares the release protocol this build speaks. It is also the
one place that learns the server speaks another: a 426 answer marks the
session's release outdated, which the page shows as the update notice.

===========================================================================
*/
import { HUNTING_GUIDE_BYTES_LIMIT } from "@/engine/contracts/hunting-guide";
import { isResponseByteLimitError, readBytes } from "@/engine/foundation/assets/read-bytes";
import { sha256Hex } from "@/engine/foundation/assets/sha256";
import { decodeReverseMapPoints } from "@/engine/foundation/gameplay/reverse-return-map";
import {
	RELEASE_OUTDATED_STATUS,
	REFERENCES_CONTRACT,
	RELEASE_PROTOCOL,
	RELEASE_PROTOCOL_HEADER
} from "@/engine/foundation/release/protocol";

const DEFAULT_RESPONSE_BYTES = 65536;
// Browsers refuse a keepalive fetch whose body passes 64 KiB, and a character
// can be four UTF-8 bytes, so the cap is counted in chars with the margin.
const KEEPALIVE_BODY_CHARS = 16 * 1024;
const LIST_RESPONSE_BYTES = 1 << 20;
const REFERENCE_BYTES_LIMIT = 32 * 1024 * 1024;
const REFERENCE_ROWS_LIMIT = 65536;
// Error code of a reference body that does not match its own name.
const REFERENCE_CORRUPT = "REFERENCE_CORRUPT";

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
	keepalive?: boolean;
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
		const { base, route, signal, body, token, limit = DEFAULT_RESPONSE_BYTES, keepalive = false } = options;
		const headers: Record<string, string> = { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) };
		if ( body !== undefined ) headers["Content-Type"] = "application/json";
		if ( token ) headers.Authorization = `Bearer ${token}`;
		const encoded = body !== undefined ? JSON.stringify( body ) : undefined;
		const response = await fetch( `${base}${route}`, {
			method: body === undefined ? "GET" : "POST",
			headers,
			credentials: BROWSER_AUTH_ROUTES.has( route ) ? "include" : "omit",
			redirect: "error",
			cache: "no-store",
			...(encoded !== undefined ? { body: encoded } : {}),
			...(keepalive && encoded !== undefined && encoded.length <= KEEPALIVE_BODY_CHARS ?
				{ keepalive: true } :
				{}),
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
		incident( base: string, token: string | undefined, body: unknown, signal: AbortSignal ) {
			// A failing page may be closing: keepalive lets the post finish
			// instead of reaching the Agent cut short (#2149).
			return request( { base, route: "/client/incident", signal, body, token, keepalive: true } );
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
	signal: AbortSignal,
	progress?: () => void
): Promise<{
	refSkillSnapshot: unknown[];
	refItemSnapshot: unknown[];
	refObjSnapshot: unknown[];
	itemCommandReferences?: unknown[];
	huntingGuide?: import("@/engine/contracts/hunting-guide").HuntingGuideSource;
	reverseMapPoints?: unknown[];
}> {
	const ref = value as { spawnAtlas?: unknown; huntingGuide?: unknown; } | null;
	const source = referenceSource( value, base, REFERENCE_BYTES_LIMIT );
	if ( !ref || !source ) throw Error( "Invalid world reference identity" );
	const bytes = await fetchReference( source, signal, progress );
	signal.throwIfAborted();
	const parsed = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) ) as {
		referencesVersion?: unknown;
		skillLifecycleVersion?: unknown;
		refSkillSnapshot?: unknown;
		refItemSnapshot?: unknown;
		refObjSnapshot?: unknown;
		itemCommandReferences?: unknown;
		// Port-only: present only when the server's SRO_REVERSE_RETURN_MAP is on.
		reverseMapPoints?: unknown;
	};
	if (
		!parsed ||
		Object.keys( parsed ).some( key =>
			key !== "referencesVersion" && key !== "skillLifecycleVersion" && key !== "refSkillSnapshot" &&
			key !== "refItemSnapshot" && key !== "refObjSnapshot" && key !== "itemCommandReferences" &&
			key !== "reverseMapPoints"
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
	// The atlas every shard publishes (spawnAtlas), or a server older than
	// it that publishes the same file only as huntingGuide. huntingGuide's
	// presence alone offers the port-only overlay.
	const overlay = huntingGuideSource( ref.huntingGuide, base );
	const atlas = huntingGuideSource( ref.spawnAtlas, base ) ?? overlay;
	const guide = atlas ? { ...atlas, ...(overlay ? { overlay: true } : {}) } : undefined;
	const reverseMapPoints = decodeReverseMapPoints( parsed.reverseMapPoints );
	return {
		...(guide ? { huntingGuide: guide } : {}),
		refSkillSnapshot: parsed.refSkillSnapshot,
		refItemSnapshot: parsed.refItemSnapshot,
		refObjSnapshot: parsed.refObjSnapshot,
		...(parsed.itemCommandReferences === undefined ?
			{} :
			{ itemCommandReferences: parsed.itemCommandReferences as unknown[] }),
		...(reverseMapPoints.length ? { reverseMapPoints: [ ...reverseMapPoints ] } : {})
	};
}

/*
================
huntingGuideSource

Optional metadata cannot break native admission. Only a same-transport,
content-addressed public reference resource may reach the HUD asset owner.
================
*/
export function huntingGuideSource(
	value: unknown,
	base: string
): import("@/engine/contracts/hunting-guide").HuntingGuideSource | undefined {
	const source = referenceSource( value, base, HUNTING_GUIDE_BYTES_LIMIT );
	return source ? { url: source.url.href, bytes: source.bytes } : undefined;
}

// ============================================================================

/*
================
ReferenceSource

A transport reference resolved against the transport base: where it is,
the digest that names it and its exact length.
================
*/
interface ReferenceSource {
	readonly url: URL;
	readonly sha256: string;
	readonly bytes: number;
}

/*
================
referenceSource

The one identity rule for a transport reference: a content-addressed
/transport/references/<sha256>.json of 1..limit bytes, beside the socket
under the transport base (an edge route prefix is kept). Undefined when the
value or the base breaks it; each caller decides whether that is fatal.
================
*/
function referenceSource( value: unknown, base: string, limit: number ): ReferenceSource | undefined {
	const ref = value as { path?: unknown; sha256?: unknown; bytes?: unknown; } | null;
	if (
		!ref || typeof ref.sha256 !== "string" || !/^[a-f0-9]{64}$/.test( ref.sha256 ) ||
		ref.path !== `/transport/references/${ref.sha256}.json` || typeof ref.bytes !== "number" ||
		!Number.isInteger( ref.bytes ) || ref.bytes < 1 || ref.bytes > limit
	) return undefined;
	const url = new URL( base );
	if ( url.protocol !== "http:" && url.protocol !== "https:" ) return undefined;
	url.pathname = url.pathname.replace( /\/$/, "" ) + ref.path;
	url.search = "";
	url.hash = "";
	return { url, sha256: ref.sha256, bytes: ref.bytes };
}

/*
================
corruptReference

A body that does not match its own name. The discriminant keeps the retry
decision off error prose, as isResponseByteLimitError does.
================
*/
function corruptReference( message: string ): Error {
	return Object.assign( new Error( message ), { code: REFERENCE_CORRUPT } );
}

function isCorruptReference( error: unknown ): boolean {
	return isResponseByteLimitError( error ) ||
		(error instanceof Error && "code" in error && error.code === REFERENCE_CORRUPT);
}

/*
================
fetchReferenceOnce

One read of a reference under one cache mode, checked against its name.
The decoded stream is bounded too (Content-Length describes compressed
bytes), and each chunk is progress for the caller's stall deadline (#1010).
================
*/
async function fetchReferenceOnce(
	source: ReferenceSource,
	cache: RequestCache,
	signal: AbortSignal,
	progress?: () => void
): Promise<Uint8Array<ArrayBuffer>> {
	const response = await fetch( source.url, { signal, cache, credentials: "omit", redirect: "error" } );
	if ( !response.ok || !response.body ) throw Error( `Reference unavailable (${response.status})` );
	const bytes = await readBytes( response.body, source.bytes, { received: () => progress?.() } );
	if ( bytes.length !== source.bytes ) throw corruptReference( "Reference size mismatch" );
	if ( await sha256Hex( bytes ) !== source.sha256 ) throw corruptReference( "Reference digest mismatch" );
	return bytes;
}

/*
================
fetchReference

The HTTP cache owns reference bytes across reloads: the path is the content's
hash, so a stored copy never goes stale and force-cache serves it without a
request. A stored copy that was cut or damaged would fail the check on every
later login too, so one failed check refetches with cache "reload", which
also overwrites the stored copy. A second failure is a bad publication and
throws. Cancellation, network and HTTP failures are not retried: the network
would give the same answer.
================
*/
async function fetchReference(
	source: ReferenceSource,
	signal: AbortSignal,
	progress?: () => void
): Promise<Uint8Array<ArrayBuffer>> {
	try {
		return await fetchReferenceOnce( source, "force-cache", signal, progress );
	} catch ( error ) {
		if ( !isCorruptReference( error ) ) throw error;
		signal.throwIfAborted();
		return fetchReferenceOnce( source, "reload", signal, progress );
	}
}
