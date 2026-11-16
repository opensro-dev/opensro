/*
===========================================================================

serve.mjs - serve the WebGPU call-cost page cross-origin isolated

Any static file server can serve the page, but then performance.now()
steps in 0.1 ms and the main thread's sub-microsecond costs per call
drown in rounding. This one sends the two headers that make the page
cross-origin isolated, which gives a timer of a few microseconds. It
serves only the page's three files, on loopback, and needs nothing but
Node.

	node tools/perf/constants/serve.mjs [--port 4230]

===========================================================================
*/
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const DEFAULT_PORT = 4230;
const HOST = "127.0.0.1";
const FILES = new Map( [
	[ "/", { name: "index.html", type: "text/html; charset=utf-8" } ],
	[ "/index.html", { name: "index.html", type: "text/html; charset=utf-8" } ],
	[ "/constants.js", { name: "constants.js", type: "text/javascript; charset=utf-8" } ],
	[ "/cases.js", { name: "cases.js", type: "text/javascript; charset=utf-8" } ]
] );
const ISOLATION_HEADERS = {
	"cross-origin-opener-policy": "same-origin",
	"cross-origin-embedder-policy": "require-corp",
	"cache-control": "no-store"
};

/*
================
readPort
================
*/
function readPort( args ) {
	const at = args.indexOf( "--port" );
	if ( at < 0 ) return DEFAULT_PORT;
	const port = Number( args[at + 1] );
	if ( !Number.isInteger( port ) || port < 1 || port > 65535 ) throw Error( `--port needs a port number` );
	return port;
}

/*
================
servePage

Answers one request with one of the page's files, read fresh so an edit
shows on reload, or 404.
================
*/
async function servePage( request, response ) {
	const file = FILES.get( new URL( request.url, `http://${HOST}` ).pathname );
	if ( !file ) {
		response.writeHead( 404 ).end();
		return;
	}
	const body = await readFile( fileURLToPath( new URL( file.name, import.meta.url ) ) );
	response.writeHead( 200, { ...ISOLATION_HEADERS, "content-type": file.type } ).end( body );
}

/*
================
main
================
*/
function main() {
	const port = readPort( process.argv.slice( 2 ) );
	const server = createServer( ( request, response ) => {
		servePage( request, response ).catch( error => {
			response.writeHead( 500 ).end( String( error ) );
		} );
	} );
	server.listen( port, HOST, () => {
		console.log( `http://${HOST}:${port}/  (add ?quick=1 for a smoke run)` );
	} );
}

main();
