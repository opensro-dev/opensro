/*
===========================================================================

game-options.ts - the local game options: rows, defaults and validation

One row per option: its key, its byte offset in the native option set,
its label's string code, its Reset default and its Options-window tab
group. The offsets belong to local SROptionSet state, never to
character privileges. Two rows are later-native (#3502): the title
indicators sit at the later client's own option offsets 0x1F (own
title) and 0x20 (other titles), labelled by its
UIIT_STT_CHAR_TITLE_SIGN / UIIT_STT_OTHER_TITLE_SIGN rows, which the
v1.150 catalogue cannot resolve (ui.ts hudCopy carries the text).

===========================================================================
*/

// The title rows postdate the first saved records: a stored record
// without them validates with their defaults, so a player's saved
// choices survive the rows arriving (#3502 slice 2). Every other row
// stays required, and an unknown key or a non-boolean value refuses
// the whole record, as before.
/*
================
titleOption

The two title rows (#3502 slice 2), named plainly: an older stored
record may lack exactly these.
================
*/
function titleOption( key: GameOption ): boolean {
	return key === "ownTitle" || key === "otherTitles";
}

/*
================
gameOptionRows

5CB7F0 binds labels; 5CB180 binds storage; 5CB550 defines Reset defaults.
================
*/
export function gameOptionRows() {
	return [
		[ "ownName", 12, "UIIT_STT_ONESELF_SIGN", true, 0 ],
		[ "playerNames", 13, "UIIT_STT_OTHER_CHAR_SIGN", true, 0 ],
		[ "monsterNames", 14, "UIIT_STT_MONSTER_SIGN", true, 0 ],
		[ "npcNames", 15, "UIIT_STT_NPC_SIGN", true, 0 ],
		[ "guildNames", 16, "UIIT_STT_GUILDVIEW_SIGN", true, 0 ],
		[ "fortressNames", 17, "UIIT_STT_FORT_USERMARK_NAME_SIGN", true, 0 ],
		[ "ownTitle", 31, "UIIT_STT_CHAR_TITLE_SIGN", true, 0 ],
		[ "otherTitles", 32, "UIIT_STT_OTHER_TITLE_SIGN", true, 0 ],
		[ "ownStatus", 21, "UIIT_STT_QUICKSTATE_OWNER", true, 0 ],
		[ "cosStatus", 22, "UIIT_STT_QUICKSTATE_COS", true, 0 ],
		[ "partyStatus", 23, "UIIT_STT_QUICKSTATE_PARTY", true, 0 ],
		[ "monsterStatus", 24, "UIIT_STT_QUICKSTATE_MONSTER", true, 0 ],
		[ "guide", 0, "UIIT_STT_GUIDEVIEW_SIGN", true, 1 ],
		[ "partyInvites", 1, "UIIT_STT_INVITE_PARTY_SIGN", true, 1 ],
		[ "eventGuide", 6, "UIIT_STT_EVENTGUIDE_SIGN", true, 1 ],
		[ "exchangeRequests", 2, "UIIT_STT_REQUEST_EXCHANGE_SIGN", true, 1 ],
		[ "whispers", 4, "UIIT_STT_GET_WHISPER_SIGN", true, 1 ],
		[ "hideSystemMessages", 7, "UIIT_MSG_SYSTEM_WND_HIDEMSG", true, 1 ],
		[ "partyBuffs", 8, "UIIT_MSG_QUICK_PARTY_CHARACTER_BUFF", true, 1 ],
		[ "windowMode", 9, "UIIT_STT_WINDOWMODE_CHANGE", false, 1 ],
		[ "highQualityIntro", 10, "UIIT_STT_INTRO_USER_MOSTER_BLIND", true, 1 ],
		[ "hideSilkCos", 11, "UIIT_STT_BLIND_SILK_COS", false, 1 ],
		[ "hpWarning", 18, "UIIT_STT_CAUTION_HP", true, 1 ],
		[ "mpWarning", 19, "UIIT_STT_CAUTION_MP", true, 1 ],
		[ "warningSound", 20, "UIIT_STT_CAUTION_SOUND", true, 1 ]
	] as const;
}
export type GameOption = ReturnType<typeof gameOptionRows>[number][0];
export type GameOptions = Readonly<Record<GameOption, boolean>>;

/*
================
defaultGameOptions

The Reset defaults (5CB550): every row at its table value.
================
*/
export function defaultGameOptions(): GameOptions {
	return Object.fromEntries( gameOptionRows().map( ( [key, , , value] ) => [ key, value ] ) ) as GameOptions;
}

/*
================
initialGameOptions

679C80 initializes startup state; 5CB550 is the separate Reset command.
The native constructor omits sound byte +14 entirely (also absent from
4BBD00 persistence). Use deterministic silence for that unspecified byte.
================
*/
export function initialGameOptions(): GameOptions {
	return {
		...defaultGameOptions(),
		hpWarning: false,
		mpWarning: false,
		warningSound: false,
		ownStatus: false,
		cosStatus: false,
		partyStatus: false,
		monsterStatus: false
	};
}

/*
================
gameOptions

A stored record, validated whole: every row present and boolean, no
unknown keys - except the title rows, which an older record may lack
(see titleOption above) and then takes its default.
================
*/
export function gameOptions( value: unknown ): GameOptions {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) throw Error( "Invalid local game options" );
	const raw = value as Record<string, unknown>;
	const rows = gameOptionRows();
	if ( Object.keys( raw ).some( key => !rows.some( ( [rowKey] ) => rowKey === key ) ) ) {
		throw Error( "Invalid local game option fields" );
	}
	if (
		rows.some(
			( [key] ) => key in raw ? typeof raw[key] !== "boolean" : !titleOption( key )
		)
	) {
		throw Error( "Invalid local game option fields" );
	}
	return Object.fromEntries(
		rows.map( ( [key, , , fallback] ) => [ key, key in raw ? raw[key] : fallback ] )
	) as GameOptions;
}

/*
================
gameOption

One row's key by name; an unknown name is a programming error.
================
*/
export function gameOption( value: string ): GameOption {
	const row = gameOptionRows().find( ( [key] ) => key === value );
	if ( !row ) throw Error( "Unknown game option" );
	return row[0];
}
