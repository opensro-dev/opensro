/*
===========================================================================

job-party-names.test.mjs - the "*" a job user's name carries

CICharactor_SetName (85ABF0) puts "*" before a suited user's board name.
The party settings (0x35D6 bit 1, 81B710) latch whether the local player is
in job mode; while that flag (SPartyData +0x0C) is set, the party window
(5B93A0) names every member "*%s" and shows no guild.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { socialPacket, emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const { boardText } = await import( "../../src/engine/foundation/ui/name-visibility.ts" );

// The job classes: 1..3 trader, thief, hunter; 4 outside job mode.
const THIEF = 2, NO_JOB = 4;

/*
================
character

A board's owner: its kind and, when it has one, its active job class.
================
*/
/**
 * @param {import("../../src/engine/contracts/world.ts").EntityState["kind"]} kind
 * @param {number} [activeJob]
 */
function character( kind, activeJob ) {
	return activeJob === undefined ? { kind } : { kind, activeJob };
}

/*
================
settings

A 0x35D6 frame carrying only the settings block: leader 11, options 0.
================
*/
function settings() {
	return { opcode: 0x35d6, payload: Uint8Array.of( 1, 11, 0, 0, 0, 0 ) };
}

test("party settings latch the job-party flag from the local job class", () => {
	const suited = socialPacket( emptySocial( "Me" ), settings(), { localJob: THIEF } );
	assert.equal( suited?.jobParty, true );
	const plain = socialPacket( emptySocial( "Me" ), settings(), { localJob: NO_JOB } );
	assert.equal( plain?.jobParty, false );
	const unknown = socialPacket( emptySocial( "Me" ), settings(), {} );
	assert.equal( unknown?.jobParty, false );
});

test("a suited user's board name carries the job-alias star", () => {
	assert.equal( boardText( character( "player", THIEF ), "ty4_goods" ), "*ty4_goods" );
	assert.equal( boardText( character( "local-player", 1 ), "Merchant" ), "*Merchant" );
	assert.equal( boardText( character( "player", NO_JOB ), "Plain" ), "Plain" );
	assert.equal( boardText( character( "player" ), "Unknown" ), "Unknown" );
	assert.equal( boardText( character( "monster", THIEF ), "Mangyang" ), "Mangyang" );
	assert.equal( boardText( character( "player", THIEF ), "" ), "" );
});
