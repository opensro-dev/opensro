/*
===========================================================================

server-starting.ts - a server that answered "starting" is waited for

While a GameWorld's readiness gate has never opened (the population boot
fill after a deploy, about 17 s), its Agent and control API answer 503
PROCESS_STARTING with a Retry-After (server readiness.Refusal, #246). The
title keeps its waiting state and repeats the request after that delay,
until the server opens or MAX_STARTING_WAIT_MS passes; only then is the
server's own message shown as a failure.

===========================================================================
*/

export const STARTING_CODE = "PROCESS_STARTING";
// Startup usually opens within ~17 s; a minute covers a slow boot without
// leaving a player waiting on a server that will not come up.
export const MAX_STARTING_WAIT_MS = 60_000;
const DEFAULT_RETRY_MS = 1000;
const MAX_RETRY_MS = 10_000;

/*
================
ServerStarting
================
*/
export interface ServerStarting {
	readonly message: string;
	readonly retryMs: number;
}

/*
================
serverStarting

The starting refusal in a response body, or null for any other answer.
The delay comes from the body's retryAfter (seconds), bounded.
================
*/
export function serverStarting( body: unknown ): ServerStarting | null {
	const value = body as { code?: unknown; message?: unknown; retryAfter?: unknown; } | null;
	if ( !value || value.code !== STARTING_CODE ) return null;
	const message = typeof value.message === "string" ? value.message : "The server is starting.";
	const seconds = value.retryAfter;
	const retryMs = typeof seconds === "number" && Number.isFinite( seconds ) && seconds > 0 ?
		Math.min( MAX_RETRY_MS, Math.round( seconds * 1000 ) ) :
		DEFAULT_RETRY_MS;
	return { message, retryMs };
}

// HTTP 503: the Agent proxy answers it, plain text, while the shard is not
// operating (agent server handlers.go: "shard unavailable").
const SERVICE_UNAVAILABLE = 503;

/*
================
rosterStarting

A character-list answer that means "wait", not "no characters": the
GameWorld's PROCESS_STARTING, or the Agent's bare 503 for a shard that is
not operating yet. During a restart either can reach a player who is already
logged in; an empty dock then reads as deleted characters.
================
*/
export function rosterStarting( response: { status: number; body: unknown; } ): ServerStarting | null {
	const starting = serverStarting( response.body );
	if ( starting ) return starting;
	if ( response.status !== SERVICE_UNAVAILABLE ) return null;
	return { message: "The server is starting. Your characters will appear in a moment.", retryMs: DEFAULT_RETRY_MS };
}
