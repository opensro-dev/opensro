/*
===========================================================================

native-window-sections.ts - native constructor ownership of resource sections

A resinfo file contains separately created dialogs and alternate pages. Keep
constructor order so later controls paint above the background that owns them.

===========================================================================
*/

/*
================
nativeWindowSections

An unspecified file retains the decoder's complete-layout behavior. Listed
owners admit only the sections their native constructor creates together.
================
*/
export function nativeWindowSections( name: string ): readonly string[] | undefined {
	switch ( name ) {
		case "ifitemmallconfirmbuy":
		case "ifitemmallconfirmslot":
			return [ "Create" ]; // 6C0540 / 6BF320.
		case "ifmessagebox":
			// 528430 / 52A2D0; rendering admits one modal branch.
			return [ "Create", "MsgBoxStore", "MsgBoxStoreConfirm" ];
		case "ifapprenticeship":
			return [ "Create", "NotifySubBox", "NotifyContents" ]; // 5C5B60.
		case "ifskill":
			return [ "Create", "MainSkillWnd" ];
		case "ifguild":
			return [ "Create", "GuildInfo", "NotifySubBox", "MemberView", "Command", "SortBtn" ]; // 5EA9D0.
		case "ifpartymatch":
			// 637400 creates the background before the inset search controls.
			return [ "Create", "SearchInfo", "SlotListButton", "SlotList" ];
		case "ifcos":
		case "ifcosinventory":
			return [ "Create" ]; // 6A13B0 / 6A9420.
		default:
			return undefined;
	}
}
