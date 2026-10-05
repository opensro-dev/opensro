/*
===========================================================================

ui-equality.ts - value equality of UI commands and semantics

The UI owner republishes only what changed, and renderer admission retains
equal commands. Both compare the contract field by field here instead of
serializing whole scenes.

===========================================================================
*/

import type { UiQuad, UiSemantics } from "@/engine/contracts/ui";
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
		x.doll !== y.doll && (!x.doll || !y.doll || x.doll.gid !== y.doll.gid || !Object.is( x.doll.yaw, y.doll.yaw ))
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
sameUiSemantics
================
*/
export function sameUiSemantics( a: UiSemantics, b: UiSemantics ): boolean {
	if ( a === b ) return true;
	if (
		a.title !== b.title || a.message !== b.message || a.loading !== b.loading ||
		a.loadingVisible !== b.loadingVisible || a.loadingStatus !== b.loadingStatus ||
		a.loadingProgress !== b.loadingProgress || a.loadingError !== b.loadingError ||
		!tuple( a.hudCorner, b.hudCorner ) || a.controls.length !== b.controls.length
	) return false;
	if (
		a.focusRequest !== b.focusRequest &&
		(!a.focusRequest || !b.focusRequest || a.focusRequest.id !== b.focusRequest.id ||
			a.focusRequest.revision !== b.focusRequest.revision || a.focusRequest.caret !== b.focusRequest.caret)
	) return false;
	for ( let i = 0; i < a.controls.length; i++ ) {
		const x = a.controls[i]!, y = b.controls[i]!;
		if (
			x.id !== y.id || x.label !== y.label || x.kind !== y.kind || x.value !== y.value ||
			x.disabled !== y.disabled || x.selected !== y.selected || x.captureKeys !== y.captureKeys ||
			x.draggable !== y.draggable || x.carry !== y.carry || x.min !== y.min || x.max !== y.max ||
			x.maxLength !== y.maxLength || !tuple( x.rect, y.rect )
		) return false;
	}
	return true;
}
