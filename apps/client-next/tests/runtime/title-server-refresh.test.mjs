/*
===========================================================================

title-server-refresh.test.mjs - an all-"Check" server list keeps asking

During maintenance the edge lists every shard as not operating (native
"Check", CPSTitle 0x747E1F). The open list asks again until one runs, so it
turns live without the player reopening it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { SERVER_CHECK_RETRY_MS, serverListRefreshDue } = await import(
	"../../src/engine/foundation/ui/title-status.ts"
);

const down = [ { operating: false }, { operating: false } ];

test("an open list of shards that are all down asks again on a cadence", () => {
	assert.equal( serverListRefreshDue( down, true, false, 1000, 0 ), 1000 + SERVER_CHECK_RETRY_MS );
	assert.equal( serverListRefreshDue( down, true, false, 1000, 3000 ), null, "not before the retry time" );
	assert.equal( serverListRefreshDue( down, true, true, 9000, 0 ), null, "never over a request in flight" );
});

test("a running shard, a closed list or no rows asks nothing", () => {
	assert.equal( serverListRefreshDue( [ { operating: false }, { operating: true } ], true, false, 9000, 0 ), null );
	assert.equal( serverListRefreshDue( down, false, false, 9000, 0 ), null );
	assert.equal( serverListRefreshDue( [], true, false, 9000, 0 ), null );
});
