/*
===========================================================================

guild-names.ts - the client's checks on a typed guild name or nickname

Three native inputs check their text before anything is sent, each with
the filter's guild-name column (CStringCheck_IsAllowedGuildName 790A30):

	create name   CIFGuild_OnCreateNameDialogResult 5D3D10: CP949 length
	              outside 2..12 bytes -> 0x10/0x18 (the box stays open), a
	              refused name -> 0x10/0x19, else box 0x11 NAME_CONFIRM, then
	              box 0x12 (the cost), then 0x7663, whose sender 700590
	              checks the character-name column once more (0x10/0x19).
	nickname      CIFGuild_OnGrantNameDialogResult 5E35E0: more than 12
	              characters -> 0x10/0x4D (the box stays open), a refused
	              name -> 0x10/0x4D, else box 0x17 QUESTION_NAME_GRANT, then
	              0x72BC.
	war request   CIFRequestGuildWar_OnCommand 619620 mode 1, in order: no
	              name, one's own guild, CP949 length outside 2..12, a refused
	              name, a stake above 500,000,000. Each refusal is written to
	              the chat and the centre notice (ChatSystemMessage and
	              ShowNoticeText).

===========================================================================
*/
import { checkedNameError, type NameRules } from "@/engine/foundation/ui/character-create";
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

// The guild notices' category and the three name codes it uses here.
const GUILD_NOTICE_CATEGORY = 0x10;
const GUILD_NAME_LENGTH = 0x18;
const GUILD_NAME_REFUSED = 0x19;
const GUILD_GRANT_NAME_REFUSED = 0x4d;
// 619620's compensation refusal (68AA9C formats the limit into it).
const GUILD_WAR_STAKE_REFUSED = 0x5e;
// 5D3D10 and 619620: `bytes - 2 > 10` refuses, so 2..12 CP949 bytes pass.
const GUILD_NAME_MIN_BYTES = 2;
const GUILD_NAME_MAX_BYTES = 12;
// 5E35E0 measures the wide string: at most 12 characters.
const GUILD_GRANT_NAME_MAX = 12;
// 619620: `*(this + 0x7B4) <= 0x1DCD6500`.
const GUILD_WAR_STAKE_MAX = 500_000_000;

/*
================
GuildNameCheck

What a typed name does: a notice to show (null for none), whether the
input box stays open, and whether the name goes on to its question.
================
*/
export interface GuildNameCheck {
	readonly notice: SystemNotice | null;
	readonly keepOpen: boolean;
	readonly ask: boolean;
}

/*
================
cp949Length

The text's length once WideCharToMultiByte makes it CP949, as 5D3D10 and
619620 measure it: one byte for ASCII, two for every other character
(CP949 has no wider form). Inference: a character CP949 lacks would become
one '?', but the guild filter's character table refuses those first.
================
*/
export function cp949Length( text: string ): number {
	let bytes = 0;
	for ( const c of text ) bytes += c.codePointAt( 0 )! < 0x80 ? 1 : 2;
	return bytes;
}

/*
================
guildNameAllowed

790A30: every character in the filter's table and no forbidden word of the
guild-name column.
================
*/
export function guildNameAllowed( name: string, rules: NameRules ): boolean {
	return checkedNameError( name, rules ) === null;
}

/*
================
guildCreateNameCheck

5D3D10, the create box's OK.
================
*/
export function guildCreateNameCheck( name: string, guildRules: NameRules ): GuildNameCheck {
	const bytes = cp949Length( name );
	if ( bytes < GUILD_NAME_MIN_BYTES || bytes > GUILD_NAME_MAX_BYTES ) {
		return { notice: constantNativeNotice( GUILD_NOTICE_CATEGORY, GUILD_NAME_LENGTH ), keepOpen: true, ask: false };
	}
	if ( !guildNameAllowed( name, guildRules ) ) {
		return {
			notice: constantNativeNotice( GUILD_NOTICE_CATEGORY, GUILD_NAME_REFUSED ),
			keepOpen: false,
			ask: false
		};
	}
	return { notice: null, keepOpen: false, ask: true };
}

/*
================
guildCreateSendRefusal

700590 checks the name against the character-name column before 0x7663;
the notice when it refuses, else null.
================
*/
export function guildCreateSendRefusal( name: string, nameRules: NameRules ): SystemNotice | null {
	return checkedNameError( name, nameRules ) ?
		constantNativeNotice( GUILD_NOTICE_CATEGORY, GUILD_NAME_REFUSED ) :
		null;
}

/*
================
guildGrantNameCheck

5E35E0, the nickname box's OK. An empty nickname passes the filter and is
asked about as any other.
================
*/
export function guildGrantNameCheck( name: string, guildRules: NameRules ): GuildNameCheck {
	const refused = constantNativeNotice( GUILD_NOTICE_CATEGORY, GUILD_GRANT_NAME_REFUSED );
	if ( name.length > GUILD_GRANT_NAME_MAX ) return { notice: refused, keepOpen: true, ask: false };
	if ( !guildNameAllowed( name, guildRules ) ) return { notice: refused, keepOpen: false, ask: false };
	return { notice: null, keepOpen: false, ask: true };
}

/*
================
guildWarNameRefusal

619620 mode 1's refusals in its order; null lets the request go on to its
confirmation. The text keys have no notice code, so the notice carries the
key itself, shown in the chat and the centre.
================
*/
export function guildWarNameRefusal(
	name: string,
	ownGuild: string,
	stake: number,
	guildRules: NameRules
): SystemNotice | null {
	const say = ( key: string ): SystemNotice => ({ key, value: 0, nativeType: 0, banner: true });
	if ( !name ) return say( "UIIT_MSG_GUILDWAR_HGN_INPUT" );
	if ( name === ownGuild ) return say( "UIIT_MSG_GUILDWARERR_SELFWAR" );
	const bytes = cp949Length( name );
	if ( bytes < GUILD_NAME_MIN_BYTES || bytes > GUILD_NAME_MAX_BYTES ) {
		return say( "UIIT_MSG_GUILDERR_INVALID_GUILDNAME_LEN" );
	}
	if ( !guildNameAllowed( name, guildRules ) ) return say( "UIIT_MSG_GUILDWARERR_ERRINPUT" );
	if ( stake > GUILD_WAR_STAKE_MAX ) return constantNativeNotice( GUILD_NOTICE_CATEGORY, GUILD_WAR_STAKE_REFUSED );
	return null;
}
