/*
===========================================================================

social-question.ts - the question a guild or party action asks first

The guild window's Withdraw button is UIIT_STT_GUILD_EXPULSION: it bans the
selected member, and like Secede and Disband it asks its native question
before anything is sent. The party ban asks too. The window draws the box
(simpleMessageBox); Yes sends the action, No drops it.

===========================================================================
*/

/*
================
SocialQuestionGuild

What the questions read of the player's own guild.
================
*/
export interface SocialQuestionGuild {
	readonly name?: string;
	readonly members: readonly { readonly id: number; readonly name: string; }[];
}

/*
================
SocialQuestionLine

One line of the question: a text key, and the name its %s stands for.
================
*/
export interface SocialQuestionLine {
	readonly key: string;
	readonly arg?: string;
}

/*
================
SocialQuestion
================
*/
export interface SocialQuestion {
	readonly title: string;
	readonly lines: readonly SocialQuestionLine[];
}

/*
================
socialQuestion

The native question for an armed action: ban (EXPULSION_ANYONE with the
member's name), secede (EXIT1 with the guild's, then EXIT2's 72-hour
warning), disband (BREAK_CONFIRM) and the party ban. Null when nothing is
asked, or a ban names no member. The window resolves the text keys.
================
*/
export function socialQuestion(
	asked: string,
	guild: SocialQuestionGuild | null | undefined,
	member: number
): SocialQuestion | null {
	switch ( asked ) {
		case "guild-kick": {
			const name = guild?.members.find( m => m.id === member )?.name;
			return name === undefined ? null : {
				title: "UIIT_STT_GUILD_EXPULSION",
				lines: [ { key: "UIIT_MSG_GUILD_QUESTION_EXPULSION_ANYONE", arg: name } ]
			};
		}
		case "guild-leave":
			return {
				title: "UIIT_STT_GUILD_EXIT",
				lines: [ { key: "UIIT_MSG_GUILD_QUESTION_EXIT1", arg: guild?.name ?? "" }, {
					key: "UIIT_MSG_GUILD_QUESTION_EXIT2"
				} ]
			};
		case "guild-dissolve":
			return { title: "UIIT_CTL_GUILD_BREAK", lines: [ { key: "UIIT_MSG_GUILD_BREAK_CONFIRM" } ] };
		case "party-kick":
			return { title: "UIIT_STT_AGREEMENT_BOX", lines: [ { key: "UIIT_MSG_PARTY_BAN_REQUEST" } ] };
	}
	return null;
}
