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
// The console roles console-auth names (apps/server-console-auth): every
// route takes either; approver-only routes come with the approval flow.
const CONSOLE_ROLES = [ "operator", "approver" ];
const DISCORD_OPERATOR = /^discord:\d{1,20}$/;
const OPERATOR_NAME_LIMIT = 64;
// The operator of a console reached on loopback without the edge.
export const LOCAL_OPERATOR = "local-operator";

/*
================
edgeIdentity

Who a request through the HTTPS edge is: { operator, name, role }, or
null. With edge.identity "discord", Caddy's forward_auth to console-auth
supplies the person (X-SRO-Operator discord:<id>, the display name and the
role); otherwise the single basic-auth account edge.operator stands for
everyone. The caller has already checked the edge secret.
================
*/
export function edgeIdentity( req, edge ) {
	const operator = req.headers["x-sro-operator"];
	if ( edge.identity !== "discord" ) {
		return operator === edge.operator ? { operator, name: operator, role: "operator" } : null;
	}
	const role = req.headers["x-sro-role"];
	if ( typeof operator !== "string" || !DISCORD_OPERATOR.test( operator ) || !CONSOLE_ROLES.includes( role ) ) {
		return null;
	}
	let name = operator;
	try {
		name = decodeURIComponent( req.headers["x-sro-operator-name"] ?? "" ).slice( 0, OPERATOR_NAME_LIMIT ) ||
			operator;
	} catch {}
	return { operator, name, role };
}

/*
================
requestOperator

The operator an audit row names: the edge identity, or LOCAL_OPERATOR on
loopback. Call only for an allowed request.
================
*/
export function requestOperator( req, edge ) {
	return edge ? edgeIdentity( req, edge )?.operator ?? "" : LOCAL_OPERATOR;
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
			sameSecret( req.headers["x-sro-console-auth"] ?? "", edge.secret ) &&
			edgeIdentity( req, edge ) !== null;
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
