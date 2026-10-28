/*
===========================================================================
main.mjs - loopback observatory, authenticated player recovery, invite keys, economy and shard rules reads
===========================================================================
*/
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { loadShards, createSource } from "./source.mjs";
import { allowedRequest, allowedMutation, edgeIdentity, LOCAL_OPERATOR, requestOperator } from "./security.mjs";
import { createItems } from "./items.mjs";
import { createPlayerOperations, readBody } from "./player-operations.mjs";
import { createHistory } from "./history.mjs";
import { createEconomy } from "./economy.mjs";
import { createSettings } from "./settings.mjs";
import { createInviteKeys, siteAdminURL } from "./invite-keys.mjs";
import { clientPublicPath } from "../../../scripts/lib/generatedRoot.mjs";
const port = Number( process.env.SRO_OBSERVATORY_PORT ?? 5190 );
if ( !Number.isInteger( port ) || port < 1024 || port > 65535 ) throw Error( "Invalid dashboard port" );
const config = process.env.SRO_OBSERVATORY_CONFIG ?
	JSON.parse( await readFile( process.env.SRO_OBSERVATORY_CONFIG, "utf8" ) ) :
	{};
const edge = config.edge ?? null;
if (
	edge &&
	(new URL( edge.origin ).protocol !== "https:" || typeof edge.secret !== "string" || edge.secret.length < 32 ||
		(edge.identity !== "discord" && !edge.operator))
) throw Error( "Invalid console edge configuration" );
const token = config.operatorTokenFile ? (await readFile( config.operatorTokenFile, "utf8" )).trim() : "";
const shards = await loadShards( config.shards ?? new URL( "../../server/config/shards.json", import.meta.url ) );
const source = createSource( shards ), operations = createPlayerOperations( shards, token ), items = createItems();
const agentToken = config.agentOperatorTokenFile ?
	(await readFile( config.agentOperatorTokenFile, "utf8" )).trim() :
	token;
const historySources = [ {
	id: "agent",
	name: "Agent · logins and client reports",
	url: config.agentURL ?? "http://127.0.0.1:8787",
	token: agentToken
}, ...shards.map( row => ({ ...row, token }) ) ];
const history = createHistory( historySources );
// Each GameWorld's economy aggregates (economy.mjs), read with their own
// token; absent, the Economy page says it is off.
const economyToken = config.economyTokenFile ? (await readFile( config.economyTokenFile, "utf8" )).trim() : "";
const economy = createEconomy( shards, economyToken );
// Each GameWorld's shard rules (settings.mjs), read under the operator
// token the GameWorld's settings guard shares with its other readers.
const settings = createSettings( shards, token );
// The website's invite keys (opensro-web /internal/admin/invites), with the
// console's own secret; absent, the Invite keys page says it is off.
const siteAdmin = config.siteAdmin ?? null;
const siteAdminToken = siteAdmin?.tokenFile ? (await readFile( siteAdmin.tokenFile, "utf8" )).trim() : "";
const inviteKeys = createInviteKeys( siteAdmin ? siteAdminURL( siteAdmin.url ) : null, siteAdminToken );
const INVITE_REVOKE = /^\/api\/invites\/([0-9a-f-]{36})\/revoke$/;
const INVITE_REVEAL = /^\/api\/invites\/([0-9a-f-]{36})\/reveal$/;
const publicRoot = new URL( "../public/", import.meta.url ), files = new Map( [ [ "/", "index.html" ] ] );
// Banner art from the client's published images: the Dunhuang and Jangan loading screens.
// Fixed paths only; a checkout without built client assets answers 404 and
// theme.css paints its gradient fallback.
const LOADING_SCREENS = [ "assets", "images", "Media_extracted", "interface", "loading" ];
const artwork = new Map( [
	[ "/art/jangan.png", clientPublicPath( ...LOADING_SCREENS, "loading_zangan.png" ) ],
	[ "/art/dunhuang.png", clientPublicPath( ...LOADING_SCREENS, "loading_dunwhang.png" ) ]
] );
for (
	const file of [
		"app.js",
		"styles.css",
		"view.js",
		"model.js",
		"operations.js",
		"theme.css",
		"theme-boards.css",
		"theme-fit.css",
		"items.js",
		"item-model.js",
		"items.css",
		"players.html",
		"players.js",
		"players.css",
		"history.html",
		"history.js",
		"history-view.js",
		"history.css",
		"invites.js",
		"invite-made.js",
		"invites.css",
		"request.js",
		"economy.html",
		"economy.js",
		"economy-view.js",
		"economy.css",
		"settings.html",
		"settings.js",
		"settings-view.js",
		"settings.css"
	]
) files.set( "/" + file, file );
/*
================
respondJSON
================
*/
function respondJSON( response, status, body ) {
	response.writeHead( status, { "Content-Type": "application/json" } );
	response.end( JSON.stringify( body ) );
}
/*
================
handleRequest
================
*/
async function handleRequest( req, res ) {
	try {
		res.setHeader( "Cache-Control", "no-store" );
		res.setHeader( "X-Content-Type-Options", "nosniff" );
		res.setHeader(
			"Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
		);
		if ( !allowedRequest( req, port, edge ) ) {
			res.writeHead( 403 );
			res.end( "Operator access only" );
			return;
		}
		const url = new URL( req.url, "http://127.0.0.1" );
		if ( url.pathname === "/players.html" ) {
			res.writeHead( 302, { Location: "/#recovery" } );
			res.end();
			return;
		}
		if ( url.pathname === "/api/player" ) {
			let rescue;
			if ( req.method === "POST" ) {
				if ( !allowedMutation( req, port, edge ) ) {
					res.writeHead( 403 );
					res.end( "Same-origin console request required" );
					return;
				}
				rescue = await readBody( req );
				if ( !rescue || Array.isArray( rescue ) || typeof rescue !== "object" ) throw Error( "Invalid body" );
				rescue.operator = requestOperator( req, edge );
			} else if ( req.method !== "GET" ) {
				res.writeHead( 405 );
				res.end();
				return;
			}
			const result = await operations.request(
				url.searchParams.get( "shard" ),
				url.searchParams.get( "character" ),
				rescue
			);
			respondJSON( res, result.status, result.body );
			return;
		}
		if (
			url.pathname === "/api/invites" || INVITE_REVOKE.test( url.pathname ) ||
			INVITE_REVEAL.test( url.pathname )
		) {
			const operator = requestOperator( req, edge );
			let result;
			if ( req.method === "GET" && url.pathname === "/api/invites" ) {
				try {
					result = await inviteKeys.list( operator, {
						discordUserId: url.searchParams.get( "discordUserId" ) ?? "",
						status: url.searchParams.get( "status" ) ?? "",
						source: url.searchParams.get( "source" ) ?? "",
						createdFrom: url.searchParams.get( "createdFrom" ) ?? "",
						createdTo: url.searchParams.get( "createdTo" ) ?? "",
						expiresFrom: url.searchParams.get( "expiresFrom" ) ?? "",
						expiresTo: url.searchParams.get( "expiresTo" ) ?? ""
					} );
				} catch {
					result = { status: 502, body: { error: "The website did not answer" } };
				}
			} else if ( req.method === "POST" ) {
				if ( !allowedMutation( req, port, edge ) ) {
					res.writeHead( 403 );
					res.end( "Same-origin console request required" );
					return;
				}
				let body;
				try {
					body = await readBody( req );
				} catch {
					body = null;
				}
				if ( !body || Array.isArray( body ) || typeof body !== "object" ) {
					respondJSON( res, 400, { error: "Invalid request body" } );
					return;
				}
				const revoke = url.pathname.match( INVITE_REVOKE );
				const reveal = url.pathname.match( INVITE_REVEAL );
				try {
					result = revoke ?
						await inviteKeys.revoke( operator, revoke[1] ) :
						reveal ?
						await inviteKeys.reveal( operator, reveal[1] ) :
						await inviteKeys.create( operator, body );
				} catch {
					// A key may or may not have been made: list before trying again.
					result = {
						status: 502,
						body: { error: "The website did not answer; check the key list before retrying" }
					};
				}
			} else {
				res.writeHead( 405 );
				res.end();
				return;
			}
			respondJSON( res, result.status, result.body );
			return;
		}
		// The one economy POST: run the shard's seal census (a read over a
		// fresh store copy; nothing persists). Same-origin staff only.
		if ( url.pathname === "/api/economy/sealcensus" && req.method === "POST" ) {
			if ( !allowedMutation( req, port, edge ) ) {
				res.writeHead( 403 );
				res.end( "Same-origin console request required" );
				return;
			}
			const result = await economy.request( url.pathname, url.searchParams, "POST" );
			respondJSON( res, result.status, result.body );
			return;
		}
		if ( req.method !== "GET" ) {
			res.writeHead( 405 );
			res.end( "Method not allowed" );
			return;
		}
		if ( url.pathname === "/api/console" ) {
			respondJSON( res, 200, {
				operator: edge ? edgeIdentity( req, edge )?.name : LOCAL_OPERATOR,
				role: edge ? edgeIdentity( req, edge )?.role : "operator",
				enabled: Boolean( token ),
				invites: Boolean( siteAdminToken ),
				economy: economy.enabled,
				settings: settings.enabled,
				historySources: historySources.map( ( { id, name } ) => ({ id, name }) ),
				shards: shards.map( ( { id, name } ) => ({ id, name }) )
			} );
			return;
		}
		if ( url.pathname === "/api/history" ) {
			const result = await history.request( url.searchParams );
			respondJSON( res, result.status, result.body );
			return;
		}
		if ( url.pathname.startsWith( "/api/economy/" ) ) {
			const result = await economy.request( url.pathname, url.searchParams );
			respondJSON( res, result.status, result.body );
			return;
		}
		if ( url.pathname === "/api/settings" ) {
			const result = await settings.request( url.searchParams );
			respondJSON( res, result.status, result.body );
			return;
		}
		if ( url.pathname === "/api/snapshot" ) {
			respondJSON( res, 200, await source.snapshot() );
			return;
		}
		if ( url.pathname === "/api/items" ) {
			respondJSON( res, 200, await items.catalog() );
			return;
		}
		if ( /^\/api\/item-icon\/\d+$/.test( url.pathname ) ) {
			const image = await items.icon( Number( url.pathname.split( "/" ).at( -1 ) ) );
			if ( !image ) {
				res.writeHead( 404 );
				res.end( "Icon unavailable" );
				return;
			}
			res.setHeader( "Content-Type", "image/png" );
			res.end( image );
			return;
		}
		if ( artwork.has( url.pathname ) ) {
			let image;
			try {
				image = await readFile( artwork.get( url.pathname ) );
			} catch {
				res.writeHead( 404 );
				res.end( "Artwork unavailable" );
				return;
			}
			res.setHeader( "Content-Type", "image/png" );
			res.end( image );
			return;
		}
		const file = files.get( url.pathname );
		if ( !file ) {
			res.writeHead( 404 );
			res.end( "Not found" );
			return;
		}
		res.setHeader(
			"Content-Type",
			file.endsWith( ".html" ) ?
				"text/html; charset=utf-8" :
				file.endsWith( ".css" ) ?
				"text/css; charset=utf-8" :
				"text/javascript; charset=utf-8"
		);
		res.end( await readFile( new URL( file, publicRoot ) ) );
	} catch {
		if ( !res.headersSent ) {
			respondJSON( res, 502, { error: "Console request failed; inspect the player before retrying a rescue" } );
		} else res.end();
	}
}
const server = createServer( handleRequest );
server.requestTimeout = 20000;
server.headersTimeout = 10000;
server.listen( port, "127.0.0.1", () => console.log( `Silkroad Observatory: http://localhost:${port}` ) );
for ( const signal of [ "SIGINT", "SIGTERM" ] ) process.once( signal, () => server.close( () => process.exit( 0 ) ) );
