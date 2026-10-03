/*
===========================================================================
security.mjs - local access and the authenticated HTTPS edge boundary
===========================================================================
*/
import { timingSafeEqual, createHash } from "node:crypto";
/*
================
sameSecret
================
*/
function sameSecret( left, right ) {
	return timingSafeEqual(
		createHash( "sha256" ).update( left ).digest(),
		createHash( "sha256" ).update( right ).digest()
	);
}
/*
================
allowedRequest
================
*/
export function allowedRequest( req, port, edge = null ) {
	if ( req.headers["sec-fetch-site"] === "cross-site" ) return false;
	if ( edge ) {
		return [ "127.0.0.1", "::1", "::ffff:127.0.0.1" ].includes( req.socket?.remoteAddress ) &&
			req.headers.host === new URL( edge.origin ).host &&
			(!req.headers.origin || req.headers.origin === edge.origin) &&
			req.headers["x-sro-operator"] === edge.operator &&
			sameSecret( req.headers["x-sro-console-auth"] ?? "", edge.secret );
	}
	const hosts = [ `127.0.0.1:${port}`, `localhost:${port}` ];
	return hosts.includes( req.headers.host ) &&
		(!req.headers.origin || hosts.map( h => "http://" + h ).includes( req.headers.origin )) &&
		!req.headers.forwarded && !req.headers["x-forwarded-for"];
}
/*
================
allowedMutation
================
*/
export function allowedMutation( req, port, edge ) {
	const origins = edge ? [ edge.origin ] : [ `http://localhost:${port}`, `http://127.0.0.1:${port}` ];
	return allowedRequest( req, port, edge ) && origins.includes( req.headers.origin ) &&
		req.headers["x-sro-console"] === "1" &&
		req.headers["content-type"] === "application/json";
}
