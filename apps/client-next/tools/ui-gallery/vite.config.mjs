/*
===========================================================================

vite.config.mjs - the UI gallery's local server

A developer tool, not a release: it serves the client source and the
published assets on loopback, with no game or API proxy. Requests that
would reach a server are refused, so a window can never talk to a shard.

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

Opens the gallery at / and refuses the game's server routes.
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
				if ( request.url?.startsWith( "/api/" ) || request.url?.startsWith( "/shard/" ) ) {
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
