/*
===========================================================================

frame-contract.ts - data and capabilities the coordinator lends to drawing

HUD metadata keeps the shape decoded by hud/resources. Frame primitives
borrow only the resource, text, placement and compact-layout operations they
use; the coordinator owns every provider's lifetime.

===========================================================================
*/
import type { UiRect, UiQuad, UiControl } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { PanelCloseId } from "@/engine/foundation/ui/panels";
import type { PortalCatalog } from "@/engine/foundation/gameplay/portal";
import type { TooltipMastery } from "@/engine/foundation/ui/mastery-tooltip";
import type { MallNotify } from "@/engine/foundation/ui/mall-notify";
import type { EventInterfaceData } from "@/engine/foundation/ui/event-interface";
import type { JobExpThresholds } from "@/engine/foundation/gameplay/player-info-job";
import type { TooltipSkillCatalog } from "@/engine/foundation/ui/skill-tooltip-data";
import type { ActionSlot } from "@/engine/foundation/ui/action-layout";
import type { WorldMapLayout, MapLabel, MapIcon } from "@/engine/foundation/ui/world-map";
import type { NameRules } from "@/engine/foundation/ui/character-create";
import type { SkillUi } from "@/engine/foundation/ui/skill-layout";
import type { MessageTip } from "@/engine/foundation/ui/message-tips";
import type { AuthoredLayout } from "@/engine/foundation/ui/authored-layout";
import type { CosReference } from "@/engine/foundation/ui/cos-command";
import type { FortressMapCatalog } from "@/engine/foundation/gameplay/fortress-map";
import type { CameraDataRow } from "@/engine/foundation/ui/camera-waypoints";
import type { GuideBlock, GuideToken } from "@/engine/foundation/ui/guide-content";

/*
================
HudData
================
*/
export interface HudData {
	readonly withdrawalPage: AuthoredLayout;
	readonly stallPricePage: AuthoredLayout;
	readonly stallTextPage: AuthoredLayout;
	readonly cosReferences: ReadonlyMap<number, CosReference>;
	// The battle aide's fortress map; null in a build that publishes none.
	readonly fortressMap: FortressMapCatalog | null;
	readonly portals: PortalCatalog;
	readonly tooltipMasteries: ReadonlyMap<number, TooltipMastery>;
	readonly masteryCosts: Readonly<Record<number, number>>;
	readonly withdrawalGoldPrices: Readonly<Record<number, number>>;
	// levelgold.txt column 2 by level: the trade scale's basis (649050).
	readonly tradeGoldBases: Readonly<Record<number, number>>;
	readonly mallNotify: MallNotify;
	// CIFEventInterface's three layouts (547750); PC-room and winter are
	// null when the later import never published them.
	readonly eventInterface: EventInterfaceData;
	// config\command.txt: the debug console's command names by id (68D9C0).
	readonly commandTable: ReadonlyMap<string, number>;
	// config\cameradata.txt rows: the /cw* waypoint store's keyframes (#1716).
	readonly cameraDataRows: readonly CameraDataRow[];
	readonly jobExpThresholds: JobExpThresholds;
	readonly extended: readonly AuthoredLayout[];
	readonly countries: Readonly<Record<number, number>>;
	readonly tooltipSkills: TooltipSkillCatalog;
	readonly mapLayout: WorldMapLayout;
	readonly mapIcons: readonly MapIcon[];
	readonly actions: readonly ActionSlot[];
	readonly mapLabels: readonly MapLabel[];
	readonly expandedMapIcons: readonly MapIcon[];
	readonly expandedMapLabels: readonly MapLabel[];
	readonly nameRules: NameRules;
	// The same filter's guild-name column (790A30).
	readonly guildNameRules: NameRules;
	// The same filter's chat column (790C70): what a composed chat line
	// may not contain (545EF0's local refusal).
	readonly chatRules: NameRules;
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

/*
================
CloseControlId

A panel's own close id or a service window's named close control. A bare
"close" cannot distinguish windows that share the same frame primitives.
================
*/
export type CloseControlId = PanelCloseId | `${string}-close` | "stall-leave";

/*
================
UiFrameChrome

The coordinator's shared window skin, reused by every frame.
================
*/
export interface UiFrameChrome {
	readonly frame: string;
	readonly parts: readonly string[];
	readonly button: string;
	readonly buttonFocus: string;
	readonly buttonPress: string;
	readonly buttonDisable: string;
	readonly close: string;
	readonly closePress: string;
	readonly closeFocus: string;
}

/*
================
AdmittedWindow

A complete window retained until its replacement has its own chrome.
================
*/
export interface AdmittedWindow {
	key: string;
	quads: UiQuad[];
	controls: UiControl[];
	blocks: UiRect[];
	paths: string[];
}

/*
================
UiFrame

Arrays are live aliases of step's output. When a modal replaces an array,
the coordinator replaces this reference in the same assignment. Input
changes during drawing follow the same rule; no helper holds a stale copy.
================
*/
export interface UiFrame {
	quads: UiQuad[];
	controls: UiControl[];
	blocks: UiRect[];
	paths: string[];
	readonly w: number;
	readonly h: number;
	readonly full: UiRect;
	readonly hud: HudData | null;
	readonly text: {
		quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
	};
	readonly resources: {
		has( path: string ): boolean;
		size( path: string ): readonly [number, number] | undefined;
		// True for an image no load will ever deliver (absent from the
		// manifest); it must not hold a window (#3407).
		absent?( path: string ): boolean;
	};
	readonly windowPlacement: {
		frame(
			id: string,
			initial: UiRect,
			viewport: readonly [number, number],
			restoreExtent?: readonly [number, number]
		): UiRect;
	};
	readonly compact: { readonly top: number; readonly bounds: UiRect; } | null;
	readonly chrome: UiFrameChrome;
	readonly compactPolicy: {
		windowDrag( control: UiControl ): boolean;
		closeControl( control: UiControl, bounds: UiRect ): UiControl;
	};
	readonly white: UiQuad["color"];
	readonly commonWindowPaths: readonly string[];
	readonly admittedWindows: Map<string, AdmittedWindow>;
	readonly windowMissing: string[];
	readonly contentImages: Set<string>;
	readonly phase: string;
	readonly fontPath: string | null;
	pressed: string | null;
	hover: string | null;
	focus: string | null;
}

/*
================
UiAuthoredFrame

The frame the authored widgets (authored.ts) draw into: the whole text
owner (widths, line height and SML2 layout besides quads), the system-text
catalogue, the button art family of a texture and the Experimental "Fit
window labels" preference. The coordinator's drawFrame is one.
================
*/
export interface UiAuthoredFrame extends UiFrame {
	readonly text: UiFrame["text"] & {
		run( value: string, fontStyle?: number, fontIndex?: number ): { width: number; };
		extentHeight( index?: number, style?: number ): number;
		guide(
			tokens: readonly GuideToken[],
			rect: UiRect,
			clip: UiRect,
			color: UiQuad["color"],
			size: ( path: string ) => readonly [number, number] | undefined,
			block?: GuideBlock
		): { quads: UiQuad[]; paths: string[]; height: number; };
	};
	copy( key: string ): string;
	buttonFamily( texture: string ): readonly string[];
	fitWindowLabels(): boolean;
}
