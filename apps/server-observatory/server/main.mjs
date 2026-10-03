/*
===========================================================================
main.mjs - loopback observatory and authenticated player recovery gateway
===========================================================================
*/
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { loadShards, createSource } from "./source.mjs";
import { allowedRequest, allowedMutation } from "./security.mjs";
import { createItems } from "./items.mjs";
import { createPlayerOperations, readBody } from "./player-operations.mjs";
const port = Number( process.env.SRO_OBSERVATORY_PORT ?? 5190 );
if ( !Number.isInteger( port ) || port < 1024 || port > 65535 ) throw Error( "Invalid dashboard port" );
const config = process.env.SRO_OBSERVATORY_CONFIG ?
	JSON.parse( await readFile( process.env.SRO_OBSERVATORY_CONFIG, "utf8" ) ) :
	{};
const edge = config.edge ?? null;
if (
	edge &&
	(new URL( edge.origin ).protocol !== "https:" || typeof edge.secret !== "string" || edge.secret.length < 32 ||
		!edge.operator)
) throw Error( "Invalid console edge configuration" );
const token = config.operatorTokenFile ? (await readFile( config.operatorTokenFile, "utf8" )).trim() : "";
const shards = await loadShards( config.shards ?? new URL( "../../server/config/shards.json", import.meta.url ) );
const source = createSource( shards ), operations = createPlayerOperations( shards, token ), items = createItems();
const publicRoot = new URL( "../public/", import.meta.url ), files = new Map( [ [ "/", "index.html" ] ] );
for (
	const file of [
		"app.js",
		"styles.css",
		"view.js",
		"model.js",
		"operations.js",
		"theme.css",
		"items.js",
		"item-model.js",
		"items.css",
		"players.html",
		"players.js",
		"players.css"
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
				rescue.operator = edge?.operator ?? "local-operator";
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
		if ( req.method !== "GET" ) {
			res.writeHead( 405 );
			res.end( "Method not allowed" );
			return;
		}
		if ( url.pathname === "/api/console" ) {
			respondJSON( res, 200, {
				operator: edge?.operator ?? "local-operator",
				enabled: Boolean( token ),
				shards: shards.map( ( { id, name } ) => ({ id, name }) )
			} );
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
