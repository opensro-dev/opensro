/*
===========================================================================

dev-lag-proxy.test.mjs - a delayed answer survives its server's close

===========================================================================
*/
import assert from "node:assert/strict";
import net from "node:net";
import { test } from "node:test";

const { delayProxy } = await import( "../../tools/dev/dev-lag.mjs" );

const DELAY_MS = 80;
const ANSWER = "HTTP/1.1 200 OK\r\ncontent-length: 5\r\nconnection: close\r\n\r\nhello";

/*
================
listen

A loopback server on a free port; resolves with it and its port.
================
*/
function listen( server ) {
	return new Promise( resolve => server.listen( 0, "127.0.0.1", () => resolve( server.address().port ) ) );
}

test("an upstream that answers and closes at once still delivers the whole answer", async () => {
	// The upstream writes its answer and closes in the same turn, the shape
	// that dropped held bytes and showed a browser "socket hang up".
	const upstream = net.createServer( socket => {
		socket.once( "data", () => {
			socket.end( ANSWER );
		} );
	} );
	const upstreamPort = await listen( upstream );
	const probe = net.createServer();
	const proxyPort = await listen( probe );
	await new Promise( resolve => probe.close( resolve ) );
	const proxy = await delayProxy( proxyPort, `http://127.0.0.1:${upstreamPort}`, DELAY_MS, 0 );
	try {
		const received = await new Promise( ( resolve, reject ) => {
			const client = net.connect( proxyPort, "127.0.0.1", () => client.write( "GET / HTTP/1.1\r\n\r\n" ) );
			let text = "";
			client.on( "data", chunk => text += chunk );
			client.on( "end", () => resolve( text ) );
			client.on( "close", () => resolve( text ) );
			client.on( "error", reject );
		} );
		assert.equal( received, ANSWER );
	} finally {
		await new Promise( resolve => proxy.close( resolve ) );
		await new Promise( resolve => upstream.close( resolve ) );
	}
});
