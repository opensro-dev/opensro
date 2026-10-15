/*
===========================================================================

settings.mjs - read-only reads of each GameWorld's shard rules

The Shard rules page's server half. GameWorld serves its tunable values
and its shard settings (contract v1: schemaVersion, settings, flags) on its
loopback operator listener at /internal/operations/settings/v1, under the
shard's operator token (platform/settingsapi). This owner forwards that
one read, adds the token server-side (the browser never sees it), bounds
the answer and passes on only the fields the contract names, each checked
for its type, so a GameWorld can never hand the page markup or extra data.

Proposals, the second approval and the audit come with the contract's
second part and the console's staff sign-in; there is no write route here.

===========================================================================
*/
import { boundedText } from "./bounded-body.mjs";

const SETTINGS_PATH = "/internal/operations/settings/v1";
const SCHEMA_VERSION = 1;
const MAX_RESPONSE_BYTES = 1024 * 1024;
const REQUEST_TIMEOUT_MS = 10000;
const MAX_ROWS = 512;
const MAX_TEXT = 512;
const KINDS = new Set( [ "int", "factor" ] );
const CLASSES = new Set( [ "live", "restart" ] );
const SOURCES = new Set( [ "default", "env", "store" ] );
const ENV = /^SRO_[A-Z0-9_]{1,64}$/;
const KEY = /^[a-z0-9_]{1,64}$/;

/*
================
text

A bounded string, or the fallback for anything else.
================
*/
function text( value, fallback = "" ) {
	return typeof value === "string" ? value.slice( 0, MAX_TEXT ) : fallback;
}

/*
================
number
================
*/
function number( value ) {
	return typeof value === "number" && Number.isFinite( value ) ? value : null;
}

/*
================
settingRow

One contract SettingRow, or null when a required field is missing or of
the wrong type.
================
*/
function settingRow( row ) {
	if ( !row || typeof row !== "object" ) return null;
	const value = number( row.value ),
		fallback = number( row.default ),
		min = number( row.min ),
		max = number( row.max ),
		step = number( row.step );
	if (
		!KEY.test( row.key ?? "" ) || !ENV.test( row.env ?? "" ) || !KINDS.has( row.kind ) ||
		!CLASSES.has( row.class ) || value === null || fallback === null || min === null || max === null
	) return null;
	const allowed = Array.isArray( row.allowed ) ?
		row.allowed.map( number ).filter( n => n !== null ).slice( 0, 64 ) :
		null;
	return {
		key: row.key,
		env: row.env,
		group: text( row.group, "Other" ),
		label: text( row.label, row.key ),
		help: text( row.help ),
		kind: row.kind,
		unit: text( row.unit ),
		value,
		enabled: row.enabled !== false,
		default: fallback,
		native: typeof row.native === "string" ? text( row.native ) : null,
		min,
		max,
		allowed,
		step: step ?? 1,
		class: row.class,
		source: SOURCES.has( row.source ) ? row.source : "default"
	};
}

/*
================
flagRow
================
*/
function flagRow( row ) {
	if ( !row || typeof row !== "object" || !ENV.test( row.env ?? "" ) ) return null;
	return { env: row.env, value: text( row.value ), on: typeof row.on === "boolean" ? row.on : null };
}

/*
================
settingsBody

The contract body as the page may see it, or null when it is not v1.
================
*/
export function settingsBody( body ) {
	if ( !body || body.schemaVersion !== SCHEMA_VERSION ) return null;
	if ( !Array.isArray( body.settings ) || !Array.isArray( body.flags ) ) return null;
	return {
		schemaVersion: SCHEMA_VERSION,
		settings: body.settings.slice( 0, MAX_ROWS ).map( settingRow ).filter( Boolean ),
		flags: body.flags.slice( 0, MAX_ROWS ).map( flagRow ).filter( Boolean )
	};
}

/*
================
createSettings

shards are loadShards rows (their url is on the loopback operator
listener); token is the shard operator token, empty when the console has
none.
================
*/
export function createSettings( shards, token, fetcher = fetch ) {
	return {
		enabled: Boolean( token ),
		/*
		================
		request
		================
		*/
		async request( params ) {
			const shard = shards.find( row => row.id === params.get( "shard" ) );
			if ( !shard ) return { status: 404, body: { error: "Unknown server" } };
			if ( !token ) return { status: 503, body: { error: "Shard rules are not configured on this console" } };
			try {
				const response = await fetcher( new URL( SETTINGS_PATH, shard.url ), {
					headers: { Authorization: "Bearer " + token, "X-SRO-Local-Diagnostics": "1" },
					signal: AbortSignal.timeout( REQUEST_TIMEOUT_MS ),
					redirect: "error"
				} );
				if ( !response.ok ) {
					await response.body?.cancel();
					return { status: 502, body: { error: "Shard rules unavailable: HTTP " + response.status } };
				}
				let body;
				try {
					body = settingsBody( JSON.parse( await boundedText( response, MAX_RESPONSE_BYTES ) ) );
				} catch {
					body = null;
				}
				if ( !body ) return { status: 502, body: { error: "This GameWorld does not speak shard rules v1" } };
				return { status: 200, body };
			} catch ( error ) {
				return { status: 503, body: { error: "Shard rules unavailable: " + error.message } };
			}
		}
	};
}
