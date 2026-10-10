/*
===========================================================================

resources.ts - HUD metadata admission and lifetime

Owns layout and catalogue requests, decoded HUD data and image warm paths.
The UI calls step every frame. Demand permits new requests; completions are
always collected so a hidden HUD cannot retain the shared asset slots.
Dispose cancels any requests still owned by this module.

===========================================================================
*/

import { decodePortalCatalog, type PortalCatalog } from "@/engine/foundation/gameplay/portal";
import { decodeTooltipMasteries, type TooltipMastery } from "@/engine/foundation/ui/mastery-tooltip";
import { masteryCosts } from "@/engine/foundation/gameplay/skill-catalog";
import { withdrawalGoldPrices } from "@/engine/foundation/gameplay/withdrawal";
import { tradeGoldBases } from "@/engine/foundation/gameplay/specialty-deal";
import { decodeMallNotify, type MallNotify } from "@/engine/foundation/ui/mall-notify";
import { type JobExpThresholds, jobExpThresholds } from "@/engine/foundation/gameplay/player-info-job";
import { nativeWindowSections } from "@/engine/foundation/ui/native-window-sections";
import { partyCharacterCountries } from "@/engine/foundation/gameplay/party-matching";
import { createTooltipSkillDecoder } from "@/engine/foundation/ui/skill-tooltip-catalog";
import type { TooltipSkillCatalog } from "@/engine/foundation/ui/skill-tooltip-data";
import { decodeActionSlots, type ActionSlot } from "@/engine/foundation/ui/action-layout";
import { decodeMapLabels, decodeMapIcons, type MapLabel, type MapIcon } from "@/engine/foundation/ui/world-map";
import { creationNameRules, type NameRules } from "@/engine/foundation/ui/character-create";
import { decodeSkillUi, type SkillUi } from "@/engine/foundation/ui/skill-layout";
import { equipmentSocket } from "@/engine/foundation/ui/inventory-layout";
import { decodeMessageTips, type MessageTip } from "@/engine/foundation/ui/message-tips";
import type { AssetOwner } from "@/engine/contracts/assets";
import { decodeAuthoredLayout, type AuthoredLayout } from "@/engine/foundation/ui/authored-layout";
import { decodeCosReferences, type CosReference } from "@/engine/foundation/ui/cos-command";
import { decodeCommandTable } from "@/engine/foundation/ui/debug-commands";
// Skill catalogue rows decoded per HUD step: about 2.5 ms of a frame, so the
// 27,835-row catalogue settles in under 30 frames.
const TOOLTIP_ROWS_PER_STEP = 1024;
// Authored button state images: normal, focus, press, disable.
const BUTTON_STATE_SUFFIXES = [ "", "_focus", "_press", "_disable" ] as const;
/*
================
Load
================
*/
type Load = { kind: "idle"; } | { kind: "loading"; id: number; } | { kind: "ready"; value: unknown; } | {
	kind: "decoding";
} | {
	kind: "failed";
	message: string;
} | { kind: "disposed"; };
/*
================
HudData
================
*/
interface HudData {
	readonly withdrawalPage: AuthoredLayout;
	readonly stallPricePage: AuthoredLayout;
	readonly stallTextPage: AuthoredLayout;
	readonly cosReferences: ReadonlyMap<number, CosReference>;
	readonly portals: PortalCatalog;
	readonly tooltipMasteries: ReadonlyMap<number, TooltipMastery>;
	readonly masteryCosts: Readonly<Record<number, number>>;
	readonly withdrawalGoldPrices: Readonly<Record<number, number>>;
	// levelgold.txt column 2 by level: the trade scale's basis (649050).
	readonly tradeGoldBases: Readonly<Record<number, number>>;
	readonly mallNotify: MallNotify;
	// config\command.txt: the debug console's command names by id (68D9C0).
	readonly commandTable: ReadonlyMap<string, number>;
	readonly jobExpThresholds: JobExpThresholds;
	readonly extended: readonly AuthoredLayout[];
	readonly countries: Readonly<Record<number, number>>;
	readonly tooltipSkills: TooltipSkillCatalog;
	readonly mapIcons: readonly MapIcon[];
	readonly actions: readonly ActionSlot[];
	readonly mapLabels: readonly MapLabel[];
	readonly nameRules: NameRules;
	readonly skillUi: SkillUi;
	readonly popupArt: {
		readonly tab: string;
		readonly blocked: string;
		readonly portrait: string;
		readonly sockets: Readonly<Record<number, string>>;
	};
	readonly warmPaths: readonly string[];
	readonly windows: Readonly<Record<string, AuthoredLayout>>;
	readonly tips: readonly MessageTip[];
	readonly targets: Readonly<Record<string, AuthoredLayout>>;
	readonly root: AuthoredLayout;
	readonly player: AuthoredLayout;
	readonly bar: AuthoredLayout;
	readonly minimap: AuthoredLayout;
	readonly map: AuthoredLayout;
	readonly chat: AuthoredLayout;
	readonly status: AuthoredLayout;
	readonly regionCodes: Readonly<Record<string, string>>;
	readonly zones: Readonly<Record<string, string>>;
	readonly strings: Readonly<Record<string, string>>;
}
// Polled requests are children of the existing UI/asset lifetime. Layout and text
// share admission and cancellation; neither can outlive its HUD.
/*
================
createHudResources
================
*/
export function createHudResources(
	assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">,
	base: string
) {
	const targetNames = [
		"iftargetwindow",
		"iftw_specialmob",
		"iftw_commonenemy",
		"iftw_player",
		"iftw_jobplayer_trijob2",
		"iftw_fortressstructure"
	];
	const windowNames = [
		"ifextquickslotoption",
		"ifpetminiinfo",
		"if_npcwindow",
		"if_npctalk",
		"ifstore",
		"ifitemmall",
		"ifitemmallshop",
		"ifitemmallshopslot",
		"ifitemmallmyinfo",
		"ifitemmalltrunk",
		"ifitemmallinventory",
		"ifitemmalltrunkexpbar",
		"ifpagemanager",
		"ifspincontrol",
		"ifitemmallusepoint",
		"ifitemmallconfirmbuy",
		"ifitemmallconfirmslot",
		"ifmessagebox",
		"ifcommunity",
		"ifguild",
		"ifguildmemberslot",
		"ifguildnotifywrite",
		"ifguildpointup",
		"ifguildpositiongrant",
		"ifguildgrantpower",
		"ifguildgrantpowerslot",
		"ifallianceguild",
		"ifguildrelations",
		"ifhostileguild",
		"ifhostileguildslot",
		"ifwarstate",
		"ifguildwar",
		"ifguildwarrequest",
		"ifguildwarconfirm",
		"ifguildwaragree",
		"ifexchange",
		"ifstall",
		"ifchatmodule",
		"ifstallslot",
		"ifstallnetwork",
		"ifstallnetworkslot",
		"ifallianceguildslot",
		"ifcos",
		"ifcosinventory",
		"ifcosinfo",
		"ifcossetup",
		"ifstorageroom",
		"iffortresswarapplywnd",
		"iffortresswarapplywndslot",
		"iffortressbusiness",
		"iffortressbusinessslot",
		"iftaxmanagement",
		"iffortressmakeitemwnd",
		"iffortressmakeitemwndslot",
		"ifchangeplayermodel",
		"ifgrantmagicattributewnd",
		"ifnewalchemybox",
		"ifalchemyprocess",
		"ifnewalchemyreinforce",
		"ifaction",
		"ifskillpracticebox",
		"ifskillremovalbox",
		"ifskillwithdrawal",
		"ifquestreward",
		"ifskill",
		"ifskillboard",
		"ifskill_slot",
		"ifskill_mastery",
		"ifquest",
		"ifquestslot",
		"ifquestslotmain",
		"ifquestslotsub",
		"ifpartymatch",
		"ifpartymatchregister",
		"ifpartymatchreqjoin",
		"ifpartyjoinprogress",
		"ifpartymatchauto",
		"ifpartymatchslot",
		"ifsystemwnd",
		"ifmainpopup",
		"ifinventory",
		"ifequipment",
		"ifplayerinfo_trijob2",
		"ifmallnotifywnd",
		"ifjobrank",
		"ifjobrankslot",
		"ifjobcontributionrank",
		"ifjobcontributionrankslot",
		"ifparty",
		"ifapprenticeship",
		"ifapprenticeshipslot",
		"ifpartyslot",
		"ifsetpartymode",
		"ifoption",
		"ifoption_video",
		"ifoption_audio",
		"ifoption_camera",
		"ifoption_input",
		"ifoption_game",
		"ifgameoptionslot",
		"ifkeyoptionslot",
		"ifvideooptionslot",
		"ifautopotion",
		"ifautopotionslot",
		"ifquickstatewnd",
		"ifquickstatehalfwnd",
		"ifquickpartywnd",
		"ifquickpartyslot",
		"ifblocking",
		"ifchattingblocking",
		"ifwhisperblocking",
		"ifchattingblockingslot",
		"ifwhisperblockingslot",
		"ifcompositeitemwnd",
		"ifwholechat",
		"ifspecialtydeal"
	];
	const layouts = [
		"ginterface",
		"ifplayerminiinfo",
		"ifunderbar",
		"ifminimap",
		"ifworldmap",
		"ifchatviewer",
		"ifsystemmessage",
		...targetNames,
		...windowNames,
		"ifextquickslot"
	];
	const paths = [
			...layouts.map( p => "/assets/cif/layouts/" + p + ".json" ),
			"/assets/text/textzonename.en.json",
			"/assets/text/textuisystem.en.json",
			"/assets/text/messagetips.en.json",
			"/assets/data/skillUi.json",
			"/assets/textdata/abusefilter.txt",
			"/assets/data/worldmap-localinfo.json",
			"/assets/text/textdataname.en.json",
			"/assets/data/actionwnddata.json",
			"/assets/data/skillData.json",
			"/assets/text/regioncode.json",
			"/assets/data/characterDataCountry.json",
			"/assets/data/levelData.json",
			"/assets/data/skillMasteryData.json",
			"/assets/data/teleportData.json",
			"/assets/data/cosPresentation.json",
			"/assets/data/mall-notify.json",
			"/assets/config/command.txt"
		],
		states: Load[] = paths.map( () => ({ kind: "idle" }) );
	let data: HudData | null = null;
	let withdrawalPage: AuthoredLayout = {};
	let stallPricePage: AuthoredLayout = {}, stallTextPage: AuthoredLayout = {};
	let goldPrices: Readonly<Record<number, number>> = {};
	let tradeBases: Readonly<Record<number, number>> = {};
	let jobThresholds: JobExpThresholds = {};
	// The skill catalogue's decode while its state is "decoding".
	let skillDecoder: ReturnType<typeof createTooltipSkillDecoder> | null = null;
	const warm = new Set<string>();
	// Resolved button families, valid for one admitted HUD data.
	const families: {
		data: HudData | null;
		published: ReadonlySet<string> | null;
		readonly byTexture: Map<string, readonly string[]>;
	} = { data: null, published: null, byTexture: new Map() };
	return {
		/*
		================
		step

		Demand gates admission only. Always collect previously admitted work.
		================
		*/
		step( needed = true ) {
			let changed = false;
			for ( let i = 0; i < states.length; i++ ) {
				const state = states[i]!;
				if ( state.kind === "decoding" ) {
					// The skill catalogue decodes a bounded slice per step: 27,835
					// rows in one frame froze the game for a third of a second.
					try {
						const value = skillDecoder!.step( TOOLTIP_ROWS_PER_STEP );
						if ( value ) {
							states[i] = { kind: "ready", value };
							skillDecoder = null;
							changed = true;
						}
					} catch ( e ) {
						states[i] = { kind: "failed", message: String( e ) };
						skillDecoder = null;
						changed = true;
					}
				} else if ( state.kind === "loading" ) {
					const r = assets.take( state.id );
					if ( r ) {
						changed = true;
						try {
							if ( r.kind !== "bytes" ) throw Error( "HUD resource unavailable: " + paths[i] );
							const raw = i === layouts.length + 4 || i === layouts.length + 16 ?
								null :
								JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( r.buffer ) );
							let value: unknown;
							if ( i < layouts.length ) {
								if ( layouts[i] === "ifskill" ) {
									withdrawalPage = decodeAuthoredLayout( raw, [ "Create", "Withdrawal" ] );
								}
								if ( layouts[i] === "ifmessagebox" ) {
									// 528670 / 529E00: separate modal constructions. StoreMoney
									// reuses Store names and IDs, so it cannot join the NPC page.
									stallPricePage = decodeAuthoredLayout( raw, [ "MsgBoxStoreMoney" ] );
									stallTextPage = decodeAuthoredLayout( raw, [ "MsgBoxInsertMsg" ] );
								}
								value = layouts[i] === "ifextquickslot" ?
									[ "Type1", "Type2", "Type3", "Type4", "Option" ].map( section =>
										decodeAuthoredLayout( raw, [ section ] )
									) :
									decodeAuthoredLayout( raw, nativeWindowSections( layouts[i]! ) );
								// The publisher already expands actual frame/button families, including
								// controls with only two states. Consume its catalogue without guessing URLs.
								{
									// Every admitted layout owns warm artwork, including target gauges.
									for (
										const entry of Object.values( raw.resourcesByDdjPath ?? {} ) as {
											publicPath?: unknown;
										}[]
									) {
										if (
											typeof entry?.publicPath !== "string" ||
											!entry.publicPath.startsWith( "/assets/images/" ) ||
											!entry.publicPath.endsWith( ".png" )
										) throw Error( "Invalid HUD image catalogue" );
										warm.add( entry.publicPath );
									}
								}
							} else if ( i === layouts.length + 16 ) value = decodeCommandTable( r.buffer );
							else if ( i === layouts.length + 15 ) value = decodeMallNotify( raw );
							else if ( i === layouts.length + 14 ) value = decodeCosReferences( raw );
							else if ( i === layouts.length + 13 ) value = decodePortalCatalog( raw );
							else if ( i === layouts.length + 12 ) value = decodeTooltipMasteries( raw );
							else if ( i === layouts.length + 11 ) {
								value = masteryCosts( raw );
								goldPrices = withdrawalGoldPrices( raw );
								tradeBases = tradeGoldBases( raw );
								jobThresholds = jobExpThresholds( raw );
							} else if ( i === layouts.length + 10 ) value = partyCharacterCountries( raw );
							else if ( i === layouts.length + 8 ) {
								skillDecoder = createTooltipSkillDecoder( raw );
								states[i] = { kind: "decoding" };
								continue;
							} else if ( i === layouts.length + 7 ) value = decodeActionSlots( raw );
							else if ( i === layouts.length + 5 ) value = raw;
							else if ( i === layouts.length + 4 ) value = creationNameRules( r.buffer );
							else if ( i === layouts.length + 2 ) value = decodeMessageTips( raw );
							else if ( i === layouts.length + 3 ) value = decodeSkillUi( raw );
							else {
								if (
									!raw?.entries || typeof raw.entries !== "object" || Array.isArray( raw.entries ) ||
									Object.values( raw.entries ).some( v => typeof v !== "string" )
								) throw Error( "Invalid HUD text catalog" );
								value = raw.entries;
							}
							states[i] = { kind: "ready", value };
						} catch ( e ) {
							states[i] = { kind: "failed", message: String( e ) };
						}
					}
				} else if ( needed && state.kind === "idle" && assets.available() > 0 ) {
					states[i] = {
						kind: "loading",
						id: assets.request(
							new URL( paths[i]!, base ).href,
							i === layouts.length + 8 ? 16 << 20 : 4 << 20
						)
					};
				}
			}
			if ( !data && states.every( s => s.kind === "ready" ) ) {
				const values = states.map( s => s.kind === "ready" ? s.value : null );
				data = {
					withdrawalPage,
					stallPricePage,
					stallTextPage,
					withdrawalGoldPrices: goldPrices,
					tradeGoldBases: tradeBases,
					jobExpThresholds: jobThresholds,
					portals: values[layouts.length + 13] as PortalCatalog,
					cosReferences: values[layouts.length + 14] as HudData["cosReferences"],
					tooltipMasteries: values[layouts.length + 12] as HudData["tooltipMasteries"],
					masteryCosts: values[layouts.length + 11] as HudData["masteryCosts"],
					extended: values[layouts.length - 1] as readonly AuthoredLayout[],
					countries: values[layouts.length + 10] as HudData["countries"],
					regionCodes: values[layouts.length + 9] as HudData["regionCodes"],
					tooltipSkills: values[layouts.length + 8] as TooltipSkillCatalog,
					mapIcons: decodeMapIcons( values[layouts.length + 5] ),
					actions: values[layouts.length + 7] as readonly ActionSlot[],
					mapLabels: decodeMapLabels(
						values[layouts.length + 5],
						values[layouts.length + 6] as Record<string, string>
					),
					nameRules: values[layouts.length + 4] as NameRules,
					skillUi: values[layouts.length + 3] as SkillUi,
					popupArt: {
						tab: "/assets/images/Media_extracted/interface/ifcommon/com_tab_on.png",
						blocked: "/assets/images/Media_extracted/interface/pet/pt_block.png",
						portrait: "/assets/images/Media_extracted/interface/party/pt_face.png",
						sockets: {}
					},
					warmPaths: [],
					windows: Object.fromEntries(
						windowNames.map( ( name, i ) => [ name, values[7 + targetNames.length + i] as AuthoredLayout ] )
					),
					tips: values[layouts.length + 2] as readonly MessageTip[],
					mallNotify: values[layouts.length + 15] as MallNotify,
					commandTable: values[layouts.length + 16] as ReadonlyMap<string, number>,
					targets: Object.fromEntries(
						targetNames.map( ( name, i ) => [ name, values[7 + i] as AuthoredLayout ] )
					),
					root: values[0] as AuthoredLayout,
					player: values[1] as AuthoredLayout,
					bar: values[2] as AuthoredLayout,
					minimap: values[3] as AuthoredLayout,
					map: values[4] as AuthoredLayout,
					chat: values[5] as AuthoredLayout,
					status: values[6] as AuthoredLayout,
					zones: values[layouts.length] as HudData["zones"],
					strings: {
						...values[layouts.length + 6] as HudData["strings"],
						...values[layouts.length + 1] as HudData["strings"]
					}
				};
				const sockets = Object.fromEntries(
					Object.values( data.windows.ifequipment! ).filter( node => node.id >= 100 && node.id <= 112 ).map(
						node => [
							node.id - 100,
							"/assets/images/Media_extracted/interface/equipment/equip_slot_" +
							equipmentSocket( node.id - 100 ) + ".png"
						]
					)
				);
				const popupArt = { ...data.popupArt, sockets };
				data = {
					...data,
					popupArt,
					warmPaths: [
						...warm,
						"/assets/images/Media_extracted/icon/icon_disable.png",
						"/assets/images/Media_extracted/icon/icon_item_broken.png",
						"/assets/images/Media_extracted/icon/icon_item_warning.png",
						...[ "", "_focus", "_press" ].map( state =>
							"/assets/images/Media_extracted/interface/skill/skl_button_up" + state + ".png"
						),
						...[ "h", "v" ].flatMap( axis =>
							[ "", "_focus", "_press" ].map( state =>
								"/assets/images/Media_extracted/interface/quick_slot/qsl_" + axis + "close_button" +
								state + ".png"
							)
						),
						"/assets/images/Media_extracted/interface/party/pt_hp_disable.png",
						"/assets/images/Media_extracted/interface/party/pt_mp_disable.png",
						popupArt.tab,
						popupArt.blocked,
						popupArt.portrait,
						...Object.values( sockets )
					]
				};
				changed = true;
			}
			return changed;
		},
		/*
		================
		settling

		True while an admitted catalogue is still decoding in bounded steps.
		================
		*/
		settling: () => skillDecoder !== null,
		/*
		================
		data
		================
		*/
		data: () => data,
		/*
		================
		buttonFamily

		The four state images of an authored button (normal, focus, press,
		disable). A state image the HUD does not publish falls back to the
		normal one. Buttons draw every frame, so each family is resolved once
		per admitted HUD data.
		================
		*/
		buttonFamily( texture: string ): readonly string[] {
			if ( families.data !== data ) {
				families.data = data;
				families.published = data ? new Set( data.warmPaths ) : null;
				families.byTexture.clear();
			}
			let family = families.byTexture.get( texture );
			if ( !family ) {
				const published = families.published;
				family = BUTTON_STATE_SUFFIXES.map( suffix => {
					const path = texture.replace( ".png", suffix + ".png" );
					return suffix && published && !published.has( path ) ? texture : path;
				} );
				families.byTexture.set( texture, family );
			}
			return family;
		},
		/*
		================
		error
		================
		*/
		error: () => states.find( s => s.kind === "failed" )?.message ?? null,
		/*
		================
		dispose

		Release owned children and pending work before discarding local state.
		================
		*/
		dispose() {
			for ( let i = 0; i < states.length; i++ ) {
				const s = states[i]!;
				if ( s.kind === "loading" ) assets.cancel( s.id );
				states[i] = { kind: "disposed" };
			}
			data = null;
			skillDecoder = null;
			warm.clear();
			families.data = null;
			families.published = null;
			families.byTexture.clear();
		}
	};
}
