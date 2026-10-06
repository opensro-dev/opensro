import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readFileSync, statSync } from "node:fs";
import { createHash, randomUUID } from "node:crypto";
import { fileURLToPath } from "node:url";

// Development adapter only: a document keeps its entire runtime generation.
export function devUpdates() {
	const sessionId = randomUUID(), history = [], tracked = new Map();
	let generation = 0, server;
	const endpoint = "/__client-next-dev-session", client = "/__client-next-dev-updates.js";
	const manifest = CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json";
	let manifestStamp, manifestDigest;
	const digest = value => createHash( "sha256" ).update( value ).digest( "hex" );
	function resources() {
		try {
			const stat = statSync( manifest ), stamp = `${stat.mtimeMs}:${stat.ctimeMs}:${stat.size}`;
			if ( stamp !== manifestStamp ) {
				manifestDigest = digest( readFileSync( manifest ) );
				manifestStamp = stamp;
			}
			return manifestDigest;
		} catch {
			return "unavailable";
		}
	}
	const snapshot = () => ({ sessionId, generation, resources: resources() });
	const normalize = file => file.replaceAll( "\\", "/" );
	function bump( file ) {
		const nodes = server.moduleGraph.getModulesByFile( normalize( file ) );
		if (
			!tracked.has( normalize( file ) ) &&
			!normalize( file ).startsWith( normalize( server.config.root ) + "/src/" ) &&
			!/\/index.html$/.test( normalize( file ) )
		) return;
		generation++;
		// Worker imports have their own timing buffer, invisible to the document.
		const visited = new Set();
		function workerDependency( node ) {
			if ( visited.has( node ) ) return false;
			visited.add( node );
			return /\/worker\//.test( node.url ) || [ ...(node.importers ?? []) ].some( workerDependency );
		}
		const worker = /\/worker\//.test( normalize( file ) ) || [ ...(nodes ?? []) ].some( workerDependency );
		history.push( {
			generation,
			paths: !worker && nodes?.size ? [ ...nodes ].map( n => n.url.split( "?" )[0] ) : null
		} );
		if ( history.length > 500 ) history.shift();
	}
	function reconcile( file ) {
		const key = normalize( file ), previous = tracked.get( key );
		if ( !previous ) return;
		try {
			const next = digest( readFileSync( file ) );
			if ( next !== previous ) {
				bump( file );
				tracked.set( key, next );
				for ( const env of Object.values( server.environments ?? {} ) ) env.moduleGraph.onFileChange( key );
				server.moduleGraph.onFileChange( key );
			}
		} catch {}
	}
	function changed( since ) {
		if (
			!Number.isSafeInteger( since ) || since < 0 || since > generation ||
			since < (history[0]?.generation ?? generation + 1) - 1
		) return null;
		const rows = history.filter( r => r.generation > since );
		return rows.some( r => r.paths === null ) ? null : [ ...new Set( rows.flatMap( r => r.paths ) ) ];
	}
	return {
		name: "client-next-dev-updates",
		apply: "serve",
		enforce: "pre",
		config: () => ({ server: { hmr: false, watch: { ignored: [ "**/temp/**" ] } } }),
		transform( code, id ) {
			const file = normalize( id.split( "?" )[0] );
			if (
				!file.includes( "/node_modules/" ) && /\.(?:[cm]?[jt]sx?|css|json)$/.test( file ) &&
				!file.startsWith( "\0" )
			) {
				try {
					tracked.set( file, digest( readFileSync( file ) ) );
				} catch {}
			}
		},
		transformIndexHtml: {
			order: "post",
			handler( html ) {
				// No Vite websocket client: even reconnect/config changes cannot reload a tab.
				// Repeat until stable so a removal can never splice a new tag together.
				let clean = html, previous;
				do {
					previous = clean;
					clean = clean.replace( /<script\b[^>]*\bsrc=["']\/@vite\/client["'][^>]*>\s*<\/script>/g, "" );
				} while ( clean !== previous );
				return {
					html: clean,
					tags: [ {
						tag: "script",
						injectTo: "head-prepend",
						children: "performance.setResourceTimingBufferSize(20000);"
					}, {
						tag: "script",
						injectTo: "head-prepend",
						children: `import {installDevUpdates} from ${JSON.stringify( client )};installDevUpdates(${
							JSON.stringify( snapshot() )
						},${JSON.stringify( endpoint )});`,
						attrs: { type: "module" }
					} ]
				};
			}
		},
		configureServer( value ) {
			server = value;
			const change = file => {
				bump( file );
				const key = normalize( file );
				if ( tracked.has( key ) ) {
					try {
						tracked.set( key, digest( readFileSync( file ) ) );
					} catch {}
				}
			};
			server.watcher.on( "change", change );
			server.watcher.on( "unlink", change );
			server.watcher.on( "add", change );
			server.httpServer?.once( "close", () => {
				for ( const event of [ "change", "unlink", "add" ] ) server.watcher.off( event, change );
			} );
			server.middlewares.use( ( req, res, next ) => {
				const url = new URL( req.url ?? "/", "http://localhost" );
				if ( url.pathname === client ) {
					res.setHeader( "Content-Type", "text/javascript" );
					res.setHeader( "Cache-Control", "no-store" );
					res.end( readFileSync( new URL( "./updates-client.mjs", import.meta.url ) ) );
					return;
				}
				// Catch missed Windows watcher events before serving a fresh document or poll.
				if ( url.pathname === endpoint || url.pathname === "/" || url.pathname === "/index.html" ) {
					for ( const file of tracked.keys() ) {
						reconcile( file );
					}
				}
				if ( url.pathname !== endpoint ) return next();
				res.setHeader( "Content-Type", "application/json" );
				res.setHeader( "Cache-Control", "no-store" );
				res.end(
					JSON.stringify( {
						...snapshot(),
						changed: changed(
							url.searchParams.has( "since" ) ? Number( url.searchParams.get( "since" ) ) : NaN
						)
					} )
				);
			} );
		}
	};
}
