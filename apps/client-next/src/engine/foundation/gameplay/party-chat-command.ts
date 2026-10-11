/*
===========================================================================

party-chat-command.ts - the party's chat commands

CIFChatViewer's command parser (6AD990) answers three party commands typed
in chat: "/Invite to party name" invites a visible player, "/Banish from
party name" removes a member (leader only), "/Leave the party" leaves.

The parser splits the line on single spaces (509B30) and compares word 0
with the localized command, so a command whose text holds a space can never
match. Every English party command does. Port-only, not native: the port
matches the command's whole text and takes the rest of the line as the
name, so the commands work in the English client.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import type { SocialState } from "./social";
import { effectivePartyOptions } from "../ui/party-options";

// Party data option bit 4 (6FD830's +8): any member may invite.
const JOIN_ANYONE = 4;

export type PartyChatCommand =
	| { readonly kind: "invite"; readonly name: string; }
	| { readonly kind: "banish"; readonly name: string; }
	| { readonly kind: "leave"; };

/*
================
partyChatCommand

The party command a chat line names, by the localized command words. The
command compares without case, as the premium commands do; the name keeps
what was typed.
================
*/
export function partyChatCommand( text: string, copy: ( symbol: string ) => string ): PartyChatCommand | null {
	const line = text.trim();
	if ( !line.startsWith( "/" ) ) return null;
	const leave = copy( "UIIT_STT_CHAT_COMMAND_PARTY_EXIT" ).trim();
	if ( leave && line.toLowerCase() === leave.toLowerCase() ) return { kind: "leave" };
	for (
		const [symbol, kind] of [
			[ "UIIT_STT_CHAT_COMMAND_PARTY_INVITE", "invite" ],
			[ "UIIT_STT_CHAT_COMMAND_PARTY_EXPEL", "banish" ]
		] as const
	) {
		const command = copy( symbol ).trim();
		if ( !command || !line.toLowerCase().startsWith( command.toLowerCase() + " " ) ) continue;
		const name = line.slice( command.length ).trim();
		if ( name ) return { kind, name };
	}
	return null;
}

/*
================
partyChatOrder

The request 6AD990 makes of a command, or null where it sends nothing:
- leave: only inside a party (0x704F);
- banish: only the leader, and only a member of that name other than
  itself (0x7664 by member id);
- invite: outside a party, or as its leader, or in a party whose options
  let anyone invite; the world core finds the visible player by name.
================
*/
export function partyChatOrder(
	command: PartyChatCommand,
	social: SocialState | undefined,
	formation: number
): GameplayCommand | null {
	const partied = !!social?.leader;
	switch ( command.kind ) {
		case "leave":
			return partied ? { kind: "party-leave" } : null;
		case "banish": {
			if ( !partied || social!.self !== social!.leader ) return null;
			const member = social!.members.find( m => m.name === command.name && m.id !== social!.self );
			return member ? { kind: "party-kick", id: member.id } : null;
		}
		case "invite":
			if ( partied && social!.self !== social!.leader && !(social!.options & JOIN_ANYONE) ) return null;
			return {
				kind: "party-invite-named",
				name: command.name,
				options: effectivePartyOptions( social, formation )
			};
	}
}
