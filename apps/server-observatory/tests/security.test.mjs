import {test} from 'node:test';
import assert from 'node:assert/strict';
import {allowedRequest} from '../server/security.mjs';
test('dashboard rejects foreign Host, Origin and forwarded requests',()=>{
 const req=headers=>({headers:{host:'localhost:5190',...headers}});
 assert.ok(allowedRequest(req({}),5190));
 assert.ok(allowedRequest(req({origin:'http://localhost:5190'}),5190));
 for(const headers of [{host:'evil.example:5190'},{origin:'https://evil.example'},{'sec-fetch-site':'cross-site'},{forwarded:'for=127.0.0.1'},{'x-forwarded-for':'127.0.0.1'}])assert.equal(allowedRequest(req(headers),5190),false);
});

test( "a cross-site link may open a console page but never reach the API", () => {
	const nav = { host: "localhost:5190", "sec-fetch-site": "cross-site", "sec-fetch-mode": "navigate", "sec-fetch-dest": "document" };
	const req = ( url, headers, method = "GET" ) => ({ url, method, headers: { ...nav, ...headers } });
	assert.ok( allowedRequest( req( "/board.html", {} ), 5190 ) );
	assert.equal( allowedRequest( req( "/api/board", {} ), 5190 ), false, "an API read stays same-site only" );
	assert.equal( allowedRequest( req( "/board.html", {}, "POST" ), 5190 ), false, "a cross-site form post is refused" );
	assert.equal( allowedRequest( req( "/board.js", { "sec-fetch-mode": "no-cors", "sec-fetch-dest": "script" } ), 5190 ), false );
	assert.equal( allowedRequest( req( "/board.html", { host: "evil.example:5190" } ), 5190 ), false, "the host check still holds" );
} );
