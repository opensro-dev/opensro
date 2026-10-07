/*
===========================================================================

perf-build-identity.test.mjs - identify the served page, not the harness tree

Execute the real page callback in an isolated browser-like context. The
fixture controls script URLs and capture state without rewriting sources.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { createContext, runInContext } from "node:vm";
import { buildIdentity, verifyMeasuredIdentity } from "../../tools/perf/core/build-identity.mjs";

/*
================
fixture
================
*/
function fixture( scripts = [ "/assets/index-old-build.js" ], video = /** @type {any} */ (null) ) {
	const state = { scripts, video, timeOrigin: 1000, preference: null };
	const context = createContext( {
		URL,
		location: { origin: "http://other-checkout.test", href: "http://other-checkout.test/play" },
		performance: {
			get timeOrigin() {
				return state.timeOrigin;
			}
		},
		localStorage: { getItem: () => state.preference },
		document: {
			querySelectorAll: () => state.scripts.map( src => ({ getAttribute: () => src }) ),
			querySelector: selector => selector === "video.sro-replay-source" ? state.video : { srcObject: {} }
		}
	} );
	return {
		state,
		read: () =>
			buildIdentity( {
				evaluate: async fn => runInContext( `(${fn.toString()})()`, context )
			}, "new-harness-commit" )
	};
}

/*
================
capture
================
*/
function capture( extra = {} ) {
	return {
		paused: false,
		ended: false,
		readyState: 2,
		currentTime: 1,
		srcObject: { getVideoTracks: () => [ { readyState: "live" } ] },
		...extra
	};
}

test("another checkout or stale bundle never inherits the harness commit", async () => {
	const identity = await fixture().read();
	assert.equal( identity.entry, "http://other-checkout.test/assets/index-old-build.js" );
	assert.equal( identity.harnessCommit, "new-harness-commit" );
	assert.equal( identity.servedCommit, "unknown" );
	assert.equal( identity.build, "bundle" );
	assert.equal( identity.replay, "off", "unrelated videos do not imply replay capture" );
});

test("Vite is development and an unrecognized page is unknown", async () => {
	assert.equal( (await fixture( [ "/@vite/client", "/src/bootstrap.ts" ] ).read()).build, "dev-server" );
	const identity = await fixture( [ "/other.js" ] ).read();
	assert.equal( identity.build, "unknown" );
	assert.throws( () => verifyMeasuredIdentity( identity, identity ), /unknown/ );
});

for (
	const extra of [ { paused: true }, { ended: true }, { readyState: 1 }, { srcObject: null }, {
		srcObject: { getVideoTracks: () => [ { readyState: "ended" } ] }
	} ]
) {
	test(`inactive recorder is unknown: ${JSON.stringify( extra )}`, async () => {
		const identity = await fixture( undefined, capture( extra ) ).read();
		assert.equal( identity.replay, "unknown" );
		assert.throws( () => verifyMeasuredIdentity( identity, identity ), /unknown/ );
	});
}

test("capture must advance between measurement boundaries", async () => {
	const video = capture(), f = fixture( undefined, video );
	const before = await f.read();
	assert.equal( before.replay, "capture-playing" );
	assert.throws( () => verifyMeasuredIdentity( before, before ), /did not advance/ );
	video.currentTime = 2;
	verifyMeasuredIdentity( before, await f.read() );
});

test("late recorder startup, reload and changed entry invalidate a measurement", async () => {
	const f = fixture(), before = await f.read();
	verifyMeasuredIdentity( before, await f.read() );
	f.state.video = capture();
	const started = await f.read();
	assert.throws( () => verifyMeasuredIdentity( before, started ), /replay/ );
	f.state.video = null;
	f.state.timeOrigin++;
	const reloaded = await f.read();
	assert.throws( () => verifyMeasuredIdentity( before, reloaded ), /documentTimeOrigin/ );
	f.state.scripts = [ "/assets/index-new-build.js" ];
	const after = await f.read();
	assert.throws( () => verifyMeasuredIdentity( before, after ), /entry/ );
});
