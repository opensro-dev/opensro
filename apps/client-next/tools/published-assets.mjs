import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createGzip } from "node:zlib";
// RFC 9110 12.5.3: explicit exclusions override wildcard acceptance.
// Prefer gzip on equal weights; an absent/empty header keeps identity.
export function assetEncoding( header, compress ) {
	const weights = new Map();
	for ( const entry of String( header ?? "" ).split( "," ) ) {
		const [raw, ...parameters] = entry.trim().toLowerCase().split( ";" ), coding = raw.trim();
		if ( !coding ) continue;
		let quality = 1;
		for ( const parameter of parameters ) {
			const pair = parameter.trim().split( "=" );
			if ( pair[0] === "q" ) {
				quality = /^(?:0(?:\.\d{0,3})?|1(?:\.0{0,3})?)$/.test( pair[1] ?? "" ) ?
					Number( pair[1] ) :
					0;
			}
		}
		weights.set( coding, Math.min( weights.get( coding ) ?? 1, quality ) );
	}
	const identity = weights.get( "identity" ) ?? (weights.get( "*" ) === 0 ? 0 : 1);
	const gzip = compress ? (weights.get( "gzip" ) ?? weights.get( "*" ) ?? 0) : 0;
	if ( gzip > 0 && gzip >= identity ) return "gzip";
	return identity > 0 ? "identity" : gzip > 0 ? "gzip" : null;
}
// Dev and preview use the same installed public-asset authority. Build output
// never copies the multi-gigabyte asset tree or starts the legacy application.
export function publishedAssets( root = CLIENT_PUBLIC_ROOT + "/" ) {
	function install( server ) {
		const buildRoot = path.resolve( server.config.root, server.config.build.outDir );
		server.middlewares.use( ( request, response, next ) => {
			let pathname;
			try {
				pathname = decodeURIComponent( new URL( request.url, "http://localhost" ).pathname );
			} catch {
				response.statusCode = 400;
				response.end();
				return;
			}
			if ( !pathname.startsWith( "/assets/" ) ) return next();
			if ( request.method && request.method !== "GET" && request.method !== "HEAD" ) {
				response.statusCode = 405;
				response.setHeader( "Allow", "GET, HEAD" );
				response.end();
				return;
			}
			// Vite owns emitted application chunks, including worker entry points. They
			// share /assets with published game data but are not in that data authority.
			const built = path.resolve( buildRoot, "." + pathname ), builtRelative = path.relative( buildRoot, built );
			if (
				!builtRelative.startsWith( ".." ) && !path.isAbsolute( builtRelative ) && fs.existsSync( built ) &&
				fs.statSync( built ).isFile()
			) {
				if ( /-[\w-]{8,}\.(?:js|css)$/.test( pathname ) ) {
					response.setHeader( "Cache-Control", "public, max-age=31536000, immutable" );
				}
				return next();
			}
			const target = path.resolve( root, "." + pathname ), relative = path.relative( root, target );
			if ( relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
				response.statusCode = 403;
				response.end();
				return;
			}
			fs.stat( target, ( error, stat ) => {
				if ( error || !stat.isFile() ) {
					response.statusCode = 404;
					response.end( "Published asset absent" );
					return;
				}
				if ( response.destroyed ) return;
				const etag = `W/"${stat.size.toString( 16 )}-${stat.mtimeMs.toString( 16 )}-${
					stat.ctimeMs.toString( 16 )
				}"`;
				response.setHeader( "ETag", etag );
				response.setHeader( "Last-Modified", stat.mtime.toUTCString() );
				response.setHeader( "Cache-Control", "no-cache" );
				response.setHeader( "Accept-Ranges", "bytes" );
				response.setHeader(
					"Content-Type",
					target.endsWith( ".json" ) ?
						"application/json" :
						target.endsWith( ".png" ) ?
						"image/png" :
						"application/octet-stream"
				);
				const compress = /\.(?:json|glb)$/i.test( target );
				if ( compress ) response.setHeader( "Vary", "Accept-Encoding" );
				const range = request.headers?.range;
				const encoding = assetEncoding( request.headers?.["accept-encoding"], compress && !range );
				if ( encoding === null ) {
					response.statusCode = 406;
					response.end();
					return;
				}
				const tags = request.headers?.["if-none-match"];
				if (
					tags &&
					(tags === "*" ||
						tags.split( "," ).some( tag =>
							tag.trim().replace( /^W\//, "" ) === etag.replace( /^W\//, "" )
						))
				) {
					response.statusCode = 304;
					response.end();
					return;
				}
				let start = 0, end = stat.size - 1;
				if ( range && !request.headers?.["if-range"] ) {
					const match = /^bytes=(\d+)-(\d*)$/.exec( range );
					if (
						!match || !Number.isSafeInteger( start = Number( match[1] ) ) ||
						!Number.isSafeInteger( end = match[2] ? Number( match[2] ) : end ) || start > end ||
						start >= stat.size
					) {
						response.statusCode = 416;
						response.setHeader( "Content-Range", `bytes */${stat.size}` );
						response.end();
						return;
					}
					end = Math.min( end, stat.size - 1 );
					response.statusCode = 206;
					response.setHeader( "Content-Range", `bytes ${start}-${end}/${stat.size}` );
					response.setHeader( "Cache-Control", "no-store" );
				}
				const gzip = encoding === "gzip";
				if ( gzip ) response.setHeader( "Content-Encoding", "gzip" );
				else response.setHeader( "Content-Length", Math.max( 0, end - start + 1 ) );
				if ( request.method === "HEAD" ) {
					response.end();
					return;
				}
				const stream = fs.createReadStream( target, stat.size ? { start, end } : undefined );
				stream.on( "error", () => response.destroy() );
				response.on( "close", () => stream.destroy() );
				if ( gzip ) {
					const encoder = createGzip();
					encoder.on( "error", () => response.destroy() );
					response.on( "close", () => encoder.destroy() );
					stream.pipe( encoder ).pipe( response );
				} else stream.pipe( response );
			} );
		} );
	}
	return { name: "replacement-published-assets", configureServer: install, configurePreviewServer: install };
}
