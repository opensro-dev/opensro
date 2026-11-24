/*
===========================================================================

stuck.fixture.mjs - one passing and one stuck browser test, faked

probeFailureCapture.test.mjs runs this under node --test in a scratch
directory and reads what it left. The page stand-in answers a screenshot
and the diagnostics text and emits Playwright's request events.

===========================================================================
*/
import test from "node:test";
import { EventEmitter } from "node:events";
import { watchProbeBrowser } from "../../../lib/probeFailureCapture.mjs";

export const PNG = Buffer.from( [ 0x89, 0x50, 0x4e, 0x47 ] );

/*
================
fakeLaunch
================
*/
function fakeLaunch() {
	const page = new EventEmitter();
	let closed = false;
	page.isClosed = () => closed;
	page.screenshot = async () => PNG;
	page.evaluate = async () => "url: http://probe/\nstatus: signed-out\n\nSession: signed-out";
	const browser = {
		async close() {
			closed = true;
			page.emit( "close" );
		}
	};
	watchProbeBrowser( browser, page );
	return { browser, page };
}

/*
================
request
================
*/
function request( url ) {
	return { method: () => "GET", url: () => url };
}

test("passes", async () => {
	const { browser } = fakeLaunch();
	await browser.close();
});

test("stuck", async () => {
	const { browser, page } = fakeLaunch();
	try {
		page.emit( "request", request( "http://probe/src/main.ts" ) );
		const answered = request( "http://probe/api/title/servers" );
		page.emit( "request", answered );
		page.emit( "requestfinished", answered );
		throw new Error( "locator.click: Timeout exceeded" );
	} finally {
		await browser.close();
	}
});

test("stuck with the page left open", async () => {
	const { page } = fakeLaunch();
	page.emit( "request", request( "http://probe/src/held.ts" ) );
	throw new Error( "test timed out" );
});
