// Publish the resinfo\ginterface.txt Toggle*Window layout sections as RAW
// authored lines: the native CIFMainFrame section table (CIFMainFrame+0xc)
// is filled from this file by the resinfo INIF parser (CIRMManager_GetResInfo /
// ResinfoRegistry_FindOrParseSection, sole caller CIFWnd_LoadResourceInfo
// AttachResourceSection), and the CGInterface Toggle*Window family
// (CGInterface_ToggleAlchemyWindow AlchemyBox 0x2c / CGInterface_ToggleAutoPotionWindow AutoPotion 0x87 / CGInterface_ToggleStallNetworkWindow
// StallNetwork 0x96) instantiates each section through CIFWnd_CreateChildrenFromLayout /
// CIFMainFrame_CreateControlsFromSection.
//
// NO field decoding happens here (the actionwnddata raw-rows precedent): the
// browser bridge (bridge/ui/section/sectionWindowPlane.ts) decodes the authored
// Key=TYPE,"value" properties in exactly one place. This builder only frames
// the shipped bytes: it extracts each requested `Section = Name,...` block
// verbatim (header line + brace-balanced body) and ships the raw lines.
//
// Output: .generated/client-public/assets/data/ginterface-sections.json
// Fetched lazily by the section-window plane (ensureWipSectionWindowAuthoring,
// kicked at mission scene mount - the consoleCommandBridge prefetch pattern).

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { extractResinfoSection } from "../shared/resinfoParser.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";

import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const ginterfacePath = path.join(
  extractedRoot,
  "Media_extracted",
  "resinfo",
  "ginterface.txt"
);

/**
 * The window-plane sections consumed by the browser Toggle*Window binds.
 * IFStall (0x21, CGInterface_SetStallWindowActive) joined for W3's case-9 anchor arm
 * (frontier-wave seq278). Ghacha (0x8c, CGInterface_SetGachaWindowVisible) and
 * GrantMagicAttribute (0x9c, CGInterface_SetGrantMagicAttributeWindowVisible) joined for the 0xB338
 * window-action arms; the GrantMagicAttribute block sits inside an
 * `#ifdef APPLY_AVATAR_SYSTEM` guard in the authored file, but the
 * extraction is brace-balanced from the `Section =` header so the raw
 * block ships clean (and the live v1.150 client dispatches the
 * 0x80000000 arm that opens it). The same file authors ~50 more
 * sections - they join this list when their window planes land.
 */
const SECTION_NAMES = [
  "AlchemyBox",
  "AutoPotion",
  "StallNetwork",
  "IFStall",
  "PartyMatch",
  "MentorMatch",
  "COSWnd",
  "CompositeItem",
  // Generic NPC-talk actions 0x17/0x18 lazily create these two authored
  // guild-management windows through CGInterface_ShowGuildLevelUpWnd/CGInterface_ShowGuildMasterLeaveWnd.
  "GuildLevelUp",
  "GuildMasterElection",
  "GuildMasterLeave",
  "Ghacha",
  "GrantMagicAttribute",
  "SkillWithdrawal",
  // NPC-talk 0x22/0x23 and opcode 0xB37E lazily materialize the two job
  // ranking windows through CGInterface_SetJobActiveRankWindowVisible/CGInterface_SetJobContributionRankWindowVisible.
  "JobActiveRank",
  "JobContributionRank",
  // Opcode 0xB5EE result-1: CGInterface_SetPrevJobInfoWindowVisible lazily creates control 0x42 from
  // this authored outer-window section.
  "PrevJobInfo"
];

/**
 * Per-window resinfo files whose OWN sections the native attaches through
 * CIFWnd_LoadResourceInfo / CIFMainFrame_AttachResourceSection (frame+0xc source) before
 * running CIFWnd_CreateChildrenFromLayout over a section name WITHIN that file. Shipped keyed by
 * the native attach path so the browser registry twin (data_cec7d0) can
 * cache-hit exactly like the native find-or-parse (CIRMManager_GetResInfo).
 * COS Stage 2 (server-wave W4): the two COS entry-row frames
 * (CIFCOSCommand OnCreate CIFCOSCommand_OnCreate / CIFCOSStatus OnCreate CIFCOSStatus_OnCreate).
 */
const RESINFO_FILE_SECTIONS = [
  // The six always-mounted mission roots are one synchronous CGInterface
  // construction unit. Their React JSON publication is not sufficient for
  // the WIP vFunc_9 folds: the native attach/create registry must carry each
  // class's own resinfo bytes, including nested OnCreate dependencies.
  {
    file: "ifplayerminiinfo.txt",
    attachPath: "resinfo\\ifplayerminiinfo.txt",
    sections: ["Create"]
  },
  // CGInterface constructs three persistent CIFNotify siblings during the
  // same HUD OnCreate walk. Their class OnCreate attaches this file before
  // the owner applies the per-channel skin/Y/RGB overrides; publishing only
  // ifnotify.json left the native section registry unable to run that path.
  {
    file: "ifnotify.txt",
    attachPath: "resinfo\\ifnotify.txt",
    sections: ["Create"]
  },
  {
    file: "ifpetminiinfo.txt",
    attachPath: "resinfo\\ifpetminiinfo.txt",
    sections: ["Create"]
  },
  {
    file: "ifunderbar.txt",
    attachPath: "resinfo\\ifunderbar.txt",
    sections: ["Create", "AdditionalQuickSlot", "QuickSlotNumber"]
  },
  // CIFSlotWithHelp::OnCreate attaches an intentionally empty Create section;
  // it still must exist in the registry so attach is a real cache hit.
  {
    file: "ifslotwithhelp.txt",
    attachPath: "resinfo\\ifslotwithhelp.txt",
    sections: ["Create"]
  },
  // CIFHelperBubbleWindow is a persistent CGInterface child. Its OnCreate
  // owns a second, embedded CIFMainFrame walk over these four authored
  // controls; publishing only the React layout left the WIP registry unable
  // to execute the native attach/create lifecycle.
  {
    file: "ifhelpbubblewindow.txt",
    attachPath: "resinfo\\ifhelpbubblewindow.txt",
    sections: ["Create"]
  },
  {
    file: "ifminimap.txt",
    attachPath: "resinfo\\ifminimap.txt",
    sections: ["Create"]
  },
  {
    file: "ifchatviewer.txt",
    attachPath: "resinfo\\ifchatviewer.txt",
    sections: ["Create"]
  },
  {
    file: "ifwhisperlist.txt",
    attachPath: "resinfo\\ifwhisperlist.txt",
    sections: ["Create"]
  },
  {
    file: "ifsystemmessage.txt",
    attachPath: "resinfo\\ifsystemmessage.txt",
    sections: ["Create"]
  },
  {
    file: "ifchatoptionboard.txt",
    attachPath: "resinfo\\ifchatoptionboard.txt",
    sections: ["Create"]
  },
  // CIFRegionView::OnCreate/Initialize (CIFRegionView_OnCreate) attaches this child
  // frame during CGInterface::OnCreate.  It is a mandatory fresh-mission
  // boot dependency: region-crossed and mission-reveal dereference control
  // 0x53 and its ids 1/0xb/0xc before any optional HUD window is opened.
  {
    file: "ifregionview.txt",
    attachPath: "resinfo\\ifregionview.txt",
    sections: ["Create"]
  },
  // The target-status family is one authored object graph, not a collection
  // of browser-sized rectangles. CIFTargetStatusPanel::OnCreate
  // (CIFTargetWindow_OnCreate) creates the six children from iftargetwindow.txt; each
  // target-kind child then attaches its own Create section. Shipping the
  // complete family preserves the native DFS construction order, including
  // the id-11 CIFCloseButton and id-100 CIFBuffViewer prerequisites.
  {
    file: "iftargetwindow.txt",
    attachPath: "resinfo\\iftargetwindow.txt",
    sections: ["Create"]
  },
  {
    file: "iftw_specialmob.txt",
    attachPath: "resinfo\\iftw_specialmob.txt",
    sections: ["Create"]
  },
  {
    file: "iftw_commonenemy.txt",
    attachPath: "resinfo\\iftw_commonenemy.txt",
    sections: ["Create"]
  },
  {
    file: "iftw_player.txt",
    attachPath: "resinfo\\iftw_player.txt",
    sections: ["Create"]
  },
  {
    file: "iftw_jobplayer_trijob2.txt",
    attachPath: "resinfo\\iftw_jobplayer_trijob2.txt",
    sections: ["Create"]
  },
  {
    file: "iftw_fortressstructure.txt",
    attachPath: "resinfo\\iftw_fortressstructure.txt",
    sections: ["Create"]
  },
  { file: "ifcoscommand.txt", attachPath: "resinfo\\ifcoscommand.txt", sections: ["Create"] },
  { file: "ifcosstatus.txt", attachPath: "resinfo\\ifcosstatus.txt", sections: ["Create"] },
  // The COS window frame (CIFCOS OnCreate CIFCOS_Initialize attaches it @0x6a13bb;
  // "Create" = the int_window_ inner frame, the three page sections are the
  // CIFCOSCommandPanel_ShowInfoWindow/CIFWnd_Str_COSInventoryWnd/CIFWnd_Str_COSSetupWnd page-visible creates).
  {
    file: "ifcos.txt",
    attachPath: "resinfo\\ifcos.txt",
    sections: ["Create", "COSInfoWnd", "COSInventoryWnd", "COSSetupWnd"]
  },
  // The three COS page interiors (COS window unit, page-interior wave):
  // CIFCOSInfo OnCreate CIFCOSInfo_OnCreate attaches ifcosinfo.txt @0x6a5762,
  // CIFCOSInventory OnCreate CIFCos_InitializeCommandSlots attaches ifcosinventory.txt
  // @0x6a8c80 ("Create" + the type-1 "TradeInfo" section the record bind
  // CIFCosBag_BindCompanion creates @0x6a9465), CIFCOSSetup OnCreate CIFCOSSetup_vf24
  // attaches ifcossetup.txt @0x6a9a24. ifcosinfoslot.txt is the dynamic
  // passenger-row class (CIFCOSInfoSlot OnCreate CIFCosInfoSlot_vf24 @0x6a7ad2) -
  // shipped with its parent page so the transport rows can fold later.
  { file: "ifcosinfo.txt", attachPath: "resinfo\\ifcosinfo.txt", sections: ["Create"] },
  { file: "ifcosinfoslot.txt", attachPath: "resinfo\\ifcosinfoslot.txt", sections: ["Create"] },
  {
    file: "ifcosinventory.txt",
    attachPath: "resinfo\\ifcosinventory.txt",
    sections: ["Create", "TradeInfo"]
  },
  { file: "ifcossetup.txt", attachPath: "resinfo\\ifcossetup.txt", sections: ["Create"] },
  // The CIFCheckBox chrome (OnCreate CIFCheckBox_OnCreate attaches it @0x541823:
  // the id-0 GDR_STATIC_SELECTED com_checkbutton_on check mark) and the
  // CIFSpinButtonCtrl chrome (OnCreate CIFSpinButtonCtrl_OnCreate attaches it @0x542a40:
  // ids 0/1/2 page text + prev/next arrows) - the ifcossetup checkboxes
  // and the ifcosinventory pager author these classes.
  { file: "ifcheckbox.txt", attachPath: "resinfo\\ifcheckbox.txt", sections: ["Create"] },
  { file: "ifspincontrol.txt", attachPath: "resinfo\\ifspincontrol.txt", sections: ["Create"] },
  // The message-box frame (CIFMessageBox OnCreate CIFMessageBox_OnCreate attaches it;
  // "Create" = the shared chrome, "MsgBoxSimple" = the kind-9 confirm-box
  // layout the 0x3f1 premium-avatar arm builds through CIFSimpleMessageBox_OnCreate,
  // "MsgBoxINIF" = the kind-1 tracking-entry layout built through CIFMessageBox_BuildTrackingLayout
  // by the native 0x7164 friend-tracking push).
  {
    file: "ifmessagebox.txt",
    attachPath: "resinfo\\ifmessagebox.txt",
    // "MsgBoxInsertMsg" = the kind-4 input-box layout (CIFMessageBox_BuildInputLayout; the
    // stall slot-spinner overlay's content kind).
    sections: ["Create", "MsgBoxSimple", "MsgBoxINIF", "MsgBoxInsertMsg"]
  },
  // The personal-stall window family (the CIFStall OnCreate CIFStall_OnCreate
  // cascade): the stall frame chrome, the per-slot chrome (CIFStallSlot_OnCreate),
  // the chat module (CIFChatModule_OnCreate) and the scroll manager (CIFScrollManager_OnCreate).
  { file: "ifstall.txt", attachPath: "resinfo\\ifstall.txt", sections: ["Create"] },
  { file: "ifstallslot.txt", attachPath: "resinfo\\ifstallslot.txt", sections: ["Create"] },
  { file: "ifchatmodule.txt", attachPath: "resinfo\\ifchatmodule.txt", sections: ["Create"] },
  {
    file: "ifscrollmanager.txt",
    attachPath: "resinfo\\ifscrollmanager.txt",
    sections: ["Create"]
  },
  // The CIFVerticalScroll OnCreate (CIFVerticalScroll_OnCreate) attaches its own chrome.
  {
    file: "ifverticalscroll.txt",
    attachPath: "resinfo\\ifverticalscroll.txt",
    sections: ["Create"]
  },
  // The composite-item window interior (CIFCompositeItemWnd OnCreate
  // CIFCompositeItemWnd_OnCreate attaches it @0x6af6d2; "Create" = the GDR_COMPOSITE_FRAME
  // int_window_ ring id 6 + the GDR_COMPOSITE_BGTILE com_bg_tile_b id 5).
  {
    file: "ifcompositeitemwnd.txt",
    attachPath: "resinfo\\ifcompositeitemwnd.txt",
    sections: ["Create"]
  },
  // The party-match window interior (CIFPartyMatch OnCreate CIFPartyMatch_OnCreate
  // attaches it @0x637420 and creates the four sections @0x63744d..
  // 0x6374bf; "PartyRegister" is the register form child 0x69 the
  // register/modify button legs create on demand, CIFPartyMatch_OpenRegisterDialog @0x6351a0/
  // @0x63510c). "JoinProgress"/"AutoMatch" stay un-shipped: their windows
  // (CIFPartyJoinProgress 0x64 / CIFPartyMatchAuto 0x6e) are unfolded
  // frontiers.
  {
    file: "ifpartymatch.txt",
    attachPath: "resinfo\\ifpartymatch.txt",
    sections: ["Create", "SearchInfo", "SlotListButton", "SlotList", "PartyRegister"]
  },
  // The slot-row interior (CIFPartyMatchSlot OnCreate CIFPartyMatchSlot_OnCreate attaches
  // it @0x63da32: the id-5 com_bar01_ select bar + statics 10..17).
  { file: "ifpartymatchslot.txt", attachPath: "resinfo\\ifpartymatchslot.txt", sections: ["Create"] },
  // The register form interior (CIFPartyMatchRegister OnCreate CIFPartyMatchRegister_Initialize
  // attaches it @0x63c02d: purpose radio 0x28, level edits 0x2a/0x2b,
  // share statics 0x2d/0x2f, title edit 0x31, OK/Cancel 0x3c/0x3d).
  {
    file: "ifpartymatchregister.txt",
    attachPath: "resinfo\\ifpartymatchregister.txt",
    sections: ["Create"]
  },
  // The exchange (player-trade) window interior (CIFExchange OnCreate
  // CIFExchange_OnCreate attaches it @0x6b279d via the pinned data_bfbb60
  // "resinfo\\ifexchange.txt" and creates "Create" @0x6b27c9: the 12+12
  // CIFSlotWithHelp offer grids ids 200..211 / 100..111, the EXCHANGE /
  // CANCEL / money buttons 11/12/15 and the money statics 13/14/18/19).
  { file: "ifexchange.txt", attachPath: "resinfo\\ifexchange.txt", sections: ["Create"] },
  // The alchemy window interior family (alchemy packet-domain wave):
  // CIFAlchemyBox OnCreate CIFAlchemyBox_OnCreate attaches ifnewalchemybox.txt
  // @0x620461 and caches the created panels (+0x370 = id 0x15
  // CIFAlchemyProcess, +0x374 = id 0x16 CIFAlchemyReinforce, chrome ids
  // 0/1/2); the two panel OnCreates attach their own interiors -
  // CIFAlchemyProcess CIFAlchemyProcess_OnCreate @0x622714 (9 staging slots ids
  // 0x26..0x2e + the id-0x32 CIFDecoratedStatic gauge + the 0x1e..0x23
  // button row), CIFAlchemyReinforce CIFAlchemyReinforce_OnCreate @0x625614 (5 slots ids
  // 0x26..0x2a + the id-0x32 gauge + the id-0x1e process button).
  {
    file: "ifnewalchemybox.txt",
    attachPath: "resinfo\\ifnewalchemybox.txt",
    sections: ["Create"]
  },
  {
    file: "ifalchemyprocess.txt",
    attachPath: "resinfo\\ifalchemyprocess.txt",
    sections: ["Create"]
  },
  {
    file: "ifnewalchemyreinforce.txt",
    attachPath: "resinfo\\ifnewalchemyreinforce.txt",
    sections: ["Create"]
  },
  // The packetless 0x3230 skill-withdrawal open chain:
  // CIFSkillWithdrawal OnCreate CIFSkillWithdrawal_CreateControls attaches
  // ifskillwithdrawal.txt and creates its CIFSkill child; CIFSkill
  // OnCreate CIFSkill_OnCreate attaches ifskill.txt; its CIFSkillBoard child
  // OnCreate CIFSkillBoard_OnCreate attaches ifskillboard.txt.
  {
    file: "ifskillwithdrawal.txt",
    attachPath: "resinfo\\ifskillwithdrawal.txt",
    sections: ["Create"]
  },
  {
    file: "ifskill.txt",
    attachPath: "resinfo\\ifskill.txt",
    sections: ["Create", "MainSkillWnd", "Withdrawal"]
  },
  {
    file: "ifskillboard.txt",
    attachPath: "resinfo\\ifskillboard.txt",
    sections: ["Create"]
  },
  // CIFPrevJobInfo OnCreate CIFPrevJobInfo_OnCreate attaches this file and creates its
  // 22-control Merchant/Hunter/Thief interior.
  {
    file: "ifprevjobinfo.txt",
    attachPath: "resinfo\\ifprevjobinfo.txt",
    sections: ["Create"]
  },
  // JobActiveRank (CIFJobRank OnCreate) and JobContributionRank attach
  // their authored interiors; each of their ten dynamic rows attaches the
  // matching slot file before creating its own "Create" section.
  {
    file: "ifjobrank.txt",
    attachPath: "resinfo\\ifjobrank.txt",
    sections: ["Create"]
  },
  {
    file: "ifjobrankslot.txt",
    attachPath: "resinfo\\ifjobrankslot.txt",
    sections: ["Create"]
  },
  {
    file: "ifjobcontributionrank.txt",
    attachPath: "resinfo\\ifjobcontributionrank.txt",
    sections: ["Create"]
  },
  {
    file: "ifjobcontributionrankslot.txt",
    attachPath: "resinfo\\ifjobcontributionrankslot.txt",
    sections: ["Create"]
  }
];

export async function buildGInterfaceSectionsAsset() {
  if (!fs.existsSync(ginterfacePath)) {
    throw new Error(`[ginterface-sections] required source missing (${ginterfacePath})`);
  }

  // resinfo\ginterface.txt is a single-byte-encoded authored file (unlike
  // the UTF-16LE textdata tables); latin1 preserves the bytes 1:1. The three
  // shipped window sections are pure ASCII - assert that so a future
  // re-extract cannot silently ship mis-decoded bytes.
  const text = fs.readFileSync(ginterfacePath, "latin1");
  const lines = text.split(/\r?\n/);

  const sections = {};
  for (const name of SECTION_NAMES) {
    const block = extractResinfoSection(lines, name);
    if (!block) {
      throw new Error(`[ginterface-sections] required ginterface section ${name} not found`);
    }
    for (const line of block) {
      // eslint-disable-next-line no-control-regex
      if (/[^\x00-\x7f]/.test(line)) {
        throw new Error(
          `[ginterface-sections] non-ASCII byte in section ${name}: ${JSON.stringify(line)}`
        );
      }
    }
    sections[name] = block;
  }

  // The per-window resinfo files, keyed by their native attach path. Same
  // raw-lines discipline: brace-balanced verbatim blocks, zero decoding.
  const resinfoSections = {};
  for (const { file, attachPath, sections: names } of RESINFO_FILE_SECTIONS) {
    const filePath = path.join(extractedRoot, "Media_extracted", "resinfo", file);
    if (!fs.existsSync(filePath)) {
      throw new Error(`[ginterface-sections] required resinfo source missing (${filePath})`);
    }
    const fileLines = fs.readFileSync(filePath, "latin1").split(/\r?\n/);
    const fileSections = {};
    for (const name of names) {
      const block = extractResinfoSection(fileLines, name);
      if (!block) {
        throw new Error(`[ginterface-sections] required ${file} section ${name} not found`);
      }
      for (const line of block) {
        // eslint-disable-next-line no-control-regex
        if (/[^\x00-\x7f]/.test(line)) {
          throw new Error(
            `[ginterface-sections] non-ASCII byte in ${file} section ${name}: ${JSON.stringify(line)}`
          );
        }
      }
      fileSections[name] = block;
    }
    resinfoSections[attachPath] = fileSections;
  }

  const catalog = {
    sourcePath: path.relative(gameRoot, ginterfacePath).replaceAll("\\", "/"),
    format: "sro-ginterface-sections",
    version: 2,
    sections,
    resinfoSections
  };

  const { outPath } = exportDataAsset({
    publicRoot,
    outputFileName: "ginterface-sections.json",
    value: catalog
  });

  // This builder is also a supported standalone entry point. A browser asks
  // for the gzip member from the game-data pack before it falls back to loose
  // JSON, while Vite serves Brotli directly when available. Publishing only
  // the new raw JSON therefore leaves two older authorities able to hide it.
  await refreshPrecompressedSidecars([outPath]);

  console.log(
    `[ginterface-sections] wrote ${Object.keys(sections).length} raw sections + ` +
      `${Object.keys(resinfoSections).length} resinfo paths -> ${path.relative(publicRoot, outPath)}`
  );
  return {
    written: true,
    sections: Object.keys(sections).length,
    resinfoPaths: Object.keys(resinfoSections).length,
    outPath
  };
}

// Run directly (node scripts/build/data/buildGInterfaceSectionsAsset.mjs) or
// via the resource build aggregator.
if (isMainScript(import.meta.url)) {
  await buildGInterfaceSectionsAsset();
}
