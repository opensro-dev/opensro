/*
===========================================================================

ui-equality.test.mjs - tests for ui-equality.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { sameUiQuads, sameUiSemantics } = await import(
	sourceFileUrl( "src/engine/foundation/ui/ui-equality.ts" ).href
);
const quad = () => ({
	texture: "atlas",
	rect: [ 1, 2, 3, 4 ],
	clip: [ 0, 0, 100, 100 ],
	uv: [ 0, 0, 1, 1 ],
	color: [ 1, 1, 1, 1 ]
});
test("retained UI compares pixels, anchors, masks and preview state without losing updates", () => {
	const first = quad();
	assert.equal( sameUiQuads( [ first ], [ structuredClone( first ) ] ), true );
	for ( const key of [ "rect", "clip", "uv", "color" ] ) {
		for ( let i = 0; i < 4; i++ ) {
			const changed = structuredClone( first );
			changed[key][i] += .1;
			assert.equal( sameUiQuads( [ first ], [ changed ] ), false );
		}
	}
	for (
		const extra of [
			{ occlusion: "none" },
			{ occlusion: "scene" },
			{ rightColor: [ 0, 0, 0, 0 ] },
			{ texture: "new" },
			{ layer: "background" },
			{ depth: .5 },
			{ rotation: .1 },
			{ uvTurn: 1 },
			{ alphaCutoff: .5 },
			{ characterAnchor: 5 },
			{ portraitGid: 5 },
			{ mask: { texture: "mask", rect: [ 0, 0, 1, 1 ] } },
			{ doll: { gid: 5, yaw: 1 } },
			{ worldAnchor: { regionId: 1, x: 2, y: 3, z: 4 } }
		]
	) {
		const changed = { ...first, ...extra };
		assert.equal( sameUiQuads( [ first ], [ changed ] ), false );
		assert.equal( sameUiQuads( [ changed ], [ structuredClone( changed ) ] ), true );
	}
	assert.equal( sameUiQuads( [ first ], [] ), false );
});
test("focus, caret, controls and loading changes remain observable even when pixels are unchanged", () => {
	const first = {
		title: "world",
		message: "ready",
		controls: [ { id: "chat", kind: "text", label: "Chat", rect: [ 0, 0, 100, 20 ], value: "" } ],
		focusRequest: { id: "chat", revision: 1, caret: 0 }
	};
	assert.equal( sameUiSemantics( first, structuredClone( first ) ), true );
	for (
		const update of [
			{ title: "dock" },
			{ message: "error" },
			{ loading: true },
			{ loadingVisible: true },
			{ loadingProgress: .5 },
			{ loadingStatus: "upload" },
			{ loadingError: "failed" },
			{ hudCorner: [ 1, 2, 3, 4 ] },
			{ hudToolsTop: 80 },
			{ focusRequest: { id: "chat", revision: 2, caret: 0 } },
			{ focusRequest: { id: "chat", revision: 1, caret: 1 } }
		]
	) assert.equal( sameUiSemantics( first, { ...first, ...update } ), false );
	for (
		const update of [
			{ id: "password" },
			{ kind: "password" },
			{ label: "Password" },
			{ rect: [ 1, 0, 100, 20 ] },
			{ value: "hello" },
			{ disabled: true },
			{ selected: true },
			{ captureKeys: true },
			{ draggable: true },
			{ min: 1 },
			{ max: 2 },
			{ maxLength: 10 }
		]
	) assert.equal( sameUiSemantics( first, { ...first, controls: [ { ...first.controls[0], ...update } ] } ), false );
});
