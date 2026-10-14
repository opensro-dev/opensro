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

const text = {
	UIIT_MSG_GUILD_QUESTION_EXPULSION_ANYONE: "Will you ban [%s]from the guild?",
	UIIT_MSG_GUILD_QUESTION_EXIT1: "Will you leave [%s]guild?",
	UIIT_MSG_GUILD_QUESTION_EXIT2: "Cannot join another guild for 72 hours if you leave your guild.",
	UIIT_MSG_GUILD_BREAK_CONFIRM: "Will you disband the guild?",
	UIIT_MSG_PARTY_BAN_REQUEST: "Ban from the party?"
};
const copy = key => text[key] ?? key;
const guild = { name: "Bandits", members: [ { id: 7, name: "Alt$1" } ] };

test("Withdraw asks to ban the selected member, by name", () => {
	assert.deepEqual( socialQuestion( "guild-kick", guild, 7, copy ), {
		title: "UIIT_STT_GUILD_EXPULSION",
		lines: [ "Will you ban [Alt$1]from the guild?" ]
	} );
	// Without a selected member there is nothing to ask.
	assert.equal( socialQuestion( "guild-kick", guild, 9, copy ), null );
});

test("secede, disband and the party ban ask their native questions", () => {
	assert.deepEqual( socialQuestion( "guild-leave", guild, 0, copy ).lines, [
		"Will you leave [Bandits]guild?",
		"Cannot join another guild for 72 hours if you leave your guild."
	] );
	assert.deepEqual( socialQuestion( "guild-dissolve", guild, 0, copy ).lines, [ "Will you disband the guild?" ] );
	assert.deepEqual( socialQuestion( "party-kick", undefined, 3, copy ).lines, [ "Ban from the party?" ] );
	assert.equal( socialQuestion( "", guild, 7, copy ), null );
});
