/*
===========================================================================

entry-refusal.test.mjs - a refused world entry shows its native message

The GameWorld refuses an entry with a native error code: 0x11 when the
account already has another character in the world (a second browser
profile on the same account, account_world.go). The character screen
shows that code's message (characterStatus, SRO_Client 0x73EE40), not
the generic "Failed to connect to server. (S2)", which stays for a
failure that named no code.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { entryFixture } from "../helpers/entry-fixture.mjs";

const NATIVE_ERROR_OVERLAP = 0x11;

/*
================
refuseEntry

Starts an entry from the dock and answers it with a refusal carrying
entryFailure (or none); returns the frontend snapshot and the report.
================
*/
function refuseEntry( entryFailure ) {
	const sent = [];
	const f = entryFixture( false, sent );
	f.toDock();
	f.frontend.select( "fixture" );
	for ( let i = 0; i < 30; i++ ) f.step();
	assert.equal( f.frontend.start(), true );
	f.bind( "connecting" );
	f.step();
	f.session = {
		...f.session,
		phase: "character-select",
		revision: f.session.revision + 1,
		error: "EnterWorld rejected",
		...(entryFailure ? { entryFailure } : {})
	};
	const refused = f.step();
	const reports = sent.filter( command => command.kind === "client-report" );
	f.frontend.dispose();
	return { refused, reports };
}

/*
================
an account already in the world
================
*/
test("an entry refused because the account is already in the world says so", () => {
	const { refused, reports } = refuseEntry( {
		stage: "entering-world",
		bytesReceived: 9,
		framesReceived: 2,
		closeReason: "enter_world_refused",
		nativeErrorCode: NATIVE_ERROR_OVERLAP
	} );
	assert.deepEqual( refused.status, { key: "UIO_MSG_ERROR_OVERLAP", suffix: "" } );
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].code, "world_entry_rejected" );
	assert.match( reports[0].message, /^Entry rejected \(S17\) at dock\// );
});

/*
================
a failure that names no code
================
*/
test("a failure that names no code keeps the generic (S2)", () => {
	for ( const entryFailure of [ undefined, { stage: "connecting", bytesReceived: 0, framesReceived: 0 } ] ) {
		const { refused, reports } = refuseEntry( entryFailure );
		assert.deepEqual( refused.status, { key: "UIO_MSG_ERROR_SEVER_CONNECT", suffix: "(S2)" } );
		assert.match( reports[0].message, /^Entry rejected \(S2\) at dock\// );
	}
});
