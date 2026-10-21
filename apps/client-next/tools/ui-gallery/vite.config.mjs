/*
===========================================================================

vite.config.mjs - the UI gallery's local server

A developer tool, not a release: it serves the client source and the
published assets on loopback. It has no proxy, so no request can reach a
game server: /api and /shard answer 403 with a reason, and any other
server route (a shard's own path) is simply not found.

===========================================================================
*/
import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";
import { publishedAssets } from "../published-assets.mjs";
import { clientBuildDefinitions } from "../build-metadata.mjs";
import { UI_GALLERY_PORT } from "./settings.mjs";

/*
================
galleryServer

Opens the gallery at / and answers the Agent and shard prefixes with 403.
================
*/
function galleryServer() {
	return {
		name: "ui-gallery",
		configureServer( server ) {
			server.middlewares.use( ( request, response, next ) => {
				response.setHeader(
					"Content-Security-Policy",
					"default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' ws://127.0.0.1:*; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'self'"
				);
				if ( request.url === "/" ) {
					response.writeHead( 302, { Location: "/tools/ui-gallery/index.html" } );
					return response.end();
				}
				if ( /^\/(api|shard)(\/|\?|$)/i.test( request.url ?? "" ) ) {
					response.writeHead( 403 );
					return response.end( "The UI gallery has no game connection" );
				}
				next();
			} );
		}
	};
}

export default defineConfig( ( { command } ) => {
	if ( command !== "serve" ) throw Error( "The UI gallery is a developer server, not a release build" );
	return {
		root: fileURLToPath( new URL( "../../", import.meta.url ) ),
		publicDir: false,
		define: clientBuildDefinitions(),
		server: { host: "127.0.0.1", port: UI_GALLERY_PORT, strictPort: true },
		plugins: [ galleryServer(), publishedAssets() ],
		resolve: { alias: { "@": fileURLToPath( new URL( "../../src", import.meta.url ) ) } }
	};
} );
