/*
===========================================================================

cifRuntimeImageCatalog.mjs - native UI images selected by executable code

Authored resource files cover static controls. These explicit families close
the image dependency graph for runtime swaps, including button-state siblings.
Keep native provenance beside each family so missing artwork is fixed in the
publisher instead of hidden by a client fallback.

===========================================================================
*/

/*
================
worldMapMarkerRuntimeImageReferences
================
*/
export const worldMapMarkerRuntimeImageReferences = [
	// CIFWorldMap marker sprites. CIFWorldMap_InitPageResources @0x00576bd0
	// acquires five signs into this+0x80c..+0x81c and the destructor
	// @0x0057a8b0 releases the same five; none of them is reachable from
	// resinfo\ifworldmap.txt or from the two worldmap_*.txt tables, so the
	// data-driven world-map closure cannot discover them.
	//   +0x80c mm_sign_character      (shared with the minimap pass)
	//   +0x810 wmap_sign_party        drawn @0x0057d5af
	//   +0x814 wmap_sign_apprenticeship drawn @0x0057d0fd
	//   +0x818 wmap_sign_questnpc     drawn @0x0057b4a3
	//   +0x81c wmap_sign_huntingpoint (shared with the minimap pass)
	// CIFWorldMap_OnRender @0x0057fe60 paints them after the overlay icons and
	// labels, each a 16x16 quad centred on the projected point (corners +-8.0);
	// unlike the minimap passes there is NO 47px ring clamp and NO arrow swap,
	// which is why the world map uses these wmap_* sprites instead of the
	// mm_sign_*arrow pair.
	"interface/worldmap/wmap_sign_party.ddj",
	"interface/worldmap/wmap_sign_apprenticeship.ddj",
	"interface/worldmap/wmap_sign_questnpc.ddj",
	// The minimap section below lists these two again with its own evidence;
	// both consumers dedupe, and repeating them here keeps this set equal to
	// the five handles 576bd0 actually acquires.
	"interface/minimap/mm_sign_character.ddj",
	"interface/worldmap/wmap_sign_huntingpoint.ddj"
];
/*
================
returnScrollRuntimeImageReferences
================
*/
export const returnScrollRuntimeImageReferences = [
	"interface/ifcommon/com_casting_window.ddj",
	// 6B1B30 selects collection for kind 1 through code, outside resinfo.
	"interface/ifcommon/com_casting_gauge_collection.ddj",
	"interface/ifcommon/com_casting_gauge_return.ddj",
	"interface/ifcommon/com_casting_gauge_return_bright.ddj"
];
// DDJ paths selected by native code rather than authored resinfo fields.
// Keep the reconstruction evidence beside each runtime-only reference.

// CIFTargetStatusPanel's native refresh family selects these paths entirely
// from code, so no resinfo DDJ field can make the resource copy pass discover
// them:
// - sub_5814d0 chooses the level-difference and entity-kind gem;
// - sub_5828e0 chooses the player job mark;
// - sub_5830f0 chooses the monster-grade mark.
// Keep the complete reachable family together. Splitting out only the four
// commonly seen monster grades would leave the same publication bug waiting
// for another target kind or a titan/elite spawn.
/*
================
targetStatusRuntimeImageReferences
================
*/
export const targetStatusRuntimeImageReferences = [
	"interface/targetwindow/tw_gem_weak2.ddj",
	"interface/targetwindow/tw_gem_weak1.ddj",
	"interface/targetwindow/tw_gem_normal.ddj",
	"interface/targetwindow/tw_gem_strong1.ddj",
	"interface/targetwindow/tw_gem_strong2.ddj",
	"interface/targetwindow/tw_gem_animal.ddj",
	"interface/targetwindow/tw_gem_player.ddj",
	"interface/targetwindow/tw_job_merchant.ddj",
	"interface/targetwindow/tw_job_thief.ddj",
	"interface/targetwindow/tw_job_hunter.ddj",
	"interface/targetwindow/tw_icon_normal.ddj",
	"interface/targetwindow/tw_icon_champion.ddj",
	"interface/targetwindow/tw_icon_unique.ddj",
	"interface/targetwindow/tw_icon_giant.ddj",
	"interface/targetwindow/tw_icon_titan.ddj",
	"interface/targetwindow/tw_icon_elite.ddj"
];

// sub_914ae0 constructs the complete CIDamageText sprite-provider tables in
// code. Each visible glyph is a shadow/face pair: ten hitcount digits plus
// critical, blocking and resist (the miss result). No resinfo field names
// these resources, so the normal CIF discovery pass cannot publish them.
/*
================
damageTextRuntimeImageReferences
================
*/
export const damageTextRuntimeImageReferences = [
	...Array.from( { length: 10 }, ( _, digit ) => `interface/hitcount/hitcount_${digit}.ddj` ),
	...Array.from( { length: 10 }, ( _, digit ) => `interface/hitcount/hitcount_${digit}_shadow.ddj` ),
	"interface/hitcount/critical.ddj",
	"interface/hitcount/critical_shadow.ddj",
	"interface/hitcount/blocking.ddj",
	"interface/hitcount/blocking_shadow.ddj",
	"interface/hitcount/resist.ddj",
	"interface/hitcount/resist_shadow.ddj"
];

// CIFPartyMatchSlot::SetSelected (sub_63d470 @ 0x0063d470) swaps the
// id-5 CIFBarWnd prefix between com_bar01_ and com_bar01select_. Neither
// prefix's three piece suffixes are authored as complete DDJ paths in
// resinfo, so the resource graph cannot discover them by parsing layouts.
// Keep both reachable families together: the same selected prefix is also
// consumed by the mentor-match row mirror.
/*
================
partyMatchRowRuntimeImageReferences
================
*/
export const partyMatchRowRuntimeImageReferences = [
	"interface/ifcommon/com_bar01_left.ddj",
	"interface/ifcommon/com_bar01_mid.ddj",
	"interface/ifcommon/com_bar01_right.ddj",
	"interface/ifcommon/com_bar01select_left.ddj",
	"interface/ifcommon/com_bar01select_mid.ddj",
	"interface/ifcommon/com_bar01select_right.ddj"
];

// CPSMission's native reset/death paths select these resources from code,
// after the static resinfo layouts have been loaded:
// - sub_7292e0 selects loading_rebirth.ddj for loading mode 1;
// - CIFMsgBox_ConfigureKindPayload sub_52f460 selects the rebirth dialog and
//   its button family for the death-state prompt.
// They must therefore ride the runtime-reference publication pass. Keeping
// the button's normal path in runtimeCifButtonImageReferences below also
// publishes the focus/press siblings selected by the CIF button state fold.
/*
================
missionRebirthRuntimeImageReferences
================
*/
export const missionRebirthRuntimeImageReferences = [
	"interface/loading/loading_rebirth.ddj",
	"interface/messagebox/msgbox_rebirth.ddj"
];

// CPSMission's native region/mode loading resolver selects these backgrounds
// from code. They are client presentation and therefore must be published by
// the browser resource compiler; EnterWorld v2 deliberately does not carry
// their paths. The small loading controls and character-custom backgrounds
// are already discoverable through resinfo, so this list closes only the
// code-selected regional family.
/*
================
missionRegionLoadingRuntimeImageReferences
================
*/
export const missionRegionLoadingRuntimeImageReferences = [
	"interface/loading/loading_europe_1.ddj",
	"interface/loading/loading_europe_2.ddj",
	"interface/loading/loading_china_1.ddj",
	"interface/loading/loading_dunwhang.ddj",
	"interface/loading/loading_port2.ddj",
	"interface/loading/loading_dungeons_donwhang.ddj",
	"interface/loading/loading_constantinople.ddj",
	"interface/loading/loading_samarkand.ddj",
	"interface/loading/loading_hotan.ddj",
	"interface/loading/loading_zangan.ddj",
	"interface/loading/loading_river.ddj",
	"interface/loading/loading_thief.ddj",
	"interface/loading/loading_thief2.ddj"
];

// CPSVersionCheck::OnCreate selects one of start_loading_01..10 entirely in
// native code before the browser's packed-resource warmup begins. Publish the
// complete random family; compact releases retain these few boot-critical
// images loose while also indexing them with the native UI family.
/*
================
versionCheckLoadingRuntimeImageReferences
================
*/
export const versionCheckLoadingRuntimeImageReferences = [
	...Array.from(
		{ length: 10 },
		( _, index ) => `interface/loading/start_loading_${String( index + 1 ).padStart( 2, "0" )}.ddj`
	)
];

// CIFChatViewer_OnCreate (sub_6ac4d0 @0x006ac62e/@0x006ac65a)
// constructs its four selectable tab areas and assigns chat_tab.ddj from
// code. ifchatviewer.txt therefore cannot make the resinfo-driven copy pass
// discover it. The browser's generatedTabs mirror uses the same native art,
// so publish it through the runtime-only CIF image catalog.
/*
================
chatViewerRuntimeImageReferences
================
*/
export const chatViewerRuntimeImageReferences = [
	"interface/chattingwnd/chat_tab.ddj"
];

// CIFSlotWithHelp 554800 initializes these; 565850 selects 240 atlas frames.
/*
================
quickslotRuntimeImageReferences
================
*/
export const quickslotRuntimeImageReferences = [
	"interface/skill/skill_delay.ddj",
	"interface/skill/skill_charge.ddj",
	...Array.from( { length: 10 }, ( _, i ) => `effect/icon/cool_time_${i}.ddj` ),
	...Array.from( { length: 10 }, ( _, i ) => `interface/item_number/item_number_${i}.ddj` )
];
/*
================
itemMallRuntimeImageReferences

Category controls created by the native Item Mall constructor.
================
*/
export const itemMallRuntimeImageReferences = [
	// CIFItemMall_OnCreate 6BBFA0 creates these controls outside resinfo.
	"interface/mall/mall_tab_on.ddj",
	"interface/mall/mall_tab_off.ddj",
	"interface/mall/mall_tab_disable.ddj",
	"interface/mall/mall_tab2_on.ddj",
	"interface/mall/mall_tab2_off.ddj",
	"interface/mall/mall_hot_icon.ddj",
	"interface/mall/mall_hot_icon_disable.ddj",
	"interface/mall/mall_consum_icon.ddj",
	"interface/mall/mall_consum_icon_disable.ddj",
	"interface/mall/mall_avatar_icon.ddj",
	"interface/mall/mall_avatar_icon_disable.ddj",
	"interface/mall/mall_pet_icon.ddj",
	"interface/mall/mall_pet_icon_disable.ddj",
	"interface/mall/mall_bundle_icon.ddj",
	"interface/mall/mall_bundle_icon_disable.ddj",
	"interface/mall/mall_premium_icon.ddj",
	"interface/mall/mall_premium_icon_disable.ddj",
	"interface/mall/mall_alchemy_icon.ddj",
	"interface/mall/mall_alchemy_icon_disable.ddj",
	"interface/mall/mall_choicelist_icon.ddj",
	"interface/mall/mall_inven_icon.ddj",
	"interface/mall/mall_inven_icon_disable.ddj"
];

/*
================
slotEffectRuntimeImageReferences

CIFSlotWithHelp item-slot effect sheets (CIFSlotWithHelp_LoadOverlayTextures
5548C0, drawn by CIFControl_RenderIconOverlays 565850): the rare (SOX)
shine, the summoned-companion glow, the revival flash, the item-changed
flash and the repair flash. Code-selected; no resinfo file names them.
================
*/
export const slotEffectRuntimeImageReferences = [
	"icon/item/etc/icon_edge_rare.ddj",
	"interface/pet/pt_edge_effect.ddj",
	"interface/pet/pt_life_effect.ddj",
	"icon/icon_mall_transgender.ddj",
	"icon/icon_mall_repair.ddj"
];

/*
================
runtimeCifImageReferences
================
*/
export const runtimeCifImageReferences = [
	...slotEffectRuntimeImageReferences,
	...itemMallRuntimeImageReferences,
	// CIFDelayInfo 6B14E0 / 6B1B30 assigns these outside resinfo.
	...returnScrollRuntimeImageReferences,
	...quickslotRuntimeImageReferences,
	// Shared quick-status windows select their fills in code (85E3F0 / 85F660).
	"interface/ifcommon/quick_hp.ddj",
	"interface/ifcommon/quick_mp.ddj",
	...targetStatusRuntimeImageReferences,
	...damageTextRuntimeImageReferences,
	...partyMatchRowRuntimeImageReferences,
	...missionRebirthRuntimeImageReferences,
	...missionRegionLoadingRuntimeImageReferences,
	...versionCheckLoadingRuntimeImageReferences,
	...chatViewerRuntimeImageReferences,
	// CIFMainPopup window frame chrome: ginterface.txt GDR_MAINPOPUP carries the
	// DDJ PREFIX "interface\\frame\\mframe_wnd_" (style 64 frame window); the
	// native frame renderer appends the piece suffixes at draw time, so the
	// pieces are runtime references the resinfo copy pass cannot see.
	"interface/frame/mframe_wnd_left_up.ddj",
	"interface/frame/mframe_wnd_mid_up.ddj",
	"interface/frame/mframe_wnd_right_up.ddj",
	"interface/frame/mframe_wnd_left_side.ddj",
	"interface/frame/mframe_wnd_right_side.ddj",
	"interface/frame/mframe_wnd_left_down.ddj",
	"interface/frame/mframe_wnd_mid_down.ddj",
	"interface/frame/mframe_wnd_right_down.ddj",
	// CIF_NPCTalk pane chrome: if_npcwindow.txt GDR_NW_NPCTALK carries the DDJ
	// PREFIX "interface\\npc\\npc_conversation_window_" (the CIFFrame 9-slice);
	// the native frame renderer appends the piece suffixes at draw time, so
	// the pieces are runtime references the resinfo copy pass cannot see
	// (hudLayerNpcTalk.tsx renders them - the mframe_wnd_ precedent above).
	"interface/npc/npc_conversation_window_left_up.ddj",
	"interface/npc/npc_conversation_window_mid_up.ddj",
	"interface/npc/npc_conversation_window_right_up.ddj",
	"interface/npc/npc_conversation_window_left_side.ddj",
	"interface/npc/npc_conversation_window_right_side.ddj",
	"interface/npc/npc_conversation_window_left_down.ddj",
	"interface/npc/npc_conversation_window_mid_down.ddj",
	"interface/npc/npc_conversation_window_right_down.ddj",
	// CPSTitle runtime layout replaces GDR_STA_LOGINWINDOW after pstitle.txt load.
	"interface/outer/login_window_europe.ddj",
	// CPSTitle CIFOListCtrl setup loads these row-state textures from code.
	"interface/outer/server_select.ddj",
	"interface/outer/server_rollover.ddj",
	// CIFPlayerMiniInfo_OnCreate assigns these at runtime; their resinfo DDJ
	// fields are intentionally blank.
	"interface/playerminiinfo/pmi_face.ddj",
	"interface/playerminiinfo/pmi_hp.ddj",
	"interface/playerminiinfo/pmi_mp.ddj",
	"interface/playerminiinfo/pmi_jahwan_burn.ddj",
	// CIFPetMiniInfo_OnCreate (6B3760) loads the pet portrait frame from code.
	"interface/playerminiinfo/pmi_pet_face.ddj",
	"interface/ifcommon/com_kindred_china.ddj",
	"interface/ifcommon/com_kindred_europe.ddj",
	// CIFUnderBar_OnCreate @0x573963 loads the custom 20x20 EXP piece into
	// parent+0x36c. ifunderbar.txt only authors ub_sp_bar on id 0x10, so the
	// segmented EXP renderer @0x5742b0 cannot be discovered from resinfo.
	"interface/underbar/ub_exp_bar.ddj",
	// CIFMinimap_Render binds this through pSprCharacter (+0x374) for the
	// local-player center marker.
	"interface/minimap/mm_sign_character.ddj",
	// CIFMinimap entity-sign pass (loop @0x0054ce5c): per-entity RTTI dispatch
	// picks CICNPC (0xcf000c) -> +0x3b4 mm_sign_npc and CICMonster (0xceffcc)
	// -> +0x3ac mm_sign_monster (loads @0x0054a745 / @0x0054a69b). Loaded from
	// code, not resinfo, so they are runtime references like the character sign.
	"interface/minimap/mm_sign_npc.ddj",
	"interface/minimap/mm_sign_monster.ddj",
	// Same dispatch, the remaining live branches: a CICMonster whose rarity
	// byte +0x770 == 3 draws +0x3b0 mm_sign_unique instead of the monster sign
	// (@0x0054cf24 branch; slot loaded @0x0054a707), CICCos (0xcedc38)
	// draws +0x3c8 mm_sign_animal (@0x0054cf46 branch; slot loaded @0x0054a9af),
	// and CICUser (0xcf0108, the remote player) draws +0x3b8
	// mm_sign_otherplayer (@0x0054cf9e pure IsKindOf(CICUser) branch; slot
	// loaded @0x0054a79a, stored @0x0054a7b1). The otherplayer client chain
	// (factory/view/surface/asset) is closed; peer rows await the server
	// character-appearance lane (see the OPEN FRONTIER block in
	// MissionMinimapSurface.tsx).
	"interface/minimap/mm_sign_unique.ddj",
	"interface/minimap/mm_sign_animal.ddj",
	"interface/minimap/mm_sign_otherplayer.ddj",
	// CIFMinimap APPRENTICESHIP roster/edge pass (@0x0054d24a): a training-camp
	// member inside the 47px ring draws +0x3a0 mm_sign_apprenticeship 8x8
	// (@0x0054d4db; name built @0x0054a899 in the sub_54a5a0 init), at/outside
	// it the +0x3a4 mm_sign_apprenticeshiparrow 32x32 rotated on the ring
	// (@0x0054d6b7; name built @0x0054a844). Fed by the REAL 0x3AC5 member
	// plane (ced160+0x2c8); consumed by MissionMinimapApprenticeshipPass.ts.
	"interface/minimap/mm_sign_apprenticeship.ddj",
	"interface/minimap/mm_sign_apprenticeshiparrow.ddj",
	// CIFMinimap HUNTING-POINT pass (sub_54bd90, called @0x0054e9db): every
	// tracked object in the CGInterface+0x760 write/track map (+0x18; rows
	// inserted by the 0xB5ED leg's REAL sub_84f130) draws +0x3cc
	// wmap_sign_huntingpoint as a 16x16 quad rotated by the record heading
	// (@0x0054c2a4), ring-clamped at 47px with NO sprite swap. The slot name
	// is built @0x0054a9ed and stored @0x0054aa04 in the sub_54a5a0 init -
	// NOTE the WORLDMAP directory, not interface/minimap/. Consumed by
	// MissionMinimapHuntingPass.ts.
	"interface/worldmap/wmap_sign_huntingpoint.ddj",
	// CIFMinimap QUEST-TARGET pass (@0x0054e287): each tracked-quest target
	// inside the 47px ring draws +0x3c4 mm_sign_questnpc 16x16 (@0x0054e3ea,
	// corners +-8; name built @0x0054a8ee, stored @0x0054a905 in the
	// sub_54a5a0 init), at/outside it the +0x3c0 mm_sign_questarrow 32x32
	// rotated on the ring (@0x0054e5c6, corners +-16; name built @0x0054a943,
	// stored @0x0054a95a). Fed by the REAL tracked-quest chain (questPlane
	// +0x398 / +0x18d0 registry) and the npcpos table below; consumed by
	// MissionMinimapQuestPass.ts.
	"interface/minimap/mm_sign_questnpc.ddj",
	"interface/minimap/mm_sign_questarrow.ddj",
	// CIFMinimap PARTY roster/edge pass (@0x0054db1e): a party member inside
	// the 47px ring draws +0x39c mm_sign_party 8x8 (@0x0054dca1, corners +-4;
	// name built @0x0054a7ef, stored @0x0054a806 in the sub_54a5a0 init),
	// at/outside it the +0x36c mm_sign_partyarrow 16x16 rotated on the ring
	// (@0x0054de7d, corners +-8; name built @0x0054a5f2, stored @0x0054a605 -
	// the FIRST of the 13 marker sprites). Fed by the REAL 0xB0D5/0x35D6/0x3E58
	// roster writers (75db30 -> 829f30/822110/828a30); consumed by
	// MissionMinimapPartyPass.ts. With these two, all 13 sub_54c360 sprites
	// are published.
	"interface/minimap/mm_sign_party.ddj",
	"interface/minimap/mm_sign_partyarrow.ddj",
	...worldMapMarkerRuntimeImageReferences,
	// CGInterface_OnMissionLoadingRevealed registers these native select effect
	// slots for mission click/target feedback after loading reveals the world.
	"effect/select_01.ddj",
	"effect/select_02.ddj",
	"effect/select_03.ddj",
	"effect/select_04.ddj",
	"effect/footstep_sand.ddj",
	"effect/footstep_snow.ddj",
	// EventGuide/GameGuide runtime-created controls and zero-sized DDJ statics.
	// CIFQuestInfoGuide_OnCreate sub_62e090 assigns this blink pair at runtime;
	// the level-up arm creates the control only after the map reveal, so neither
	// frame can rely on the static resinfo copy pass or a late browser request.
	"icon/etc/qstinfoic_1.ddj",
	"icon/etc/qstinfoic_2.ddj",
	"icon/etc/eventguide_1.ddj",
	"icon/etc/eventguide_2.ddj",
	"icon/etc/wintereventguide_1.ddj",
	"icon/etc/wintereventguide_2.ddj",
	"interface/guide/gd_guide.ddj",
	"interface/guide/gd_paper.ddj",
	"interface/guide/gd_scroll_down.ddj",
	"interface/guide/gd_scroll_up.ddj",
	"interface/guide/gd_scroll_button.ddj",
	"interface/guide/gd_scroll_line.ddj",
	"interface/guide/gd_side_tab.ddj",
	"interface/ifcommon/com_side_button.ddj",
	// CIFVerticalScroll_OnCreate (sub_545190 @0x545206) assigns the scroll
	// TRACK texture "interface\ifcommon\com_scroll_bar.ddj" to the bar window
	// itself at runtime; the resinfo VSCROLL nodes carry an empty DDJ field
	// (e.g. ifsystemmessage.txt GDR_SYETEM_MESSAGE_VSCROLL DDJ=""), so the
	// resinfo copy pass never sees it. Arrow/thumb children are authored in
	// ifverticalscroll.txt and ride the normal resinfo pass.
	"interface/ifcommon/com_scroll_bar.ddj",
	// CIFMallNotifyWnd root texture is assigned by CGInterface after creation.
	"interface/mall/mall_communicate.ddj",
	// CIFInventory_RebuildCapacityAndGold (sub_59df10) creates the bag page
	// tabs (com_tab_off/_on CIFSelectableArea triplet, empty normal) and marks
	// beyond-capacity slots with pt_block - all runtime references from code.
	"interface/ifcommon/com_tab_off.ddj",
	"interface/ifcommon/com_tab_on.ddj",
	"interface/pet/pt_block.ddj",
	// CIFInventory pane chrome, authored in resinfo\ifinventory.txt (created by
	// CIFInventory_OnCreate sub_59a080). The lattice/frame carry a DDJ *prefix*
	// (multi-piece classes CIFLattice/CIFFrame), so the resinfo copy pass never
	// sees the concrete piece names - list them as runtime references:
	//   id 2 GDR_INVENTORY_LATTICE (com_lattice_ 36x36 cells) = socket grid,
	//   id 10 GDR_INVENTORY_STA_MONEY (int_window_downbox) = gold strip,
	//   id 11 GDR_INVENTORY_BTN_MONEY (com_moneybutton) = coin button.
	"interface/ifcommon/lattice_window/com_lattice_left_up.ddj",
	"interface/ifcommon/lattice_window/com_lattice_right_up.ddj",
	"interface/ifcommon/lattice_window/com_lattice_left_down.ddj",
	"interface/ifcommon/lattice_window/com_lattice_right_down.ddj",
	// id 3 GDR_INVENTORY_LATTICE_OUTLINE (CIFStretchWnd, com_lattice_outline_
	// prefix): 4 corners + 2 vertical side strips framing the socket grid.
	"interface/ifcommon/lattice_window/com_lattice_outline_left_up.ddj",
	"interface/ifcommon/lattice_window/com_lattice_outline_left_side.ddj",
	"interface/ifcommon/lattice_window/com_lattice_outline_left_down.ddj",
	"interface/ifcommon/lattice_window/com_lattice_outline_right_up.ddj",
	"interface/ifcommon/lattice_window/com_lattice_outline_right_side.ddj",
	"interface/ifcommon/lattice_window/com_lattice_outline_right_down.ddj",
	// id 0 GDR_INVENTORY_FRAME (CIFFrame, int_window_ prefix): the 16px
	// 8-piece ring the inventory pane draws under its content.
	"interface/inventory/int_window_left_up.ddj",
	"interface/inventory/int_window_mid_up.ddj",
	"interface/inventory/int_window_right_up.ddj",
	"interface/inventory/int_window_left_side.ddj",
	"interface/inventory/int_window_right_side.ddj",
	"interface/inventory/int_window_left_down.ddj",
	"interface/inventory/int_window_mid_down.ddj",
	"interface/inventory/int_window_right_down.ddj",
	// id 4 GDR_EQ_FRAME (CIFFrame, equip_window_ prefix): the equipment
	// pane's 12px ring.
	"interface/equipment/equip_window_left_up.ddj",
	"interface/equipment/equip_window_mid_up.ddj",
	"interface/equipment/equip_window_right_up.ddj",
	"interface/equipment/equip_window_left_side.ddj",
	"interface/equipment/equip_window_right_side.ddj",
	"interface/equipment/equip_window_left_down.ddj",
	"interface/equipment/equip_window_mid_down.ddj",
	"interface/equipment/equip_window_right_down.ddj",
	// id 5 GDR_EQ_NORMALTILE_BG (CIFNormalTile): the equipment pane's tiled
	// interior fill (rect 12,12,154,331).
	"interface/ifcommon/bg_tile/com_bg_tile_d.ddj",
	"interface/inventory/int_window_downbox.ddj",
	"interface/ifcommon/com_moneybutton.ddj",
	// CIFHelperBubbleWindow (item tooltip, hud control 0x61) row ornaments,
	// authored in resinfo\ifhelpbubblewindow.txt ids 11-13 (GDR_ITEMNAME_L/R
	// 12x12 + GDR_ITEMNAME_DIA); AppendRow's indent mode shows/hides them at
	// runtime (sub_679460). The chrome paths are code-owned by OnCreate
	// (sub_6792e0), so no resinfo DDJ property can discover them for the
	// process-global sprite catalog.
	"interface/ifcommon/com_itemsign.ddj",
	"interface/ifcommon/com_diamond.ddj",
	"interface/ifcommon/com_tooltip_corner.ddj",
	"interface/ifcommon/com_tooltip_edge.ddj",
	// CIFNotify (notice banners, hud controls 0x14/0x23/0x2a) chrome:
	// OnCreate sub_6b32b0 loads com_notice_* as the class default (@0x6b3341
	// edge, @0x6b3363 edge2, @0x6b33e2 corner). CGInterface's HUD-create
	// owner retains that family for 0x14, overrides 0x23 with com_warning_*
	// (@0x68e841..0x68e922), and overrides 0x2a with com_quest_*
	// (@0x68e966..0x68ea4b). resinfo\ifnotify.txt authors only the empty-DDJ
	// text child, so none of these code-owned resources are discoverable by
	// the ordinary resinfo copy pass.
	"interface/ifcommon/com_notice_corner.ddj",
	"interface/ifcommon/com_notice_edge.ddj",
	"interface/ifcommon/com_notice_edge2.ddj",
	"interface/ifcommon/com_warning_corner.ddj",
	"interface/ifcommon/com_warning_edge.ddj",
	"interface/ifcommon/com_warning_edge2.ddj",
	"interface/ifcommon/com_quest_corner.ddj",
	"interface/ifcommon/com_quest_edge.ddj",
	"interface/ifcommon/com_quest_edge2.ddj",
	// CIFMessageBox chrome (resinfo\ifmessagebox.txt / OnCreate sub_527990):
	// the msgbox2_window_ 8-piece frame prefix bound at create, the authored
	// StoreMoney (kind 3) boards, the com_button state family (Simple kind 9
	// YES/NO + StoreMoney CONFIRM/CANCEL), and the drop-gold icon the
	// inventory opener (sub_59a260) loads at runtime.
	"interface/messagebox/msgbox2_window_left_up.ddj",
	"interface/messagebox/msgbox2_window_mid_up.ddj",
	"interface/messagebox/msgbox2_window_right_up.ddj",
	"interface/messagebox/msgbox2_window_left_side.ddj",
	"interface/messagebox/msgbox2_window_right_side.ddj",
	"interface/messagebox/msgbox2_window_left_down.ddj",
	"interface/messagebox/msgbox2_window_mid_down.ddj",
	"interface/messagebox/msgbox2_window_right_down.ddj",
	"interface/messagebox/msgbox_quantity.ddj",
	"interface/messagebox/msgbox_iteminfo.ddj",
	"interface/messagebox/msgbox_iteminfo_2.ddj",
	"interface/messagebox/msgbox_itemwindow.ddj",
	// CIFSetPartyMode overlay (resinfo\ifsetpartymode.txt): the two authored
	// GDR_SETPARTYMODE_BG_BOX statics ride this natural-size (128x52) art;
	// the rest of its chrome (msgbox2_window_ ring, com_bg_tile_b,
	// com_radiobutton_off/_on, com_button family) already ships above.
	"interface/messagebox/msgbox_blackbox_03.ddj",
	"interface/ifcommon/com_button.ddj",
	"interface/ifcommon/com_button_focus.ddj",
	"interface/ifcommon/com_button_press.ddj",
	"interface/ifcommon/com_button_disable.ddj",
	// CIFGuild donate button (resinfo\ifguild.txt id 45
	// GDR_GUILD_INFO_GUILD_POINT_BTN at {410,72}; shown IN-GUILD by the
	// sub_5e3090 mode-1 leg, tooltip UIIT_MSG_GUILD_SP_GP_TOOLTIP): the
	// three authored state arts (no _disable variant ships in the PK2).
	"interface/ifcommon/com_donation_button.ddj",
	"interface/ifcommon/com_donation_button_focus.ddj",
	"interface/ifcommon/com_donation_button_press.ddj",
	"icon/mini_gold_icon.ddj",
	// CIFEquipment pane chrome (resinfo\ifequipment.txt / CIFEquipment_OnCreate
	// sub_593a80): the 17 socket sprites are runtime DDJ loads (sub_4ba2c0 with
	// built path strings, +0x50c..+0x554), and the rotate/view buttons author
	// per-state art the resinfo copy pass resolves from the button base name.
	"interface/equipment/equip_slot_helm.ddj",
	"interface/equipment/equip_slot_mail.ddj",
	"interface/equipment/equip_slot_shoulderguard.ddj",
	"interface/equipment/equip_slot_gauntlet.ddj",
	"interface/equipment/equip_slot_pants.ddj",
	"interface/equipment/equip_slot_boots.ddj",
	"interface/equipment/equip_slot_weapon.ddj",
	"interface/equipment/equip_slot_shield.ddj",
	"interface/equipment/equip_slot_specialdress.ddj",
	"interface/equipment/equip_slot_earring.ddj",
	"interface/equipment/equip_slot_necklace.ddj",
	"interface/equipment/equip_slot_l_ring.ddj",
	"interface/equipment/equip_slot_r_ring.ddj",
	"interface/equipment/equip_slot_cloth.ddj",
	"interface/equipment/equip_slot_pandernt.ddj",
	"interface/equipment/equip_slot_plag.ddj",
	"interface/equipment/equip_rotate_left_button.ddj",
	"interface/equipment/equip_rotate_right_button.ddj",
	"interface/equipment/equip_rotate_reset_button.ddj",
	"interface/equipment/equip_slot_avata_button.ddj",
	"interface/equipment/equip_slot_equipment_button.ddj",
	// CIFAction pane chrome (resinfo\ifaction.txt / CIFAction::OnCreate
	// sub_58b720): the four CIFSubFrame group boxes carry the DDJ *prefix*
	// interface\frame\sframe_wnd_ (8-piece ring, 36px top band), so the copy
	// pass never sees the concrete piece names. The pane's com_lattice_ /
	// com_lattice_outline_ pieces are already listed above; com_bg_tile_b is
	// the authored CIFNormalTile column art. All resinfo layouts now publish;
	// this explicit row remains because the hand-mirrored pane also consumes
	// the image directly outside generic layout materialization.
	"interface/frame/sframe_wnd_left_up.ddj",
	"interface/frame/sframe_wnd_mid_up.ddj",
	"interface/frame/sframe_wnd_right_up.ddj",
	"interface/frame/sframe_wnd_left_side.ddj",
	"interface/frame/sframe_wnd_right_side.ddj",
	"interface/frame/sframe_wnd_left_down.ddj",
	"interface/frame/sframe_wnd_mid_down.ddj",
	"interface/frame/sframe_wnd_right_down.ddj",
	"interface/ifcommon/bg_tile/com_bg_tile_b.ddj",
	// CIFPlayerInfo pane chrome (resinfo\ifplayerinfo_trijob2.txt - the ONE
	// resinfo the v1.150 binary references, string @0x00bdc514, loaded by
	// CIFPlayerInfo::OnCreate sub_59fb70): another hand-mirrored pane, so its
	// authored art rides this list. chr_window_mid_up/mid_down ship with the
	// set but are only authored in the dead base ifplayerinfo.txt - carried
	// for completeness of the chr_window family.
	"interface/character/chr_hp.ddj",
	"interface/character/chr_mp.ddj",
	"interface/character/chr_job.ddj",
	"interface/character/chr_job_window.ddj",
	"interface/character/chr_stat_window.ddj",
	"interface/character/chr_window_left_01.ddj",
	"interface/character/chr_window_left_02.ddj",
	"interface/character/chr_window_right_01.ddj",
	"interface/character/chr_window_right_02.ddj",
	"interface/character/chr_window_mid_mid01.ddj",
	"interface/character/chr_window_mid_mid02.ddj",
	// GDR_PI_BTN_ADDHP/ADDMP authored art + the disabled texture OnCreate
	// assigns at runtime (sub_59fb70 @0x59fdad/0x59fdda -> sub_540ec0).
	"interface/ifcommon/com_plus_button.ddj",
	"interface/ifcommon/com_plus_button_disable.ddj",
	// Job icons the OnUpdate job branch assigns at runtime (sub_59ffa0
	// @0x5a0972/0x5a0a17/0x5a0abc).
	"interface/ifcommon/com_job_merchant.ddj",
	"interface/ifcommon/com_job_thief.ddj",
	"interface/ifcommon/com_job_hunter.ddj",
	// CIFAction toolbar icons: runtime references written onto the buttons by
	// CIFAction_SetupActionButton (sub_58bbc0 jump table - the four paths are
	// the fold's own string constants). The wider 0xcec870 action-data record
	// icons (emote row etc.) land with the bridge host's record seeding.
	"icon/action/icon_cha_sit.ddj",
	"icon/action/icon_cha_run.ddj",
	"icon/action/icon_cha_walk.ddj",
	"icon/action/icon_cha_stand.ddj",
	// CIFCommunity window chrome ('U' hotkey, ginterface id 23): the six
	// CIFSelectableArea tabs are built in code by CIFCommunity::OnCreate
	// (sub_5dfba0, texture strings @0x005dfdcf off / @0x005dfdfc on), so the
	// resinfo copy pass never sees them. The window frame (mframe_wnd_) and
	// the sub-panel ring (equip_window_) are already listed above; the panel
	// interiors are a hand-mirrored-frontier (communityPlane.ts).
	"interface/ifcommon/com_long_tab_off.ddj",
	"interface/ifcommon/com_long_tab_on.ddj",
	// CIFFriend panel interior (community panel member +0x7dc, layout id 13;
	// resinfo\iffriend.txt bound by CIFFriend::OnCreate sub_6054d0
	// @0x006054ed; hand-mirrored - hudLayerCommunityFriend.tsx).
	// frameg01_wnd_ is the authored CIFFrame *prefix* (id 1) so the resinfo
	// copy pass never sees the piece names; gil_bar02_deselect is the
	// filler-row art the pad leg sub_604830 binds in code (@0x006048cd);
	// gil_subj_button04/09 are the FriendList header (id 21 static / id 22
	// CIFButton with the sub_5419c0 _focus/_press state family). gil_shape /
	// com_mid_button* / com_bg_tile_b / com_blacksquare_* / com_scroll_* are
	// already listed.
	"interface/frame/frameg01_wnd_left_up.ddj",
	"interface/frame/frameg01_wnd_mid_up.ddj",
	"interface/frame/frameg01_wnd_right_up.ddj",
	"interface/frame/frameg01_wnd_left_side.ddj",
	"interface/frame/frameg01_wnd_right_side.ddj",
	"interface/frame/frameg01_wnd_left_down.ddj",
	"interface/frame/frameg01_wnd_mid_down.ddj",
	"interface/frame/frameg01_wnd_right_down.ddj",
	"interface/guild/gil_bar02_deselect.ddj",
	// CIFGuildMemberSlot selected-state board (the sub_5f3c60 swap
	// @0x005f3c85: arg truthy binds _select, else _deselect) - the guild
	// pane's kick route needs a selectable member row.
	"interface/guild/gil_bar02_select.ddj",
	// CIFFriendSlot contact icon (iffriendslot.txt id 10, authored default
	// gil_contact_off; the sub_605880 swap binds _on for state byte 0
	// @0x006058b4 and _off for 1 @0x006058a0 - the friend roster plane's
	// live rows). com_kindred_*16 (the slot race mark, sub_605940
	// @0x006059fa) is already listed with the party pane set.
	"interface/guild/gil_contact_on.ddj",
	"interface/guild/gil_contact_off.ddj",
	"interface/guild/gil_subj_button04.ddj",
	"interface/guild/gil_subj_button09.ddj",
	"interface/guild/gil_subj_button09_focus.ddj",
	"interface/guild/gil_subj_button09_press.ddj",
	// CIFGuild / CIFGuildRelations / CIFWarState panel interiors (community
	// panel members +0x7d8/+0x7dc-siblings, layout ids 10..12; hand-mirrored -
	// hudLayerCommunityGuild/Relations/WarState.tsx). Their resinfo files
	// (ifguild.txt, ifguildrelations.txt + ifallianceguild/ifhostileguild
	// satellites, ifwarstate.txt + ifguildwar.txt) all publish as layouts.
	// Their hand-mirrored React consumers still resolve these direct runtime
	// references: gil_windo01 is the GuildInfo
	// header art (ifguild.txt id 3), gil_bar01/gil_point the GP bar+gauge
	// (ids 31/32), gil_shape01 + gil_subj_button01/02 (+ the 04 state family;
	// base 04 already listed) the SortBtn header row, gil_subj_tab_off/_on
	// the relations/warstate tabs the OnCreates build in code (sub_5fba50
	// @0x005fbbdf / sub_619ec0 @0x0061a02b), gil_subj_button11/12 the
	// ifguildwar.txt header buttons, stl_condition* the guild sort-condition
	// button (ifguild.txt id 126), and com_bar01_* the CIFBarWnd 3-piece bar
	// the alliance/hostile pad legs bind in code (sub_5fa2f0 @0x005fa390 /
	// sub_5ff670 @0x005ff710) and the authored ifguildwar.txt slots carry.
	"interface/guild/gil_windo01.ddj",
	"interface/guild/gil_bar01.ddj",
	"interface/guild/gil_point.ddj",
	"interface/guild/gil_shape01.ddj",
	"interface/guild/gil_subj_tab_off.ddj",
	"interface/guild/gil_subj_tab_on.ddj",
	"interface/guild/gil_subj_button01.ddj",
	"interface/guild/gil_subj_button01_focus.ddj",
	"interface/guild/gil_subj_button01_press.ddj",
	"interface/guild/gil_subj_button02.ddj",
	"interface/guild/gil_subj_button02_focus.ddj",
	"interface/guild/gil_subj_button02_press.ddj",
	"interface/guild/gil_subj_button04_focus.ddj",
	"interface/guild/gil_subj_button04_press.ddj",
	"interface/guild/gil_subj_button11.ddj",
	"interface/guild/gil_subj_button11_focus.ddj",
	"interface/guild/gil_subj_button11_press.ddj",
	"interface/guild/gil_subj_button12.ddj",
	"interface/guild/gil_subj_button12_focus.ddj",
	"interface/guild/gil_subj_button12_press.ddj",
	// CIFStall_SetTradingState @0x5A2277 / 0x5A23F7 overrides resinfo id 15.
	"interface/stall/stl_condition_icon_01.ddj",
	"interface/stall/stl_condition_icon_02.ddj",
	// CIFStallSlot_UpdateContents / Clear @0x5B0609 / 0x5B07F8.
	"interface/stall/stl_slot_02.ddj",
	"interface/stall/stl_slot_05.ddj",
	"interface/stall/stl_condition.ddj",
	"interface/stall/stl_condition_focus.ddj",
	"interface/stall/stl_condition_press.ddj",
	"interface/stall/stl_condition_disable.ddj",
	// CIFLetter/CIFBlocking community sub-panel interiors (panel members
	// +0x7e8/+0x7ec, layout ids 14/15; hand-mirrored -
	// hudLayerCommunityLetter/Blocking.tsx). Their resinfo files
	// (ifletter.txt, ifblocking.txt + ifwhisperblocking/ifchattingblocking
	// satellites) now publish as layouts. Direct hand-mirror references remain
	// cataloged here: gil_subj_button10 (+ the
	// sub_5419c0 _focus/_press family; no _disable ships) is the letter
	// sender/time sort header pair (ifletter.txt ids 21/22, natural 156x24),
	// com_question_button_focus the blocking lists' id-24 tooltip end shape
	// (natural 16x24). frameg01_wnd_ / com_blacksquare_* / com_bg_tile_b/_e /
	// gil_bar02_deselect / gil_subj_button04*/09* / gil_shape /
	// gil_subj_tab_off/_on / com_mid_button* / com_scroll_* are already
	// listed.
	"interface/guild/gil_subj_button10.ddj",
	"interface/guild/gil_subj_button10_focus.ddj",
	"interface/guild/gil_subj_button10_press.ddj",
	"interface/ifcommon/com_question_button_focus.ddj",
	// CIFPartyMatch window chrome ('E' hotkey, ginterface Section=PartyMatch
	// id 130/0x82, REAL sub_69deb0 toggle; hand-mirrored window -
	// partyMatchPlane.ts / hudLayerPartyMatch.tsx). The interior authoring is
	// resinfo\ifpartymatch.txt (window OnCreate sub_637400 creates its
	// Create/SearchInfo/SlotListButton/SlotList sections): the frameg_wnd_
	// search-bar ring (id 5), the com_blacksquare_ stretch family (slot-list
	// backdrop id 6, search edit boxes ids 30..32, and the CIFComboBox
	// OnCreate sub_51dc80 closed-box backdrop @0x0051dd6f), com_bg_tile_e
	// edit-line tiles (ids 35..37), the gil_subj_button* header row + the
	// gil_shape "L" dummy (SlotListButton section), the com_bar02_ subject
	// bar (id 64), the spin-control arrows (CIFSpinButtonCtrl OnCreate
	// sub_542a20 -> resinfo\ifspincontrol.txt) and the combo drop-arrow
	// (sub_51dc80 @0x0051dd0d). mframe_wnd_ / int_window_ / com_bg_tile_b /
	// com_mid_button* / com_scroll_* / chat_arrow_* are already listed.
	"interface/frame/frameg_wnd_left_up.ddj",
	"interface/frame/frameg_wnd_mid_up.ddj",
	"interface/frame/frameg_wnd_right_up.ddj",
	"interface/frame/frameg_wnd_left_side.ddj",
	"interface/frame/frameg_wnd_right_side.ddj",
	"interface/frame/frameg_wnd_left_down.ddj",
	"interface/frame/frameg_wnd_mid_down.ddj",
	"interface/frame/frameg_wnd_right_down.ddj",
	"interface/ifcommon/com_blacksquare_left_up.ddj",
	"interface/ifcommon/com_blacksquare_left_side.ddj",
	"interface/ifcommon/com_blacksquare_left_down.ddj",
	"interface/ifcommon/com_blacksquare_right_up.ddj",
	"interface/ifcommon/com_blacksquare_right_side.ddj",
	"interface/ifcommon/com_blacksquare_right_down.ddj",
	"interface/ifcommon/bg_tile/com_bg_tile_e.ddj",
	"interface/guild/gil_subj_button03.ddj",
	"interface/guild/gil_subj_button03_focus.ddj",
	"interface/guild/gil_subj_button03_press.ddj",
	"interface/guild/gil_subj_button05.ddj",
	"interface/guild/gil_subj_button05_focus.ddj",
	"interface/guild/gil_subj_button05_press.ddj",
	"interface/guild/gil_subj_button06.ddj",
	"interface/guild/gil_subj_button06_focus.ddj",
	"interface/guild/gil_subj_button06_press.ddj",
	"interface/guild/gil_subj_button08.ddj",
	"interface/guild/gil_subj_button08_focus.ddj",
	"interface/guild/gil_subj_button08_press.ddj",
	"interface/guild/gil_shape.ddj",
	"interface/ifcommon/com_bar02_left.ddj",
	"interface/ifcommon/com_bar02_mid.ddj",
	"interface/ifcommon/com_bar02_right.ddj",
	"interface/ifcommon/com_left_arrow.ddj",
	"interface/ifcommon/com_left_arrow_focus.ddj",
	"interface/ifcommon/com_left_arrow_press.ddj",
	"interface/ifcommon/com_right_arrow.ddj",
	"interface/ifcommon/com_right_arrow_focus.ddj",
	"interface/ifcommon/com_right_arrow_press.ddj",
	"interface/ifcommon/com_qst_downarrow_button.ddj",
	"interface/ifcommon/com_qst_downarrow_button_focus.ddj",
	"interface/ifcommon/com_qst_downarrow_button_press.ddj",
	// CIFMentorMatch window chrome (the Academy pane's Matching button,
	// ginterface Section=MentorMatch id 153/0x99, REAL sub_69e150 toggle;
	// hand-mirrored window - mentorMatchPlane.ts / hudLayerMentorMatch.tsx).
	// The interior authoring is resinfo\ifmentormatch.txt (window OnCreate
	// sub_672ed0 creates Create/SearchInfo/SlotListButton/SlotList): every
	// piece is shared with the party-match set directly above (frameg_wnd_,
	// com_blacksquare_*, com_bg_tile_b/_e, com_mid_button* incl. the
	// sub_671a50 disabled state via the _focus/_press/_disable suffix
	// expansion, gil_subj_button03/05/06/08, gil_shape, com_bar02_*, the
	// spin arrows, the combo drop-arrow and com_scroll_*/chat_arrow_*) -
	// zero new files; this note records the dependency.
	// CIFCompositeItemWnd window (ginterface Section=CompositeItem id 36/0x24,
	// REAL sub_69f0f0 toggle; compositeItemPlane.ts / hudLayerCompositeItem
	// .tsx). The interior authoring is resinfo\ifcompositeitemwnd.txt (window
	// OnCreate sub_6af680 creates "Create": the int_window_ ring and
	// com_bg_tile_b floor are already listed above). The sys_button family is
	// the sub_6af780 option-button skin, assigned in CODE (@0x006af8f7, with
	// the sub_5419c0 _focus/_press/_disable suffix expansion) so the resinfo
	// copy pass never sees it - listed as runtime references.
	"interface/system/sys_button.ddj",
	"interface/system/sys_button_focus.ddj",
	"interface/system/sys_button_press.ddj",
	"interface/system/sys_button_disable.ddj",
	// CIFCOS window page interiors (ginterface Section=COSWnd id 120/0x78,
	// REAL sub_69d920 toggle; cosWindowPlane.ts / hudLayerCosWindow.tsx).
	// The pages attach resinfo\ifcosinfo.txt / ifcosinventory.txt /
	// ifcossetup.txt in their OnCreates (sub_6a5750 / sub_6a8c60 /
	// sub_6a9a10). Those files now publish; the explicit runtime list continues
	// to serve the hand-mirrored plane: the info page's pt_* statics and the three CIFGauge
	// fill strips (ids 30..32/35..37/50), the com_mall_button rename button
	// (id 26, sub_5419c0 _focus/_press/_disable state family), the trade-pet
	// com_star level markers (ifcosinventory TradeInfo ids 26..30), and the
	// ifcossetup opt_video_tab row headers (ids 10/12/14). The radio-unit
	// skins (com_radiobutton_off/_on, sub_5424c0 @0x54262b/@0x542657), the
	// checkbox pair (com_checkbutton_off/_on), the spin-control chrome
	// (com_left/right_arrow + the sub_542a20 str_slot_02 backdrop @0x542ac2),
	// the lattice/outline pieces, opt_inner_box_/int_window_/frameg_wnd_
	// rings and com_bg_tile_b/_e already ship above - only str_slot_02 is
	// new from that set.
	"interface/pet/pt_time.ddj",
	"interface/pet/pt_stat_window.ddj",
	"interface/pet/pt_messagebox.ddj",
	"interface/pet/pt_hp.ddj",
	"interface/pet/pt_hgp.ddj",
	"interface/pet/pt_exp.ddj",
	"interface/ifcommon/com_mall_button.ddj",
	"interface/ifcommon/com_mall_button_focus.ddj",
	"interface/ifcommon/com_mall_button_press.ddj",
	"interface/ifcommon/com_mall_button_disable.ddj",
	"interface/ifcommon/com_star.ddj",
	"interface/option/opt_video_tab.ddj",
	"interface/store/str_slot_02.ddj",
	// The COS mini-list strip (durable control 0x27; REAL sub_6aa290 fill /
	// sub_6a39e0 command build / sub_6a1be0 per-kind icon map - all runtime
	// CODE references the resinfo pass never sees; ifcosstatus/ifcoscommand
	// author EMPTY DDJ fields for these slots). Ships the am_* row chrome,
	// the row-select outlines and the full cos_cmd_* icon family (base +
	// _disable states the 6a1be0 map picks per command availability).
	"interface/animal/am_window.ddj",
	"interface/animal/am_cos_window.ddj",
	"interface/animal/am_hp.ddj",
	"interface/animal/am_hgp.ddj",
	"interface/animal/am_ctrl_window_front.ddj",
	"interface/animal/am_ctrl_window_middle.ddj",
	"interface/animal/am_ctrl_window_end.ddj",
	"interface/animal/am_ctrl_open.ddj",
	"interface/animal/am_ctrl_open_focus.ddj",
	"interface/animal/am_ctrl_open_press.ddj",
	"interface/animal/am_ctrl_close.ddj",
	"interface/animal/am_ctrl_close_focus.ddj",
	"interface/animal/am_ctrl_close_press.ddj",
	"icon/etc/cos_outline_1.ddj",
	"icon/etc/cos_outline_2.ddj",
	"icon/action/cos_cmd_aggressive.ddj",
	"icon/action/cos_cmd_aggressive_disable.ddj",
	"icon/action/cos_cmd_ai_attack.ddj",
	"icon/action/cos_cmd_ai_auto.ddj",
	"icon/action/cos_cmd_ai_destruction.ddj",
	"icon/action/cos_cmd_ai_destruction_disable.ddj",
	"icon/action/cos_cmd_ai_hold.ddj",
	"icon/action/cos_cmd_ai_page.ddj",
	"icon/action/cos_cmd_charm.ddj",
	"icon/action/cos_cmd_coswindow.ddj",
	"icon/action/cos_cmd_coswindow_disable.ddj",
	"icon/action/cos_cmd_defensive.ddj",
	"icon/action/cos_cmd_defensive_disable.ddj",
	"icon/action/cos_cmd_disembark.ddj",
	"icon/action/cos_cmd_disembark_disable.ddj",
	"icon/action/cos_cmd_embark.ddj",
	"icon/action/cos_cmd_embark_disable.ddj",
	"icon/action/cos_cmd_follower.ddj",
	"icon/action/cos_cmd_follower_disable.ddj",
	"icon/action/cos_cmd_inventory.ddj",
	"icon/action/cos_cmd_inventory_close.ddj",
	"icon/action/cos_cmd_inventory_disable.ddj",
	"icon/action/cos_cmd_inventory_open.ddj",
	"icon/action/cos_cmd_prev.ddj",
	"icon/action/cos_cmd_skill_page.ddj",
	"icon/action/cos_cmd_skill_page_disable.ddj",
	"icon/action/cos_cmd_summon.ddj",
	"icon/action/cos_cmd_unsummon.ddj",
	"icon/action/cos_cmd_unsummon_disable.ddj",
	// CIFSkill pane chrome (hand-mirrored pane; popup tab 0x49): the pane
	// resinfo resinfo\ifskill.txt is loaded by CIFSkill::OnCreate sub_58de80
	// (@0x0058de9e) and its satellites by CIFSkillBoard::OnCreate sub_5840c0
	// (ifskillboard.txt @0x005840db), CIFSkillMastery sub_5874a0
	// (ifskill_mastery.txt @0x005874b2) and CIFSkillGroup (ifskill_group.txt
	// @0x00585bc2). The board fill sub_585370 assigns skl_mastery_bar per
	// group row (@0x00585475), the empty-column filler sub_586b40 assigns
	// skl_mastery_nothing (@0x00586e11), the mastery tab flip sub_587500
	// swaps skl_mastery_tab_on/_off (@0x0058752b/0x0058754b), and the frame /
	// board rings (equip_window_ / int_window_) plus com_bg_tile_d and
	// com_tab_off/_on (the runtime tab-group check-tabs, sub_592030
	// @0x00592512/0x0059253c) are already listed above.
	"interface/skill/skl_mastery_tab_off.ddj",
	"interface/skill/skl_mastery_tab_on.ddj",
	"interface/skill/skl_mastery_subject.ddj",
	"interface/skill/skl_mastery_bar.ddj",
	"interface/skill/skl_mastery_levelup.ddj",
	"interface/skill/skl_mastery_levelup_focus.ddj",
	"interface/skill/skl_mastery_levelup_press.ddj",
	"interface/skill/skl_mastery_nothing.ddj",
	"interface/skill/skl_wnd_box.ddj",
	// CIFSkillSlot art: the bind's not-learnable fallback icon (sub_589050
	// @0x00589197 "interface\skill\skl_mastery_disable.ddj"), the level-tab
	// board (ifskill_slot.txt id 3 skl_level_tab.ddj) and the runtime
	// "interface\skill\skl_lv_number_%d.ddj" digit textures the level-digit
	// refresh formats (sub_588620 @0x005886f3/0x0058873f).
	"interface/skill/skl_mastery_disable.ddj",
	"interface/skill/skl_level_tab.ddj",
	"interface/skill/skl_lv_number_0.ddj",
	"interface/skill/skl_lv_number_1.ddj",
	"interface/skill/skl_lv_number_2.ddj",
	"interface/skill/skl_lv_number_3.ddj",
	"interface/skill/skl_lv_number_4.ddj",
	"interface/skill/skl_lv_number_5.ddj",
	"interface/skill/skl_lv_number_6.ddj",
	"interface/skill/skl_lv_number_7.ddj",
	"interface/skill/skl_lv_number_8.ddj",
	"interface/skill/skl_lv_number_9.ddj",
	// The per-slot add-button arts (ifskill_slot.txt id 2 GDR_STMS_BTN_LEVELUP;
	// sub_588af0 picks skl_button_add @0x00588f83 for a state-8 learn,
	// skl_button_up @0x00588e68 for a state-0 next-level train and
	// skl_level_max @0x00588c6b for a maxed group).
	"interface/skill/skl_button_add.ddj",
	"interface/skill/skl_button_up.ddj",
	"interface/skill/skl_level_max.ddj",
	// CIFSkillPracticeBox chrome (resinfo\ifskillpracticebox.txt; the msgbox2
	// learn-confirm box sub_682d10 kind 0 creates over runtime class
	// data_ce9f50 and sub_5de040 configures): the authored msgbox_blackbox /
	// npc_mastery_namebox statics and the com_square_ 6-piece CIFStretchWnd
	// description ring. The msgbox2_window_ ring, com_bg_tile_b,
	// msgbox_itemwindow and the com_button family already ship above.
	"interface/messagebox/msgbox_blackbox.ddj",
	"interface/npc/npc_mastery_namebox.ddj",
	"interface/ifcommon/com_square_left_up.ddj",
	"interface/ifcommon/com_square_right_up.ddj",
	"interface/ifcommon/com_square_left_down.ddj",
	"interface/ifcommon/com_square_right_down.ddj",
	"interface/ifcommon/com_square_left_side.ddj",
	"interface/ifcommon/com_square_right_side.ddj",
	// Skill-pane mastery and group sprites are intentionally absent here.
	// cifResources.mjs derives both native base/focus paths from the complete
	// shipped skillmasterydata.txt and skillgroup.txt tables, covering China,
	// Europe, and future data rows without a race-specific allowlist.
	// CIFParty pane chrome (hand-mirrored pane; popup tab 0x48): the pane
	// resinfo resinfo\ifparty.txt is loaded by CIFParty::OnCreate sub_5b8ce0
	// (@0x005b8d21) and the slot rows by CIFPartySlot::OnCreate sub_5b9900
	// (ifpartyslot.txt @0x005b9935). pt_face is the OnCreate picture texture
	// (@0x005b8e80 pane / @0x005b99f8 slot), pt_hp_disable/pt_mp_disable are
	// the sub_5b8a20 no-party gauge swaps (@0x005b8c2d/0x005b8c64), the
	// kindred 16px marks are the sub_81d5b0 race-byte pick (@0x005b8533 /
	// @0x005b9502; both races carried), and com_pt_leader is the authored
	// leader crown (ifparty.txt id 14). sframe_wnd_ / com_bg_tile_b /
	// com_diamond / com_button* are already listed above.
	"interface/ifcommon/bg_tile/com_bg_tile_a.ddj",
	"interface/ifcommon/com_pt_leader.ddj",
	"interface/ifcommon/com_kindred_china16.ddj",
	"interface/ifcommon/com_kindred_europe16.ddj",
	// Quick-party distance shades: CIFQuickPartySlot_SetDistanceOverlay
	// (5BA400) sets them on GDR_QPS_STATUS by code (5BD0A0 picks the step).
	"interface/quickparty/qpt_face_faraway_60.ddj",
	"interface/quickparty/qpt_face_faraway_70.ddj",
	"interface/quickparty/qpt_face_faraway_80.ddj",
	"interface/quickparty/qpt_face_faraway_90.ddj",
	"interface/quickparty/qpt_face_faraway_100.ddj",
	"interface/party/pt_icon_frame.ddj",
	"interface/party/pt_box.ddj",
	"interface/party/pt_guildname_01.ddj",
	"interface/party/pt_guildname_02.ddj",
	"interface/party/pt_face.ddj",
	"interface/party/pt_hp.ddj",
	"interface/party/pt_mp.ddj",
	"interface/party/pt_hp_disable.ddj",
	"interface/party/pt_mp_disable.ddj",
	"interface/party/pt_msg.ddj",
	"interface/party/pt_slot.ddj",
	"interface/party/pt_button.ddj",
	"interface/party/pt_button_focus.ddj",
	"interface/party/pt_button_press.ddj",
	"interface/party/pt_button_disable.ddj",
	// CIFPartyMatchSlot type-mark art (ifpartymatchslot.txt child 11, the
	// sub_63d500 flag0b-bit0 pick @0x0063d97a/@0x0063d9c9): pt_eachone =
	// each-one distribution (/4), pt_association = union party (/8).
	"interface/party/pt_eachone.ddj",
	"interface/party/pt_association.ddj",
	// CIFApprenticeShip pane chrome (hand-mirrored pane; popup tab 0x4d): the
	// pane resinfo resinfo\ifapprenticeship.txt is loaded by
	// CIFApprenticeShip::OnCreate sub_5c5b60 (@0x005c5b96, plus NotifySubBox)
	// and the slot rows by CIFApprenticeShipSlot::OnCreate (ifapprenticeshipslot
	// .txt @0x005c79b5). pt_slot_co/pt_slot_re are the authored sub-mentor/
	// apprentice row art (ifapprenticeship.txt ids 30..36); pt_no_face is the
	// record+0x74 face leg (@0x005c6f8d/@0x005c634d); com_icon_level_%d is the
	// slot rank mark from record+0x45 (@0x005c7076); com_honor_level_%d is the
	// header honor mark from mgr+0x28c (@0x005c61f2); gil_windo02_off/_on is
	// the notify-subject CIFSelectableArea art pair (@0x005c5e56..0x005c5e91);
	// stl_edit_button* is the notify edit button (NotifySubBox id 62).
	// sframe_wnd_ / com_bg_tile_a/_b / com_button* / pt_face / pt_msg /
	// pt_icon_frame / pt_button* / com_pt_leader are already listed above.
	"interface/party/pt_slot_co.ddj",
	"interface/party/pt_slot_re.ddj",
	"interface/party/pt_no_face.ddj",
	"interface/ifcommon/com_icon_level_1.ddj",
	"interface/ifcommon/com_icon_level_2.ddj",
	"interface/ifcommon/com_icon_level_3.ddj",
	"interface/ifcommon/com_icon_level_4.ddj",
	"interface/ifcommon/com_icon_level_5.ddj",
	"interface/ifcommon/com_icon_level_6.ddj",
	"interface/ifcommon/com_honor_level_1.ddj",
	"interface/ifcommon/com_honor_level_2.ddj",
	"interface/ifcommon/com_honor_level_3.ddj",
	"interface/ifcommon/com_honor_level_4.ddj",
	"interface/ifcommon/com_honor_level_5.ddj",
	"interface/guild/gil_windo02_off.ddj",
	"interface/guild/gil_windo02_on.ddj",
	"interface/stall/stl_edit_button.ddj",
	"interface/stall/stl_edit_button_focus.ddj",
	"interface/stall/stl_edit_button_press.ddj",
	"interface/stall/stl_edit_button_disable.ddj",
	// CIFGuildNotifyWrite window chrome (resinfo\ifguildnotifywrite.txt,
	// opened by the Academy pane's notify-edit route sub_5c50b0 ->
	// sub_69cf00(gi, 1, 2)): frame_msg_ is the subject-edit CIFFrame ring
	// (id 3, 4x4 pieces). msgbox2_window_ / com_bg_tile_b/_e /
	// com_blacksquare_* / com_button* are already listed; the NotifyContents
	// window (ifguildnotifycontents.txt) rides int_window_ / com_scroll_*,
	// also listed.
	"interface/frame/frame_msg_left_up.ddj",
	"interface/frame/frame_msg_left_side.ddj",
	"interface/frame/frame_msg_left_down.ddj",
	"interface/frame/frame_msg_mid_up.ddj",
	"interface/frame/frame_msg_mid_down.ddj",
	"interface/frame/frame_msg_right_up.ddj",
	"interface/frame/frame_msg_right_side.ddj",
	"interface/frame/frame_msg_right_down.ddj",
	// CIFQuest pane chrome (hand-mirrored pane; popup tab 0x4c): the pane
	// resinfo resinfo\ifquest.txt is loaded by CIFQuest::OnCreate sub_5bf4b0
	// (@0x005bf4c2); the 12-row blank fill sub_5c0ff0 spawns base CIFQuestSlot
	// rows (ifquestslot.txt @0x005c25d2, qst_blankwindo art) and the live-quest
	// rows are CIFQuestSlotMain (ifquestslotmain.txt @0x005c3773:
	// qst_subjectwindo base, qst_colorbar_blue authored title bar,
	// com_qst_rightarrow_button dropdown, qst_contentview_button, and the
	// qst_subjectwindo_select tracked bar). The green/red/lightgreen colorbar
	// swaps are the sub_5c3f90 record+0x09 picks (@0x005c4018/0x005c4040/
	// 0x005c4068). com_bg_tile_c is the authored separator/divider tile art
	// (ifquest.txt ids 5, 10..20). sframe_wnd_ / com_scroll_bar are already
	// listed above.
	"interface/ifcommon/bg_tile/com_bg_tile_c.ddj",
	"interface/ifcommon/com_qst_rightarrow_button.ddj",
	"interface/quest/qst_blankwindo.ddj",
	"interface/quest/qst_subjectwindo.ddj",
	"interface/quest/qst_subjectwindo_select.ddj",
	"interface/quest/qst_contentview_button.ddj",
	// sub_5c3840 content-button level-delta swaps (@0x005c389d: player 10+
	// levels above the quest = blue, below the quest level = red).
	"interface/quest/qst_contentviewblue_button.ddj",
	"interface/quest/qst_contentviewred_button.ddj",
	"interface/quest/qst_colorbar_blue.ddj",
	"interface/quest/qst_colorbar_green.ddj",
	"interface/quest/qst_colorbar_red.ddj",
	"interface/quest/qst_colorbar_lightgreen.ddj",
	// CIFQuestSlotSub detail expansion (sub_5c44c0, ifquestslotsub.txt id 1
	// GDR_QUESTSLOT_SUB_WND): the qst_detailwindo row base. The dropdown
	// button's DROPPED art is com_qst_downarrow_button (CIFQuestSlotMain
	// OnCreate sub_5c3760 @0x005c3816 stores it at button+0x3d4; the
	// CIFDropDownButton state swap sub_541c40 picks it when +0x428 is set).
	"interface/quest/qst_detailwindo.ddj",
	"interface/ifcommon/com_qst_downarrow_button.ddj",
	// CIFQuestReward (the content-view popup, resinfo\ifquestreward.txt +
	// OnCreate sub_5c20b0): the authored qst_subwindow_title deco (id 12) and
	// the runtime-assigned qst_scroll_line scroll track (@0x005c2240 onto the
	// id-15 CIFVerticalScroll; the id-14 SCROLLBOARD static authors the same
	// art). gd_paper / gd_scroll_* / sys_button* are already listed above.
	"interface/quest/qst_subwindow_title.ddj",
	"interface/quest/qst_scroll_line.ddj",
	// Wire-item icons (RefItemData icon column under the native icon\ root) +
	// the icon_default fallback (CIFSlotWithHelp_SetSpritePathWithFallback
	// sub_55b450). The mission starter roster derives from the character's
	// visual loadout (weapon kind + CH clothes garment pieces), so carry every
	// CH starter weapon and both gendered clothes-01 piece sets.
	"icon/icon_default.ddj",
	"icon/item/china/weapon/sword_01.ddj",
	"icon/item/china/weapon/blade_01.ddj",
	"icon/item/china/weapon/spear_01.ddj",
	"icon/item/china/weapon/tblade_01.ddj",
	"icon/item/china/weapon/bow_01.ddj",
	"icon/item/china/man_item/clothes_01_ba.ddj",
	"icon/item/china/man_item/clothes_01_la.ddj",
	"icon/item/china/man_item/clothes_01_fa.ddj",
	"icon/item/china/woman_item/clothes_01_ba.ddj",
	"icon/item/china/woman_item/clothes_01_la.ddj",
	"icon/item/china/woman_item/clothes_01_fa.ddj",
	// CH starter armor variants: the account roster derivation also emits the
	// LIGHT and HEAVY garment classes (observed live: heavy_01_{ba,la,fa}
	// 404s for a heavy-set starter character).
	"icon/item/china/man_item/light_01_ba.ddj",
	"icon/item/china/man_item/light_01_la.ddj",
	"icon/item/china/man_item/light_01_fa.ddj",
	"icon/item/china/woman_item/light_01_ba.ddj",
	"icon/item/china/woman_item/light_01_la.ddj",
	"icon/item/china/woman_item/light_01_fa.ddj",
	"icon/item/china/man_item/heavy_01_ba.ddj",
	"icon/item/china/man_item/heavy_01_la.ddj",
	"icon/item/china/man_item/heavy_01_fa.ddj",
	"icon/item/china/woman_item/heavy_01_ba.ddj",
	"icon/item/china/woman_item/heavy_01_la.ddj",
	"icon/item/china/woman_item/heavy_01_fa.ddj",
	// EU starter equivalents (observed live: an EU dagger + light-set character
	// 404'd europe/weapon/dagger_01 and europe/man_item/light_01_{ba,la,fa}).
	// Same derivation as the CH block: every EU create-screen weapon kind plus
	// both gendered degree-01 garment piece sets for all three armor classes.
	"icon/item/europe/weapon/sword_01.ddj",
	"icon/item/europe/weapon/tsword_01.ddj",
	"icon/item/europe/weapon/axe_01.ddj",
	"icon/item/europe/weapon/dagger_01.ddj",
	"icon/item/europe/weapon/crossbow_01.ddj",
	"icon/item/europe/weapon/darkstaff_01.ddj",
	"icon/item/europe/weapon/tstaff_01.ddj",
	"icon/item/europe/weapon/wand_01.ddj",
	"icon/item/europe/weapon/harp_01.ddj",
	"icon/item/europe/weapon/staff_01.ddj",
	"icon/item/europe/weapon/shield_01.ddj",
	"icon/item/europe/man_item/clothes_01_ba.ddj",
	"icon/item/europe/man_item/clothes_01_la.ddj",
	"icon/item/europe/man_item/clothes_01_fa.ddj",
	"icon/item/europe/woman_item/clothes_01_ba.ddj",
	"icon/item/europe/woman_item/clothes_01_la.ddj",
	"icon/item/europe/woman_item/clothes_01_fa.ddj",
	"icon/item/europe/man_item/light_01_ba.ddj",
	"icon/item/europe/man_item/light_01_la.ddj",
	"icon/item/europe/man_item/light_01_fa.ddj",
	"icon/item/europe/woman_item/light_01_ba.ddj",
	"icon/item/europe/woman_item/light_01_la.ddj",
	"icon/item/europe/woman_item/light_01_fa.ddj",
	"icon/item/europe/man_item/heavy_01_ba.ddj",
	"icon/item/europe/man_item/heavy_01_la.ddj",
	"icon/item/europe/man_item/heavy_01_fa.ddj",
	"icon/item/europe/woman_item/heavy_01_ba.ddj",
	"icon/item/europe/woman_item/heavy_01_la.ddj",
	"icon/item/europe/woman_item/heavy_01_fa.ddj",
	// CIFOption window family (ifoption_*.txt pages hosted by the CIFSystemWnd
	// ESC/system menu). Runtime references the resinfo copy pass cannot see:
	// opt_inner_box_ is a CIFFrame ring PREFIX (ifoption_audio.txt group
	// frames ids 4..6, ifoption_input.txt id 11 author DDJ
	// "interface\\option\\opt_inner_box_"); the frame renderer appends the
	// eight piece suffixes at draw time. com_radiobutton_on is the CHECKED
	// state sibling the CIFCheckBox art family ships next to the authored
	// com_radiobutton_off (audio mutes, camera sight modes, input mouse rule).
	"interface/option/opt_inner_box_left_up.ddj",
	"interface/option/opt_inner_box_mid_up.ddj",
	"interface/option/opt_inner_box_right_up.ddj",
	"interface/option/opt_inner_box_left_side.ddj",
	"interface/option/opt_inner_box_right_side.ddj",
	"interface/option/opt_inner_box_left_down.ddj",
	"interface/option/opt_inner_box_mid_down.ddj",
	"interface/option/opt_inner_box_right_down.ddj",
	"interface/ifcommon/com_radiobutton_on.ddj",
	// CIFOption page-interior slot rows, all assigned from OnCreate code:
	// CIFGameOptionSlot rows take opt_key02 (sub_5cb7f0 @0x005cb99b),
	// CIFKeyOptionSlot rows take opt_key (sub_5cc8c0 @0x005ccaa8) with the
	// opt_key_select overlay under the selected slot's key cell (sub_5c9020
	// @0x005c904d), and CIFVideoOptionSlot rows take opt_video_control_02
	// (sub_5ce410 @0x005ce9c5). The game-slot checkbox pair rides the
	// authored ifgameoptionslot.txt com_checkbutton_off + its _on sibling.
	// The three slot resinfos are generated layouts; these row backgrounds
	// remain explicit because their parent OnCreate code assigns them.
	"interface/option/opt_key.ddj",
	"interface/option/opt_key02.ddj",
	"interface/option/opt_key_select.ddj",
	"interface/option/opt_video_control_02.ddj",
	// Entity overhead-overlay marks, all runtime CODE references the resinfo
	// pass never sees: europe_partymob is the 16x16 monster party-mob mark the
	// overhead pass draws when CICMonster+0x771 == 1, icon_rudiment the 16x16
	// CICUser visual-flag bit0 icon. (com_job_merchant/thief/hunter above
	// double as the overhead job marks.)
	"icon/item/etc/europe_partymob.ddj",
	"icon/etc/icon_rudiment.ddj",
	// Fortress-war overhead marks (32x32), the same runtime-reference lane:
	// published with the set so the deferred fortress-war chrome finds them.
	"icon/etc/mark_fortress2.ddj",
	"icon/etc/mark_fortress3.ddj",
	"icon/etc/mark_aggressive2.ddj",
	"icon/etc/mark_aggressive3.ddj",
	"icon/etc/mark_defensive2.ddj",
	"icon/etc/mark_defensive3.ddj",
	// Fortress-war EMBLEM sprites (a different lane from the mark_* status
	// icons above): sub_914ae0 walks the CSiegeFortressData records
	// (GlobalDataManager map488 off 0xcec870, stride 0xb8) parsed from
	// textdata\siegefortress.txt, DDJ_SpriteCreate's each record's CrestPath128
	// (record+0x9c, rooted under icon\) and inserts the sprite into the
	// fortress-id -> emblem map at 0xf0935c keyed by record+0x00. This Media's
	// siegefortress.txt ships ONE row (id 1 FORTRESS_JANGAN ->
	// etc\fort_jangan.ddj); the retail/Eternity reference tables also carry
	// id 3 hotan and id 6 bijeokdan and both DDJs ship in this Media.pk2, so
	// publish them too - the client stays correct if the textdata grows back.
	// NOT listed here (on disk under icon/etc/ but referenced by no known
	// siegefortress table row): fort_constantinople, fort_donwhang,
	// fort_evilorder, fort_heukmakdan, fort_progress, fort_samarkand,
	// fort_worldmap. (fort_progress alone still publishes - the
	// ifplayerminiinfo.txt resinfo authors it as a DDJ field, a different
	// lane from the emblem map.)
	"icon/etc/fort_jangan.ddj",
	"icon/etc/fort_hotan.ddj",
	"icon/etc/fort_bijeokdan.ddj"
];
// CPSCharacterCreate sub_730d00 @ 0x00730d00 swaps GDR_BTN_ZOOM art at runtime
// (zoomin.ddj on create, zoomout.ddj after first click). Layout only references zoomin.
/*
================
runtimeCifButtonImageReferences

The publisher expands each base path to its authored focus/press siblings.
================
*/
export const runtimeCifButtonImageReferences = [
	// CIFSkillBoard 5841D0 and CIFSkillSlot 588AF0 select restoration mode.
	"interface/recycle/rec_setup_button.ddj",
	"interface/recycle/rec_set_button.ddj",
	// 548060 swaps open/close independently of the authored initial image.
	"interface/skill/skl_button_up.ddj",
	"interface/quick_slot/qsl_hclose_button.ddj",
	"interface/quick_slot/qsl_vclose_button.ddj",
	"interface/messagebox/msgbox_rebirth_button.ddj",
	// CPSCharacterCreate swaps both sex controls between *_on and *_off at
	// runtime. The layouts expose only the initial pair, so all four normal
	// paths must enter the button-state publication pass with their focus and
	// press siblings.
	"interface/outer/man_on.ddj",
	"interface/outer/man_off.ddj",
	"interface/outer/woman_on.ddj",
	"interface/outer/woman_off.ddj",
	"interface/outer/zoomout.ddj",
	"interface/mall/mall_communicate_direct.ddj",
	"interface/mall/mall_communicate_close.ddj",
	// CIFCloseButton (sub_5419c0 OnCreate): the frame chrome close button -
	// created programmatically for every style-0x40 frame window, so no
	// resinfo references it; copyButtonStateImages probes every state sibling
	// SetDdjStateTexturesFromPath derives. This media ships _focus/_press but
	// no _disable; the generated catalog preserves that native cache miss.
	"interface/ifcommon/com_windowclose.ddj"
];
