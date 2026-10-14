/*
===========================================================================

main.mjs - the console sign-in service on loopback

Reads the owner's root-owned config (SRO_CONSOLE_AUTH_CONFIG) and the
Discord client secret file it names, then serves /auth/* on
127.0.0.1 for Caddy only (auth.mjs owns the rules). It logs one JSON line
per sign-in event to stdout for the journal; never a token, code or
cookie.

===========================================================================
*/
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createAuth, DEFAULT_LIMITS } from "./auth.mjs";
import { createDiscord } from "./discord.mjs";

const DEFAULT_PORT = 5191;
const SNOWFLAKE = /^\d{1,20}$/;

/*
================
loadConfig

The service config, checked before anything listens: a bad one stops the
service rather than letting the wrong people in.
================
*/
export async function loadConfig( file, read = readFile ) {
	const raw = JSON.parse( await read( file, "utf8" ) );
	const origin = new URL( raw.origin );
	if ( origin.protocol !== "https:" || origin.pathname !== "/" ) throw Error( "origin must be an https origin" );
	for ( const name of [ "staff", "operator", "approver" ] ) {
		if ( !SNOWFLAKE.test( raw.roles?.[name] ?? "" ) ) throw Error( `roles.${name} must be a Discord role id` );
	}
	if ( !SNOWFLAKE.test( raw.discord?.clientId ?? "" ) || !SNOWFLAKE.test( raw.discord?.guildId ?? "" ) ) {
		throw Error( "discord.clientId and discord.guildId must be Discord ids" );
	}
	if ( !Array.isArray( raw.approverIds ) || !raw.approverIds.every( id => SNOWFLAKE.test( id ) ) ) {
		throw Error( "approverIds must be a list of Discord user ids" );
	}
	const clientSecret = (await read( raw.discord.clientSecretFile, "utf8" )).trim();
	if ( clientSecret.length < 16 ) throw Error( "the Discord client secret file is empty" );
	const limits = { ...DEFAULT_LIMITS };
	for ( const [key, value] of Object.entries( raw.limits ?? {} ) ) {
		if ( !Object.hasOwn( DEFAULT_LIMITS, key ) || !Number.isFinite( value ) || value <= 0 ) {
			throw Error( `limits.${key} is not a known positive number` );
		}
		limits[key] = value;
	}
	return {
		port: raw.port ?? DEFAULT_PORT,
		origin: origin.origin,
		roles: raw.roles,
		approverIds: raw.approverIds,
		limits,
		app: {
			clientId: raw.discord.clientId,
			clientSecret,
			guildId: raw.discord.guildId,
			redirectUri: origin.origin + "/auth/callback"
		}
	};
}

/*
================
readRequest
================
*/
function readRequest( req ) {
	const url = new URL( req.url ?? "/", "http://127.0.0.1" );
	return { method: req.method ?? "GET", path: url.pathname, query: url.searchParams, headers: req.headers };
}

/*
================
start
================
*/
async function start() {
	const file = process.env.SRO_CONSOLE_AUTH_CONFIG;
	if ( !file ) throw Error( "SRO_CONSOLE_AUTH_CONFIG names the config file" );
	const config = await loadConfig( file );
	const log = row => console.log( JSON.stringify( { at: new Date().toISOString(), ...row } ) );
	const auth = createAuth( { config, discord: createDiscord( config.app ), log } );
	const server = createServer( async ( req, res ) => {
		let answer;
		try {
			answer = await auth.handle( readRequest( req ) );
		} catch {
			answer = { status: 500, headers: { "Cache-Control": "no-store" }, body: "" };
			log( { event: "error" } );
		}
		res.writeHead( answer.status, answer.headers );
		res.end( answer.body );
	} );
	server.requestTimeout = 20000;
	server.headersTimeout = 10000;
	server.listen( config.port, "127.0.0.1", () => log( { event: "listening", port: config.port } ) );
	for ( const signal of [ "SIGINT", "SIGTERM" ] ) {
		process.once( signal, () => server.close( () => process.exit( 0 ) ) );
	}
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	await start();
}
