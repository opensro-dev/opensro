/*
===========================================================================

dev-lag-proxy.test.mjs - the delay proxy's two close modes

The Agent proxy ({ redial: true }) re-dials per request: its upstream
answers Connection: close, so a pooled downstream socket must survive
each answer and carry the next request to a fresh upstream. The shard
transport proxies (the default) mirror closes instead: a server close
reaches the client once the held bytes are out, and no second upstream
is ever dialled for one downstream.

===========================================================================
*/
import assert from "node:assert/strict";
import net from "node:net";
import { test } from "node:test";

// A proxy that drops a close leaves its client waiting forever: every test
// fails at this bound instead of hanging the runner.
const PROXY_TEST_TIMEOUT_MS = 5000;

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

/*
================
closeServer

Close a server, tolerating one already closed.
================
*/
function closeServer( server ) {
	return new Promise( resolve => {
		if ( !server.listening ) return resolve();
		server.close( () => resolve() );
	} );
}

/*
================
readResponse

One HTTP answer off a pooled socket: the head and content-length bytes
of body, as one string. Resolves from the buffered stream, so the same
socket can be read again for the answer behind it.
================
*/
function readResponse( client ) {
	return new Promise( ( resolve, reject ) => {
		let text = client.__lagText ?? "";
		const drain = () => {
			const headEnd = text.indexOf( "\r\n\r\n" );
			if ( headEnd < 0 ) return false;
			const match = /content-length: (\d+)/i.exec( text.slice( 0, headEnd ) );
			if ( !match ) {
				reject( new Error( "answer without content-length" ) );
				return true;
			}
			const whole = headEnd + 4 + Number( match[1] );
			if ( text.length < whole ) return false;
			client.__lagText = text.slice( whole );
			cleanup();
			resolve( text.slice( 0, whole ) );
			return true;
		};
		const onData = chunk => {
			text += chunk;
			client.__lagText = text;
			drain();
		};
		const onError = error => {
			cleanup();
			reject( error );
		};
		const cleanup = () => {
			client.off( "data", onData );
			client.off( "error", onError );
		};
		client.on( "data", onData );
		client.on( "error", onError );
		if ( text ) drain();
	} );
}

test(
	"an upstream that answers and closes at once still delivers the whole answer",
	{ timeout: PROXY_TEST_TIMEOUT_MS },
	async () => {
		// The upstream writes its answer and closes in the same turn, the shape
		// that dropped held bytes and showed a browser "socket hang up".
		const upstream = net.createServer( socket => {
			// The proxy may close its side abruptly; an unread accepted
			// socket must never raise that as an unhandled error here.
			socket.on( "error", () => {} );
			socket.once( "data", () => {
				socket.end( ANSWER );
			} );
		} );
		const upstreamPort = await listen( upstream );
		const proxy = await delayProxy( 0, `http://127.0.0.1:${upstreamPort}`, DELAY_MS, 0 );
		try {
			const received = await new Promise( ( resolve, reject ) => {
				const client = net.connect(
					proxy.address().port,
					"127.0.0.1",
					() => client.write( "GET / HTTP/1.1\r\n\r\n" )
				);
				let text = "";
				client.on( "data", chunk => text += chunk );
				client.on( "end", () => resolve( text ) );
				client.on( "close", () => resolve( text ) );
				client.on( "error", reject );
			} );
			assert.equal( received, ANSWER );
		} finally {
			await closeServer( proxy );
			await closeServer( upstream );
		}
	}
);

test( "redial: Connection: close answers on a pooled socket, the next request succeeds", {
	timeout: PROXY_TEST_TIMEOUT_MS
}, async () => {
	// The Agent's shape: every answer closes its upstream, while the
	// downstream (vite's pooled hop) writes the next request the moment
	// the answer completes. Each request must reach a fresh upstream on
	// the same downstream socket.
	let accepts = 0;
	const upstream = net.createServer( socket => {
		socket.on( "error", () => {} );
		accepts++;
		socket.once( "data", () => {
			socket.end( ANSWER );
		} );
	} );
	const upstreamPort = await listen( upstream );
	const proxy = await delayProxy( 0, `http://127.0.0.1:${upstreamPort}`, DELAY_MS, 0, { redial: true } );
	const client = net.connect( proxy.address().port, "127.0.0.1" );
	try {
		await new Promise( ( resolve, reject ) => {
			client.once( "connect", resolve );
			client.once( "error", reject );
		} );
		client.write( "GET /one HTTP/1.1\r\n\r\n" );
		assert.equal( await readResponse( client ), ANSWER );
		assert.equal( client.destroyed, false, "the pooled socket survives the first answer" );
		client.write( "GET /two HTTP/1.1\r\n\r\n" );
		assert.equal( await readResponse( client ), ANSWER );
		assert.equal( accepts, 2, "each request dialled its own upstream" );
	} finally {
		client.destroy();
		await closeServer( proxy );
		await closeServer( upstream );
	}
} );

test( "a transport server close reaches the client after the held bytes, on one upstream", {
	timeout: PROXY_TEST_TIMEOUT_MS
}, async () => {
	// A GameWorld close (bye, kick, restart) must end the browser's
	// connection once the bytes still in the delay line are out - and
	// the downstream's later bytes must never dial a second upstream
	// and write mid-stream frames into it.
	const PAYLOAD = "bye-frame";
	let accepts = 0, upstreamEnds = 0;
	const upstream = net.createServer( socket => {
		socket.on( "error", () => {} );
		accepts++;
		socket.on( "end", () => upstreamEnds++ );
		socket.once( "data", () => {
			socket.end( PAYLOAD );
		} );
	} );
	const upstreamPort = await listen( upstream );
	const proxy = await delayProxy( 0, `http://127.0.0.1:${upstreamPort}`, DELAY_MS, 0 );
	const client = net.connect( proxy.address().port, "127.0.0.1", () => client.write( "frame" ) );
	try {
		const received = await new Promise( ( resolve, reject ) => {
			let text = "";
			client.on( "data", chunk => text += chunk );
			client.on( "end", () => resolve( text ) );
			client.on( "error", reject );
		} );
		assert.equal( received, PAYLOAD, "the held bytes arrive before the close does" );
		await new Promise( resolve => setTimeout( resolve, DELAY_MS * 3 ) );
		assert.equal( accepts, 1, "no second upstream was ever dialled" );
		// The other direction mirrors too: a downstream that ends ends
		// its upstream.
		const endsBefore = upstreamEnds;
		const second = net.connect( proxy.address().port, "127.0.0.1" );
		second.on( "error", () => {} );
		await new Promise( ( resolve, reject ) => {
			second.once( "connect", resolve );
			second.once( "error", reject );
		} );
		second.end();
		await new Promise( resolve => setTimeout( resolve, DELAY_MS * 3 ) );
		assert.equal( upstreamEnds, endsBefore + 1, "the downstream's end ended its upstream" );
		second.destroy();
	} finally {
		client.destroy();
		await closeServer( proxy );
		await closeServer( upstream );
	}
} );
