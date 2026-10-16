/*
===========================================================================
vite.config.mjs - local-only review server, independent of game bootstrap

No game/API proxy, no release entry, and no writes to shared assets.
===========================================================================
*/
import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";
import { publishedAssets } from "../published-assets.mjs";
import { clientBuildDefinitions } from "../build-metadata.mjs";
import { UI_QA_PORT, UI_QA_CATALOG_PATH } from "./settings.mjs";
import { readCatalog } from "./catalog.mjs";

/*
================
reviewServer
================
*/
function reviewServer() {
	return {
		name: "local-ui-review",
		configureServer( server ) {
			server.middlewares.use( ( request, response, next ) => {
				response.setHeader(
					"Content-Security-Policy",
					"default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' ws://127.0.0.1:*; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'self'"
				);
				if ( request.url === "/" ) {
					response.writeHead( 302, { Location: "/tools/ui-qa/index.html" } );
					return response.end();
				}
				if ( request.url?.startsWith( "/api/" ) || request.url?.startsWith( "/shard/" ) ) {
					response.writeHead( 403 );
					return response.end( "No game connection in UI review" );
				}
				if ( request.url !== UI_QA_CATALOG_PATH ) return next();
				if ( request.method !== "GET" ) {
					response.writeHead( 405 );
					return response.end();
				}
				try {
					response.writeHead( 200, { "Content-Type": "application/json", "Cache-Control": "no-store" } );
					response.end( JSON.stringify( readCatalog() ) );
				} catch ( error ) {
					response.statusCode = 500;
					response.end( JSON.stringify( { error: String( error ) } ) );
				}
			} );
		}
	};
}

export default defineConfig( ( { command } ) => {
	if ( command !== "serve" ) throw Error( "UI review is a developer server, not a release build" );
	return {
		root: fileURLToPath( new URL( "../../", import.meta.url ) ),
		publicDir: false,
		define: clientBuildDefinitions(),
		server: { host: "127.0.0.1", port: UI_QA_PORT, strictPort: true },
		plugins: [ reviewServer(), publishedAssets() ],
		resolve: { alias: { "@": fileURLToPath( new URL( "../../src", import.meta.url ) ) } }
	};
} );
