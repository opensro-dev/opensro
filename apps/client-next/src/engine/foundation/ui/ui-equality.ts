/*
===========================================================================

ui-equality.ts - value equality of UI commands and semantics

The UI owner republishes only what changed, and renderer admission retains
equal commands. Both compare the contract field by field here instead of
serializing whole scenes.

===========================================================================
*/

import type { WorldCamera } from "@/engine/contracts/scene";
import type { UiControl, UiHelpSource, UiQuad, UiSemantics } from "@/engine/contracts/ui";
import { sameTextRun } from "@/engine/foundation/rendering/text-run";

/*
================
tuple
================
*/
function tuple( a: readonly number[] | undefined, b: readonly number[] | undefined ): boolean {
	if ( a === b ) return true;
	if ( !a || !b || a.length !== b.length ) return false;
	for ( let i = 0; i < a.length; i++ ) if ( !Object.is( a[i], b[i] ) ) return false;
	return true;
}
/*
================
sameDollCamera

The 3D viewer's doll camera (port-only): equal when absent on both or the
same framing. A zoom or re-framing changes only the camera, so it must
not compare equal to the previous one.
Compares only the fields a doll camera sets (eye, target, fov, near,
far); the viewer never sets WorldCamera's follow or dungeon fields.
================
*/
function sameDollCamera( a: WorldCamera | undefined, b: WorldCamera | undefined ): boolean {
	return a === b ||
		!!a && !!b && tuple( a.eye, b.eye ) && tuple( a.target, b.target ) && Object.is( a.fov, b.fov ) &&
			Object.is( a.near, b.near ) && Object.is( a.far, b.far );
}

/*
================
sameUiQuads

Compare the renderer contract directly. Avoid serializing thousands of glyph
quads into a new multi-megabyte string to discover an unchanged HUD.
================
*/
export function sameUiQuads( a: readonly UiQuad[], b: readonly UiQuad[] ): boolean {
	if ( a === b ) return true;
	if ( a.length !== b.length ) return false;
	for ( let i = 0; i < a.length; i++ ) {
		const x = a[i]!, y = b[i]!;
		if ( x === y ) continue;
		if ( !sameUiQuad( x, y ) ) return false;
	}
	return true;
}
/*
================
sameUiQuad
================
*/
export function sameUiQuad( x: UiQuad, y: UiQuad ): boolean {
	if ( x === y ) return true;
	if (
		x.occlusion !== y.occlusion || x.sampling !== y.sampling || x.texture !== y.texture || x.layer !== y.layer ||
		!Object.is( x.depth, y.depth ) || !Object.is( x.rotation, y.rotation ) || !Object.is( x.uvTurn, y.uvTurn ) ||
		!x.tiled !== !y.tiled || !x.additive !== !y.additive ||
		!Object.is( x.alphaCutoff, y.alphaCutoff ) || x.characterAnchor !== y.characterAnchor ||
		x.portraitGid !== y.portraitGid ||
		!tuple( x.rect, y.rect ) || !tuple( x.uv, y.uv ) || !tuple( x.color, y.color ) ||
		!tuple( x.rightColor, y.rightColor ) || !tuple( x.clip, y.clip ) || !sameTextRun( x.run, y.run )
	) return false;
	if (
		x.mask !== y.mask &&
		(!x.mask || !y.mask || x.mask.texture !== y.mask.texture || !tuple( x.mask.rect, y.mask.rect ))
	) return false;
	if (
		x.doll !== y.doll &&
		(!x.doll || !y.doll || x.doll.gid !== y.doll.gid || !Object.is( x.doll.yaw, y.doll.yaw ) ||
			!sameDollCamera( x.doll.camera, y.doll.camera ) || x.doll.mount !== y.doll.mount)
	) return false;
	if (
		x.worldAnchor !== y.worldAnchor &&
		(!x.worldAnchor || !y.worldAnchor || !Object.is( x.worldAnchor.regionId, y.worldAnchor.regionId ) ||
			!Object.is( x.worldAnchor.x, y.worldAnchor.x ) || !Object.is( x.worldAnchor.y, y.worldAnchor.y ) ||
			!Object.is( x.worldAnchor.z, y.worldAnchor.z ))
	) return false;
	return true;
}
/*
================
samePolygon
================
*/
function samePolygon(
	a: readonly (readonly [number, number])[] | undefined,
	b: readonly (readonly [number, number])[] | undefined
): boolean {
	if ( a === b ) return true;
	if ( !a || !b || a.length !== b.length ) return false;
	for ( let i = 0; i < a.length; i++ ) if ( !tuple( a[i], b[i] ) ) return false;
	return true;
}
/*
================
sameHelpSource
================
*/
function sameHelpSource( a: UiHelpSource | undefined, b: UiHelpSource | undefined ): boolean {
	if ( a === b ) return true;
	if ( !a || !b || a.kind !== b.kind || a.gid !== b.gid || a.viewer !== b.viewer ) return false;
	if ( a.kind === "effect" ) {
		return b.kind === "effect" && a.token === b.token && a.skill === b.skill && a.suppressed === b.suppressed;
	}
	return b.kind === "abnormal" && a.bit === b.bit && a.unlevelled === b.unlevelled;
}
/*
================
sameUiControl

Every field of the contract: the bridge keeps the last presented control
and its input handlers read it (rightActivate, carry, inspect, helpText,
keepFocusOnAltPress), so a field left out here would leave them stale.
================
*/
function sameUiControl( x: UiControl, y: UiControl ): boolean {
	return x === y || x.id === y.id && x.label === y.label && x.kind === y.kind && x.value === y.value &&
			x.valueText === y.valueText && x.disabled === y.disabled && x.selected === y.selected &&
			x.multiline === y.multiline && x.whisperTarget === y.whisperTarget && x.textAlign === y.textAlign &&
			x.rightActivate === y.rightActivate && x.helpText === y.helpText && x.captureKeys === y.captureKeys &&
			x.draggable === y.draggable && x.carry === y.carry && x.inspect === y.inspect &&
			x.keepFocusOnAltPress === y.keepFocusOnAltPress && x.min === y.min && x.max === y.max &&
			x.maxLength === y.maxLength && tuple( x.rect, y.rect ) && tuple( x.textInsets, y.textInsets ) &&
			samePolygon( x.hitPolygon, y.hitPolygon ) && sameHelpSource( x.helpSource, y.helpSource );
}
/*
================
sameUiSemantics

Every field of the contract, by value. The UI owner presents semantics
only when this is false, so it must never call two different semantics
equal.
================
*/
export function sameUiSemantics( a: UiSemantics, b: UiSemantics ): boolean {
	if ( a === b ) return true;
	if (
		a.title !== b.title || a.message !== b.message || a.loading !== b.loading ||
		a.loadingVisible !== b.loadingVisible || a.loadingStatus !== b.loadingStatus ||
		a.loadingProgress !== b.loadingProgress || a.loadingError !== b.loadingError ||
		a.titleCredit !== b.titleCredit || a.marketButton !== b.marketButton || a.marketOpen !== b.marketOpen ||
		!tuple( a.hudCorner, b.hudCorner ) || a.hudToolsTop !== b.hudToolsTop ||
		a.controls.length !== b.controls.length
	) return false;
	if (
		a.focusRequest !== b.focusRequest &&
		(!a.focusRequest || !b.focusRequest || a.focusRequest.id !== b.focusRequest.id ||
			a.focusRequest.revision !== b.focusRequest.revision || a.focusRequest.caret !== b.focusRequest.caret ||
			a.focusRequest.anchor !== b.focusRequest.anchor)
	) return false;
	for ( let i = 0; i < a.controls.length; i++ ) if ( !sameUiControl( a.controls[i]!, b.controls[i]! ) ) return false;
	return true;
}
/*
================
UiProduct

One UI step's output: the frame it was laid out for, its quads and the
semantics beside them.
================
*/
export interface UiProduct {
	readonly width: number;
	readonly height: number;
	readonly damageText: boolean;
	readonly quads: readonly UiQuad[];
	readonly semantics: UiSemantics;
}
/*
================
uiPublication

What a step does with its product, against the last one it published
(whose semantics are the last presented): nothing when frame, quads and
semantics are equal; otherwise it publishes the quads, and presents the
semantics too when they or the frame changed (the bridge reads canvas
geometry), or on the first product.

A loading state presents every publication: the platform refreshes its
loading detail (clock and transfer) on each presentation
(platform.ts refreshLoadingDetail), which semantics alone do not carry.
================
*/
export function uiPublication( last: UiProduct | null, next: UiProduct ): "unchanged" | "quads" | "present" {
	const sameSemantics = last !== null && last.width === next.width && last.height === next.height &&
		last.damageText === next.damageText && sameUiSemantics( last.semantics, next.semantics );
	if ( sameSemantics && sameUiQuads( last.quads, next.quads ) ) return "unchanged";
	const loading = next.semantics.loading || next.semantics.loadingVisible || !!next.semantics.loadingError;
	return sameSemantics && !loading ? "quads" : "present";
}
