/*
===========================================================================

ui.ts - semantic controls, input events and rendered interface contracts

The DOM bridge reports gestures without executing gameplay. The HUD owns
control metadata and resolves those gestures against the current view.
===========================================================================
*/
import type { SessionState } from "./session";
import type { GameplayState } from "./gameplay";
import type { EntityState } from "./world";
/*
================
UiRect
================
*/
export type UiRect = readonly [number, number, number, number];
// Shared text-layout sidecar; consumed before scene publication, never by GPU code.
/*
================
UiTextLayout
================
*/
export interface UiTextLayout {
	readonly box: UiRect;
	// The shortened (ellipsis) run that replaces this one when its ink would
	// enter a neighbouring text box; null when no glyph of it fits, absent
	// when the text fits its box.
	readonly fitted?: UiQuad | null;
}
/*
================
UiTextGlyph

One glyph of a text run: its rectangle relative to the run quad's rect
origin, and its atlas window.
================
*/
export interface UiTextGlyph {
	readonly x: number;
	readonly y: number;
	readonly width: number;
	readonly height: number;
	readonly uv: UiRect;
}
/*
================
UiTextRun

A laid-out string carried as one quad. The quad's rect is the glyphs'
bounding box, so moving the rect moves every glyph; its color, clip,
texture and anchors apply to each glyph. Layouts are frozen and cached, so
a string costs one quad however many glyphs it has. Every text the UI
draws is a run; the GPU packer writes one record per glyph, and
expandTextRuns (text-run.ts) defines the glyph quads a run stands for.
================
*/
export interface UiTextRun {
	readonly glyphs: readonly UiTextGlyph[];
}
/*
================
UiQuad
================
*/
export interface UiQuad {
	readonly textLayout?: UiTextLayout;
	readonly run?: UiTextRun;
	readonly sampling?: "linear" | "nearest";
	readonly depth?: number;
	readonly occlusion?: "scene" | "none";
	readonly doll?: { readonly gid: number; readonly yaw: number; };
	readonly worldAnchor?: Omit<import("./gameplay").Pose, "angle">;
	readonly portraitGid?: number;
	readonly rotation?: number;
	readonly uvTurn?: 0 | 1 | 2 | 3;
	readonly alphaCutoff?: number;
	readonly characterAnchor?: number;
	readonly mask?: { readonly texture: string; readonly rect: UiRect; };
	readonly layer?: "background";
	readonly rect: UiRect;
	readonly uv: UiRect;
	readonly color: readonly [number, number, number, number];
	readonly rightColor?: readonly [number, number, number, number];
	readonly texture: string;
	readonly clip: UiRect;
}
/*
================
UiScene

damageText: the world's annotations show, so the renderer draws the
current damage text after them every frame. Damage text rises and fades
with time, so it is not part of the retained product: a rebuild per frame
for it was most of a fight's interface cost.
================
*/
export interface UiScene {
	readonly revision: number;
	readonly width: number;
	readonly height: number;
	readonly quads: readonly UiQuad[];
	readonly damageText?: boolean;
}
// viewer: a CIFBuffViewer cell (6DE6F0). Its entry never carries a remaining
// time, and an owner without a character (party) formats abnormal bits unlevelled.
/*
================
UiHelpSource
================
*/
export type UiHelpSource = {
	readonly kind: "effect";
	readonly gid: number;
	readonly token: number;
	readonly skill: number;
	readonly viewer?: boolean;
} | {
	readonly kind: "abnormal";
	readonly gid: number;
	readonly bit: number;
	readonly viewer?: boolean;
	readonly unlevelled?: boolean;
};
/*
================
UiControl
================
*/
export interface UiControl {
	readonly whisperTarget?: string;
	readonly textAlign?: "left" | "center" | "right";
	readonly textInsets?: UiRect;
	readonly rightActivate?: boolean;
	readonly helpSource?: UiHelpSource;
	readonly helpText?: string;
	readonly captureKeys?: boolean;
	readonly draggable?: boolean;
	// A click lifts the control's item onto the cursor and the next press
	// places it, as the retail inventory does; a drag still works too.
	readonly carry?: boolean;
	readonly min?: number;
	readonly max?: number;
	readonly maxLength?: number;
	readonly id: string;
	readonly label: string;
	readonly rect: UiRect;
	readonly kind: "button" | "text" | "password" | "range" | "region";
	readonly value?: string;
	readonly disabled?: boolean;
	readonly selected?: boolean;
}
/*
================
UiSemantics
================
*/
export interface UiSemantics {
	readonly loadingVisible?: boolean;
	readonly hudCorner?: UiRect;
	readonly focusRequest?: {
		readonly id: string | null;
		readonly revision: number;
		readonly caret: number;
		readonly anchor?: number;
	};
	readonly loadingStatus?: string;
	readonly loadingProgress?: number;
	readonly loading?: boolean;
	readonly loadingError?: string;
	readonly title: string;
	readonly message: string;
	readonly controls: readonly UiControl[];
}
/*
================
UiEvent
================
*/
export type UiEvent =
	| { kind: "whisper-target"; gid: number; }
	| { kind: "world-select"; gid: number; }
	| {
		kind: "quickslot-preferences";
		value: import("@/engine/foundation/ui/extended-quickslot").ExtendedQuickslotOptions;
	}
	| { kind: "chat-blocks"; value: readonly string[]; }
	| { kind: "window-positions"; value: import("@/engine/foundation/ui/window-positions").WindowPositions | null; }
	| { kind: "video-preferences"; value: import("@/engine/foundation/rendering/video-options").VideoOptions; }
	| { kind: "input-preferences"; value: import("@/engine/foundation/ui/input-options").InputOptions; }
	| { kind: "camera-preferences"; value: import("@/engine/foundation/rendering/camera-options").SightMode; }
	| { kind: "audio-preferences"; value: import("@/engine/foundation/audio/options").AudioOptions; }
	| {
		kind: "experimental-preferences";
		value: import("@/engine/foundation/ui/experimental-options").ExperimentalOptions;
	}
	| { kind: "preferences"; value: import("@/engine/foundation/gameplay/game-options").GameOptions; }
	| { kind: "drag-end"; id: string; x: number; y: number; }
	// The bridge abandoned the drag or click-carry that `id` started.
	| { kind: "drag-cancel"; id: string; }
	| { kind: "region-double"; id: string; x: number; y: number; }
	| { kind: "drag"; id: string; dx: number; dy: number; }
	| { kind: "scroll"; x: number; y: number; delta: number; }
	| {
		kind: "activate" | "double-activate" | "right-activate";
		id: string;
		shift?: boolean;
		ctrl?: boolean;
		alt?: boolean;
	}
	| { kind: "hover" | "press"; id: string | null; }
	| { kind: "edit"; id: string; value: string; start: number; end: number; composing: boolean; }
	| { kind: "focus"; id: string | null; }
	| { kind: "key"; code: string; shift?: boolean; ctrl?: boolean; };
/*
================
UiBridge
================
*/
export interface UiBridge {
	present( state: UiSemantics ): void;
	dispose(): void;
}
/*
================
UiView
================
*/
export interface UiView {
	readonly worldError?: string | null;
	readonly worldRetrying?: boolean;
	readonly resourceError?: string | null;
	readonly dropNamesHeld?: boolean;
	readonly blindHeld?: boolean;
	readonly simulationTimeMs?: number;
	readonly hoveredEntity?: number | null;
	readonly travel?: import("./world").WorldTravel | null;
	readonly worldTransitionRegion?: number;
	readonly loadingProgress?: number;
	readonly berserkGauge?: import("./orb").BerserkGauge;
	readonly frontend?: import("./frontend").FrontendSnapshot;
	readonly session: SessionState | null;
	readonly gameplay: GameplayState | null;
	readonly entities: readonly EntityState[];
	readonly width: number;
	readonly height: number;
	readonly worldReady: boolean;
}
