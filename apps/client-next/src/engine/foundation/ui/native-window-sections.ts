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
		case "ifitemmall":
			return [ "Create", "ShopList", "MyInfo", "Trunk" ]; // 6BBFA0.
		case "ifitemmallshop":
			return [ "Create", "ShopDescription", "ShopMain", "ShopSlot", "PageManager", "ShopZzimBtn" ];
		case "ifitemmallshopslot":
			return [ "Create", "ShopSlotIcon" ]; // 6C9F80; package icons share this rectangle.
		case "ifitemmallmyinfo":
			return [ "Create" ];
		case "ifitemmalltrunk":
			return [ "Create", "Inventory" ];
		case "ifitemmallinventory":
			return [ "Create", "ExpandInven" ];

		case "ifitemmallconfirmbuy":
			return [ "Create" ];
		case "ifitemmallconfirmslot":
			return [ "Create", "PointButton" ]; // 6BD350 constructs the button; currency type owns visibility.
		case "ifmessagebox":
			// 528430 / 52A2D0; rendering admits one modal branch. The fortress tax
			// boxes (52A8D0 / 52A7E0) follow so the store's section indices hold.
			return [
				"Create",
				"MsgBoxStore",
				"MsgBoxStoreConfirm",
				"MsgBoxTaxModify",
				"MsgBoxTaxLevy",
				// 52A960 / 52DC80: the fortress production boxes.
				"MsgBoxMakeItem",
				"MsgBoxMakeItemCancel"
			];
		case "ifapprenticeship":
			return [ "Create", "NotifySubBox", "NotifyContents" ]; // 5C5B60.
		case "ifskill":
			return [ "Create", "MainSkillWnd" ];
		case "ifguildwarrequest":
			return [ "Create", "Message", "Condition", "InputGuild" ]; // 618260.
		case "ifguildwarconfirm":
		case "ifguildwaragree":
			return [ "Create", "Message", "Condition" ]; // 617B10 / 617C30.
		case "ifhostileguild":
			return [ "Create", "GuildDetail", "HostileList", "CommandButton" ]; // 600120.
		case "ifguild":
			return [ "Create", "GuildInfo", "NotifySubBox", "MemberView", "Command", "SortBtn" ]; // 5EA9D0.
		case "ifpartymatch":
			// 637400 creates the background before the inset search controls.
			return [ "Create", "SearchInfo", "SlotListButton", "SlotList" ];
		case "iftaxmanagement":
			return [ "Create", "TexListSlot", "TexChangeSlot", "TaxLevySlot" ]; // 664DA0.
		case "ifcos":
		case "ifcosinventory":
			return [ "Create" ]; // 6A13B0 / 6A9420.
		default:
			return undefined;
	}
}
