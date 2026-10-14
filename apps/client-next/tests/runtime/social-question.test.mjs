/*
===========================================================================

social-question.test.mjs - the guild window's Withdraw asks before it bans

The guild window's Withdraw (UIIT_STT_GUILD_EXPULSION) used to arm a hidden
confirmation that nothing drew, so one press did nothing (BR-261014-0515-103B).
Each armed action now names its native question.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { socialQuestion } = await import( sourceFileUrl( "src/engine/foundation/ui/social-question.ts" ).href );

const guild = { name: "Bandits", members: [ { id: 7, name: "Alt$1" } ] };

test("Withdraw asks to ban the selected member, by name", () => {
	assert.deepEqual( socialQuestion( "guild-kick", guild, 7 ), {
		title: "UIIT_STT_GUILD_EXPULSION",
		lines: [ { key: "UIIT_MSG_GUILD_QUESTION_EXPULSION_ANYONE", arg: "Alt$1" } ]
	} );
	// Without a selected member there is nothing to ask.
	assert.equal( socialQuestion( "guild-kick", guild, 9 ), null );
});

test("secede, disband and the party ban ask their native questions", () => {
	assert.deepEqual( socialQuestion( "guild-leave", guild, 0 ).lines, [
		{ key: "UIIT_MSG_GUILD_QUESTION_EXIT1", arg: "Bandits" },
		{ key: "UIIT_MSG_GUILD_QUESTION_EXIT2" }
	] );
	assert.deepEqual( socialQuestion( "guild-dissolve", guild, 0 ).lines, [ { key: "UIIT_MSG_GUILD_BREAK_CONFIRM" } ] );
	assert.deepEqual( socialQuestion( "party-kick", null, 3 ).lines, [ { key: "UIIT_MSG_PARTY_BAN_REQUEST" } ] );
	assert.equal( socialQuestion( "", guild, 7 ), null );
});
