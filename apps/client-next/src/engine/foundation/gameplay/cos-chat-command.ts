/*
===========================================================================

cos-chat-command.ts - the pet's chat commands (UIIT_KEY_COS_*)

6AD990's compare chain ends with nine pet commands, matched on word 0 of
the line against the localized command text (516AA0 equality on the
796390-resolved string) and consumed whether or not a pet answers. All
nine shipped texts are single tokens with the slash, so unlike the
English party commands they match in every locale. The port parses them
here; ui.ts routes each onto the dispatch the COS command bar already
uses, with stance explicit and charm on the quickslot's action 5000.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import type { CosCommandContext } from "../ui/cos-command";

export type CosChatCommand =
	| { readonly kind: "attack"; }
	| { readonly kind: "return"; }
	| { readonly kind: "ride"; }
	| { readonly kind: "clean"; }
	| { readonly kind: "follow"; }
	| { readonly kind: "aggressive"; }
	| { readonly kind: "defensive"; }
	| { readonly kind: "charm"; };

// 6AE823-6AE9EA: the chain order is native; board and dismount are the
// same mode-1 toggle (6F2520 mode 1), so both words return "ride".
const COMMANDS = [
	[ "UIIT_KEY_COS_ATTACK", "attack" ],
	[ "UIIT_KEY_COS_RETURN", "return" ],
	[ "UIIT_KEY_COS_RIDING", "ride" ],
	[ "UIIT_KEY_COS_DISMOUNT", "ride" ],
	[ "UIIT_KEY_COS_LEAVEOUT", "clean" ],
	[ "UIIT_KEY_COS_FOLLOW", "follow" ],
	[ "UIIT_KEY_COS_AGGRESSIVE", "aggressive" ],
	[ "UIIT_KEY_COS_DEFENSIVE", "defensive" ],
	[ "UIIT_KEY_COS_CHARM", "charm" ]
] as const;

/*
================
cosChatCommand

The pet command a chat line names, by word 0 against the localized
command words. The commands take no argument, so trailing words are
ignored, as 6AD990 ignores them. The compare is without case, as the
party and premium parsers already do here (native 516AA0 is exact).
================
*/
export function cosChatCommand( text: string, copy: ( symbol: string ) => string ): CosChatCommand | null {
	const line = text.trim();
	if ( !line.startsWith( "/" ) ) return null;
	const word = line.split( " ", 1 )[0]!.toLowerCase();
	for ( const [symbol, kind] of COMMANDS ) {
		const command = copy( symbol ).trim().toLowerCase();
		if ( command && word === command ) return { kind };
	}
	return null;
}

/*
================
cosChatStance

The order a stance word makes, or null where 6A2350 case 6 sends
nothing: the pet alive under a living owner, a band that takes a
stance (82D330 admits bands 3 and 4; the command bar's own stance
gate admits band 3 only), the mode known, and the stance actually
changing (+0xAB08 is written only on a change). The worker throws on
the states refused here, so the refusal belongs on this side.
================
*/
export function cosChatStance(
	kind: "aggressive" | "defensive",
	context: CosCommandContext
): GameplayCommand | null {
	const record = context.record, mode = kind === "aggressive" ? 1 : 0;
	if ( !record || context.ownerDead || record.dead || record.hp <= 0 ) return null;
	if ( record.band !== 3 && record.band !== 4 ) return null;
	if ( record.commandMode === undefined || record.commandMode === mode ) return null;
	return { kind: "cos-behavior", gid: record.gid, mode };
}
